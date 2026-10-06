package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type receiveResult struct {
	kind    byte
	payload []byte
	err     error
}

func pipePair(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	key := bytes.Repeat([]byte{0x42}, 32)
	serverResult := make(chan struct {
		conn *Conn
		err  error
	}, 1)
	go func() {
		s, err := Handshake(right, key, true)
		serverResult <- struct {
			conn *Conn
			err  error
		}{s, err}
	}()
	c, err := Handshake(left, key, false)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-serverResult:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return c, result.conn
	case <-time.After(2 * time.Second):
		t.Fatal("server handshake did not complete")
		return nil, nil
	}
}

func exchange(t *testing.T, sender, receiver *Conn, kind byte, payload []byte) {
	t.Helper()
	result := make(chan receiveResult, 1)
	go func() {
		k, p, err := receiver.Receive()
		result <- receiveResult{k, p, err}
	}()
	if err := sender.Send(kind, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.kind != kind || !bytes.Equal(got.payload, payload) {
			t.Fatalf("receive = type %d, %d bytes, %v; wanted type %d, %d bytes", got.kind, len(got.payload), got.err, kind, len(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("frame receive did not complete")
	}
}

func TestKeyValidation(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	got, err := Key(strings.ToUpper(hex.EncodeToString(key)))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("valid 256-bit hexadecimal key was rejected")
	}
	for _, bad := range []string{"", "password", strings.Repeat("42", 31), strings.Repeat("42", 33), strings.Repeat("gg", 32)} {
		if _, err := Key(bad); err == nil {
			t.Fatalf("accepted invalid key %q", bad)
		}
	}
}

func TestHandshakeBidirectionalFramesAndLargestIPv4(t *testing.T) {
	client, server := pipePair(t)
	exchange(t, client, server, Packet, []byte("request"))
	exchange(t, server, client, Packet, []byte("reply"))
	exchange(t, client, server, Ping, nil)
	exchange(t, server, client, Pong, nil)
	exchange(t, client, server, Packet, bytes.Repeat([]byte{0xa5}, 65535))
	// The same nonce/plaintext must use different directional session keys.
	if bytes.Equal(client.tx.Seal(nil, nonce(0), []byte("same"), nil), server.tx.Seal(nil, nonce(0), []byte("same"), nil)) {
		t.Fatal("client and server transmit directions reused the same key")
	}
	otherClient, _ := pipePair(t)
	if bytes.Equal(client.tx.Seal(nil, nonce(0), []byte("same"), nil), otherClient.tx.Seal(nil, nonce(0), []byte("same"), nil)) {
		t.Fatal("fresh handshake reused a previous session key")
	}
}

func TestHandshakeRejectsWrongPSK(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	result := make(chan error, 1)
	go func() {
		_, err := Handshake(right, bytes.Repeat([]byte{0x42}, 32), true)
		result <- err
	}()
	if _, err := Handshake(left, bytes.Repeat([]byte{0x43}, 32), false); err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("client accepted wrong PSK: %v", err)
	}
	_ = left.Close() // Unblock server waiting for a proof the rejected client must not send.
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("server accepted a client that failed authentication")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rejected client left server handshake blocked")
	}
}

func TestServerRejectsInvalidClientProofAndProtocol(t *testing.T) {
	for _, invalidProtocol := range []bool{false, true} {
		left, right := net.Pipe()
		key := bytes.Repeat([]byte{0x42}, 32)
		result := make(chan error, 1)
		go func() {
			_, err := Handshake(right, key, true)
			result <- err
		}()
		hello := append([]byte("IHT1"), bytes.Repeat([]byte{0x11}, 32)...)
		if invalidProtocol {
			hello[3] = '2'
		}
		if _, err := left.Write(hello); err != nil {
			t.Fatal(err)
		}
		if !invalidProtocol {
			reply := make([]byte, 64)
			if _, err := io.ReadFull(left, reply); err != nil {
				t.Fatal(err)
			}
			if _, err := left.Write(make([]byte, 32)); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("server accepted invalid protocol or client proof")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("invalid handshake was not rejected")
		}
		_ = left.Close()
		_ = right.Close()
	}
}

// A byte-backed net.Conn lets tests alter the actual output of Send before
// Receive consumes it, without duplicating the framing or encryption algorithm.
type byteConn struct {
	reader io.Reader
	writer bytes.Buffer
	chunk  int
	zero   bool
}

func (c *byteConn) Read(b []byte) (int, error) {
	if c.reader == nil {
		return 0, io.EOF
	}
	return c.reader.Read(b)
}
func (c *byteConn) Write(b []byte) (int, error) {
	if c.zero {
		return 0, nil
	}
	if c.chunk > 0 && len(b) > c.chunk {
		b = b[:c.chunk]
	}
	return c.writer.Write(b)
}
func (*byteConn) Close() error                     { return nil }
func (*byteConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*byteConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*byteConn) SetDeadline(time.Time) error      { return nil }
func (*byteConn) SetReadDeadline(time.Time) error  { return nil }
func (*byteConn) SetWriteDeadline(time.Time) error { return nil }

func bufferedConn(t *testing.T, raw *byteConn) *Conn {
	t.Helper()
	a, err := aead(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return &Conn{Conn: raw, tx: a, rx: a}
}

func frameBytes(t *testing.T) []byte {
	t.Helper()
	raw := &byteConn{}
	if err := bufferedConn(t, raw).Send(Packet, []byte("payload with authentication")); err != nil {
		t.Fatal(err)
	}
	return append([]byte{}, raw.writer.Bytes()...)
}

func TestFrameRejectsCiphertextAndAuthenticatedLengthTampering(t *testing.T) {
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		func(b []byte) []byte { b[8] ^= 1; return b },
		func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[:4], binary.BigEndian.Uint32(b[:4])+1)
			return append(b, 0)
		},
	} {
		encoded := mutate(frameBytes(t))
		receiver := bufferedConn(t, &byteConn{reader: bytes.NewReader(encoded)})
		if _, p, err := receiver.Receive(); err == nil || p != nil || !strings.Contains(err.Error(), "authentication") {
			t.Fatalf("accepted tampered frame or exposed plaintext: %v", err)
		}
		if receiver.rxSeq != 0 {
			t.Fatal("failed authentication advanced receive sequence")
		}
	}
}

func TestFrameRejectsReplay(t *testing.T) {
	encoded := frameBytes(t)
	receiver := bufferedConn(t, &byteConn{reader: bytes.NewReader(append(append([]byte{}, encoded...), encoded...))})
	if kind, p, err := receiver.Receive(); err != nil || kind != Packet || string(p) != "payload with authentication" {
		t.Fatalf("initial genuine frame failed: %v", err)
	}
	if _, p, err := receiver.Receive(); err == nil || p != nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("accepted replay: %v", err)
	}
}

func TestFrameRejectsInvalidLengthsAndTruncation(t *testing.T) {
	for _, size := range []uint32{0, 16, maxPlain + 17, ^uint32(0)} {
		head := make([]byte, 4)
		binary.BigEndian.PutUint32(head, size)
		receiver := bufferedConn(t, &byteConn{reader: bytes.NewReader(head)})
		if _, _, err := receiver.Receive(); err == nil || !strings.Contains(err.Error(), "invalid frame size") {
			t.Fatalf("declared length %d was not rejected before payload read: %v", size, err)
		}
	}
	encoded := frameBytes(t)
	for _, short := range [][]byte{encoded[:3], encoded[:len(encoded)-1]} {
		receiver := bufferedConn(t, &byteConn{reader: bytes.NewReader(short)})
		if _, p, err := receiver.Receive(); err == nil || p != nil {
			t.Fatal("accepted truncated frame")
		}
	}
	writer := &byteConn{}
	sender := bufferedConn(t, writer)
	if err := sender.Send(Packet, make([]byte, maxPlain)); err == nil || writer.writer.Len() != 0 || sender.txSeq != 0 {
		t.Fatal("oversized IPv4 frame wrote data or consumed a nonce")
	}
}

func TestSequenceExhaustionPreventsNonceReuse(t *testing.T) {
	raw := &byteConn{}
	c := bufferedConn(t, raw)
	c.txSeq, c.rxSeq = ^uint64(0), ^uint64(0)
	if err := c.Send(Packet, nil); err == nil || raw.writer.Len() != 0 {
		t.Fatal("exhausted send sequence was reused")
	}
	if _, _, err := c.Receive(); err == nil || !strings.Contains(err.Error(), "sequence exhausted") {
		t.Fatal("exhausted receive sequence was reused")
	}
}

func TestShortWritesAndConcurrentSend(t *testing.T) {
	raw := &byteConn{chunk: 3}
	sender := bufferedConn(t, raw)
	var wg sync.WaitGroup
	errors := make(chan error, 64)
	for i := range 64 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errors <- sender.Send(Packet, []byte{byte(i)})
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	receiver := bufferedConn(t, &byteConn{reader: bytes.NewReader(raw.writer.Bytes())})
	seen := make(map[byte]bool)
	for range 64 {
		kind, p, err := receiver.Receive()
		if err != nil || kind != Packet || len(p) != 1 || seen[p[0]] {
			t.Fatalf("concurrent short writes corrupted or duplicated frames: %v", err)
		}
		seen[p[0]] = true
	}
	if err := bufferedConn(t, &byteConn{zero: true}).Send(Packet, nil); err != io.ErrShortWrite {
		t.Fatalf("zero progress write returned %v, want ErrShortWrite", err)
	}
}
