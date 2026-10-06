#!/usr/bin/env python3
"""Build a BusyBox 1.25.1 ash with Merlin's relevant shell feature switches.

Usage: python3 tools/build_merlin_test_shell.py /path/to/busybox-1.25.1
This builds a test shell only; it is not shipped to or installed on the router.
"""
from pathlib import Path
import subprocess
import sys


def main():
    source = Path(sys.argv[1]).resolve()
    makefile = (source / "Makefile").read_text()
    if not all(line in makefile for line in ("VERSION = 1", "PATCHLEVEL = 25", "SUBLEVEL = 1")):
        raise SystemExit("BusyBox 1.25.1 source is required")
    subprocess.run(["make", "allnoconfig"], cwd=source, check=True, stdout=subprocess.DEVNULL)
    settings = {
        "CONFIG_ASH": "y",
        "CONFIG_ASH_BASH_COMPAT": "y",
        "CONFIG_ASH_ALIAS": "y",
        "CONFIG_ASH_OPTIMIZE_FOR_SIZE": "y",
        "CONFIG_FEATURE_SH_IS_ASH": "y",
        "CONFIG_SH_MATH_SUPPORT": "y",
        "CONFIG_FEATURE_SH_EXTRA_QUIET": "y",
        "CONFIG_ASH_CMDCMD": "n",
        "CONFIG_ASH_JOB_CONTROL": "n",
        "CONFIG_ASH_GETOPTS": "n",
        "CONFIG_ASH_BUILTIN_ECHO": "n",
        "CONFIG_ASH_BUILTIN_PRINTF": "n",
        "CONFIG_ASH_BUILTIN_TEST": "n",
        "CONFIG_SH_MATH_SUPPORT_64": "n",
        "CONFIG_FEATURE_SH_IS_NONE": "n",
        "CONFIG_FEATURE_SH_IS_HUSH": "n",
    }
    lines = (source / ".config").read_text().splitlines()
    for key, value in settings.items():
        lines = [line for line in lines if not line.startswith(key + "=") and line != "# " + key + " is not set"]
        lines.append(key + "=y" if value == "y" else "# " + key + " is not set")
    (source / ".config").write_text("\n".join(lines) + "\n")
    subprocess.run(["make", "oldconfig"], cwd=source, input=b"\n" * 300, check=True, stdout=subprocess.DEVNULL)
    with (source / "icmp-shell-build.log").open("w") as log:
        result = subprocess.run(["make", "-j2"], cwd=source, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        raise SystemExit("BusyBox build failed; inspect " + str(source / "icmp-shell-build.log"))
    probe = subprocess.run([str(source / "busybox"), "ash", "-c", "command -v iptables"], capture_output=True, text=True)
    if probe.returncode != 127 or "command" not in probe.stderr:
        raise SystemExit("Test shell unexpectedly has the command builtin")
    print("Built test shell with command builtin disabled:", source / "busybox")


if __name__ == "__main__":
    main()
