package packet

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
)

func fixturePacket(src, dst string, ttl byte, options, icmp []byte) []byte {
	h := 20 + len(options)
	b := make([]byte, h+len(icmp))
	b[0], b[8], b[9] = 0x40|byte(h/4), ttl, 1
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	binary.BigEndian.PutUint16(b[4:6], 0x3141)
	binary.BigEndian.PutUint16(b[6:8], 0x4000) // DF is not fragmentation.
	copy(b[12:16], net.ParseIP(src).To4())
	copy(b[16:20], net.ParseIP(dst).To4())
	copy(b[20:h], options)
	copy(b[h:], icmp)
	FixICMP(b[h:])
	FixIP(b)
	return b
}

func fixtureEcho(src string, ttl byte, id, seq uint16, options, data []byte) []byte {
	icmp := make([]byte, 8+len(data))
	icmp[0] = 8
	binary.BigEndian.PutUint16(icmp[4:6], id)
	binary.BigEndian.PutUint16(icmp[6:8], seq)
	copy(icmp[8:], data)
	return fixturePacket(src, "203.0.113.9", ttl, options, icmp)
}

func parsed(t *testing.T, b []byte) IPv4 {
	t.Helper()
	p, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func validChecksums(t *testing.T, p IPv4) {
	t.Helper()
	if Checksum(p.Bytes[:p.IHL]) != 0 || Checksum(p.ICMP()) != 0 {
		t.Fatalf("invalid IPv4 or ICMP checksum: %x", p.Bytes)
	}
}

func TestChecksumKnownVectors(t *testing.T) {
	for _, tc := range []struct {
		name, hex string
		want      uint16
	}{
		{"empty", "", 0xffff},
		{"RFC1071 arithmetic", "0001f203f4f5f6f7", 0x220d},
		{"odd trailing byte", "0001f2", 0x0dfe},
		{"valid echo", "0800f7ff00000000", 0},
		{"valid IPv4 header", "45000073000040004011b861c0a80001c0a800c7", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			if got := Checksum(b); got != tc.want {
				t.Fatalf("checksum = %#04x, want %#04x", got, tc.want)
			}
		})
	}
}

func TestParseOptionsAndTrailingPadding(t *testing.T) {
	b := fixtureEcho("192.168.50.100", 63, 0x1234, 7, []byte{1, 1, 0, 0}, []byte("odd"))
	p := parsed(t, append(append([]byte{}, b...), 0xa5, 0xa5))
	if p.IHL != 24 || len(p.Bytes) != len(b) || !p.EchoRequest() || p.ID() != 0x1234 || p.Seq() != 7 {
		t.Fatalf("incorrect header or echo fields: %+v", p)
	}
	if !p.Source().Equal(net.ParseIP("192.168.50.100")) || !p.Destination().Equal(net.ParseIP("203.0.113.9")) {
		t.Fatal("wrong source or destination")
	}
}

func TestParseRejectsMalformedAndFragments(t *testing.T) {
	base := fixtureEcho("192.168.50.100", 63, 1, 2, nil, []byte("data"))
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"short header", func(b []byte) []byte { return b[:19] }},
		{"IPv6", func(b []byte) []byte { b[0] = 0x65; return b }},
		{"small IHL", func(b []byte) []byte { b[0] = 0x44; return b }},
		{"IHL beyond buffer", func(b []byte) []byte { b[0] = 0x4f; return b }},
		{"short declared ICMP", func(b []byte) []byte { binary.BigEndian.PutUint16(b[2:4], 27); return b }},
		{"truncated packet", func(b []byte) []byte { binary.BigEndian.PutUint16(b[2:4], 100); return b }},
		{"UDP", func(b []byte) []byte { b[9] = 17; return b }},
		{"first fragment", func(b []byte) []byte { binary.BigEndian.PutUint16(b[6:8], 0x2000); return b }},
		{"later fragment", func(b []byte) []byte { binary.BigEndian.PutUint16(b[6:8], 1); return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.mutate(append([]byte{}, base...))); err == nil {
				t.Fatal("accepted malformed or fragmented packet")
			}
		})
	}
}

func TestOutboundDecrementsTTLAndRemapsEchoWithoutMutatingProbe(t *testing.T) {
	b := fixtureEcho("192.168.50.100", 63, 0x1234, 0xabcd, []byte{1, 1, 0, 0}, []byte("unchanged payload"))
	before := append([]byte{}, b...)
	p := parsed(t, b)
	out := parsed(t, Outbound(p, 0x5678))
	if out.TTL() != 62 || !out.Source().Equal(net.IPv4zero) || out.ID() != 0x5678 || out.Seq() != p.Seq() {
		t.Fatal("incorrect egress TTL, source or echo identity")
	}
	if !out.Destination().Equal(p.Destination()) || !bytes.Equal(out.ICMP()[8:], p.ICMP()[8:]) || !bytes.Equal(out.Bytes[20:out.IHL], p.Bytes[20:p.IHL]) {
		t.Fatal("egress changed destination, payload or IPv4 options")
	}
	if !bytes.Equal(b, before) {
		t.Fatal("egress mutated the saved original probe")
	}
	validChecksums(t, out)
}

func TestSyntheticTimeExceededAndEchoReply(t *testing.T) {
	original := parsed(t, fixtureEcho("192.168.50.100", 1, 0x1234, 7, []byte{1, 1, 0, 0}, []byte("hello")))
	source := net.ParseIP("14.137.20.5")
	for _, reply := range []IPv4{
		parsed(t, Error(original, source, 11, 0, 0)),
		parsed(t, EchoReply(original, source)),
	} {
		if !reply.Source().Equal(source) || !reply.Destination().Equal(original.Source()) {
			t.Fatal("synthetic hop used incorrect addresses")
		}
		validChecksums(t, reply)
	}
	err := parsed(t, Error(original, source, 11, 0, 0))
	if !bytes.Equal(err.ICMP()[8:], original.Bytes[:original.IHL+8]) {
		t.Fatal("time exceeded lost the original probe identity")
	}
}

func TestRestoreEchoReply(t *testing.T) {
	original := parsed(t, fixtureEcho("192.168.50.100", 63, 0x1234, 7, nil, []byte("echo payload")))
	egress := parsed(t, Outbound(original, 0x5678))
	reply := parsed(t, EchoReply(egress, original.Destination()))
	before := append([]byte{}, reply.Bytes...)
	restoredBytes, err := RestoreReply(reply, original)
	if err != nil {
		t.Fatal(err)
	}
	restored := parsed(t, restoredBytes)
	if restored.ICMP()[0] != 0 || restored.ID() != original.ID() || restored.Seq() != original.Seq() || !restored.Source().Equal(original.Destination()) || !restored.Destination().Equal(original.Source()) {
		t.Fatal("echo reply did not restore client identity and preserve responding host")
	}
	if !bytes.Equal(restored.ICMP()[8:], original.ICMP()[8:]) || !bytes.Equal(before, reply.Bytes) {
		t.Fatal("restore changed echo data or mutated raw reply")
	}
	validChecksums(t, restored)
}

func TestRestoreTruncatedErrorsWithOptions(t *testing.T) {
	for _, options := range [][]byte{nil, {1, 1, 0, 0}} {
		original := parsed(t, fixtureEcho("192.168.50.100", 63, 0x1234, 7, options, bytes.Repeat([]byte{0xab}, 257)))
		egress := parsed(t, Outbound(original, 0x5678))
		for _, tc := range []struct {
			kind, code byte
			mtu        uint16
		}{{11, 0, 0}, {3, 1, 0}, {3, 4, 1280}, {12, 0, 0}} {
			reply := parsed(t, Error(egress, net.ParseIP("198.51.100.1"), tc.kind, tc.code, tc.mtu))
			id, seq, dst, ok := QuotedEcho(reply.ICMP())
			if !ok || id != 0x5678 || seq != original.Seq() || !dst.Equal(original.Destination()) {
				t.Fatal("failed to recognize a minimum-size ICMP quote")
			}
			restoredBytes, err := RestoreReply(reply, original)
			if err != nil {
				t.Fatal(err)
			}
			restored := parsed(t, restoredBytes)
			quote := restored.ICMP()[8:]
			if !bytes.Equal(quote[:original.IHL+8], original.Bytes[:original.IHL+8]) {
				t.Fatal("restored quote changed original IPv4 or echo checksum/identity")
			}
			if Checksum(quote[:original.IHL]) != 0 {
				t.Fatal("restored quoted IPv4 header checksum is invalid")
			}
			if !restored.Source().Equal(reply.Source()) || !restored.Destination().Equal(original.Source()) || restored.ICMP()[0] != tc.kind || restored.ICMP()[1] != tc.code || binary.BigEndian.Uint16(restored.ICMP()[6:8]) != tc.mtu {
				t.Fatal("lost responding hop, error code, or PMTU")
			}
			validChecksums(t, restored)
		}
	}
}

func TestRestorePreservesRFC4884ExtensionsAndLongQuote(t *testing.T) {
	for _, lengthIndicator := range []byte{32, 0} { // Both RFC 4884 and older MPLS-aware routers.
		original := parsed(t, fixtureEcho("192.168.50.100", 63, 0x1234, 7, []byte{1, 1, 0, 0}, bytes.Repeat([]byte{0xab}, 200)))
		egress := parsed(t, Outbound(original, 0x5678))
		extension := []byte{0x20, 0, 0, 0, 0, 8, 1, 1, 0, 0, 0, 1}
		binary.BigEndian.PutUint16(extension[2:4], Checksum(extension))
		icmp := make([]byte, 8+128+len(extension))
		icmp[0], icmp[5] = 11, lengthIndicator
		copy(icmp[8:136], egress.Bytes[:128])
		copy(icmp[136:], extension)
		reply := parsed(t, fixturePacket("198.51.100.1", "198.51.100.5", 51, nil, icmp))
		b, err := RestoreReply(reply, original)
		if err != nil {
			t.Fatal(err)
		}
		restored := parsed(t, b)
		if len(restored.ICMP()) != len(reply.ICMP()) || restored.ICMP()[5] != lengthIndicator || !bytes.Equal(restored.ICMP()[136:], extension) {
			t.Fatal("restore discarded or overwrote ICMP extensions")
		}
		prefix := 8 + original.IHL + 8
		if !bytes.Equal(restored.ICMP()[prefix:], reply.ICMP()[prefix:]) || !bytes.Equal(restored.ICMP()[8:prefix], original.Bytes[:original.IHL+8]) {
			t.Fatal("restore changed data outside the translated quote prefix")
		}
		validChecksums(t, restored)
	}
}

func TestQuotedEchoRejectsMalformed(t *testing.T) {
	p := parsed(t, fixtureEcho("192.168.50.100", 63, 1, 2, nil, []byte("data")))
	base := Error(p, net.ParseIP("198.51.100.1"), 11, 0, 0)[20:]
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { b[8] = 0x65; return b },
		func(b []byte) []byte { b[8] = 0x44; return b },
		func(b []byte) []byte { b[8] = 0x4f; return b },
		func(b []byte) []byte { b[8+9] = 17; return b },
		func(b []byte) []byte { b[8+6] = 0x20; return b },
		func(b []byte) []byte { b[8+7] = 1; return b },
		func(b []byte) []byte { b[8+20] = 0; return b },
		func(b []byte) []byte { b[8+21] = 1; return b },
	} {
		if _, _, _, ok := QuotedEcho(mutate(append([]byte{}, base...))); ok {
			t.Fatal("accepted malformed quoted echo")
		}
	}
}

func TestRestoreRejectsIncompatibleQuoteHeaderAndUnsupportedReply(t *testing.T) {
	original := parsed(t, fixtureEcho("192.168.50.100", 63, 1, 2, nil, []byte("data")))
	withOptions := parsed(t, fixtureEcho("192.168.50.100", 63, 1, 2, []byte{1, 1, 0, 0}, []byte("data")))
	reply := parsed(t, Error(withOptions, net.ParseIP("198.51.100.1"), 11, 0, 0))
	if _, err := RestoreReply(reply, original); err == nil {
		t.Fatal("restored a quote whose IPv4 header length differs from the saved probe")
	}
	if _, err := RestoreReply(original, original); err == nil {
		t.Fatal("treated an Echo Request as a server reply")
	}
}

func TestRestoreAllowsLowTTLRepliesThroughOneMoreRouterHop(t *testing.T) {
	original := parsed(t, fixtureEcho("192.168.50.100", 63, 0x1234, 7, []byte{1, 1, 0, 0}, []byte("payload")))
	egress := parsed(t, Outbound(original, 0x5678))
	outerOptions := []byte{1, 1, 1, 1, 0, 0, 0, 0}
	echo := append([]byte{}, egress.ICMP()...)
	echo[0] = 0
	quotedError := Error(egress, net.ParseIP("198.51.100.1"), 11, 0, 0)[20:]
	for _, tc := range []struct {
		name, source string
		icmp         []byte
	}{
		{"echo", original.Destination().String(), echo},
		{"time-exceeded", "198.51.100.1", quotedError},
	} {
		for _, ttl := range []byte{1, 2, 51} {
			t.Run(fmt.Sprintf("%s/TTL%d", tc.name, ttl), func(t *testing.T) {
				reply := parsed(t, fixturePacket(tc.source, "198.51.100.5", ttl, outerOptions, tc.icmp))
				before := append([]byte{}, reply.Bytes...)
				b, err := RestoreReply(reply, original)
				if err != nil {
					t.Fatal(err)
				}
				restored := parsed(t, b)
				if ttl == 1 && restored.TTL() != 2 || ttl >= 2 && restored.TTL() != ttl {
					t.Fatalf("return TTL = %d for received TTL %d", restored.TTL(), ttl)
				}
				if restored.IHL != reply.IHL || !bytes.Equal(restored.Bytes[20:restored.IHL], outerOptions) {
					t.Fatal("TTL adjustment changed response IPv4 options")
				}
				if !restored.Source().Equal(reply.Source()) || !restored.Destination().Equal(original.Source()) || !bytes.Equal(before, reply.Bytes) {
					t.Fatal("TTL adjustment changed responding hop or mutated its original packet")
				}
				if tc.name == "time-exceeded" && !bytes.Equal(restored.ICMP()[8:], original.Bytes[:original.IHL+8]) {
					t.Fatal("return TTL adjustment changed the quoted request")
				}
				validChecksums(t, restored)
			})
		}
	}
}

func TestQuotedEchoRequiresDeclaredHeaderAndEchoBytesButAllowsTruncation(t *testing.T) {
	for _, options := range [][]byte{nil, {1, 1, 0, 0}} {
		original := parsed(t, fixtureEcho("192.168.50.100", 63, 0x1234, 7, options, bytes.Repeat([]byte{0xab}, 257)))
		base := Error(original, net.ParseIP("198.51.100.1"), 11, 0, 0)[20:]
		for _, badLength := range []uint16{0, uint16(original.IHL), uint16(original.IHL + 7)} {
			bad := append([]byte{}, base...)
			binary.BigEndian.PutUint16(bad[8+2:8+4], badLength)
			if _, _, _, ok := QuotedEcho(bad); ok {
				t.Fatalf("accepted quote with IHL %d and declared total length %d", original.IHL, badLength)
			}
		}
		for _, validLength := range []uint16{uint16(original.IHL + 8), uint16(len(original.Bytes))} {
			valid := append([]byte{}, base...)
			binary.BigEndian.PutUint16(valid[8+2:8+4], validLength)
			if id, seq, dst, ok := QuotedEcho(valid); !ok || id != original.ID() || seq != original.Seq() || !dst.Equal(original.Destination()) {
				t.Fatalf("rejected structurally valid truncated quote: declared %d, present %d", validLength, len(valid)-8)
			}
		}
	}
}

func TestConcurrentClientIdentityRestoration(t *testing.T) {
	// Identical client IDs/sequences are common. Their NAT IDs and saved originals
	// are distinct, and packet transforms must remain independent and immutable.
	for i := range 32 {
		source := net.IPv4(192, 168, 50, byte(100+i)).String()
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			original := parsed(t, fixtureEcho(source, 63, 0x1234, 7, nil, []byte{byte(i)}))
			egress := parsed(t, Outbound(original, uint16(400+i)))
			reply := parsed(t, EchoReply(egress, original.Destination()))
			b, err := RestoreReply(reply, original)
			if err != nil {
				t.Error(err)
				return
			}
			got := parsed(t, b)
			if !got.Destination().Equal(original.Source()) || got.ID() != 0x1234 || got.Seq() != 7 || got.ICMP()[8] != byte(i) {
				t.Error("concurrent reply was restored to the wrong client identity")
			}
			validChecksums(t, got)
		})
	}
}
