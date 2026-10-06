#!/bin/sh
# Keep the caller's command overrides, then add firmware and Software Center paths.
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH

setup_commands() {
    ICMP_IP_BINARY=$1
    [ -x "$ICMP_IP_BINARY" ] || { echo "Tunnel binary is not executable: $ICMP_IP_BINARY" >&2; return 1; }
}

# The executable speaks rtnetlink directly for this plugin's IPv4 routing subset.
# No iproute2 command, BusyBox ip applet, Entware, or shared symlink is needed.
ip() {
    "$ICMP_IP_BINARY" ip "$@"
}
