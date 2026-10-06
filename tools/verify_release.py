#!/usr/bin/env python3
"""Check archive checksums, installation paths, modes and target ELF machines."""
import hashlib
import re
from pathlib import Path
import struct
import tarfile

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "dist"


def main():
    manifest = (DIST / "SHA256SUMS").read_text().splitlines()
    router_name = next(line.split()[1] for line in manifest if line.split()[1].startswith("icmp_hijack-"))
    match = re.fullmatch(r"icmp_hijack-([0-9.]+)\.tar\.gz", router_name)
    assert match, "unrecognized router release filename"
    version = match.group(1)
    for line in manifest:
        expected, name = line.split()
        assert hashlib.sha256((DIST / name).read_bytes()).hexdigest() == expected, name
    specifications = {
        f"icmp_hijack-{version}.tar.gz": {
            "icmp_hijack/.valid": None,
            "icmp_hijack/install.sh": None,
            "icmp_hijack/uninstall.sh": None,
            "icmp_hijack/bin/icmptunnel-armv7": 40,
            "icmp_hijack/bin/icmptunnel-armv8": 183,
            "icmp_hijack/webs/Module_icmp_hijack.asp": None,
            "icmp_hijack/res/icon-icmp_hijack.png": None,
        },
        f"icmptunnel-server-{version}.tar.gz": {
            "icmptunnel-server/server/install.sh": None,
            "icmptunnel-server/server/uninstall.sh": None,
            "icmptunnel-server/bin/linux-amd64/icmptunnel": 62,
            "icmptunnel-server/bin/linux-arm64/icmptunnel": 183,
        },
        f"icmptunnel-source-{version}.tar.gz": {
            "icmptunnel/go.mod": None,
            "icmptunnel/README.md": None,
            "icmptunnel/docs/verification.md": None,
            "icmptunnel/tools/release.py": None,
        },
    }
    for name, required in specifications.items():
        with tarfile.open(DIST / name) as tar:
            members = {member.name: member for member in tar.getmembers()}
            assert len(members) == len(tar.getmembers()), "duplicate archive entry"
            for filename, machine in required.items():
                assert filename in members, (name, filename)
                member = members[filename]
                assert member.isfile()
                data = tar.extractfile(member).read()
                if filename.endswith(".sh") or machine:
                    assert member.mode == 0o755, filename
                if machine:
                    assert data[:4] == b"\x7fELF" and data[5] == 1, filename
                    assert struct.unpack_from("<H", data, 18)[0] == machine, filename
            for filename, member in members.items():
                assert not filename.startswith("/") and ".." not in Path(filename).parts
                assert not member.issym() and not member.islnk()
                if filename.endswith(".sh"):
                    data = tar.extractfile(member).read()
                    assert data.startswith(b"#!") and b"\r" not in data and not data.startswith(b"\xef\xbb\xbf"), filename
            if name.startswith("icmp_hijack-"):
                marker = tar.extractfile(members["icmp_hijack/.valid"]).read()
                assert marker == b"hnd\n", "HND offline platform marker missing/incorrect"
                install = tar.extractfile(members["icmp_hijack/install.sh"]).read()
                assert f"VERSION={version}\n".encode() in install, "package/installer version mismatch"
                for forbidden in (b"detect_package", b"ks_tar_install"):
                    assert forbidden not in install, "Software Center rejects this token in install.sh"
        print("PASS:", name)


if __name__ == "__main__":
    main()
