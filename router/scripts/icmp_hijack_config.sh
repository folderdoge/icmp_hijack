#!/bin/sh
# Software Center RPC writes validated settings to dbus before this callback.
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH
# shellcheck disable=SC1091
. /koolshare/scripts/base.sh
http_response "$1"
/koolshare/icmp_hijack/icmp_hijack.sh apply
