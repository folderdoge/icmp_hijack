//go:build linux

package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestIPCommandSubset(t *testing.T) {
	valid := []string{
		"link show dev lo", "link show lo", "rule show",
		"rule add pref 100 fwmark 0x40000000/0x40000000 table 18888",
		"rule del pref 100 fwmark 0x40000000/0x40000000 table 18888",
		"route replace blackhole default table 18888 metric 32767",
		"route replace default dev icmptun0 table 18888 metric 10",
		"route del blackhole default table 18888 metric 32767",
		"route del default dev icmptun0 table 18888 metric 10",
		"route show table 18888", "route flush cache",
	}
	for _, s := range valid {
		if _, err := parseIPCommand(strings.Fields(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	invalid := []string{
		"", "route", "link show", "link set lo up", "rule del pref 100",
		"rule add pref 100 fwmark bad/0xffffffff table 18888",
		"rule add pref 100 fwmark 0x40000000 table 18888",
		"rule add pref -1 fwmark 1/1 table 18888",
		"rule add pref 100 fwmark 1/1 table 4294967296",
		"rule add pref 100 fwmark 1/1 table 0",
		"route flush table 18888", "route flush cache extra",
		"route del default table 18888", "route show table 0",
		"route replace default dev lo table 18888 metric 10 extra",
		"route replace default dev interface-too-long table 18888 metric 10",
	}
	for _, s := range invalid {
		if _, err := parseIPCommand(strings.Fields(s)); err == nil {
			t.Errorf("accepted unsupported command %q", s)
		}
	}
}

func TestIPRuleWireContract(t *testing.T) {
	for _, op := range []string{"add", "del"} {
		c, err := parseIPCommand(strings.Fields("rule " + op + " pref 100 fwmark 0x40000000/0x40000000 table 18888"))
		if err != nil {
			t.Fatal(err)
		}
		kind, flags, b, err := ipBuildRequest(c, 0)
		if err != nil {
			t.Fatal(err)
		}
		expectedKind := uint16(syscall.RTM_NEWRULE)
		if op == "del" {
			expectedKind = syscall.RTM_DELRULE
		}
		if kind != expectedKind || flags&syscall.NLM_F_ACK == 0 || b[0] != syscall.AF_INET || b[4] != 0 || b[7] != 1 {
			t.Fatalf("incorrect %s fib_rule header: kind=%d flags=%x bytes=%x", op, kind, flags, b[:12])
		}
		a, err := ipAttributes(b[12:])
		if err != nil {
			t.Fatal(err)
		}
		for attr, wanted := range map[uint16]uint32{6: 100, 10: 0x40000000, 16: 0x40000000, 15: 18888} {
			got, err := ipUint32(a, attr, 0)
			if err != nil || got != wanted {
				t.Fatalf("%s attr %d: got %x want %x (%v)", op, attr, got, wanted, err)
			}
		}
		line, err := ipRuleLine(b)
		if err != nil || line != "100: from all fwmark 0x40000000/0x40000000 lookup 18888" {
			t.Fatalf("incompatible rule output: %q (%v)", line, err)
		}
	}
}

func TestIPRouteWireContract(t *testing.T) {
	for _, s := range []string{
		"route replace blackhole default table 18888 metric 32767",
		"route del blackhole default table 18888 metric 32767",
		"route replace default dev tun0 table 18888 metric 10",
		"route del default dev tun0 table 18888 metric 10",
	} {
		c, err := parseIPCommand(strings.Fields(s))
		if err != nil {
			t.Fatal(err)
		}
		kind, flags, b, err := ipBuildRequest(c, 12345)
		if err != nil {
			t.Fatal(err)
		}
		if flags&syscall.NLM_F_ACK == 0 || b[0] != syscall.AF_INET || b[4] != 0 || b[1] != 0 {
			t.Fatalf("invalid rtmsg: %x", b[:12])
		}
		if c.operation == "route-del" && (kind != syscall.RTM_DELROUTE || b[5] != 0) {
			t.Fatalf("route deletion does not leave protocol unspecified: %x", b[:12])
		}
		a, err := ipAttributes(b[12:])
		if err != nil {
			t.Fatal(err)
		}
		table, _ := ipUint32(a, 15, 0)
		metric, _ := ipUint32(a, 6, 0)
		if table != 18888 || metric != c.metric {
			t.Fatalf("route missing exact table/metric: %v", a)
		}
		if c.blackhole {
			if b[7] != 6 || b[6] != 0 || a[4] != nil {
				t.Fatalf("incorrect blackhole type/scope/interface: %x", b)
			}
			line, err := ipRouteLine(b, 18888)
			if err != nil || line != "blackhole default metric 32767" {
				t.Fatalf("incorrect route output %q: %v", line, err)
			}
		} else {
			index, _ := ipUint32(a, 4, 0)
			if b[7] != 1 || b[6] != 253 || index != 12345 {
				t.Fatalf("route missing exact output interface/type/scope: %x", b)
			}
		}
		line, err := ipRouteLine(b, 18889)
		if err != nil || line != "" {
			t.Fatalf("route from another table shown: %q (%v)", line, err)
		}
	}
}

func TestIPMalformedNetlinkDump(t *testing.T) {
	for _, b := range [][]byte{{1}, {0, 0, 1, 0}, {3, 0, 1, 0}, {8, 0, 1, 0}, {5, 0, 1, 0, 9}} {
		if _, err := ipAttributes(b); err == nil {
			t.Fatalf("accepted malformed attributes %x", b)
		}
	}
	if _, err := ipRuleLine([]byte{2}); err == nil {
		t.Fatal("accepted short rule")
	}
	if _, err := ipRouteLine([]byte{2}, 18888); err == nil {
		t.Fatal("accepted short route")
	}
	b := make([]byte, 12)
	b[0] = syscall.AF_INET
	malformedU32 := []byte{8, 0, 15, 0, 0, 0, 0, 0}
	binary.NativeEndian.PutUint16(malformedU32[:2], 5)
	b = append(b, malformedU32...)
	if _, err := ipRuleLine(b); err == nil {
		t.Fatal("accepted short table attribute")
	}
	if _, err := ipRouteLine(b, 18888); err == nil {
		t.Fatal("accepted short route table attribute")
	}
}

// Opt-in real kernel tests run only in a fresh namespace; never alter host routes.
// ICMPTUNNEL_IP_NETNS_TEST=1 go test ./cmd/icmptunnel -run TestIPNetNS
func TestIPNetNSIntegration(t *testing.T) {
	if os.Getenv("ICMPTUNNEL_IP_NETNS_TEST") != "1" {
		t.Skip("set ICMPTUNNEL_IP_NETNS_TEST=1 for isolated root integration")
	}
	if os.Geteuid() != 0 {
		t.Fatal("namespace integration requires root")
	}
	ns, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("unshare", "--net", os.Args[0], "-test.run=^TestIPNetNSHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "ICMPTUNNEL_IP_NS_CHILD=1", "ICMPTUNNEL_IP_PARENT_NS="+ns)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated ip helper test: %v\n%s", err, b)
	} else {
		t.Log(string(b))
	}
}

func TestIPNetNSHelper(t *testing.T) {
	if os.Getenv("ICMPTUNNEL_IP_NS_CHILD") != "1" {
		t.Skip("invoked only by unshare integration")
	}
	ns, err := os.Readlink("/proc/self/ns/net")
	if err != nil || os.Getenv("ICMPTUNNEL_IP_PARENT_NS") == "" || ns == os.Getenv("ICMPTUNNEL_IP_PARENT_NS") {
		t.Fatalf("refusing to alter the host namespace: %q (%v)", ns, err)
	}
	reference := func(args ...string) string {
		t.Helper()
		b, err := exec.Command("ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("reference ip %v: %v\n%s", args, err, b)
		}
		return string(b)
	}
	run := func(s string) {
		t.Helper()
		if err := runIP(strings.Fields(s)); err != nil {
			t.Fatalf("fallback ip %s: %v", s, err)
		}
	}
	reference("link", "set", "lo", "up")
	run("link show dev lo")
	if err := runIP(strings.Fields("link show dev missing0")); err == nil {
		t.Fatal("missing interface returned success")
	}
	// Foreign rules at the same preference must survive exact deletion.
	foreignRules := []struct{ mark, table, line string }{
		{"0x20/0x20", "18888", "fwmark 0x20/0x20 lookup 18888"},
		{"0x40000000/0x40000000", "18889", "fwmark 0x40000000/0x40000000 lookup 18889"},
		{"0x40000000/0x60000000", "18888", "fwmark 0x40000000/0x60000000 lookup 18888"},
	}
	for _, foreign := range foreignRules {
		reference("rule", "add", "pref", "100", "fwmark", foreign.mark, "table", foreign.table)
	}
	run("rule add pref 100 fwmark 0x40000000/0x40000000 table 18888")
	if !strings.Contains(reference("rule", "show"), "fwmark 0x40000000/0x40000000 lookup 18888") {
		t.Fatal("reference ip cannot see the fallback rule")
	}
	run("rule show")
	run("rule del pref 100 fwmark 0x40000000/0x40000000 table 18888")
	rules := reference("rule", "show")
	if strings.Contains(rules, "fwmark 0x40000000/0x40000000 lookup 18888") {
		t.Fatalf("exact rule deletion left own rule: %s", rules)
	}
	for _, foreign := range foreignRules {
		if !strings.Contains(rules, foreign.line) {
			t.Fatalf("exact rule deletion damaged foreign rule %s: %s", foreign.line, rules)
		}
	}
	if err := runIP(strings.Fields("rule del pref 100 fwmark 0x40000000/0x40000000 table 18888")); err == nil {
		t.Fatal("deleting absent rule returned success")
	}
	run("route replace blackhole default table 18888 metric 32767")
	run("route replace default dev lo table 18888 metric 10")
	// Replacement must also work when the same route already exists.
	run("route replace default dev lo table 18888 metric 10")
	routes := reference("route", "show", "table", "18888")
	if !strings.Contains(routes, "blackhole default") || !strings.Contains(routes, "default dev lo") {
		t.Fatalf("missing fallback routes: %s", routes)
	}
	run("route show table 18888")
	reference("route", "add", "blackhole", "default", "table", "18888", "metric", "32768")
	run("route del default dev lo table 18888 metric 10")
	run("route del blackhole default table 18888 metric 32767")
	routes = reference("route", "show", "table", "18888")
	if !strings.Contains(routes, "metric 32768") || strings.Contains(routes, "metric 32767") || strings.Contains(routes, "dev lo") {
		t.Fatalf("exact route deletion damaged foreign metric or left own route: %s", routes)
	}
	run("route flush cache")
}
