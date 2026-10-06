#!/usr/bin/env python3
"""Build static Linux binaries and deterministic, installable release archives."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import gzip
import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]


def archive(destination, entries):
    with destination.open("wb") as output:
        with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as tar:
                directories = set()
                for filename, source in sorted(entries):
                    for parent in Path(filename).parents:
                        if str(parent) != ".":
                            directories.add(parent.as_posix())
                for directory in sorted(directories, key=lambda x: (x.count("/"), x)):
                    info = tarfile.TarInfo(directory)
                    info.type = tarfile.DIRTYPE
                    info.mode = 0o755
                    tar.addfile(info)
                for filename, source in sorted(entries):
                    data = source.read_bytes()
                    if source.suffix in (".sh", ".asp", ".md", ".json"):
                        data = data.replace(b"\r\n", b"\n")
                    info = tarfile.TarInfo(filename)
                    info.mode = 0o755 if source.suffix == ".sh" or source.name.startswith("icmptunnel") else 0o644
                    info.size = len(data)
                    tar.addfile(info, io.BytesIO(data))


def files(directory):
    return sorted(p for p in directory.rglob("*") if p.is_file() and "__pycache__" not in p.parts)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", default="go")
    parser.add_argument("--version", default="1.0.5")
    args = parser.parse_args()
    if not all(c.isdigit() or c == "." for c in args.version):
        parser.error("Version must be digits and dots")
    # Software Center checks this hidden platform file before running install.sh.
    marker = ROOT / "router/.valid"
    if not marker.is_file() or "hnd" not in marker.read_text().splitlines():
        parser.error("router/.valid must contain the HND platform marker")
    targets = (("amd64", None, "linux-amd64"), ("arm64", None, "linux-arm64"), ("arm", "7", "linux-armv7"))

    def build(target):
        arch, arm, directory = target
        destination = ROOT / "bin" / directory / "icmptunnel"
        destination.parent.mkdir(parents=True, exist_ok=True)
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH=arch, GOARM64="v8.0", GOAMD64="v1")
        if arm:
            env["GOARM"] = arm
        subprocess.run([args.go, "build", "-buildvcs=false", "-trimpath", "-ldflags", f"-s -w -X main.version={args.version}", "-o", str(destination), "./cmd/icmptunnel"], cwd=ROOT, env=env, check=True)
        print(f"Built {directory}: {destination.stat().st_size} bytes", flush=True)

    with ThreadPoolExecutor(max_workers=3) as executor:
        list(executor.map(build, targets))
    (ROOT / "router/bin").mkdir(exist_ok=True)
    shutil.copyfile(ROOT / "bin/linux-armv7/icmptunnel", ROOT / "router/bin/icmptunnel-armv7")
    shutil.copyfile(ROOT / "bin/linux-arm64/icmptunnel", ROOT / "router/bin/icmptunnel-armv8")
    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    router_entries = [("icmphijack/" + p.relative_to(ROOT / "router").as_posix(), p) for p in files(ROOT / "router") if "tests" not in p.parts and p.name != "make_icon.py"]
    archive(dist / f"icmphijack-{args.version}.tar.gz", router_entries)
    server_entries = [("icmptunnel-server/" + p.relative_to(ROOT).as_posix(), p) for p in files(ROOT / "server") if "tests" not in p.relative_to(ROOT / "server").parts]
    for directory in ("linux-amd64", "linux-arm64"):
        path = ROOT / "bin" / directory / "icmptunnel"
        server_entries.append(("icmptunnel-server/bin/" + directory + "/icmptunnel", path))
    archive(dist / f"icmptunnel-server-{args.version}.tar.gz", server_entries)
    source_entries = []
    for directory in ("cmd", "internal", "server", "router", "docs", "tests", "tools"):
        for p in files(ROOT / directory):
            if "bin" not in p.relative_to(ROOT).parts:
                source_entries.append(("icmptunnel/" + p.relative_to(ROOT).as_posix(), p))
    for name in ("go.mod", "README.md", "CHANGELOG.md", ".gitignore", ".gitattributes"):
        if (ROOT / name).exists():
            source_entries.append(("icmptunnel/" + name, ROOT / name))
    archive(dist / f"icmptunnel-source-{args.version}.tar.gz", source_entries)
    names = (f"icmphijack-{args.version}.tar.gz", f"icmptunnel-server-{args.version}.tar.gz", f"icmptunnel-source-{args.version}.tar.gz")
    (dist / "SHA256SUMS").write_text("".join(f"{hashlib.sha256((dist / name).read_bytes()).hexdigest()}  {name}\n" for name in names), encoding="utf-8")
    print("Release archives ready in", dist)


if __name__ == "__main__":
    main()
