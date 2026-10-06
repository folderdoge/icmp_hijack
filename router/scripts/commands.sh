#!/bin/sh
# Keep the caller's command overrides, then add firmware and Software Center paths.
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH

# Merlin's BusyBox ash can omit the command builtin. Check executable files
# directly; this helper is intentionally for external utilities, not builtins.
has_command() {
    case "$1" in
        */*) [ -f "$1" ] && [ -x "$1" ]; return $?;;
    esac
    icmp_lookup_path=${PATH}:
    while [ -n "$icmp_lookup_path" ]; do
        icmp_lookup_dir=${icmp_lookup_path%%:*}
        icmp_lookup_path=${icmp_lookup_path#*:}
        [ -n "$icmp_lookup_dir" ] || icmp_lookup_dir=.
        if [ -f "$icmp_lookup_dir/$1" ] && [ -x "$icmp_lookup_dir/$1" ]; then
            return 0
        fi
    done
    return 1
}

setup_commands() {
    ICMP_IP_BINARY=$1
    [ -x "$ICMP_IP_BINARY" ] || { echo "Tunnel binary is not executable: $ICMP_IP_BINARY" >&2; return 1; }
}

# The executable speaks rtnetlink directly for this plugin's IPv4 routing subset.
# No iproute2 command, BusyBox ip applet, Entware, or shared symlink is needed.
ip() {
    "$ICMP_IP_BINARY" ip "$@"
}
