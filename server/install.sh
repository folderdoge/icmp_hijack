#!/usr/bin/env bash
set -euo pipefail

# Run from the extracted release bundle; no download or build is performed here.
SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
BUNDLE_DIR=$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)
CONFIG_DIR=/etc/icmptunnel
CONFIG_FILE=$CONFIG_DIR/server.json
BINARY_TARGET=/usr/local/bin/icmptunnel
UNIT_FILE=/etc/systemd/system/icmptunnel-server.service
SERVICE=icmptunnel-server.service
SUPPORT_DIR=/usr/local/lib/icmptunnel
UNINSTALL_TARGET=$SUPPORT_DIR/uninstall.sh

PUBLIC_IP=
PORT=39070
KEY=
TIMEOUT=10
BINARY_SOURCE=
PORT_SET=0
TIMEOUT_SET=0

usage() {
    cat <<'EOF'
Usage: sudo bash server/install.sh --public-ip <server IPv4> [options]

  --public-ip IP       IPv4 shown as the tunnel's second traceroute hop.
                       Use the server's public/NAT IPv4, not a private NIC address.
  --port PORT          TCP listen port (default: 39070).
  --key HEX            Shared key: exactly 64 hexadecimal characters.
                       If omitted, keep the installed key or generate a new one.
  --timeout SECONDS    Probe timeout, 1-60 seconds (default: 10).
  --binary FILE        Use a binary from this path instead of the release bundle.
  --help               Show this help.

Re-running the installer updates the binary and preserves the existing public IP,
key, port and timeout unless you supply their options. No firewall rules are added.
EOF
}

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need_value() { [[ $# -ge 2 && -n $2 ]] || die "$1 needs a value"; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --public-ip) need_value "$@"; PUBLIC_IP=$2; shift 2 ;;
        --port) need_value "$@"; PORT=$2; PORT_SET=1; shift 2 ;;
        --key) need_value "$@"; KEY=$2; shift 2 ;;
        --timeout) need_value "$@"; TIMEOUT=$2; TIMEOUT_SET=1; shift 2 ;;
        --binary) need_value "$@"; BINARY_SOURCE=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) die "Unknown option: $1 (use --help)" ;;
    esac
done

# Only the four scalar fields written by this installer are read on updates.
if [[ -f $CONFIG_FILE ]]; then
    [[ -n $PUBLIC_IP ]] || PUBLIC_IP=$(sed -n 's/^[[:space:]]*"public_ip"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG_FILE")
    [[ -n $KEY ]] || KEY=$(sed -n 's/^[[:space:]]*"key"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$CONFIG_FILE")
    if [[ $PORT_SET -eq 0 ]]; then
        OLD_PORT=$(sed -n 's/^[[:space:]]*"listen"[[:space:]]*:[[:space:]]*":\([0-9]*\)".*/\1/p' "$CONFIG_FILE")
        [[ -z $OLD_PORT ]] || PORT=$OLD_PORT
    fi
    if [[ $TIMEOUT_SET -eq 0 ]]; then
        OLD_TIMEOUT=$(sed -n 's/^[[:space:]]*"timeout_seconds"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$CONFIG_FILE")
        [[ -z $OLD_TIMEOUT ]] || TIMEOUT=$OLD_TIMEOUT
    fi
fi

valid_ipv4() {
    local octet
    local -a octets
    [[ $1 =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]] || return 1
    IFS=. read -r -a octets <<< "$1"
    for octet in "${octets[@]}"; do
        [[ $octet == 0 || $octet != 0* ]] || return 1
        (( 10#$octet <= 255 )) || return 1
    done
    [[ $1 != 0.0.0.0 && $1 != 255.255.255.255 ]]
}

[[ -n $PUBLIC_IP ]] || die '--public-ip is required on the first installation'
valid_ipv4 "$PUBLIC_IP" || die 'Invalid --public-ip; use a dotted-decimal IPv4 address'
if ! [[ $PORT =~ ^[0-9]{1,5}$ ]] || ! (( 10#$PORT >= 1 && 10#$PORT <= 65535 )); then
    die 'Port must be 1-65535'
fi
if ! [[ $TIMEOUT =~ ^[0-9]{1,2}$ ]] || ! (( 10#$TIMEOUT >= 1 && 10#$TIMEOUT <= 60 )); then
    die 'Timeout must be 1-60 seconds'
fi
PORT=$((10#$PORT))
TIMEOUT=$((10#$TIMEOUT))
[[ -z $KEY || $KEY =~ ^[[:xdigit:]]{64}$ ]] || die 'Key must be exactly 64 hexadecimal characters'
[[ $EUID -eq 0 ]] || die 'Run this installer as root (sudo bash server/install.sh ...)'
[[ $(uname -s) == Linux ]] || die 'Linux is required'
[[ -d /run/systemd/system ]] || die 'A running systemd service manager is required'
command -v systemctl >/dev/null || die 'systemctl is required'

if [[ -z $BINARY_SOURCE ]]; then
    case "$(uname -m)" in
        x86_64|amd64) ARCH=amd64 ;;
        aarch64|arm64) ARCH=arm64 ;;
        *) die 'Supported server architectures: x86_64 and aarch64' ;;
    esac
    BINARY_SOURCE=$BUNDLE_DIR/bin/linux-$ARCH/icmptunnel
fi
[[ -f $BINARY_SOURCE ]] || die "Binary missing: $BINARY_SOURCE; extract the complete release bundle"
[[ -f $SCRIPT_DIR/uninstall.sh ]] || die "Uninstaller missing: $SCRIPT_DIR/uninstall.sh; extract the complete release bundle"
if [[ -e $BINARY_TARGET && ! -f $UNIT_FILE ]]; then
    die "$BINARY_TARGET already exists without our service; move it before installing"
fi

umask 077
if [[ -z $KEY ]]; then
    KEY=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
fi
KEY=${KEY,,}
STAGING_DIR=$(mktemp -d)
trap 'rm -rf -- "$STAGING_DIR"' EXIT

cat > "$STAGING_DIR/server.json" <<EOF
{
  "listen": ":$PORT",
  "key": "$KEY",
  "public_ip": "$PUBLIC_IP",
  "timeout_seconds": $TIMEOUT
}
EOF

cat > "$STAGING_DIR/icmptunnel-server.service" <<'EOF'
[Unit]
Description=ICMP tunnel remote endpoint
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/icmptunnel server --config /etc/icmptunnel/server.json
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

if [[ -f $UNIT_FILE ]]; then
    systemctl stop "$SERVICE"
fi
install -d -m 700 "$CONFIG_DIR"
install -m 755 "$BINARY_SOURCE" "$BINARY_TARGET"
install -m 600 "$STAGING_DIR/server.json" "$CONFIG_FILE"
install -m 644 "$STAGING_DIR/icmptunnel-server.service" "$UNIT_FILE"
install -d -m 755 "$SUPPORT_DIR"
install -m 755 "$SCRIPT_DIR/uninstall.sh" "$UNINSTALL_TARGET"
systemctl daemon-reload
systemctl reset-failed "$SERVICE" >/dev/null 2>&1 || true
systemctl enable --now "$SERVICE"

# Type=simple starts before the daemon has parsed its config and opened its socket.
sleep 1
if ! systemctl is-active --quiet "$SERVICE"; then
    journalctl -u "$SERVICE" -n 30 --no-pager >&2 || true
    die "Service did not start; inspect journalctl -u $SERVICE"
fi

printf '\nInstalled and running. Configure the router with:\n'
printf '  Server IPv4: %s\n  TCP port:    %s\n  Shared key:  %s\n' "$PUBLIC_IP" "$PORT" "$KEY"
printf '\nAllow TCP/%s in the server firewall and cloud security group.\n' "$PORT"
printf 'The tunnel itself uses its encrypted TCP protocol; no HTTP service is started.\n'
printf 'Status:    systemctl status %s\n' "$SERVICE"
printf 'Logs:      journalctl -u %s -f\n' "$SERVICE"
printf 'Uninstall: sudo bash %q\n' "$UNINSTALL_TARGET"
