#!/usr/bin/env bash
set -euo pipefail

SERVICE=icmptunnel-server.service
UNIT_FILE=/etc/systemd/system/icmptunnel-server.service
CONFIG_FILE=/etc/icmptunnel/server.json
BINARY=/usr/local/bin/icmptunnel
UNINSTALL_SCRIPT=/usr/local/lib/icmptunnel/uninstall.sh

if [[ $# -gt 0 ]]; then
    case "$1" in
        --help|-h)
            printf 'Usage: sudo bash server/uninstall.sh\nStops the service and removes its unit, binary and config.\n'
            exit 0 ;;
        *) printf 'Unknown option: %s\n' "$1" >&2; exit 1 ;;
    esac
fi
[[ $EUID -eq 0 ]] || { printf 'Run this uninstaller as root.\n' >&2; exit 1; }
command -v systemctl >/dev/null || { printf 'systemctl is required.\n' >&2; exit 1; }

if [[ -f $UNIT_FILE ]]; then
    systemctl disable --now "$SERVICE"
fi
rm -f -- "$UNIT_FILE" "$BINARY" "$CONFIG_FILE" "$UNINSTALL_SCRIPT"
# Leave any files the user added to this directory untouched.
rmdir -- /etc/icmptunnel 2>/dev/null || true
rmdir -- /usr/local/lib/icmptunnel 2>/dev/null || true
systemctl daemon-reload
systemctl reset-failed "$SERVICE" >/dev/null 2>&1 || true
printf 'ICMP tunnel server removed.\n'
