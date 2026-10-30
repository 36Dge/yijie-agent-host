"""Collect only the executable reported by this successful canonical Cargo build."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import tempfile


def executable(messages, manifest):
    candidates = set()
    finished = []
    for line in messages.splitlines():
        value = json.loads(line)
        if value.get("reason") == "build-finished":
            finished.append(value.get("success"))
        target = value.get("target", {})
        if (value.get("reason") == "compiler-artifact"
                and value.get("manifest_path") == str(manifest)
                and target.get("name") == "yijie-artifact-mcp"
                and target.get("kind") == ["bin"]
                and value.get("profile", {}).get("test") is False):
            if not value.get("executable"):
                raise ValueError("Cargo executable missing")
            candidates.add(value["executable"])
    if finished != [True] or len(candidates) != 1:
        raise ValueError("successful unique Cargo artifact required; no old-path fallback")
    path = Path(candidates.pop())
    if not path.is_absolute() or path.resolve(strict=True) != path:
        raise ValueError("canonical Cargo executable required")
    s = path.lstat()
    if not stat.S_ISREG(s.st_mode) or not s.st_mode & 0o111:
        raise ValueError("Cargo executable is not an executable regular file")
    return path


def publish(source, output):
    output.mkdir(parents=True, exist_ok=True)
    if output.resolve(strict=True) != output:
        raise ValueError("canonical output directory required")
    destination = output / "yijie-artifact-mcp"
    if source == destination:
        raise ValueError("collection must be separate from Cargo output")
    with source.open("rb") as stream:
        sha = hashlib.file_digest(stream, "sha256").hexdigest()
    fd, temp = tempfile.mkstemp(prefix=".artifact-", dir=output)
    try:
        with os.fdopen(fd, "wb") as dst, source.open("rb") as src:
            shutil.copyfileobj(src, dst)
        shutil.copymode(source, temp)
        with open(temp, "rb") as stream:
            if hashlib.file_digest(stream, "sha256").hexdigest() != sha:
                raise ValueError("Cargo artifact changed during collection")
        os.replace(temp, destination)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)
    (output / "yijie-artifact-mcp.sha256").write_text(sha + "\n")
    (output / "yijie-artifact-mcp.build.json").write_text(json.dumps({
        "schema_version": 1, "source": str(source), "sha256": sha,
        "size_bytes": destination.stat().st_size,
        "collection": "successful-cargo-compiler-artifact",
    }, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--messages", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    publish(executable(args.messages.read_text(), args.manifest.resolve(strict=True)), args.output)
