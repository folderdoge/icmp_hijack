#!/usr/bin/env python3
"""Exercise private release downloads without network access or installation."""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

BOOTSTRAP = Path(__file__).resolve().parents[1] / "bootstrap.sh"
SUPPORTED = (
    os.name == "posix"
    and os.geteuid() == 0
    and Path("/run/systemd/system").is_dir()
    and all(shutil.which(command) for command in ("bash", "jq", "tar", "sha256sum"))
)

FAKE_CURL = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
assert "--location-trusted" not in args
assert "test_token" not in " ".join(args)
assert sys.stdin.read() == "Authorization: Bearer test_token\n"
headers = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "--header"]
assert "X-GitHub-Api-Version: 2022-11-28" in headers
output = pathlib.Path(args[args.index("--output") + 1])
url = args[-1]
scenario = os.environ["SCENARIO"]
root = pathlib.Path(os.environ["FIXTURE_DIR"])
with (root / "requests.jsonl").open("a") as log:
    log.write(json.dumps({"url": url, "headers": headers}) + "\n")
if "/releases/assets/" not in url:
    assert "Accept: application/vnd.github+json" in headers
    if scenario == "auth_failure":
        print("curl: (22) HTTP 404", file=sys.stderr)
        sys.exit(22)
    if scenario == "invalid_json":
        output.write_text("{invalid")
    else:
        release = json.loads((root / "release.json").read_text())
        if scenario == "missing_asset":
            release["assets"] = release["assets"][1:]
        if scenario == "duplicate_asset":
            release["assets"].append(release["assets"][0])
        output.write_text(json.dumps(release, ensure_ascii=False))
else:
    assert "Accept: application/octet-stream" in headers
    if scenario == "download_failure":
        sys.exit(22)
    if url.endswith("/101"):
        output.write_bytes((root / "bundle.tar.gz").read_bytes())
    elif url.endswith("/102"):
        output.write_bytes((root / "SHA256SUMS").read_bytes())
    else:
        raise AssertionError(url)
'''

FAKE_INSTALLER = b'''#!/usr/bin/env bash
set -euo pipefail
[[ -z ${GH_TOKEN-} && -z ${GITHUB_TOKEN-} && -z ${TOKEN-} ]] || exit 90
printf '%s\\0' "$@" > "$FIXTURE_DIR/installer-args"
printf 'Fixture installed\\n'
[[ $SCENARIO != install_failure ]] || exit 7
'''


@unittest.skipUnless(SUPPORTED, "requires Linux root, systemd directory and jq")
class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="icmptunnel-bootstrap-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        tools = self.root / "tools"
        tools.mkdir()
        curl = tools / "curl"
        curl.write_text(FAKE_CURL)
        curl.chmod(0o755)
        (self.root / "tmp").mkdir()
        bundle = self.root / "bundle.tar.gz"
        with tarfile.open(bundle, "w:gz") as archive:
            info = tarfile.TarInfo("icmptunnel-server/server/install.sh")
            info.mode = 0o755
            info.size = len(FAKE_INSTALLER)
            archive.addfile(info, io.BytesIO(FAKE_INSTALLER))
        self.bundle_name = "icmptunnel-server-1.0.2.tar.gz"
        self.checksum = hashlib.sha256(bundle.read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(
            f"{self.checksum}  {self.bundle_name}\n"
            + "0" * 64 + "  icmp_hijack-1.0.2.tar.gz\n"
        )
        (self.root / "release.json").write_text(json.dumps({
            "tag_name": "v1.0.2",
            "body": "测试: unrelated text containing id/name should be ignored",
            "assets": [
                {"id": 101, "name": self.bundle_name, "state": "uploaded"},
                {"id": 102, "name": "SHA256SUMS", "state": "uploaded"},
                {"id": 103, "name": "icmp_hijack-1.0.2.tar.gz", "state": "uploaded"},
            ],
        }))
        self.env = dict(os.environ)
        self.env.pop("GH_TOKEN", None)
        self.env.pop("GITHUB_TOKEN", None)
        self.env.update(
            GH_TOKEN="test_token",
            SCENARIO="success",
            FIXTURE_DIR=str(self.root),
            TMPDIR=str(self.root / "tmp"),
            PATH=str(tools) + os.pathsep + self.env["PATH"],
        )

    def run_bootstrap(self, *args, scenario="success"):
        self.env["SCENARIO"] = scenario
        result = subprocess.run(
            ["bash", str(BOOTSTRAP), *args],
            env=self.env, capture_output=True, text=True, timeout=15,
        )
        self.assertEqual(list((self.root / "tmp").iterdir()), [], result.stderr)
        self.assertNotIn("test_token", result.stdout + result.stderr)
        return result

    def requests(self):
        return [json.loads(line) for line in (self.root / "requests.jsonl").read_text().splitlines()]

    def test_latest_forwards_arguments_and_removes_auth(self):
        args = ["--public-ip", "14.137.20.5", "--port", "39071", "--key", "value with spaces"]
        result = self.run_bootstrap(*args)
        self.assertEqual(result.returncode, 0, result.stderr)
        actual = (self.root / "installer-args").read_bytes().split(b"\0")[:-1]
        self.assertEqual(actual, [arg.encode() for arg in args])
        self.assertTrue(self.requests()[0]["url"].endswith("/releases/latest"))
        self.assertEqual(len(self.requests()), 3)

    def test_explicit_tag_and_github_token_fallback(self):
        self.env.pop("GH_TOKEN")
        self.env["GITHUB_TOKEN"] = "test_token"
        result = self.run_bootstrap("--version", "v1.0.2", "--public-ip", "14.137.20.5")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.requests()[0]["url"].endswith("/releases/tags/v1.0.2"))

    def test_failures_do_not_run_installer(self):
        for scenario in ("auth_failure", "invalid_json", "missing_asset", "duplicate_asset", "download_failure"):
            with self.subTest(scenario=scenario):
                result = self.run_bootstrap("--public-ip", "14.137.20.5", scenario=scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.root / "installer-args").exists())

    def test_bad_checksum_stops_install(self):
        (self.root / "SHA256SUMS").write_text("0" * 64 + f"  {self.bundle_name}\n")
        result = self.run_bootstrap("--public-ip", "14.137.20.5")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SHA-256 verification failed", result.stderr)
        self.assertFalse((self.root / "installer-args").exists())

    def test_installer_exit_status_is_preserved(self):
        result = self.run_bootstrap("--public-ip", "14.137.20.5", scenario="install_failure")
        self.assertEqual(result.returncode, 7, result.stderr)

    def test_missing_token_stops_before_download(self):
        self.env.pop("GH_TOKEN")
        result = self.run_bootstrap("--public-ip", "14.137.20.5")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "requests.jsonl").exists())

    def test_help_does_not_need_auth_or_download(self):
        self.env.pop("GH_TOKEN")
        result = self.run_bootstrap("--help")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "requests.jsonl").exists())


if __name__ == "__main__":
    unittest.main()
