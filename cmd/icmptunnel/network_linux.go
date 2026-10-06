//go:build linux

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Linux ifreq is 32 bytes on 32-bit ARM and 40 on 64-bit platforms.
// A 40-byte buffer works for both; the fields used here have the same offsets.
func ioctl(fd int, req uintptr, b []byte) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&b[0])))
	if e != 0 {
		return e
	}
	return nil
}

func openTun(name string) (*os.File, error) {
	if len(name) == 0 || len(name) > 15 {
		return nil, fmt.Errorf("invalid TUN interface name")
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*os.File, error) { _ = f.Close(); return nil, e }
	req := make([]byte, 40)
	copy(req, name)
	binary.LittleEndian.PutUint16(req[16:], 0x1001) // TUN | NO_PI
	if err = ioctl(int(f.Fd()), 0x400454ca, req); err != nil {
		return fail(fmt.Errorf("TUNSETIFF: %w", err))
	}
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return fail(err)
	}
	defer syscall.Close(s)
	// Even loose rp_filter rejects an unnumbered ingress device in Linux's
	// fib_validate_source. Give this nonpersistent TUN a /32 plumbing address.
	// No connected subnet is installed; the address disappears with the TUN.
	req = make([]byte, 40)
	copy(req, name)
	binary.LittleEndian.PutUint16(req[16:], syscall.AF_INET)
	copy(req[20:24], []byte{198, 18, 0, 1})
	if err = ioctl(s, 0x8916, req); err != nil { // SIOCSIFADDR
		return fail(err)
	}
	req = make([]byte, 40)
	copy(req, name)
	binary.LittleEndian.PutUint16(req[16:], syscall.AF_INET)
	copy(req[20:24], []byte{255, 255, 255, 255})
	if err = ioctl(s, 0x891c, req); err != nil { // SIOCSIFNETMASK
		return fail(err)
	}
	req = make([]byte, 40)
	copy(req, name)
	if err = ioctl(s, 0x8913, req); err != nil {
		return fail(err)
	} // SIOCGIFFLAGS
	flags := binary.LittleEndian.Uint16(req[16:]) | 1
	binary.LittleEndian.PutUint16(req[16:], flags)
	if err = ioctl(s, 0x8914, req); err != nil {
		return fail(err)
	} // SIOCSIFFLAGS
	req = make([]byte, 40)
	copy(req, name)
	binary.LittleEndian.PutUint32(req[16:], 1500)
	if err = ioctl(s, 0x8922, req); err != nil {
		return fail(err)
	} // SIOCSIFMTU
	// loose mode wins over an all.rp_filter=1 setting without changing global state.
	if err = os.WriteFile("/proc/sys/net/ipv4/conf/"+name+"/rp_filter", []byte("2\n"), 0600); err != nil {
		return fail(err)
	}
	return f, nil
}

type rawICMP struct{ send, recv int }

func openRaw() (*rawICMP, error) {
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		return nil, err
	}
	r, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		_ = syscall.Close(s)
		return nil, err
	}
	return &rawICMP{send: s, recv: r}, nil
}

func (r *rawICMP) Close() { _ = syscall.Close(r.send); _ = syscall.Close(r.recv) }
func (r *rawICMP) Send(b []byte) error {
	var dst [4]byte
	copy(dst[:], b[16:20])
	return syscall.Sendto(r.send, b, 0, &syscall.SockaddrInet4{Addr: dst})
}
func (r *rawICMP) Receive(b []byte) (int, error) {
	n, _, err := syscall.Recvfrom(r.recv, b, 0)
	return n, err
}

func routeMTU(dst []byte) uint16 {
	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return 0
	}
	defer syscall.Close(s)
	var addr [4]byte
	copy(addr[:], dst)
	if err = syscall.Connect(s, &syscall.SockaddrInet4{Addr: addr, Port: 9}); err != nil {
		return 0
	}
	n, err := syscall.GetsockoptInt(s, syscall.IPPROTO_IP, syscall.IP_MTU)
	if err != nil || n > 65535 {
		return 0
	}
	return uint16(n)
}
