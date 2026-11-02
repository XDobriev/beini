#!/usr/bin/env python3
"""Build a Linux/amd64 release containing only the server and public assets."""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    commit = subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
    ).strip()
    if subprocess.run(["git", "diff", "--quiet", "HEAD"], cwd=ROOT).returncode:
        parser.error("Commit tracked changes before packaging a release")
    with tempfile.TemporaryDirectory(prefix="beini-release-") as temp:
        release = Path(temp)
        (release / "bin").mkdir()
        (release / "public/data").mkdir(parents=True)
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        subprocess.run(
            ["go", "build", "-trimpath", "-ldflags=-s -w", "-o",
             str(release / "bin/beini-server"), "./cmd/server"],
            cwd=ROOT, env=env, check=True,
        )
        tracked = subprocess.check_output(
            ["git", "ls-files", "-z", "--", "index.html", "demo.html",
             "data/menu_catalog.json", "assets", "css", "js"], cwd=ROOT
        ).decode("utf-8").split("\0")
        for name in filter(None, tracked):
            source = ROOT / name
            if source.is_symlink():
                raise ValueError(f"Release assets cannot be symlinks: {name}")
            destination = release / "public" / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination)
        manifest = {
            "commit": commit, "platform": "linux/amd64",
            "sha256": {
                path.relative_to(release).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in sorted(release.rglob("*")) if path.is_file()
            },
        }
        (release / "release.json").write_text(
            json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        with tarfile.open(output, "w:gz") as archive:
            def permissions(info):
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mode = 0o755 if info.isdir() or info.name == "bin/beini-server" else 0o644
                return info
            for name in ("bin", "public", "release.json"):
                archive.add(release / name, arcname=name, filter=permissions)
    print(f"Release {commit}: {output}")


if __name__ == "__main__":
    main()
