//go:build linux

package main

// This is deliberately only the IPv4 iproute2 subset used by the router scripts.
// It avoids depending on an ip applet in the firmware's BusyBox build.
import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	ipFRAPriority = 6
	ipFRAFWMark   = 10
	ipFRATable    = 15
	ipFRAFWMask   = 16
	ipRTATable    = 15
	ipRTAOIF      = 4
	ipRTAPriority = 6
)

type ipCommand struct {
	operation              string
	preference, mark, mask uint32
	table, metric          uint32
	device                 string
	blackhole              bool
}

func ipNumber(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid ip numeric argument %q", s)
	}
	return uint32(n), nil
}

func parseIPCommand(args []string) (ipCommand, error) {
	c := ipCommand{}
	bad := func() (ipCommand, error) {
		return c, fmt.Errorf("unsupported ip command: %s", strings.Join(args, " "))
	}
	if len(args) < 2 {
		return bad()
	}
	var err error
	switch args[0] {
	case "link":
		if args[1] != "show" {
			return bad()
		}
		if len(args) == 3 {
			c.device = args[2]
		} else if len(args) == 4 && args[2] == "dev" {
			c.device = args[3]
		} else {
			return bad()
		}
		c.operation = "link-show"
	case "rule":
		if len(args) == 2 && args[1] == "show" {
			c.operation = "rule-show"
			return c, nil
		}
		if len(args) != 8 || (args[1] != "add" && args[1] != "del") || args[2] != "pref" || args[4] != "fwmark" || args[6] != "table" {
			return bad()
		}
		c.operation = "rule-" + args[1]
		c.preference, err = ipNumber(args[3])
		if err != nil {
			return c, err
		}
		mark := strings.Split(args[5], "/")
		if len(mark) != 2 {
			return bad()
		}
		c.mark, err = ipNumber(mark[0])
		if err != nil {
			return c, err
		}
		c.mask, err = ipNumber(mark[1])
		if err != nil {
			return c, err
		}
		c.table, err = ipNumber(args[7])
	case "route":
		if len(args) == 3 && args[1] == "flush" && args[2] == "cache" {
			c.operation = "cache-flush"
			return c, nil // IPv4's global route cache was removed in Linux 3.6.
		}
		if len(args) == 4 && args[1] == "show" && args[2] == "table" {
			c.operation = "route-show"
			c.table, err = ipNumber(args[3])
			break
		}
		if args[1] != "replace" && args[1] != "del" {
			return bad()
		}
		c.operation = "route-" + args[1]
		var table, metric string
		if len(args) == 8 && args[2] == "blackhole" && args[3] == "default" && args[4] == "table" && args[6] == "metric" {
			c.blackhole, table, metric = true, args[5], args[7]
		} else if len(args) == 9 && args[2] == "default" && args[3] == "dev" && args[5] == "table" && args[7] == "metric" {
			c.device, table, metric = args[4], args[6], args[8]
		} else {
			return bad()
		}
		c.table, err = ipNumber(table)
		if err != nil {
			return c, err
		}
		c.metric, err = ipNumber(metric)
	default:
		return bad()
	}
	if err != nil {
		return c, err
	}
	if strings.HasPrefix(c.operation, "route-") && c.table == 0 || strings.HasPrefix(c.operation, "rule-") && c.operation != "rule-show" && c.table == 0 {
		return c, fmt.Errorf("ip table must be nonzero")
	}
	if (c.operation == "link-show" || c.operation == "route-replace" && !c.blackhole || c.operation == "route-del" && !c.blackhole) && (c.device == "" || len(c.device) > 15 || strings.IndexByte(c.device, 0) >= 0) {
		return c, fmt.Errorf("invalid interface name %q", c.device)
	}
	return c, nil
}

func ipAttr32(dst []byte, kind uint16, value uint32) []byte {
	b := make([]byte, 8)
	binary.NativeEndian.PutUint16(b[0:2], 8)
	binary.NativeEndian.PutUint16(b[2:4], kind)
	binary.NativeEndian.PutUint32(b[4:8], value)
	return append(dst, b...)
}

// Both fib_rule_hdr and rtmsg have a fixed 12-byte wire layout on all Linux
// architectures. No Go struct or pointer is used as a kernel wire structure.
func ipBuildRequest(c ipCommand, ifindex int) (uint16, uint16, []byte, error) {
	b := make([]byte, 12)
	b[0] = syscall.AF_INET
	flags := uint16(syscall.NLM_F_REQUEST | syscall.NLM_F_ACK)
	var kind uint16
	switch c.operation {
	case "rule-show", "route-show":
		flags = syscall.NLM_F_REQUEST | syscall.NLM_F_DUMP
		if c.operation == "rule-show" {
			kind = syscall.RTM_GETRULE
		} else {
			kind = syscall.RTM_GETROUTE
		}
	case "rule-add", "rule-del":
		kind = syscall.RTM_DELRULE
		if c.operation == "rule-add" {
			kind = syscall.RTM_NEWRULE
			flags |= syscall.NLM_F_CREATE | syscall.NLM_F_EXCL
		}
		b[7] = 1 // FR_ACT_TO_TBL
		if c.table < 256 {
			b[4] = byte(c.table)
		}
		b = ipAttr32(b, ipFRAPriority, c.preference)
		b = ipAttr32(b, ipFRAFWMark, c.mark)
		b = ipAttr32(b, ipFRAFWMask, c.mask)
		b = ipAttr32(b, ipFRATable, c.table)
	case "route-replace", "route-del":
		kind = syscall.RTM_DELROUTE
		if c.operation == "route-replace" {
			kind = syscall.RTM_NEWROUTE
			flags |= syscall.NLM_F_CREATE | syscall.NLM_F_REPLACE
			b[5] = 4 // RTPROT_STATIC; deletion leaves protocol unspecified.
		}
		if c.table < 256 {
			b[4] = byte(c.table)
		}
		b[7] = 1 // RTN_UNICAST
		if c.blackhole {
			b[7] = 6 // RTN_BLACKHOLE
		} else {
			if ifindex <= 0 || uint64(ifindex) > 0xffffffff {
				return 0, 0, nil, fmt.Errorf("invalid output interface index")
			}
			b[6] = 253 // RT_SCOPE_LINK, as for ip route default dev tun.
			b = ipAttr32(b, ipRTAOIF, uint32(ifindex))
		}
		b = ipAttr32(b, ipRTATable, c.table)
		b = ipAttr32(b, ipRTAPriority, c.metric)
	default:
		return 0, 0, nil, fmt.Errorf("ip command does not use netlink")
	}
	return kind, flags, b, nil
}

func ipExchange(kind, flags uint16, payload []byte) ([]syscall.NetlinkMessage, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	syscall.CloseOnExec(fd)
	if err = syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	if err = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 5}); err != nil {
		return nil, err
	}
	request := make([]byte, 16+len(payload))
	binary.NativeEndian.PutUint32(request[0:4], uint32(len(request)))
	binary.NativeEndian.PutUint16(request[4:6], kind)
	binary.NativeEndian.PutUint16(request[6:8], flags)
	binary.NativeEndian.PutUint32(request[8:12], 1)
	copy(request[16:], payload)
	if err = syscall.Sendto(fd, request, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	var result []syscall.NetlinkMessage
	buf := make([]byte, 64*1024)
	for {
		n, _, receiveFlags, from, err := syscall.Recvmsg(fd, buf, nil, 0)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("ip netlink receive: %w", err)
		}
		if receiveFlags&syscall.MSG_TRUNC != 0 {
			return nil, fmt.Errorf("truncated ip netlink response")
		}
		peer, ok := from.(*syscall.SockaddrNetlink)
		if !ok || peer.Pid != 0 {
			return nil, fmt.Errorf("unexpected ip netlink sender")
		}
		messages, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return nil, err
		}
		for _, m := range messages {
			if m.Header.Seq != 1 {
				continue
			}
			if m.Header.Flags&0x10 != 0 { // NLM_F_DUMP_INTR
				return nil, fmt.Errorf("ip netlink dump interrupted; retry")
			}
			switch m.Header.Type {
			case syscall.NLMSG_ERROR:
				if len(m.Data) < 4 {
					return nil, fmt.Errorf("short ip netlink acknowledgement")
				}
				code := int32(binary.NativeEndian.Uint32(m.Data[:4]))
				if code < 0 {
					return nil, syscall.Errno(-int64(code))
				}
				if code != 0 {
					return nil, fmt.Errorf("invalid ip netlink acknowledgement")
				}
				return result, nil
			case syscall.NLMSG_DONE:
				if len(m.Data) >= 4 {
					code := int32(binary.NativeEndian.Uint32(m.Data[:4]))
					if code < 0 {
						return nil, syscall.Errno(-int64(code))
					}
				}
				return result, nil
			case syscall.NLMSG_OVERRUN:
				return nil, fmt.Errorf("ip netlink response overrun")
			default:
				m.Data = append([]byte(nil), m.Data...) // receive buffer is reused.
				result = append(result, m)
			}
		}
	}
}

func ipAttributes(b []byte) (map[uint16][]byte, error) {
	attrs := make(map[uint16][]byte)
	for len(b) != 0 {
		if len(b) < 4 {
			return nil, fmt.Errorf("short ip netlink attribute header")
		}
		n := int(binary.NativeEndian.Uint16(b[:2]))
		if n < 4 || n > len(b) {
			return nil, fmt.Errorf("invalid ip netlink attribute length")
		}
		kind := binary.NativeEndian.Uint16(b[2:4]) & 0x3fff
		attrs[kind] = b[4:n]
		aligned := (n + 3) &^ 3
		if aligned > len(b) {
			return nil, fmt.Errorf("short ip netlink attribute padding")
		}
		b = b[aligned:]
	}
	return attrs, nil
}

func ipUint32(attrs map[uint16][]byte, kind uint16, fallback uint32) (uint32, error) {
	b, ok := attrs[kind]
	if !ok {
		return fallback, nil
	}
	if len(b) != 4 {
		return 0, fmt.Errorf("invalid ip netlink u32 attribute %d", kind)
	}
	return binary.NativeEndian.Uint32(b), nil
}

func ipRuleLine(data []byte) (string, error) {
	if len(data) < 12 {
		return "", fmt.Errorf("short ip rule response")
	}
	if data[0] != syscall.AF_INET {
		return "", nil
	}
	a, err := ipAttributes(data[12:])
	if err != nil {
		return "", err
	}
	pref, err := ipUint32(a, ipFRAPriority, 0)
	if err != nil {
		return "", err
	}
	table, err := ipUint32(a, ipFRATable, uint32(data[4]))
	if err != nil {
		return "", err
	}
	mark, err := ipUint32(a, ipFRAFWMark, 0)
	if err != nil {
		return "", err
	}
	mask, err := ipUint32(a, ipFRAFWMask, 0xffffffff)
	if err != nil {
		return "", err
	}
	from := "all"
	if data[2] != 0 {
		if len(a[2]) != 4 || data[2] > 32 {
			return "", fmt.Errorf("invalid IPv4 rule source")
		}
		from = fmt.Sprintf("%s/%d", net.IP(a[2]), data[2])
	}
	line := fmt.Sprintf("%d: from %s", pref, from)
	if data[1] != 0 {
		if len(a[1]) != 4 || data[1] > 32 {
			return "", fmt.Errorf("invalid IPv4 rule destination")
		}
		line += fmt.Sprintf(" to %s/%d", net.IP(a[1]), data[1])
	}
	if _, ok := a[ipFRAFWMark]; ok {
		line += fmt.Sprintf(" fwmark 0x%x/0x%x", mark, mask)
	}
	if data[7] == 1 {
		line += fmt.Sprintf(" lookup %d", table)
	} else {
		line += fmt.Sprintf(" action %d", data[7])
	}
	return line, nil
}

func ipRouteLine(data []byte, wantedTable uint32) (string, error) {
	if len(data) < 12 {
		return "", fmt.Errorf("short ip route response")
	}
	if data[0] != syscall.AF_INET {
		return "", nil
	}
	a, err := ipAttributes(data[12:])
	if err != nil {
		return "", err
	}
	table, err := ipUint32(a, ipRTATable, uint32(data[4]))
	if err != nil || table != wantedTable {
		return "", err
	}
	line := ""
	if data[7] == 6 {
		line = "blackhole "
	} else if data[7] != 1 {
		line = fmt.Sprintf("type %d ", data[7])
	}
	if data[1] == 0 {
		line += "default"
	} else {
		if len(a[1]) != 4 || data[1] > 32 {
			return "", fmt.Errorf("invalid IPv4 route destination")
		}
		line += fmt.Sprintf("%s/%d", net.IP(a[1]), data[1])
	}
	if gateway, ok := a[5]; ok {
		if len(gateway) != 4 {
			return "", fmt.Errorf("invalid IPv4 route gateway")
		}
		line += " via " + net.IP(gateway).String()
	}
	ifindex, err := ipUint32(a, ipRTAOIF, 0)
	if err != nil {
		return "", err
	}
	if ifindex != 0 {
		name := fmt.Sprintf("if%d", ifindex)
		if iface, err := net.InterfaceByIndex(int(ifindex)); err == nil {
			name = iface.Name
		}
		line += " dev " + name
	}
	metric, err := ipUint32(a, ipRTAPriority, 0)
	if err != nil {
		return "", err
	}
	if _, ok := a[ipRTAPriority]; ok {
		line += fmt.Sprintf(" metric %d", metric)
	}
	return line, nil
}

func runIP(args []string) error {
	c, err := parseIPCommand(args)
	if err != nil {
		return err
	}
	if c.operation == "cache-flush" {
		return nil
	}
	ifindex := 0
	if c.device != "" {
		iface, err := net.InterfaceByName(c.device)
		if err != nil {
			return fmt.Errorf("ip interface %q: %w", c.device, err)
		}
		if c.operation == "link-show" {
			_, err = fmt.Fprintf(os.Stdout, "%d: %s: <%s> mtu %d\n", iface.Index, iface.Name, iface.Flags, iface.MTU)
			return err
		}
		ifindex = iface.Index
	}
	kind, flags, payload, err := ipBuildRequest(c, ifindex)
	if err != nil {
		return err
	}
	messages, err := ipExchange(kind, flags, payload)
	if err != nil {
		return fmt.Errorf("ip %s: %w", c.operation, err)
	}
	for _, m := range messages {
		var line string
		if c.operation == "rule-show" && m.Header.Type == syscall.RTM_NEWRULE {
			line, err = ipRuleLine(m.Data)
		} else if c.operation == "route-show" && m.Header.Type == syscall.RTM_NEWROUTE {
			line, err = ipRouteLine(m.Data, c.table)
		}
		if err != nil {
			return err
		}
		if line != "" {
			if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
				return err
			}
		}
	}
	return nil
}
