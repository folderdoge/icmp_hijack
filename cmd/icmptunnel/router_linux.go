//go:build linux

package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"icmptunnel/internal/packet"
	"icmptunnel/internal/wire"
)

func runRouter(cfg config) error {
	return runRouterStatus(cfg, nil)
}

func runRouterStatus(cfg config, status *statusReporter) error {
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
	_ = status.set("connecting", "正在连接服务器")
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
				_ = status.set("connect_error", "服务器连接失败，ICMP 继续丢弃，正在自动重试")
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
				if err.Error() == "authentication failed" {
					_ = status.set("auth_error", "密钥认证失败，请核对两端密钥；ICMP 继续丢弃")
				} else {
					_ = status.set("connect_error", "握手未完成，ICMP 继续丢弃，正在自动重试")
				}
				time.Sleep(3 * time.Second)
				continue
			}
			mu.Lock()
			active = x
			mu.Unlock()
			_ = status.set("connected", "已连接服务器，ICMP 隧道工作中")
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
					_ = status.set("connect_error", "服务器连接中断，ICMP 继续丢弃，正在自动重连")
					break
				}
				switch kind {
				case wire.Packet:
					if _, err = packet.Parse(b); err != nil {
						_ = x.Close()
						break
					}
					if _, err = tun.Write(b); err != nil {
						_ = status.set("error", "隧道回包写入失败，ICMP 继续丢弃")
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
