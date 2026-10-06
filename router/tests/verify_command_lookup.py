#!/usr/bin/env python3
"""Reproduce the unavailable command builtin and test portable utility lookup."""
import os
from pathlib import Path
import subprocess
import tempfile

SOURCE = Path(__file__).resolve().parents[1]


def main():
    shell = os.environ.get("ICMPTUNNEL_TEST_SHELL", "/bin/sh")
    busybox = os.environ.get("ICMPTUNNEL_TEST_BUSYBOX") == "1"
    prefix = [shell, "ash"] if busybox else [shell]
    with tempfile.TemporaryDirectory(prefix="icmp-command-check-") as raw:
        temp = Path(raw)
        tool = temp / "iptables"
        tool.write_text("#!/bin/sh\nexit 0\n")
        tool.chmod(0o755)
        non_executable = temp / "icmp_non_executable"
        non_executable.write_text("not executable\n")
        non_executable.chmod(0o644)
        directory = temp / "icmp_directory"
        directory.mkdir()
        (temp / "icmp_link").symlink_to(tool)
        env = dict(os.environ, PATH=str(temp) + ":" + os.environ["PATH"])
        if busybox:
            legacy = 'for tool in iptables nvram awk; do command -v "$tool" >/dev/null 2>&1 || { echo "Missing command: $tool"; exit 1; }; done'
            failed = subprocess.run(prefix + ["-c", legacy], env=env, capture_output=True, text=True)
            assert failed.returncode == 1 and failed.stdout.strip() == "Missing command: iptables", failed
            print("PASS: legacy check falsely rejects existing iptables under command-disabled ash")
        script = '''
. "$1/scripts/commands.sh"
has_command iptables || exit 11
has_command icmp_link || exit 12
has_command "$2/iptables" || exit 13
has_command icmp_non_executable && exit 14
has_command icmp_directory && exit 15
has_command icmp_no_such_command_562394 && exit 16
exit 0
'''
        result = subprocess.run(prefix + ["-c", script, "lookup-test", str(SOURCE), str(temp)], env=env, capture_output=True, text=True)
        assert result.returncode == 0, result.stdout + result.stderr
        print("PASS: executable/symlink/absolute lookup; missing/non-executable/directory refusal")


if __name__ == "__main__":
    main()
