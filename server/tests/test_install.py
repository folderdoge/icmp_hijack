#!/usr/bin/env python3
"""Verify real systemd installation and persistent uninstall on a clean Linux host.

Requires root and systemd. Refuses to modify an existing installation. Installer
output is captured and redacted because it contains the generated shared key.
"""
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import tempfile
import unittest


PROJECT = Path(__file__).resolve().parents[2]
SERVER = PROJECT / "server"
BINARY = PROJECT / "bin/linux-amd64/icmptunnel"
SERVICE = "icmptunnel-server.service"
UNIT = Path("/etc/systemd/system") / SERVICE
ENABLED_LINK = Path("/etc/systemd/system/multi-user.target.wants") / SERVICE
TARGET = Path("/usr/local/bin/icmptunnel")
CONFIG_DIR = Path("/etc/icmptunnel")
SUPPORT_DIR = Path("/usr/local/lib/icmptunnel")
UNINSTALLER = SUPPORT_DIR / "uninstall.sh"
PORT = 39889
SUPPORTED = (
    os.name == "posix"
    and os.geteuid() == 0
    and Path("/run/systemd/system").is_dir()
    and shutil.which("systemctl")
    and os.uname().machine == "x86_64"
    and BINARY.is_file()
)


def execute(*arguments):
    return subprocess.run(arguments, text=True, capture_output=True, timeout=40)


def redacted(result):
    return re.sub(r"\b[0-9a-fA-F]{64}\b", "[redacted key]", result.stdout + result.stderr)


@unittest.skipUnless(SUPPORTED, "requires x86_64 Linux root, systemd and built binary")
class InstallTests(unittest.TestCase):
    def test_autostart_upgrade_and_persistent_clean_uninstall(self):
        owned_paths = (TARGET, UNIT, ENABLED_LINK, CONFIG_DIR, SUPPORT_DIR)
        existing = [str(path) for path in owned_paths if path.exists() or path.is_symlink()]
        self.assertFalse(existing, f"Refusing to modify existing installation: {existing}")
        service = execute("systemctl", "show", SERVICE, "-p", "LoadState", "--value")
        self.assertEqual(service.stdout.strip(), "not-found", redacted(service))
        with socket.socket() as listener:
            listener.bind(("0.0.0.0", PORT))

        with tempfile.TemporaryDirectory(prefix="icmptunnel-real-install-") as temporary:
            bundle = Path(temporary) / "bundle"
            scripts = bundle / "server"
            scripts.mkdir(parents=True)
            for name in ("install.sh", "uninstall.sh"):
                (scripts / name).write_bytes((SERVER / name).read_bytes().replace(b"\r\n", b"\n"))
            binary = bundle / "bin/linux-amd64/icmptunnel"
            binary.parent.mkdir(parents=True)
            shutil.copyfile(BINARY, binary)
            cleanup_script = Path(temporary) / "cleanup.sh"
            cleanup_script.write_bytes((SERVER / "uninstall.sh").read_bytes().replace(b"\r\n", b"\n"))
            sentinels = []
            installation_attempted = False
            try:
                installation_attempted = True
                installed = execute(
                    "bash", str(scripts / "install.sh"),
                    "--public-ip", "192.0.2.10", "--port", str(PORT),
                )
                self.assertEqual(installed.returncode, 0, redacted(installed))
                self.assertEqual(execute("systemctl", "is-enabled", SERVICE).stdout.strip(), "enabled")
                self.assertEqual(execute("systemctl", "is-active", SERVICE).stdout.strip(), "active")
                self.assertTrue(ENABLED_LINK.is_symlink())
                self.assertTrue(UNINSTALLER.is_file())
                self.assertEqual(stat.S_IMODE(UNINSTALLER.stat().st_mode), 0o755)
                self.assertIn(str(UNINSTALLER), installed.stdout)
                config_file = CONFIG_DIR / "server.json"
                initial_config = config_file.read_bytes()
                config = json.loads(initial_config)
                self.assertEqual(config["listen"], f":{PORT}")
                self.assertEqual(config["public_ip"], "192.0.2.10")
                self.assertRegex(config["key"], r"^[0-9a-f]{64}$")
                self.assertEqual(stat.S_IMODE(config_file.stat().st_mode), 0o600)

                upgraded = execute("bash", str(scripts / "install.sh"))
                self.assertEqual(upgraded.returncode, 0, redacted(upgraded))
                self.assertEqual(config_file.read_bytes(), initial_config)
                self.assertEqual(execute("systemctl", "is-enabled", SERVICE).stdout.strip(), "enabled")
                self.assertEqual(execute("systemctl", "is-active", SERVICE).stdout.strip(), "active")

                # Simulate bootstrap removing all downloaded/extracted files.
                shutil.rmtree(bundle)
                for directory in (CONFIG_DIR, SUPPORT_DIR):
                    sentinel = directory / "test-owned-extra-file"
                    sentinel.write_text("preserve user-added files\n")
                    sentinels.append(sentinel)
                removed = execute("bash", str(UNINSTALLER))
                self.assertEqual(removed.returncode, 0, redacted(removed))
                for path in (TARGET, UNIT, ENABLED_LINK, config_file, UNINSTALLER):
                    self.assertFalse(path.exists() or path.is_symlink(), str(path))
                self.assertEqual(execute("systemctl", "show", SERVICE, "-p", "LoadState", "--value").stdout.strip(), "not-found")
                for sentinel in sentinels:
                    self.assertEqual(sentinel.read_text(), "preserve user-added files\n")
                # Repeat using retained original source; uninstall is idempotent.
                repeated = execute("bash", str(cleanup_script))
                self.assertEqual(repeated.returncode, 0, redacted(repeated))
            finally:
                if installation_attempted:
                    cleanup = execute("bash", str(cleanup_script))
                    self.assertEqual(cleanup.returncode, 0, redacted(cleanup))
                for sentinel in sentinels:
                    sentinel.unlink(missing_ok=True)
                for directory in (CONFIG_DIR, SUPPORT_DIR):
                    if directory.exists():
                        directory.rmdir()
        for path in owned_paths:
            self.assertFalse(path.exists() or path.is_symlink(), str(path))


if __name__ == "__main__":
    unittest.main()
