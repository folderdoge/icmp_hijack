#!/usr/bin/env python3
"""Root-only Linux network namespace test; no Internet/router required.

PC -> router == encrypted TCP ==> server -> gateway -> target
Uses the actual router lifecycle script with isolated filesystem paths.
"""
import json
import os
import platform
from pathlib import Path
import shutil
import shlex
import subprocess
import tempfile
import time

PROJECT = Path(__file__).resolve().parents[1]
ARCH = "arm64" if platform.machine() in ("aarch64", "arm64") else "amd64"
BINARY = PROJECT / f"bin/linux-{ARCH}/icmptunnel"
HOST_IP = shutil.which("ip") or "/usr/sbin/ip"
ROUTER_COMMAND_ENV = None


def run(*args, ns=None, check=True):
    argv = [str(a) for a in args]
    command_env = ROUTER_COMMAND_ENV if ns and ns.endswith("-rt") and argv[0] == "sh" else None
    if command_env is not None and os.environ.get("ICMPTUNNEL_TEST_SHELL"):
        shell_prefix = [os.environ["ICMPTUNNEL_TEST_SHELL"]]
        if os.environ.get("ICMPTUNNEL_TEST_BUSYBOX") == "1":
            shell_prefix.append("ash")
        argv = shell_prefix + argv[1:]
    if ns:
        argv = [HOST_IP, "netns", "exec", ns] + argv
    return subprocess.run(argv, check=check, capture_output=True, text=True, env=command_env)


def wait_for(predicate, message, seconds=15):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.2)
    raise AssertionError(message)


def main():
    global ROUTER_COMMAND_ENV
    if os.geteuid() != 0:
        raise SystemExit("Run as root on Linux with ip/iptables/ping/traceroute installed")
    if not BINARY.exists():
        raise SystemExit(f"Build {BINARY} first")
    names = {x: f"iht-{os.getpid()}-{x}" for x in ("pc", "rt", "sv", "gw", "dst")}
    created = []
    server = None
    with tempfile.TemporaryDirectory(prefix="iht-integration-") as temporary:
        temp = Path(temporary)
        home = temp / "router-home"
        runtime = temp / "router-run"
        home.mkdir()
        (home / "bin").mkdir()
        # Only the router entrypoints see this failing external-ip command. The
        # harness still uses real iproute2 to create and inspect the namespaces.
        trap_dir = temp / "missing-ip"
        trap_dir.mkdir()
        trap_calls = temp / "external-ip-calls"
        (trap_dir / "ip").write_text("#!/bin/sh\nprintf 'external ip called\\n' >> " + shlex.quote(str(trap_calls)) + "\nexit 127\n")
        (trap_dir / "ip").chmod(0o755)
        ROUTER_COMMAND_ENV = dict(os.environ, PATH=str(trap_dir) + ":" + os.environ["PATH"])
        router_binary = Path(os.environ.get("IHT_TEST_ROUTER_BINARY", str(BINARY)))
        emulator = os.environ.get("IHT_TEST_EMULATOR")
        if emulator:
            shutil.copyfile(router_binary, home / "bin/icmptunnel.bin")
            (home / "bin/icmptunnel.bin").chmod(0o755)
            routing_binary = os.environ.get("IHT_TEST_ROUTING_BINARY")
            wrapper = "#!/bin/sh\n"
            if routing_binary:
                # QEMU user 8.2 cannot translate RTM_*RULE messages. An explicit
                # native helper lets the ARM daemon exercise the remaining real
                # packet path; it is test-only and never included in a plugin.
                wrapper += 'if [ "$1" = ip ]; then exec ' + shlex.quote(str(Path(routing_binary).resolve())) + ' "$@"; fi\n'
            wrapper += "exec " + shlex.quote(emulator) + " " + shlex.quote(str(home / "bin/icmptunnel.bin")) + ' "$@"\n'
            (home / "bin/icmptunnel").write_text(wrapper)
        else:
            shutil.copyfile(router_binary, home / "bin/icmptunnel")
        (home / "bin/icmptunnel").chmod(0o755)
        control = home / "icmp_hijack.sh"
        script = (PROJECT / "router/scripts/icmp_hijack.sh").read_text()
        script = script.replace("/jffs/icmp_hijack", str(home)).replace("/tmp/icmp_hijack", str(runtime))
        if os.environ.get("ICMPTUNNEL_TEST_BUSYBOX") == "1":
            script = script.replace("#!/bin/sh", "#!" + os.environ["ICMPTUNNEL_TEST_SHELL"] + " ash", 1)
        control.write_text(script)
        control.chmod(0o755)
        shutil.copyfile(PROJECT / "router/scripts/commands.sh", home / "commands.sh")
        key = "ab" * 32
        (home / "config.json").write_text(json.dumps({"server": "10.10.0.2", "port": 39070, "key": key, "interface": "icmptun0"}))
        (home / "enabled").touch()
        server_config = temp / "server.json"
        server_config.write_text(json.dumps({"listen": ":39070", "key": key, "public_ip": "10.10.0.2", "timeout_seconds": 5}))
        server_log = (temp / "server.log").open("w")
        try:
            for ns in names.values():
                run("ip", "netns", "add", ns)
                created.append(ns)
                run("ip", "link", "set", "lo", "up", ns=ns)

            def link(a, aname, aip, b, bname, bip, number):
                left, right = f"ix{os.getpid()}{number}a", f"ix{os.getpid()}{number}b"
                run("ip", "link", "add", left, "type", "veth", "peer", "name", right)
                run("ip", "link", "set", left, "netns", names[a])
                run("ip", "link", "set", right, "netns", names[b])
                for role, old, new, address in ((a, left, aname, aip), (b, right, bname, bip)):
                    run("ip", "link", "set", old, "name", new, ns=names[role])
                    run("ip", "addr", "add", address, "dev", new, ns=names[role])
                    run("ip", "link", "set", new, "up", ns=names[role])

            link("pc", "eth0", "192.168.50.100/24", "rt", "br0", "192.168.50.1/24", 1)
            link("rt", "wan0", "10.10.0.1/24", "sv", "front0", "10.10.0.2/24", 2)
            link("sv", "exit0", "10.20.0.1/24", "gw", "front0", "10.20.0.2/24", 3)
            link("gw", "exit0", "10.30.0.1/24", "dst", "eth0", "10.30.0.2/24", 4)
            run("ip", "addr", "add", "192.168.50.101/24", "dev", "eth0", ns=names["pc"])
            for role, gateway in (("pc", "192.168.50.1"), ("rt", "10.10.0.2"), ("sv", "10.20.0.2"), ("dst", "10.30.0.1")):
                run("ip", "route", "add", "default", "via", gateway, ns=names[role])
            run("ip", "route", "add", "192.168.50.0/24", "via", "10.10.0.1", ns=names["sv"])
            run("ip", "route", "add", "10.10.0.0/24", "via", "10.20.0.1", ns=names["gw"])
            run("ip", "route", "add", "192.168.50.0/24", "via", "10.20.0.1", ns=names["gw"])
            for role in ("rt", "sv", "gw"):
                run("sysctl", "-qw", "net.ipv4.ip_forward=1", ns=names[role])
                run("sysctl", "-qw", "net.ipv6.conf.all.forwarding=1", ns=names[role])
            for role, interface, address in (("pc", "eth0", "fd00:50::100/64"), ("rt", "br0", "fd00:50::1/64"), ("rt", "wan0", "fd00:10::1/64"), ("sv", "front0", "fd00:10::2/64"), ("sv", "exit0", "fd00:20::1/64"), ("gw", "front0", "fd00:20::2/64"), ("gw", "exit0", "fd00:30::1/64"), ("dst", "eth0", "fd00:30::2/64")):
                run("ip", "-6", "addr", "add", address, "dev", interface, "nodad", ns=names[role])
            for role, gateway in (("pc", "fd00:50::1"), ("rt", "fd00:10::2"), ("sv", "fd00:20::2"), ("dst", "fd00:30::1")):
                run("ip", "-6", "route", "add", "default", "via", gateway, ns=names[role])
            run("ip", "-6", "route", "add", "fd00:50::/64", "via", "fd00:10::1", ns=names["sv"])
            run("ip", "-6", "route", "add", "fd00:50::/64", "via", "fd00:20::1", ns=names["gw"])
            # Replicate Merlin's strict reverse path checks and WAN masquerade.
            run("sysctl", "-qw", "net.ipv4.conf.all.rp_filter=1", ns=names["rt"])
            run("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "192.168.50.0/24", "-j", "MASQUERADE", ns=names["rt"])
            # Account for any direct ICMP leaking onto the router uplink.
            run("iptables", "-A", "FORWARD", "-i", "br0", "-o", "wan0", "-p", "icmp", "-j", "ACCEPT", ns=names["rt"])

            def start_server():
                return subprocess.Popen(["ip", "netns", "exec", names["sv"], str(BINARY), "server", "--config", str(server_config)], stdout=server_log, stderr=server_log)

            server = start_server()
            run("sh", control, "start", ns=names["rt"])
            status_path = runtime / "status.json"
            def current_state():
                try: return json.loads(status_path.read_text()).get('state')
                except (FileNotFoundError, json.JSONDecodeError): return None
            wait_for(lambda: current_state() == 'connected' and "default dev icmptun0" in run("ip", "route", "show", "table", "18888", ns=names["rt"]).stdout, "Tunnel/route never became ready")
            assert run("ping", "-n", "-c", "1", "-W", "2", "192.168.50.1", ns=names["pc"]).returncode == 0
            trace = run("traceroute", "-n", "-I", "-q", "1", "-w", "2", "-m", "6", "10.30.0.2", ns=names["pc"]).stdout
            print(trace, end="")
            hops = [line.split()[1] for line in trace.splitlines()[1:]]
            assert hops == ["192.168.50.1", "10.10.0.2", "10.20.0.2", "10.30.0.2"], hops
            assert run("ping", "-n", "-c", "3", "-W", "2", "10.30.0.2", ns=names["pc"]).returncode == 0
            assert run("ping", "-n", "-c", "1", "-W", "2", "10.10.0.2", ns=names["pc"]).returncode == 0
            print("PASS: local router ping, real remote path, ping destination/server")

            assert status_path.stat().st_size <= 1024
            assert not (runtime / 'daemon.log').exists() and not (runtime / 'control.log').exists()
            print("PASS: only bounded current state retained; no router logs")

            procs = [subprocess.Popen(["ip", "netns", "exec", names["pc"], "ping", "-n", "-I", src, "-e", "4242", "-c", "3", "-W", "2", "10.30.0.2"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) for src in ("192.168.50.100", "192.168.50.101")]
            for proc in procs:
                output, errors = proc.communicate(timeout=10)
                assert proc.returncode == 0 and "0% packet loss" in output, output + errors
            print("PASS: two LAN addresses with identical echo identifier/sequence")

            assert run("ping", "-6", "-n", "-c", "1", "-W", "2", "fd00:50::1", ns=names["pc"]).returncode == 0
            assert run("ping", "-6", "-n", "-c", "1", "-W", "1", "fd00:30::2", ns=names["pc"], check=False).returncode != 0
            print("PASS: ICMPv6 forwarding blocked, local IPv6/NDP works")

            tcp_code = "import socket; s=socket.socket(); s.bind(('10.30.0.2',40001)); s.listen(1); c,_=s.accept(); c.sendall(c.recv(16)); c.close(); s.close()"
            tcp_server = subprocess.Popen(["ip", "netns", "exec", names["dst"], "python3", "-c", tcp_code])
            time.sleep(.2)
            tcp_client = "import socket; s=socket.create_connection(('10.30.0.2',40001),2); s.sendall(b'OK'); print(s.recv(16).decode()); s.close()"
            assert run("python3", "-c", tcp_client, ns=names["pc"]).stdout.strip() == "OK"
            tcp_server.wait(timeout=3)
            print("PASS: ordinary TCP continues on original route")

            # Non-Echo LAN ICMP must also traverse TCP to the server, rather than
            # disappearing in the router or leaking onto its original uplink.
            run("iptables", "-A", "INPUT", "-s", "10.20.0.1", "-p", "icmp", "--icmp-type", "timestamp-request", "-j", "ACCEPT", ns=names["dst"])
            timestamp_code = "import socket,struct,time; b=struct.pack('!BBHHHIII',13,0,0,123,1,0,0,0); n=sum(struct.unpack('!10H',b)); n=(n&65535)+(n>>16); n=(n&65535)+(n>>16); b=b[:2]+struct.pack('!H',(~n)&65535)+b[4:]; s=socket.socket(socket.AF_INET,socket.SOCK_RAW,socket.IPPROTO_ICMP); s.sendto(b,('10.30.0.2',0)); time.sleep(.2)"
            run("python3", "-c", timestamp_code, ns=names["pc"])
            non_echo_counter = run("iptables", "-nvxL", "INPUT", ns=names["dst"]).stdout
            timestamp_line = next(line for line in non_echo_counter.splitlines() if "10.20.0.1" in line)
            assert int(timestamp_line.split()[0]) == 1, non_echo_counter
            print("PASS: non-Echo ICMP forwarded from server through TCP")

            run("ip", "link", "set", "exit0", "mtu", "1280", ns=names["sv"])
            mtu_result = run("ping", "-n", "-M", "do", "-s", "1300", "-c", "1", "-W", "2", "10.30.0.2", ns=names["pc"], check=False)
            assert "1280" in mtu_result.stdout and "Frag needed" in mtu_result.stdout, mtu_result.stdout + mtu_result.stderr
            run("ip", "link", "set", "exit0", "mtu", "1500", ns=names["sv"])
            print("PASS: server exit MTU => fragmentation-needed with real MTU")

            # Firewall hook is expected to remain idempotent and preserve traffic.
            run("sh", control, "firewall", ns=names["rt"])
            assert run("ping", "-n", "-c", "1", "-W", "2", "10.30.0.2", ns=names["pc"]).returncode == 0
            print("PASS: firewall refresh")

            server.terminate()
            server.wait(timeout=5)
            assert run("ping", "-n", "-c", "1", "-W", "1", "10.30.0.2", ns=names["pc"], check=False).returncode != 0
            print("PASS: server disconnected => drop")
            wait_for(lambda: current_state() == 'connect_error', "Disconnect current state was not updated")
            invalid_config = json.loads(server_config.read_text())
            invalid_config["key"] = "cd" * 32
            server_config.write_text(json.dumps(invalid_config))
            server = start_server()
            wait_for(lambda: current_state() == 'auth_error', "Incorrect key was not rejected")
            assert run("ping", "-n", "-c", "1", "-W", "1", "10.30.0.2", ns=names["pc"], check=False).returncode != 0
            server.terminate()
            server.wait(timeout=5)
            invalid_config["key"] = key
            server_config.write_text(json.dumps(invalid_config))
            print("PASS: incorrect shared key => authentication failure and drop")
            server = start_server()
            wait_for(lambda: current_state() == 'connected', "Reconnect failed", 20)
            assert run("ping", "-n", "-c", "1", "-W", "2", "10.30.0.2", ns=names["pc"]).returncode == 0
            print("PASS: reconnect")

            # Remove mark routing while connected: the independent forward guard
            # must still prevent packets from taking the otherwise working WAN.
            run("ip", "rule", "del", "pref", "100", "fwmark", "0x40000000/0x40000000", "table", "18888", ns=names["rt"])
            assert run("ping", "-n", "-c", "1", "-W", "1", "10.30.0.2", ns=names["pc"], check=False).returncode != 0
            counters = run("iptables", "-nvxL", "FORWARD", ns=names["rt"]).stdout
            uplink_line = next(line for line in counters.splitlines() if "br0" in line and "wan0" in line)
            assert int(uplink_line.split()[0]) == 0, counters
            print("PASS: missing policy route => drop; zero direct-WAN ICMP")
            run("sh", control, "firewall", ns=names["rt"])

            # Crash the daemon. TUN vanishes, independent blackhole survives.
            pid = int((runtime / "daemon.pid").read_text())
            os.kill(pid, 9)
            assert run("ping", "-n", "-c", "1", "-W", "1", "10.30.0.2", ns=names["pc"], check=False).returncode != 0
            wait_for(lambda: (runtime / "daemon.pid").exists() and int((runtime / "daemon.pid").read_text()) != pid, "Supervisor did not restart")
            wait_for(lambda: run("ping", "-n", "-c", "1", "-W", "1", "10.30.0.2", ns=names["pc"], check=False).returncode == 0, "Restart did not restore tunnel")
            print("PASS: daemon crash => drop, supervisor restarts")
            run("sh", control, "disable", ns=names["rt"])
            assert current_state() == 'disabled'
            assert not (runtime / 'daemon.log').exists() and not (runtime / 'control.log').exists()
            assert "ICMP_HIJACK" not in run("iptables-save", ns=names["rt"]).stdout
            assert "ICMP_HIJACK" not in run("ip6tables-save", ns=names["rt"]).stdout
            assert "18888" not in run("ip", "rule", "show", ns=names["rt"]).stdout
            assert not run("ip", "route", "show", "table", "18888", ns=names["rt"], check=False).stdout.strip()
            assert run("ip", "link", "show", "icmptun0", ns=names["rt"], check=False).returncode != 0
            assert run("ping", "-6", "-n", "-c", "1", "-W", "2", "fd00:30::2", ns=names["pc"]).returncode == 0
            assert not trap_calls.exists(), "router invoked an external ip command"
            print("PASS: disable removes own rules/routes/TUN")
            print("PASS: external ip unavailable; all routing handled by bundled executable")
        except BaseException as failure:
            if isinstance(failure, subprocess.CalledProcessError):
                print("FAILED COMMAND STDOUT:\n" + (failure.stdout or ""))
                print("FAILED COMMAND STDERR:\n" + (failure.stderr or ""))
            print("SERVER LOG:\n" + (temp / "server.log").read_text())
            for path in (runtime / "status.json",):
                if path.exists():
                    print(str(path) + ":\n" + path.read_text())
            if names["rt"] in created:
                for args in (("ip", "-s", "link", "show", "icmptun0"), ("ip", "addr", "show"), ("sysctl", "net.ipv4.conf.icmptun0.rp_filter", "net.ipv4.conf.all.rp_filter"), ("ip", "route", "show", "table", "18888"), ("ip", "rule", "show"), ("iptables-save", "-c")):
                    print(" ".join(args) + ":\n" + run(*args, ns=names["rt"], check=False).stdout)
                print(run("sysctl", "net.ipv4.conf.all.src_valid_mark", "net.ipv4.conf.br0.src_valid_mark", "net.ipv4.conf.br0.rp_filter", "net.ipv4.ip_forward", ns=names["rt"], check=False).stdout)
                print("reverse filter: " + run("nstat", "-az", "TcpExtIPReversePathFilter", ns=names["rt"], check=False).stdout)
            raise
        finally:
            if names["rt"] in created:
                run("sh", control, "disable", ns=names["rt"], check=False)
            if server and server.poll() is None:
                server.terminate()
                server.wait(timeout=5)
            server_log.close()
            for ns in reversed(created):
                for pid in run("ip", "netns", "pids", ns, check=False).stdout.split():
                    try:
                        os.kill(int(pid), 9)
                    except ProcessLookupError:
                        pass
                run("ip", "netns", "delete", ns, check=False)


if __name__ == "__main__":
    main()
