#!/bin/sh
# Source this file from install/uninstall. Own exactly one tagged line per hook.
HOOK_TAG='# icmp_hijack hook'
HOOK_OWNER='# icmp_hijack created this hook'

hook_add() {
    name=$1; action=$2; command_path=$3
    file=/jffs/scripts/$name
    created=0
    [ ! -L "$file" ] || { echo "Refusing symlink hook: $file" >&2; return 1; }
    if [ ! -e "$file" ]; then
        printf '#!/bin/sh\n%s\n' "$HOOK_OWNER" > "$file"
        created=1
    fi
    [ "$(head -n 1 "$file")" = '#!/bin/sh' ] || { echo "Hook needs #!/bin/sh: $file" >&2; return 1; }
    # Insert immediately after shebang so an existing exit does not bypass us.
    awk -v tag="$HOOK_TAG" -v cmd="$command_path $action $HOOK_TAG" '
        NR == 1 {print; print cmd; next}
        index($0, tag) == 0 {print}
    ' "$file" > "$file.icmp_hijack.new" || return 1
    cat "$file.icmp_hijack.new" > "$file"
    rm -f "$file.icmp_hijack.new"
    if [ "$created" = 1 ]; then chmod 755 "$file"; fi
}

hook_remove() {
    for name in services-start services-stop firewall-start nat-start; do
        file=/jffs/scripts/$name
        if [ ! -f "$file" ] || [ -L "$file" ]; then continue; fi
        grep -qF "$HOOK_TAG" "$file" || continue
        awk -v tag="$HOOK_TAG" 'index($0, tag) == 0 {print}' "$file" > "$file.icmp_hijack.new"
        cat "$file.icmp_hijack.new" > "$file"
        rm -f "$file.icmp_hijack.new"
        if grep -qF "$HOOK_OWNER" "$file" &&
           [ "$(awk -v own="$HOOK_OWNER" 'NR>1 && $0 != own && $0 !~ /^[[:space:]]*$/ {n++} END {print n+0}' "$file")" = 0 ]; then
            rm -f "$file"
        fi
    done
}

hooks_install() {
    mkdir -p /jffs/scripts
    hook_add services-start start "$1" &&
    hook_add services-stop shutdown "$1" &&
    hook_add firewall-start firewall "$1" &&
    hook_add nat-start nat "$1"
}
