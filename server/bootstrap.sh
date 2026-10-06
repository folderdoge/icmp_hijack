#!/usr/bin/env bash
set -euo pipefail

# Download private release assets, then hand off to the bundled installer.
REPOSITORY=folderdoge/icmp_hijack
API=https://api.github.com/repos/$REPOSITORY
TAG=
INSTALL_ARGS=()

usage() {
    cat <<'EOF'
Usage: GH_TOKEN=<GitHub token> sudo -E bash server/bootstrap.sh [options]

  --version TAG       Install this release (e.g. v1.0.2); default: latest.
  --tag TAG           Alias for --version.
  --public-ip IP      Public/NAT IPv4 shown as the second traceroute hop.
  --port PORT         TCP listen port (default: 39070).
  --key HEX           Optional 64-character hexadecimal shared key.
  --timeout SECONDS   Probe timeout, 1-60 seconds (default: 10).
  --help              Show this help.

Requires Linux, root, systemd, curl, jq, tar and sha256sum. The private repository
requires GH_TOKEN (or GITHUB_TOKEN) with Contents: read for folderdoge/icmp_hijack.
Installer options are forwarded unchanged. Updates retain unspecified settings.
EOF
}

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version|--tag)
            [[ $# -ge 2 && -n $2 ]] || die "$1 needs a release tag"
            TAG=$2
            shift 2
            ;;
        --help|-h) usage; exit 0 ;;
        *) INSTALL_ARGS+=("$1"); shift ;;
    esac
done

[[ $EUID -eq 0 ]] || die 'Run this bootstrap as root'
[[ $(uname -s) == Linux ]] || die 'Linux is required'
[[ -d /run/systemd/system ]] || die 'A running systemd service manager is required'
for command in curl jq tar sha256sum; do
    command -v "$command" >/dev/null || die "$command is required (Debian/Ubuntu: apt-get install curl jq)"
done
if [[ -n $TAG ]]; then
    [[ $TAG =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || die 'Release tag must look like v1.0.2'
fi

TOKEN=${GH_TOKEN:-${GITHUB_TOKEN:-}}
[[ -n $TOKEN ]] || die 'Set GH_TOKEN or GITHUB_TOKEN to read the private GitHub repository'
[[ $TOKEN != *$'\r'* && $TOKEN != *$'\n'* ]] || die 'Invalid GitHub token'

# Header input keeps the token out of curl's command-line arguments. curl strips
# Authorization on redirects to other hosts; never use --location-trusted.
download() {
    local accept=$1 url=$2 output=$3
    printf 'Authorization: Bearer %s\n' "$TOKEN" |
        curl --fail --silent --show-error --location \
            --proto '=https' --proto-redir '=https' \
            --connect-timeout 15 --max-time 300 --retry 2 \
            --header @- --header "Accept: $accept" \
            --header 'X-GitHub-Api-Version: 2022-11-28' \
            --output "$output" "$url"
}

umask 077
WORK_DIR=$(mktemp -d)
trap 'rm -rf -- "$WORK_DIR"' EXIT
if [[ -n $TAG ]]; then
    RELEASE_URL=$API/releases/tags/$TAG
else
    RELEASE_URL=$API/releases/latest
fi
if ! download application/vnd.github+json "$RELEASE_URL" "$WORK_DIR/release.json"; then
    die 'Cannot read GitHub release; check the token, Contents: read permission and release tag'
fi
TAG=$(jq -er '.tag_name | select(type == "string")' "$WORK_DIR/release.json") || die 'Invalid release metadata'
[[ $TAG =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "Unsupported release tag: $TAG"
BUNDLE_NAME=icmptunnel-server-${TAG#v}.tar.gz

asset_id() {
    jq -er --arg name "$1" '
        [.assets[] | select(.name == $name and .state == "uploaded") | .id]
        | if length == 1 and (.[0] | type == "number") then .[0]
          else error("Expected one uploaded asset: " + $name) end
    ' "$WORK_DIR/release.json"
}

BUNDLE_ID=$(asset_id "$BUNDLE_NAME") || die "Release is missing $BUNDLE_NAME"
CHECKSUM_ID=$(asset_id SHA256SUMS) || die 'Release is missing SHA256SUMS'
[[ $BUNDLE_ID =~ ^[0-9]+$ && $CHECKSUM_ID =~ ^[0-9]+$ ]] || die 'Invalid release asset IDs'
printf 'Downloading %s from %s (%s)...\n' "$BUNDLE_NAME" "$REPOSITORY" "$TAG"
download application/octet-stream "$API/releases/assets/$BUNDLE_ID" "$WORK_DIR/$BUNDLE_NAME"
download application/octet-stream "$API/releases/assets/$CHECKSUM_ID" "$WORK_DIR/SHA256SUMS"

EXPECTED=$(awk -v name="$BUNDLE_NAME" '$2 == name { print $1 }' "$WORK_DIR/SHA256SUMS")
[[ $EXPECTED =~ ^[[:xdigit:]]{64}$ ]] || die "Missing or invalid checksum for $BUNDLE_NAME"
if ! (cd -- "$WORK_DIR" && printf '%s  %s\n' "$EXPECTED" "$BUNDLE_NAME" | sha256sum --check --status); then
    die 'Release bundle SHA-256 verification failed'
fi
tar -xzf "$WORK_DIR/$BUNDLE_NAME" -C "$WORK_DIR"
INSTALLER=$WORK_DIR/icmptunnel-server/server/install.sh
[[ -f $INSTALLER ]] || die 'The downloaded bundle has no server/install.sh'

# Authentication is needed only for downloading, not for installation or service.
unset GH_TOKEN GITHUB_TOKEN TOKEN
bash "$INSTALLER" "${INSTALL_ARGS[@]}"
