// Package wire implements IHT1: a PSK authenticated, encrypted TCP stream.
package wire

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	Packet   byte = 1
	Ping     byte = 2
	Pong     byte = 3
	maxPlain      = 65536 // type byte + largest IPv4 packet
)

var magic = []byte("IHT1")

func Key(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil, errors.New("key must be 64 hexadecimal characters (32 random bytes)")
	}
	return b, nil
}

func mac(key []byte, pieces ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	for _, p := range pieces {
		_, _ = h.Write(p)
	}
	return h.Sum(nil)
}

// HKDF-SHA256 extract/expand, one block per direction.
func derive(key, transcript []byte, label string) []byte {
	prk := mac(transcript, key)
	return mac(prk, []byte("IHT1/"+label), []byte{1})
}

type Conn struct {
	net.Conn
	tx, rx       cipher.AEAD
	txSeq, rxSeq uint64
	mu           sync.Mutex // packets and heartbeats share one writer
}

func Handshake(c net.Conn, key []byte, server bool) (*Conn, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid PSK length")
	}
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	defer c.SetDeadline(time.Time{})
	clientNonce, serverNonce := make([]byte, 32), make([]byte, 32)
	if server {
		hello := make([]byte, 36)
		if _, err := io.ReadFull(c, hello); err != nil {
			return nil, err
		}
		if !hmac.Equal(hello[:4], magic) {
			return nil, errors.New("protocol version mismatch")
		}
		copy(clientNonce, hello[4:])
		if _, err := rand.Read(serverNonce); err != nil {
			return nil, err
		}
		proof := mac(key, []byte("IHT1/server"), clientNonce, serverNonce)
		if err := writeAll(c, append(append([]byte{}, serverNonce...), proof...)); err != nil {
			return nil, err
		}
		clientProof := make([]byte, 32)
		if _, err := io.ReadFull(c, clientProof); err != nil {
			return nil, err
		}
		if !hmac.Equal(clientProof, mac(key, []byte("IHT1/client"), clientNonce, serverNonce)) {
			return nil, errors.New("authentication failed")
		}
	} else {
		if _, err := rand.Read(clientNonce); err != nil {
			return nil, err
		}
		if err := writeAll(c, append(append([]byte{}, magic...), clientNonce...)); err != nil {
			return nil, err
		}
		reply := make([]byte, 64)
		if _, err := io.ReadFull(c, reply); err != nil {
			return nil, err
		}
		copy(serverNonce, reply[:32])
		if !hmac.Equal(reply[32:], mac(key, []byte("IHT1/server"), clientNonce, serverNonce)) {
			return nil, errors.New("authentication failed")
		}
		if err := writeAll(c, mac(key, []byte("IHT1/client"), clientNonce, serverNonce)); err != nil {
			return nil, err
		}
	}
	transcript := append(append([]byte{}, clientNonce...), serverNonce...)
	c2s, err := aead(derive(key, transcript, "c2s"))
	if err != nil {
		return nil, err
	}
	s2c, err := aead(derive(key, transcript, "s2c"))
	if err != nil {
		return nil, err
	}
	x := &Conn{Conn: c, tx: c2s, rx: s2c}
	if server {
		x.tx, x.rx = s2c, c2s
	}
	return x, nil
}

func aead(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func nonce(seq uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], seq)
	return n
}

func (c *Conn) Send(kind byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(payload)+1 > maxPlain || c.txSeq == ^uint64(0) {
		return errors.New("frame limit exceeded")
	}
	plain := append([]byte{kind}, payload...)
	head := make([]byte, 4)
	binary.BigEndian.PutUint32(head, uint32(len(plain)+c.tx.Overhead()))
	encrypted := c.tx.Seal(nil, nonce(c.txSeq), plain, head)
	c.txSeq++
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return writeAll(c.Conn, append(head, encrypted...))
}

func (c *Conn) Receive() (byte, []byte, error) {
	if c.rxSeq == ^uint64(0) {
		return 0, nil, errors.New("sequence exhausted")
	}
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
	head := make([]byte, 4)
	if _, err := io.ReadFull(c.Conn, head); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(head)
	if n < uint32(c.rx.Overhead()+1) || n > maxPlain+uint32(c.rx.Overhead()) {
		return 0, nil, fmt.Errorf("invalid frame size %d", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, data); err != nil {
		return 0, nil, err
	}
	plain, err := c.rx.Open(nil, nonce(c.rxSeq), data, head)
	if err != nil {
		return 0, nil, errors.New("frame authentication failed")
	}
	c.rxSeq++
	return plain[0], plain[1:], nil
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
