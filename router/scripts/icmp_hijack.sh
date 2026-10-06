#!/bin/sh
# IPv4 LAN ICMP -> TUN. Never flush shared firewall tables or routing tables.

case "$0" in
    /koolshare/*)
        ROOT=/koolshare/icmp_hijack
        CONFIG=/koolshare/configs/icmp_hijack.json
        SOFTCENTER=1
        ;;
    *)
        ROOT=/jffs/icmp_hijack
        CONFIG=$ROOT/config.json
        SOFTCENTER=0
        ;;
esac
SELF=$ROOT/icmp_hijack.sh
BIN=$ROOT/bin/icmptunnel
# shellcheck disable=SC1091
. "$ROOT/commands.sh"
setup_commands "$BIN" || exit 1
RUN=/tmp/icmp_hijack
TUN=icmptun0
TABLE=18888
PREF=100
MARK=0x40000000/0x40000000
CHAIN=ICMP_HIJACK
umask 077

log() {
    printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >> "$RUN/control.log"
    logger -t icmp_hijack "$*"
    if [ "$SOFTCENTER" = 1 ]; then
        dbus set "icmp_hijack_status=$*"
    fi
}

enabled() {
    if [ "$SOFTCENTER" = 1 ]; then
        [ "$(dbus get icmp_hijack_enable)" = 1 ]
    else
        [ -f "$ROOT/enabled" ]
    fi
}

pid_alive() {
    [ -s "$1" ] || return 1
    read -r saved_pid < "$1"
    case "$saved_pid" in ''|*[!0-9]*) return 1;; esac
    kill -0 "$saved_pid" 2>/dev/null
}

kill_pidfile() {
    [ -s "$1" ] || return 0
    read -r saved_pid < "$1"
    case "$saved_pid" in ''|*[!0-9]*) rm -f "$1"; return 0;; esac
    # PID reuse must not terminate another service.
    if [ -r "/proc/$saved_pid/cmdline" ] &&
       tr '\000' ' ' < "/proc/$saved_pid/cmdline" | grep -q 'icmp_hijack\|icmptunnel'; then
        kill "$saved_pid" 2>/dev/null || :
    fi
    rm -f "$1"
}

lock() {
    count=0
    until mkdir "$RUN/lock" 2>/dev/null; do
        count=$((count + 1))
        [ "$count" -lt 15 ] || { log '控制脚本忙，请稍后重试'; return 1; }
        sleep 1
    done
    trap 'rmdir "$RUN/lock" 2>/dev/null' EXIT
    trap 'exit 1' HUP INT TERM
}

chain_new() {
    "$1" -t "$2" -N "$CHAIN" 2>/dev/null || :
    "$1" -t "$2" -F "$CHAIN"
}

jump_add() {
    tool=$1; table=$2; parent=$3
    while "$tool" -t "$table" -D "$parent" -j "$CHAIN" 2>/dev/null; do :; done
    "$tool" -t "$table" -I "$parent" 1 -j "$CHAIN"
}

chain_remove() {
    tool=$1; table=$2; parent=$3
    target=${4:-$CHAIN}
    while "$tool" -t "$table" -D "$parent" -j "$target" 2>/dev/null; do :; done
    "$tool" -t "$table" -F "$target" 2>/dev/null || :
    "$tool" -t "$table" -X "$target" 2>/dev/null || :
}

guard_add() {
    guard_tool=$1; guard_protocol=$2
    "$guard_tool" -t filter -N "${CHAIN}_GUARD" 2>/dev/null || :
    # Never flush a live guard: failed reloads must remain closed.
    "$guard_tool" -t filter -A "${CHAIN}_GUARD" -i 'br+' -p "$guard_protocol" -j DROP || return 1
    "$guard_tool" -t filter -I FORWARD 1 -j "${CHAIN}_GUARD"
}

routes() {
    # A blackhole route survives daemon exits and interface deletion.
    ip route replace blackhole default table "$TABLE" metric 32767 || return 1
    existing=$(ip rule show | awk -v p="$PREF:" '$1 == p { print }')
    if [ -n "$existing" ] && printf '%s\n' "$existing" | grep -v 'fwmark 0x40000000/0x40000000.*lookup 18888' >/dev/null; then
        log "策略优先级 $PREF 已被其他功能使用；保持 ICMP 丢弃"
        return 1
    fi
    [ -n "$existing" ] || ip rule add pref "$PREF" fwmark "$MARK" table "$TABLE" || return 1
    if ip link show dev "$TUN" >/dev/null 2>&1; then
        ip route replace default dev "$TUN" table "$TABLE" metric 10 || return 1
    fi
    ip route flush cache 2>/dev/null || :
}

firewall() {
    # Install the direct-WAN guard first. It checks ingress, so missing MARK/rule
    # does not silently release intercepted ICMP to a real uplink.
    guard_add iptables icmp || return 1
    if has_command ip6tables; then
        guard_add ip6tables ipv6-icmp || return 1
    fi
    chain_new iptables filter || return 1
    iptables -t filter -A "$CHAIN" -i 'br+' -o "$TUN" -p icmp -j ACCEPT || return 1
    iptables -t filter -A "$CHAIN" -i 'br+' -p icmp -j DROP || return 1
    iptables -t filter -A "$CHAIN" -i "$TUN" -p icmp -j ACCEPT || return 1
    jump_add iptables filter FORWARD || return 1

    # IPv6 is deliberately not tunneled. Block only routed ICMPv6; NDP and
    # pinging the router itself still use INPUT/OUTPUT normally.
    if has_command ip6tables; then
        chain_new ip6tables filter || return 1
        ip6tables -t filter -A "$CHAIN" -i 'br+' -p ipv6-icmp -j DROP || return 1
        jump_add ip6tables filter FORWARD || return 1
    fi

    # Avoid WAN conntrack/NAT state rewriting original LAN addresses or IDs.
    chain_new iptables raw || return 1
    iptables -t raw -A "$CHAIN" -i 'br+' -p icmp -m addrtype ! --dst-type LOCAL -j NOTRACK || return 1
    iptables -t raw -A "$CHAIN" -i "$TUN" -p icmp -j NOTRACK || return 1
    jump_add iptables raw PREROUTING || return 1

    chain_new iptables nat || return 1
    iptables -t nat -A "$CHAIN" -o "$TUN" -p icmp -j ACCEPT || return 1
    jump_add iptables nat POSTROUTING || return 1

    chain_new iptables mangle || return 1
    iptables -t mangle -A "$CHAIN" -i 'br+' -p icmp -m addrtype ! --dst-type LOCAL -j MARK --set-xmark "$MARK" || return 1
    # ACCEPT here ends this table traversal; later QoS/VPN restore-mark rules
    # must not replace the routing decision for intercepted ICMP.
    iptables -t mangle -A "$CHAIN" -i 'br+' -p icmp -m addrtype ! --dst-type LOCAL -j ACCEPT || return 1
    iptables -t mangle -A "$CHAIN" -i "$TUN" -p icmp -j MARK --set-xmark 0/0xffffffff || return 1
    iptables -t mangle -A "$CHAIN" -i "$TUN" -p icmp -j ACCEPT || return 1
    jump_add iptables mangle PREROUTING || return 1
    routes || return 1
    chain_remove iptables filter FORWARD "${CHAIN}_GUARD"
    if has_command ip6tables; then
        chain_remove ip6tables filter FORWARD "${CHAIN}_GUARD"
    fi
}

remove_rules() {
    # First remove packet diversion, while the guard is still installed.
    chain_remove iptables mangle PREROUTING
    while ip rule del pref "$PREF" fwmark "$MARK" table "$TABLE" 2>/dev/null; do :; done
    ip route del default dev "$TUN" table "$TABLE" metric 10 2>/dev/null || :
    ip route del blackhole default table "$TABLE" metric 32767 2>/dev/null || :
    chain_remove iptables raw PREROUTING
    chain_remove iptables nat POSTROUTING
    chain_remove iptables filter FORWARD
    chain_remove iptables filter FORWARD "${CHAIN}_GUARD"
    if has_command ip6tables; then
        chain_remove ip6tables filter FORWARD
        chain_remove ip6tables filter FORWARD "${CHAIN}_GUARD"
    fi
    ip route flush cache 2>/dev/null || :
}

validate_settings() {
    case "$server" in ''|*[!0-9.]*) log '服务器必须为 IPv4 地址'; return 1;; esac
    printf '%s\n' "$server" | awk -F. 'NF != 4 {exit 1} {for(i=1;i<=4;i++) if($i == "" || $i+0 > 255 || length($i)>3 || $i ~ /^0[0-9]+$/) exit 1}' || { log '服务器 IPv4 地址不合法'; return 1; }
    case "$port" in ''|*[!0-9]*) log '端口必须为 1-65535'; return 1;; esac
    if [ "${#port}" -gt 5 ] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        log '端口必须为 1-65535'; return 1
    fi
    case "$key" in *[!a-fA-F0-9]*) log '密钥必须为 64 位十六进制'; return 1;; esac
    [ "${#key}" = 64 ] || { log '密钥必须为 64 位十六进制'; return 1; }
}

write_config() {
    server=$(dbus get icmp_hijack_server)
    port=$(dbus get icmp_hijack_port)
    key=$(dbus get icmp_hijack_key)
    [ -n "$port" ] || port=39070
    validate_settings || return 1
    port=$(printf '%s\n' "$port" | awk '{printf "%d", $0}')
    # Validated numeric/hex fields need no JSON quoting helper or eval.
    printf '{"server":"%s","port":%s,"key":"%s","interface":"%s"}\n' "$server" "$port" "$key" "$TUN" > "$CONFIG.new" || return 1
    chmod 600 "$CONFIG.new"
    mv -f "$CONFIG.new" "$CONFIG"
}

stop_daemon() {
    previous_pid=
    [ ! -s "$RUN/supervisor.pid" ] || read -r previous_pid < "$RUN/supervisor.pid"
    kill_pidfile "$RUN/supervisor.pid"
    kill_pidfile "$RUN/daemon.pid"
    case "$previous_pid" in ''|*[!0-9]*) return 0;; esac
    count=0
    while kill -0 "$previous_pid" 2>/dev/null && [ "$count" -lt 5 ]; do
        sleep 1
        count=$((count + 1))
    done
    # The supervisor's signal trap has now finished removing old PID files.
}

supervise() {
    echo "$$" > "$RUN/supervisor.pid"
    trap 'kill_pidfile "$RUN/daemon.pid"; rm -f "$RUN/supervisor.pid"; exit 0' HUP INT TERM
    while enabled; do
        # Keep logs bounded across reconnects/crashes.
        if [ -f "$RUN/daemon.log" ] && [ "$(wc -c < "$RUN/daemon.log")" -gt 262144 ]; then
            tail -n 300 "$RUN/daemon.log" > "$RUN/daemon.log.new"
            mv -f "$RUN/daemon.log.new" "$RUN/daemon.log"
        fi
        "$BIN" router --config "$CONFIG" >> "$RUN/daemon.log" 2>&1 &
        child=$!
        echo "$child" > "$RUN/daemon.pid"
        i=0
        while kill -0 "$child" 2>/dev/null && [ "$i" -lt 10 ]; do
            if ip link show dev "$TUN" >/dev/null 2>&1; then
                ip route replace default dev "$TUN" table "$TABLE" metric 10
                break
            fi
            i=$((i + 1))
            sleep 1
        done
        wait "$child"
        rm -f "$RUN/daemon.pid"
        log '隧道进程退出，ICMP 继续丢弃，3 秒后重启'
        sleep 3
    done
    rm -f "$RUN/supervisor.pid"
}

start() {
    enabled || return 0
    firewall || { log '防火墙/策略路由配置失败，ICMP 保持丢弃；检查 control.log'; return 1; }
    if [ "$SOFTCENTER" = 1 ]; then
        write_config || return 1
    fi
    [ -r "$CONFIG" ] || { log "配置不存在：$CONFIG"; return 1; }
    [ -x "$BIN" ] || { log '可执行文件缺失，ICMP 保持丢弃'; return 1; }
    [ -c /dev/net/tun ] || modprobe tun 2>/dev/null || :
    [ -c /dev/net/tun ] || { log '内核 TUN 设备不可用，ICMP 保持丢弃'; return 1; }
    pid_alive "$RUN/supervisor.pid" && return 0
    "$SELF" supervise </dev/null >> "$RUN/control.log" 2>&1 &
    echo "$!" > "$RUN/supervisor.pid"
    log '劫持已开启；服务连接状态见 daemon.log，断线不会回落真实链路'
}

mkdir -p "$RUN"
case "${1:-}" in
    supervise) supervise; exit 0;;
    status)
        if enabled; then
            echo 'enabled: yes (IPv4 tunnel; forwarded ICMPv6 blocked)'
        else
            echo 'enabled: no'
        fi
        pid_alive "$RUN/supervisor.pid" && echo 'supervisor: running' || echo 'supervisor: stopped'
        pid_alive "$RUN/daemon.pid" && echo 'daemon: running' || echo 'daemon: stopped'
        ip rule show | grep '18888' || :
        ip route show table "$TABLE" 2>/dev/null || :
        tail -n 20 "$RUN/daemon.log" 2>/dev/null || :
        exit 0
        ;;
esac
lock || exit 1
case "${1:-}" in
    start) start;;
    restart|apply) stop_daemon; if enabled; then start; else remove_rules; log '已关闭劫持'; fi;;
    firewall|nat) if enabled; then firewall; else remove_rules; fi;;
    stop) stop_daemon; remove_rules; log '已关闭劫持';;
    shutdown) stop_daemon;;
    enable)
        if [ "$SOFTCENTER" = 1 ]; then dbus set icmp_hijack_enable=1; else touch "$ROOT/enabled"; fi
        start
        ;;
    disable)
        if [ "$SOFTCENTER" = 1 ]; then dbus set icmp_hijack_enable=0; else rm -f "$ROOT/enabled"; fi
        stop_daemon; remove_rules; log '已关闭劫持'
        ;;
    *) echo "Usage: $0 {start|restart|apply|firewall|nat|stop|shutdown|enable|disable|status}"; exit 1;;
esac
