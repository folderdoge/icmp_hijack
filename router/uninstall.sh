#!/bin/sh
set -eu
# Some ASUS firmware omits id. Read the effective UID with shell builtins.
ROOT_UID=
while read -r status_field _real_uid effective_uid _rest; do
    case "$status_field" in Uid:) ROOT_UID=$effective_uid; break;; esac
done < /proc/self/status
[ "$ROOT_UID" = 0 ] || { echo 'Run as root.' >&2; exit 1; }
if [ -d /koolshare/icmp_hijack ]; then
    ROOT=/koolshare/icmp_hijack
    CONFIG=/koolshare/configs/icmp_hijack.json
    SOFTCENTER=1
else
    ROOT=/jffs/icmp_hijack
    CONFIG=$ROOT/config.json
    SOFTCENTER=0
fi
if [ -x "$ROOT/icmp_hijack.sh" ]; then
    "$ROOT/icmp_hijack.sh" disable
fi
if [ -r "$ROOT/hooks.sh" ]; then
    # shellcheck disable=SC1091
    . "$ROOT/hooks.sh"
    hook_remove
fi
if [ "$SOFTCENTER" = 1 ]; then
    # Remove only this module's keys and exact installed paths.
    dbus list icmp_hijack_ | while IFS='=' read -r name _value; do
        case "$name" in icmp_hijack_*) dbus remove "$name";; esac
    done
    dbus list softcenter_module_icmp_hijack_ | while IFS='=' read -r name _value; do
        case "$name" in softcenter_module_icmp_hijack_*) dbus remove "$name";; esac
    done
    rm -f /koolshare/scripts/icmp_hijack_config.sh /koolshare/scripts/uninstall_icmp_hijack.sh
    rm -f /koolshare/webs/Module_icmp_hijack.asp /koolshare/res/icon-icmp_hijack.png
fi
rm -f "$CONFIG" "$CONFIG.new"
# Fixed, module-owned directories; no wildcard deletion of shared paths.
case "$ROOT" in /koolshare/icmp_hijack|/jffs/icmp_hijack) rm -rf "$ROOT";; esac
rm -rf /tmp/icmp_hijack
echo '卸载完成：已移除进程、配置、策略路由、防火墙链和自己的事件钩子。'
