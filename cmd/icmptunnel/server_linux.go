//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"

	"icmptunnel/internal/packet"
	"icmptunnel/internal/wire"
)

type session struct {
	conn    *wire.Conn
	replies chan []byte
	done    chan struct{}
}

func (s *session) deliver(b []byte) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.replies <- b:
	default:
	}
}

type probe struct {
	owner    *session
	original packet.IPv4
	expires  time.Time
}
type relay struct {
	mu      sync.Mutex
	probes  map[uint16]probe
	next    uint16
	timeout time.Duration
	public  net.IP
	raw     *rawICMP
}

func runServer(cfg config) error {
	key, err := wire.Key(cfg.Key)
	if err != nil {
		return err
	}
	public, err := ipv4(cfg.PublicIP)
	if err != nil {
		return fmt.Errorf("public_ip: %w", err)
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 120 {
		return errors.New("timeout_seconds must be 1..120")
	}
	raw, err := openRaw()
	if err != nil {
		return fmt.Errorf("raw ICMP (root/CAP_NET_RAW required): %w", err)
	}
	defer raw.Close()
	l, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		return err
	}
	defer l.Close()
	r := &relay{probes: make(map[uint16]probe), timeout: time.Duration(cfg.TimeoutSeconds) * time.Second, public: public, raw: raw}
	go r.receive()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for now := range t.C {
			r.mu.Lock()
			for id, p := range r.probes {
				if !now.Before(p.expires) {
					delete(r.probes, id)
				}
			}
			r.mu.Unlock()
		}
	}()
	log.Printf("server started listen=%s advertised_ip=%s", l.Addr(), public)
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer c.Close()
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.SetNoDelay(true)
				_ = tcp.SetKeepAlive(true)
			}
			x, err := wire.Handshake(c, key, true)
			if err != nil {
				log.Printf("handshake %s: %v", c.RemoteAddr(), err)
				return
			}
			s := &session{conn: x, replies: make(chan []byte, 128), done: make(chan struct{})}
			defer func() {
				close(s.done)
				r.mu.Lock()
				for id, p := range r.probes {
					if p.owner == s {
						delete(r.probes, id)
					}
				}
				r.mu.Unlock()
			}()
			log.Printf("router connected: %s", c.RemoteAddr())
			go func() {
				for {
					select {
					case <-s.done:
						return
					case b := <-s.replies:
						if err := x.Send(wire.Packet, b); err != nil {
							_ = x.Close()
							return
						}
					}
				}
			}()
			for {
				kind, b, err := x.Receive()
				if err != nil {
					log.Printf("router disconnected: %s (%v)", c.RemoteAddr(), err)
					return
				}
				switch kind {
				case wire.Packet:
					r.forward(s, b)
				case wire.Ping:
					if len(b) != 0 || x.Send(wire.Pong, nil) != nil {
						return
					}
				default:
					return
				}
			}
		}()
	}
}

func (r *relay) forward(s *session, b []byte) {
	p, err := packet.Parse(b)
	if err != nil || p.TTL() == 0 || packet.Checksum(p.ICMP()) != 0 {
		return
	}
	if !p.EchoRequest() {
		// All complete LAN ICMP reaches this endpoint. Forward other types too,
		// without inventing return mappings for unrelated TCP/UDP flow errors.
		if p.TTL() > 1 {
			_ = r.raw.Send(packet.Forward(p))
		}
		return
	}
	if p.Destination().Equal(r.public) {
		s.deliver(packet.EchoReply(p, r.public))
		return
	}
	if p.TTL() <= 1 {
		s.deliver(packet.Error(p, r.public, 11, 0, 0))
		return
	}
	r.mu.Lock()
	var id uint16
	for i := 0; i < 65535; i++ {
		r.next++
		if r.next == 0 {
			r.next++
		}
		if _, used := r.probes[r.next]; !used {
			id = r.next
			break
		}
	}
	if id == 0 {
		r.mu.Unlock()
		return
	}
	original := packet.IPv4{Bytes: append([]byte{}, p.Bytes...), IHL: p.IHL}
	r.probes[id] = probe{s, original, time.Now().Add(r.timeout)}
	r.mu.Unlock()
	if err = r.raw.Send(packet.Outbound(p, id)); err != nil {
		r.mu.Lock()
		delete(r.probes, id)
		r.mu.Unlock()
		if errors.Is(err, syscall.EMSGSIZE) {
			s.deliver(packet.Error(p, r.public, 3, 4, routeMTU(p.Destination())))
		}
		// Route failures and unsupported sizes never use a local client fallback.
	}
}

func (r *relay) receive() {
	b := make([]byte, 65535)
	for {
		n, err := r.raw.Receive(b)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			log.Fatalf("raw receive: %v", err)
		}
		r.reply(b[:n])
	}
}

func (r *relay) reply(b []byte) {
	p, err := packet.Parse(b)
	if err != nil {
		return
	}
	icmp := p.ICMP()
	// Linux delivers raw ICMP before its ICMP handler validates the checksum.
	// Do not turn damaged network replies into valid ones while translating.
	if packet.Checksum(icmp) != 0 {
		return
	}
	var id, seq uint16
	var dst net.IP
	switch icmp[0] {
	case 0:
		if icmp[1] != 0 {
			return
		}
		id, seq, dst = p.ID(), p.Seq(), p.Source()
	case 3, 11, 12:
		var ok bool
		id, seq, dst, ok = packet.QuotedEcho(icmp)
		if !ok {
			return
		}
	default:
		return
	}
	r.mu.Lock()
	entry, ok := r.probes[id]
	if !ok || time.Now().After(entry.expires) || entry.original.Seq() != seq || !entry.original.Destination().Equal(dst) {
		r.mu.Unlock()
		return
	}
	if icmp[0] == 0 && !bytes.Equal(icmp[8:], entry.original.ICMP()[8:]) {
		r.mu.Unlock()
		return // a local server ping or a delayed, reused identifier
	}
	// Retain until timeout to allow legitimate duplicate replies.
	r.mu.Unlock()
	reply, err := packet.RestoreReply(p, entry.original)
	if err == nil {
		entry.owner.deliver(reply)
	}
}
