#!/bin/sh
set -eu
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH
PACKAGE=$(CDPATH='' cd -P "$(dirname "$0")" && pwd)
VERSION=1.0.6
# shellcheck disable=SC1091
. "$PACKAGE/scripts/commands.sh"

# Some ASUS firmware omits id. Read the effective UID with shell builtins.
ROOT_UID=
while read -r status_field _real_uid effective_uid _rest; do
    case "$status_field" in Uid:) ROOT_UID=$effective_uid; break;; esac
done < /proc/self/status
[ "$ROOT_UID" = 0 ] || { echo 'Run as root.' >&2; exit 1; }
for tool in iptables nvram awk; do
    has_command "$tool" || { echo "Missing command: $tool (PATH=$PATH)" >&2; exit 1; }
done
[ "$(nvram get jffs2_scripts)" = 1 ] || {
    echo '请先在 系统管理 → 系统设置 中启用 JFFS 自定义脚本。' >&2
    exit 1
}
case "$(uname -m)" in
    armv7*|armv8l) ARCH=armv7;;
    aarch64|arm64) ARCH=armv8;;
    *) echo 'Only ARMv7/ARMv8 Asuswrt-Merlin is supported.' >&2; exit 1;;
esac
SOURCE=$PACKAGE/bin/icmptunnel-$ARCH
[ -f "$SOURCE" ] || { echo "Missing binary: $SOURCE" >&2; exit 1; }

if [ -f /koolshare/scripts/base.sh ] && has_command dbus; then
    ROOT=/koolshare/icmp_hijack
    CONFIG=/koolshare/configs/icmp_hijack.json
    SOFTCENTER=1
else
    ROOT=/jffs/icmp_hijack
    CONFIG=$ROOT/config.json
    SOFTCENTER=0
fi
if [ -x "$ROOT/icmp_hijack.sh" ]; then
    "$ROOT/icmp_hijack.sh" shutdown
fi
mkdir -p "$ROOT/bin" "$(dirname "$CONFIG")"
chmod 700 "$ROOT"
cp "$SOURCE" "$ROOT/bin/icmptunnel"
cp "$PACKAGE/scripts/icmp_hijack.sh" "$ROOT/icmp_hijack.sh"
cp "$PACKAGE/scripts/hooks.sh" "$ROOT/hooks.sh"
cp "$PACKAGE/scripts/commands.sh" "$ROOT/commands.sh"
cp "$PACKAGE/uninstall.sh" "$ROOT/uninstall.sh"
chmod 755 "$ROOT/bin/icmptunnel" "$ROOT/icmp_hijack.sh" "$ROOT/uninstall.sh"
chmod 644 "$ROOT/hooks.sh" "$ROOT/commands.sh"

# shellcheck disable=SC1091
. "$ROOT/hooks.sh"
if ! hooks_install "$ROOT/icmp_hijack.sh"; then
    echo 'Installing event hooks failed. Running uninstall to remove this installation.' >&2
    "$ROOT/uninstall.sh"
    exit 1
fi

if [ "$SOFTCENTER" = 1 ]; then
    mkdir -p /koolshare/webs /koolshare/res
    cp "$PACKAGE/scripts/icmp_hijack_config.sh" /koolshare/scripts/icmp_hijack_config.sh
    cp "$PACKAGE/webs/Module_icmphijack.asp" /koolshare/webs/Module_icmphijack.asp
    cp "$PACKAGE/res/icon-icmphijack.png" /koolshare/res/icon-icmphijack.png
    cp "$PACKAGE/uninstall.sh" /koolshare/scripts/uninstall_icmphijack.sh
    chmod 755 /koolshare/scripts/icmp_hijack_config.sh /koolshare/scripts/uninstall_icmphijack.sh
    dbus list softcenter_module_icmp_hijack_ | while IFS='=' read -r name _value; do
        case "$name" in softcenter_module_icmp_hijack_*) dbus remove "$name";; esac
    done
    rm -f /koolshare/webs/Module_icmp_hijack.asp /koolshare/res/icon-icmp_hijack.png /koolshare/scripts/uninstall_icmp_hijack.sh
    dbus set "softcenter_module_icmphijack_name=icmphijack"
    dbus set "softcenter_module_icmphijack_title=ICMP TCP 隧道"
    dbus set "softcenter_module_icmphijack_description=LAN ICMP 经 TCP 在远端落地"
    dbus set "softcenter_module_icmphijack_version=$VERSION"
    dbus set "softcenter_module_icmphijack_home_url=Module_icmphijack.asp"
    dbus set softcenter_module_icmphijack_install=4
    dbus set "icmp_hijack_version=$VERSION"
    [ -n "$(dbus get icmp_hijack_port)" ] || dbus set icmp_hijack_port=39070
    [ -n "$(dbus get icmp_hijack_enable)" ] || dbus set icmp_hijack_enable=0
    "$ROOT/icmp_hijack.sh" start
    echo '安装完成：软件中心 → ICMP TCP 隧道。配置 IPv4、端口、64 位 hex 密钥后开启。'
else
    if [ ! -e "$CONFIG" ]; then
        umask 077
        printf '{"server":"14.137.20.5","port":39070,"key":"REPLACE_WITH_64_HEX_CHARACTERS","interface":"icmptun0"}\n' > "$CONFIG"
    fi
    "$ROOT/icmp_hijack.sh" start
    echo "安装完成：编辑 $CONFIG，执行 $ROOT/icmp_hijack.sh enable。"
fi
