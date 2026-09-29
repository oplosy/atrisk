#!/usr/bin/env python3
"""Create and verify a small, deterministic backup manifest.

The manifest contains only checksums, sizes, and object keys. It never copies
credentials, environment files, or source payload metadata into the report.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path


def digest(path: Path) -> str:
    hasher = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            hasher.update(chunk)
    return hasher.hexdigest()


def relative_files(root: Path) -> list[Path]:
    return sorted(
        path.relative_to(root)
        for path in root.rglob("*")
        if path.is_file()
    )


def checksum_text(manifest: dict) -> str:
    lines = [
        f"{manifest['database']['sha256']}  database.dump",
        *[f"{item['sha256']}  objects/{item['key']}" for item in manifest["objects"]],
    ]
    return "\n".join(lines) + "\n"


def create(root: Path) -> None:
    database_dump = root / "database.dump"
    if not database_dump.is_file():
        raise SystemExit(f"database dump is missing: {database_dump}")
    objects_root = root / "objects"
    objects = []
    if objects_root.exists():
        for relative in relative_files(objects_root):
            path = objects_root / relative
            objects.append(
                {
                    "key": relative.as_posix(),
                    "bytes": path.stat().st_size,
                    "sha256": digest(path),
                }
            )
    manifest = {
        "format": "atlasrisk-backup-v1",
        "database": {
            "file": "database.dump",
            "bytes": database_dump.stat().st_size,
            "sha256": digest(database_dump),
        },
        "objects": objects,
    }
    (root / "manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    (root / "sha256sums.txt").write_text(checksum_text(manifest), encoding="utf-8")


def verify(root: Path) -> None:
    manifest_path = root / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("format") != "atlasrisk-backup-v1":
        raise SystemExit("unsupported or missing backup manifest format")
    checks = [(root / manifest["database"]["file"], manifest["database"])]
    checks.extend((root / "objects" / item["key"], item) for item in manifest["objects"])
    for path, expected in checks:
        if not path.is_file():
            raise SystemExit(f"integrity failure: missing backup member {path}")
        actual_bytes = path.stat().st_size
        actual_hash = digest(path)
        if actual_bytes != expected["bytes"] or actual_hash != expected["sha256"]:
            raise SystemExit(
                f"integrity failure: checksum mismatch for {path} "
                f"(expected {expected['sha256']}, got {actual_hash})"
            )
    objects_root = root / "objects"
    listed = {item["key"] for item in manifest["objects"]}
    unlisted = sorted(
        relative.as_posix()
        for relative in (relative_files(objects_root) if objects_root.exists() else [])
        if relative.as_posix() not in listed
    )
    if unlisted:
        raise SystemExit(f"integrity failure: unlisted backup object(s): {', '.join(unlisted)}")
    sums_path = root / "sha256sums.txt"
    if not sums_path.is_file() or sums_path.read_text(encoding="utf-8") != checksum_text(manifest):
        raise SystemExit("integrity failure: sha256sums.txt does not match manifest.json")
    print(f"backup integrity passed: {len(checks) - 1} object(s)")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("create", "verify"))
    parser.add_argument("root", type=Path)
    args = parser.parse_args()
    if args.command == "create":
        create(args.root)
    else:
        verify(args.root)
    return 0


if __name__ == "__main__":
    sys.exit(main())
