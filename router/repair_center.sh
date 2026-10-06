#!/bin/sh
set -eu
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH
ROOT=/koolshare/icmp_hijack
[ -x "$ROOT/uninstall.sh" ] || { echo 'Existing ICMP plugin not found.' >&2; exit 1; }
[ -f /koolshare/webs/Module_icmp_hijack.asp ] || { echo 'Legacy plugin page not found.' >&2; exit 1; }
[ -f /koolshare/res/icon-icmp_hijack.png ] || { echo 'Legacy plugin icon not found.' >&2; exit 1; }
version=$(dbus get icmp_hijack_version)
[ -n "$version" ] || version=1.0.4
cp /koolshare/res/icon-icmp_hijack.png /koolshare/res/icon-icmphijack.png
cat > /koolshare/scripts/uninstall_icmphijack.sh <<'EOF'
#!/bin/sh
PATH=${PATH:-/bin:/usr/bin}:/bin:/usr/bin:/sbin:/usr/sbin:/koolshare/bin:/koolshare/scripts
export PATH
/koolshare/icmp_hijack/uninstall.sh || exit $?
for field in name title description version install home_url; do
    dbus remove "softcenter_module_icmphijack_$field"
done
rm -f /koolshare/res/icon-icmphijack.png /koolshare/scripts/uninstall_icmphijack.sh
EOF
chmod 755 /koolshare/scripts/uninstall_icmphijack.sh
dbus set softcenter_module_icmphijack_name=icmphijack
dbus set 'softcenter_module_icmphijack_title=ICMP TCP 隧道'
dbus set 'softcenter_module_icmphijack_description=LAN ICMP 经 TCP 在远端落地'
dbus set "softcenter_module_icmphijack_version=$version"
dbus set softcenter_module_icmphijack_home_url=Module_icmp_hijack.asp
dbus set softcenter_module_icmphijack_install=4
echo 'Card repaired. Refresh the installed-app page; existing configuration is preserved.'
