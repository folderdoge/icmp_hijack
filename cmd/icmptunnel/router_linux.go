//go:build linux

package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"icmptunnel/internal/packet"
	"icmptunnel/internal/wire"
)

func runRouter(cfg config) error {
	key, err := wire.Key(cfg.Key)
	if err != nil {
		return err
	}
	if _, err = ipv4(cfg.Server); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("invalid TCP port")
	}
	tun, err := openTun(cfg.Interface)
	if err != nil {
		return err
	}
	defer tun.Close()
	log.Printf("router started interface=%s server=%s:%d (disconnected packets are dropped)", cfg.Interface, cfg.Server, cfg.Port)
	var mu sync.RWMutex
	var active *wire.Conn
	// A small queue absorbs normal probes; disconnected packets are discarded at
	// read time and queued packets belong only to their originating TCP session.
	type outgoing struct {
		conn  *wire.Conn
		bytes []byte
	}
	queue := make(chan outgoing, 128)
	go func() {
		for p := range queue {
			mu.RLock()
			current := active
			mu.RUnlock()
			if current == p.conn {
				if err := p.conn.Send(wire.Packet, p.bytes); err != nil {
					_ = p.conn.Close()
				}
			}
		}
	}()
	go func() {
		for {
			c, err := net.DialTimeout("tcp4", net.JoinHostPort(cfg.Server, fmt.Sprint(cfg.Port)), 5*time.Second)
			if err != nil {
				log.Printf("connect failed: %v", err)
				time.Sleep(3 * time.Second)
				continue
			}
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.SetNoDelay(true)
				_ = tcp.SetKeepAlive(true)
			}
			x, err := wire.Handshake(c, key, false)
			if err != nil {
				_ = c.Close()
				log.Printf("handshake failed: %v", err)
				time.Sleep(3 * time.Second)
				continue
			}
			mu.Lock()
			active = x
			mu.Unlock()
			log.Print("tunnel connected")
			done := make(chan struct{})
			go func() {
				t := time.NewTicker(5 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-done:
						return
					case <-t.C:
						if err := x.Send(wire.Ping, nil); err != nil {
							_ = x.Close()
							return
						}
					}
				}
			}()
			for {
				kind, b, err := x.Receive()
				if err != nil {
					log.Printf("tunnel disconnected: %v", err)
					break
				}
				switch kind {
				case wire.Packet:
					if _, err = packet.Parse(b); err != nil {
						_ = x.Close()
						break
					}
					if _, err = tun.Write(b); err != nil {
						log.Printf("TUN write: %v", err)
						_ = x.Close()
					}
				case wire.Pong:
					if len(b) != 0 {
						_ = x.Close()
					}
				default:
					_ = x.Close()
				}
			}
			mu.Lock()
			active = nil
			mu.Unlock()
			close(done)
			_ = x.Close()
			time.Sleep(3 * time.Second)
		}
	}()
	b := make([]byte, 65535)
	for {
		n, err := tun.Read(b)
		if err != nil {
			return err
		}
		p, err := packet.Parse(b[:n])
		if err != nil {
			continue
		}
		mu.RLock()
		x := active
		mu.RUnlock()
		if x == nil {
			continue
		}
		item := outgoing{x, append([]byte{}, p.Bytes...)}
		select {
		case queue <- item:
		default:
		} // never fall back to WAN
	}
}
