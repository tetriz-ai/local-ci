#!/usr/bin/env python3
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
"""Build the two CLIs and portable release archives without publishing."""

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile
import time
import zipfile


def command(args, root, env):
    return subprocess.check_output(args, cwd=root, env=env, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="release tag, e.g. v0.1.0")
    parser.add_argument("--output", type=Path, help="default: dist/<version>")
    parser.add_argument("--go", default="go", help="Go executable")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("version must be a vMAJOR.MINOR.PATCH tag")
    root = Path(__file__).resolve().parents[1]
    env = dict(os.environ)
    env.setdefault("GOTOOLCHAIN", "go1.27.3")
    env.setdefault("GOMAXPROCS", "2")
    env.setdefault("GOMEMLIMIT", "512MiB")
    env["GOFLAGS"] = "-mod=readonly -p=2"
    if command(["git", "status", "--porcelain"], root, env):
        parser.error("release builds require a clean, committed checkout")
    commit = command(["git", "rev-parse", "HEAD"], root, env)
    epoch = int(command(["git", "show", "-s", "--format=%ct", "HEAD"], root, env))
    go_version = command([args.go, "version"], root, env).split()[2]
    if go_version != "go1.27.3":
        parser.error("release binaries require Go 1.27.3; set GOTOOLCHAIN=go1.27.3")
    targets = [(system, arch) for system in ("darwin", "linux", "windows")
               for arch in ("amd64", "arm64")]
    source = {"repository": "https://github.com/tetriz-ai/local-ci", "commit": commit,
              "version": args.version, "go_version": go_version,
              "targets": [f"{system}/{arch}" for system, arch in targets]}
    output = (args.output or root / "dist" / args.version).resolve()
    output.mkdir(parents=True, exist_ok=True)
    if any(output.iterdir()):
        parser.error("output directory must be empty")
    legal = [root / name for name in ("LICENSE", "NOTICE", "README.md")]
    legal += sorted(p for p in (root / "third_party").rglob("*") if p.is_file())
    source_bytes = (json.dumps(source, indent=2) + "\n").encode()
    for system, arch in targets:
        if shutil.disk_usage(output).free < 3 * 1024**3:
            raise RuntimeError("fewer than 3 GiB free; release build stopped before next target")
        with tempfile.TemporaryDirectory(prefix="local-ci-release-") as work:
            work = Path(work)
            target_env = dict(env, GOOS=system, GOARCH=arch, CGO_ENABLED="0",
                              GOCACHE=str(work / "cache"), GOTMPDIR=str(work / "tmp"))
            (work / "tmp").mkdir()
            files = {str(p.relative_to(root)): (p.read_bytes(), 0o644) for p in legal}
            files["SOURCE.json"] = (source_bytes, 0o644)
            for binary in ("local-ci", "deps"):
                name = binary + (".exe" if system == "windows" else "")
                flags = "-s -w"
                if binary == "local-ci":
                    flags += " -X main.buildVersion=" + args.version[1:]
                subprocess.run([args.go, "build", "-trimpath", "-buildvcs=false",
                                "-ldflags", flags, "-o", str(work / name),
                                "./cmd/" + binary], cwd=root, env=target_env, check=True)
                files[name] = ((work / name).read_bytes(), 0o755)
            stem = f"local-ci_{args.version[1:]}_{system}_{arch}"
            if system == "windows":
                with zipfile.ZipFile(output / (stem + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
                    for name, (data, mode) in sorted(files.items()):
                        entry = zipfile.ZipInfo(name, time.gmtime(max(epoch, 315532800))[:6])
                        entry.create_system = 3
                        entry.external_attr = (stat.S_IFREG | mode) << 16
                        entry.compress_type = zipfile.ZIP_DEFLATED
                        archive.writestr(entry, data)
            else:
                with (output / (stem + ".tar.gz")).open("wb") as raw:
                    with gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=epoch) as zipped:
                        with tarfile.open(fileobj=zipped, mode="w") as archive:
                            for name, (data, mode) in sorted(files.items()):
                                entry = tarfile.TarInfo(name)
                                entry.size, entry.mode, entry.mtime = len(data), mode, epoch
                                archive.addfile(entry, io.BytesIO(data))
            print(f"built {system}/{arch}", flush=True)
    (output / "SOURCE.json").write_bytes(source_bytes)
    checksum = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n"
                       for p in sorted(output.iterdir()) if p.is_file())
    (output / "checksums.txt").write_text(checksum)
    print(output)


if __name__ == "__main__":
    main()
