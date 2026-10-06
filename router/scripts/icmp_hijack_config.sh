#!/bin/sh
# Software Center RPC writes validated settings to dbus before this callback.
# shellcheck disable=SC1091
. /koolshare/scripts/base.sh
http_response "$1"
/koolshare/icmp_hijack/icmp_hijack.sh apply
