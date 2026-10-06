#!/usr/bin/env python3
"""Check the produced tar against Koolshare's external offline-install gates.

Run on Linux: python3 router/tests/verify_offline_package.py [package.tar.gz]
No network, router, plugin execution, or software-center installation is used.
The bounded shell excerpts below contain only file checks and grep. In particular,
the upstream /tmp cleanup, archive extraction, and installer execution are absent.
The upstream == comparisons are spelled = for POSIX sh, local declarations become
regular assignments, and /tmp/MODULE paths use the isolated SCRIPT_AB_DIR.
A loop gives upstream's continue its normal effect.
"""
import argparse
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile
import tempfile


ROOT = Path(__file__).resolve().parents[2]
MODULE = "icmphijack"
UPSTREAM_BASE = "https://github.com/koolshare/rogsoft/blob/"
SOURCES = {
    "historic-2023": (
        UPSTREAM_BASE
        + "ed42d0e9020c83b1872962c42e60bc8a0e68a675/"
        + "softcenter/softcenter/scripts/ks_tar_install.sh#L182-L222"
    ),
    "current-2026": (
        UPSTREAM_BASE
        + "a05d7b362dec0b663eb1bd94108a0d81778bba60/"
        + "softcenter/softcenter/scripts/ks_tar_install.sh#L187-L234"
    ),
}

# Only the shared layout and forbidden installer-token gates, not the installer.
COMMON_GATES = r'''
if [ ! -f "${SCRIPT_AB_DIR}/webs/Module_${MODULE_NAME}.asp" -a "${MODULE_NAME}" != "softcenter" ];then
    echo_date "没有找到插件的web页面！"
    exit_tar_install 1
fi
if [ ! -d "${SCRIPT_AB_DIR}/scripts" ];then
    echo_date "没有找到插件的相关脚本！"
    exit_tar_install 1
fi
local EVIL_MATCH_1=$(cat ${INSTALL_SCRIPT}|grep "detect_package")
local EVIL_MATCH_2=$(cat ${INSTALL_SCRIPT}|grep "ks_tar_install")
if [ -n "${EVIL_MATCH_1}" -o -n "${EVIL_MATCH_2}" ];then
    echo_date "发现当前插件的安装脚本会对软件中心文件进行修改操作！"
    exit_tar_install 1
fi
'''

PLATFORM_SELECTORS = {
    "historic-2023": r'''
if [ "$(nvram get odmpid)" = "TX-AX6000" ];then
    VALID_STRING="mtk"
else
    VALID_STRING="hnd"
fi
''',
    "current-2026": r'''
local RO_MODEL=$(nvram get odmpid)
if [ "${RO_MODEL}" = "TX-AX6000" -o "${RO_MODEL}" = "TUF-AX4200Q" -o "${RO_MODEL}" = "RT-AX57_Go" -o "${RO_MODEL}" = "GS7" -o "${RO_MODEL}" = "ZenWiFi_BT8P" -o "${RO_MODEL}" = "GS7_Air" -o "${RO_MODEL}" = "GS-BE7200X" ];then
    VALID_STRING="mtk"
elif [ "${RO_MODEL}" = "ZenWiFi_BD4" ];then
    VALID_STRING="ipq32"
elif [ "${RO_MODEL}" = "TUF_6500" ];then
    VALID_STRING="ipq64"
elif [ "${RO_MODEL}" = "RT-AX89X" ];then
    VALID_STRING="qca"
else
    VALID_STRING="hnd"
fi
''',
}

PLATFORM_GATE = r'''
local PLATFORM=$(grep -E $VALID_STRING ${SCRIPT_AB_DIR}/.valid)
if [ -f "${SCRIPT_AB_DIR}/.valid" -a -n "${PLATFORM}" ];then
    continue
elif [ "${MODULE_NAME}" = "shadowsocks" ];then
    continue
else
    echo_date "你上传的离线安装包不是ASUS ${VALID_STRING}平台的离线包！！！"
    echo_date "请上传正确的离线安装包！！！"
    echo_date "删除相关文件并退出..."
    exit_tar_install 1
fi
'''


def package_fixture(archive, destination):
    """Read fixed archive entries without extracting or running untrusted code."""
    with tarfile.open(archive, "r:gz") as tar:
        entries = tar.getmembers()
        members = {entry.name.rstrip("/"): entry for entry in entries}
        assert len(members) == len(entries), "duplicate archive entry"
        for entry in entries:
            path = PurePosixPath(entry.name)
            assert not path.is_absolute() and ".." not in path.parts, entry.name
            assert path.parts[0] == MODULE, entry.name
            assert entry.isfile() or entry.isdir(), entry.name
        for name in ("install.sh", ".valid", f"webs/Module_{MODULE}.asp"):
            entry = members.get(f"{MODULE}/{name}")
            assert entry is not None and entry.isfile(), f"missing package file: {name}"
            assert entry.size <= 1024 * 1024, f"unexpected package file size: {name}"
            data = tar.extractfile(entry).read()
            if name == ".valid":
                assert data == b"hnd\n", ".valid must contain exactly hnd with LF and no BOM"
            output = destination / name
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(data)
        scripts = members.get(f"{MODULE}/scripts")
        assert scripts is not None and scripts.isdir(), "missing package scripts directory"
        (destination / "scripts").mkdir()


def shell_gate(shell, script, module, odmpid):
    result = subprocess.run(
        [shell, str(script), str(module), odmpid],
        check=False, capture_output=True, text=True, encoding="utf-8", timeout=10,
    )
    return result.returncode, result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", nargs="?", type=Path)
    args = parser.parse_args()
    if args.archive is None:
        names = [line.split()[1] for line in (ROOT / "dist/SHA256SUMS").read_text().splitlines()]
        name = next(name for name in names if name.startswith("icmphijack-") and name.endswith(".tar.gz"))
        args.archive = ROOT / "dist" / name
    shell = shutil.which("sh")
    if shell is None:
        parser.error("POSIX sh is required; run this check on Linux or WSL")
    with tempfile.TemporaryDirectory(prefix="icmp-offline-gate-") as raw:
        temp = Path(raw)
        fixture = temp / MODULE
        fixture.mkdir()
        package_fixture(args.archive, fixture)
        original_install = (fixture / "install.sh").read_bytes()
        rejection = "你上传的离线安装包不是ASUS hnd平台的离线包！！！"
        profiles = (
            ("historic-2023", "RT-AX86U", "AX86U 388"),
            ("current-2026", "TUF-AX5400", "AX5400 ARMv7"),
        )
        for version, odmpid, label in profiles:
            script = temp / f"{version}.sh"
            gates = (COMMON_GATES + PLATFORM_SELECTORS[version] + PLATFORM_GATE).replace(
                "\nlocal ", "\n"
            )
            script.write_text(
                'SCRIPT_AB_DIR=$1\nINSTALL_SCRIPT=$1/install.sh\n'
                'MOCK_ODMPID=$2\nMODULE_NAME=icmphijack\n'
                'nvram() { printf "%s\\n" "$MOCK_ODMPID"; }\n'
                'echo_date() { printf "%s\\n" "$*"; }\n'
                'exit_tar_install() { exit "$1"; }\n'
                'validate() { for attempt in 1; do\n'
                + gates
                + '\ndone; }\nvalidate\nprintf "ACCEPT\\n"\n',
                encoding="utf-8",
            )
            assert shell_gate(shell, script, fixture, odmpid) == (0, "ACCEPT\n"), label
            for marker in (None, b"arm384\n", b"mtk\n"):
                marker_path = fixture / ".valid"
                if marker is None:
                    marker_path.unlink()
                else:
                    marker_path.write_bytes(marker)
                code, output = shell_gate(shell, script, fixture, odmpid)
                assert code == 1 and rejection in output, (label, marker, code, output)
            (fixture / ".valid").write_bytes(b"hnd\n")
            page = fixture / "webs" / f"Module_{MODULE}.asp"
            page.rename(page.with_suffix(".hidden"))
            code, output = shell_gate(shell, script, fixture, odmpid)
            assert code == 1 and "没有找到插件的web页面" in output, label
            page.with_suffix(".hidden").rename(page)
            (fixture / "scripts").rmdir()
            code, output = shell_gate(shell, script, fixture, odmpid)
            assert code == 1 and "没有找到插件的相关脚本" in output, label
            (fixture / "scripts").mkdir()
            for token in (b"detect_package", b"ks_tar_install"):
                (fixture / "install.sh").write_bytes(original_install + b"\n# " + token + b"\n")
                code, output = shell_gate(shell, script, fixture, odmpid)
                assert code == 1 and "安装脚本会对软件中心文件进行修改" in output, (label, token)
            (fixture / "install.sh").write_bytes(original_install)
            print(f"PASS: {label}: actual tar accepted; missing/wrong marker, layout, tokens rejected")
            print(f"SOURCE: {SOURCES[version]}")
    print(f"PASS: offline package contract: {args.archive.name}")


if __name__ == "__main__":
    main()
