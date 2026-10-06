//go:build linux

package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"icmptunnel/internal/packet"
)

func testProbe(t *testing.T, ttl byte) packet.IPv4 {
	t.Helper()
	b := make([]byte, 36)
	b[0], b[8], b[9] = 0x45, ttl, 1
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	copy(b[12:16], net.ParseIP("192.168.50.100").To4())
	copy(b[16:20], net.ParseIP("203.0.113.9").To4())
	b[20] = 8
	binary.BigEndian.PutUint16(b[24:26], 4242)
	binary.BigEndian.PutUint16(b[26:28], 7)
	copy(b[28:], []byte("lan-ping"))
	packet.FixICMP(b[20:])
	packet.FixIP(b)
	p, err := packet.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testSession() *session {
	return &session{replies: make(chan []byte, 4), done: make(chan struct{})}
}
func expectNoReply(t *testing.T, s *session) {
	t.Helper()
	select {
	case b := <-s.replies:
		t.Fatalf("unexpected packet %x", b)
	default:
	}
}

func TestServerHopAndSelfDestination(t *testing.T) {
	s := testSession()
	r := &relay{public: net.ParseIP("14.137.20.5"), probes: make(map[uint16]probe)}
	p := testProbe(t, 1)
	r.forward(s, p.Bytes)
	b := <-s.replies
	answer, err := packet.Parse(b)
	if err != nil || answer.ICMP()[0] != 11 || !answer.Source().Equal(r.public) || !answer.Destination().Equal(p.Source()) {
		t.Fatalf("bad second hop %x: %v", b, err)
	}
	if len(r.probes) != 0 {
		t.Fatal("hop2 must not allocate raw probe")
	}
	copy(p.Bytes[16:20], r.public.To4())
	packet.FixIP(p.Bytes)
	r.forward(s, p.Bytes)
	b = <-s.replies
	answer, err = packet.Parse(b)
	if err != nil || answer.ICMP()[0] != 0 || answer.ID() != p.ID() {
		t.Fatalf("server itself must terminate TTL2 trace: %x", b)
	}
	p.ICMP()[len(p.ICMP())-1] ^= 1
	r.forward(s, p.Bytes)
	expectNoReply(t, s)
}

func TestReplyIdentityExpiryAndChecksum(t *testing.T) {
	p := testProbe(t, 63)
	s := testSession()
	r := &relay{probes: map[uint16]probe{100: {s, p, time.Now().Add(time.Minute)}}}
	b := packet.EchoReply(p, p.Destination())
	copy(b[16:20], net.ParseIP("10.0.0.2").To4())
	binary.BigEndian.PutUint16(b[24:26], 100)
	packet.FixICMP(b[20:])
	packet.FixIP(b)
	r.reply(b)
	answer := <-s.replies
	parsed, _ := packet.Parse(answer)
	if parsed.ID() != 4242 || !parsed.Destination().Equal(p.Source()) || packet.Checksum(parsed.ICMP()) != 0 {
		t.Fatalf("bad client translation: %x", answer)
	}
	for _, kind := range []string{"corrupted", "different payload", "different sequence", "different target", "expired"} {
		t.Run(kind, func(t *testing.T) {
			bad := append([]byte{}, b...)
			switch kind {
			case "corrupted":
				bad[len(bad)-1] ^= 1
			case "different payload":
				bad[len(bad)-1] ^= 1
				packet.FixICMP(bad[20:])
			case "different sequence":
				bad[27]++
				packet.FixICMP(bad[20:])
			case "different target":
				bad[15]++
				packet.FixIP(bad)
			case "expired":
				e := r.probes[100]
				e.expires = time.Now().Add(-time.Second)
				r.probes[100] = e
			}
			r.reply(bad)
			expectNoReply(t, s)
		})
	}
}
