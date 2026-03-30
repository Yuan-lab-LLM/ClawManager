#!/usr/bin/env python3
"""Compute a stable fingerprint for the runtime contents of a local Docker image."""

from __future__ import annotations

import hashlib
import json
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


def run(*args: str) -> str:
    completed = subprocess.run(
        list(args),
        check=True,
        capture_output=True,
        text=True,
    )
    return completed.stdout


def hash_stream(fileobj) -> str:
    digest = hashlib.sha256()
    while True:
        chunk = fileobj.read(1024 * 1024)
        if not chunk:
            break
        digest.update(chunk)
    return digest.hexdigest()


def normalized_config(image_ref: str) -> bytes:
    inspect_payload = json.loads(run("docker", "image", "inspect", image_ref))[0]
    config_payload = {
        "architecture": inspect_payload.get("Architecture"),
        "os": inspect_payload.get("Os"),
        "variant": inspect_payload.get("Variant"),
        "config": inspect_payload.get("Config", {}),
    }
    return json.dumps(
        config_payload,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def normalized_tar_entries(tar_path: Path) -> list[str]:
    records: list[str] = []
    with tarfile.open(tar_path, mode="r") as archive:
        for member in archive.getmembers():
            path = member.name.rstrip("/") or "."
            record: dict[str, object] = {
                "path": path,
                "mode": member.mode,
            }

            if member.isdir():
                record["type"] = "dir"
            elif member.isfile():
                extracted = archive.extractfile(member)
                if extracted is None:
                    raise RuntimeError(f"Unable to read file contents for {path}")
                with extracted:
                    record["type"] = "file"
                    record["sha256"] = hash_stream(extracted)
            elif member.issym():
                record["type"] = "symlink"
                record["target"] = member.linkname
            elif member.islnk():
                record["type"] = "hardlink"
                record["target"] = member.linkname
            elif member.ischr():
                record["type"] = "char"
                record["device"] = [member.devmajor, member.devminor]
            elif member.isblk():
                record["type"] = "block"
                record["device"] = [member.devmajor, member.devminor]
            elif member.isfifo():
                record["type"] = "fifo"
            else:
                record["type"] = f"other:{member.type!r}"
                if member.linkname:
                    record["target"] = member.linkname

            records.append(
                json.dumps(record, sort_keys=True, separators=(",", ":"))
            )

    records.sort()
    return records


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: compute_image_fingerprint.py <local-image-ref>", file=sys.stderr)
        return 2

    image_ref = sys.argv[1]
    container_id = run("docker", "create", image_ref).strip()

    try:
        with tempfile.TemporaryDirectory(prefix="clawmanager-image-export-") as temp_dir:
            tar_path = Path(temp_dir) / "filesystem.tar"
            subprocess.run(
                ["docker", "export", "-o", str(tar_path), container_id],
                check=True,
            )

            digest = hashlib.sha256()
            digest.update(normalized_config(image_ref))
            digest.update(b"\n")

            for record in normalized_tar_entries(tar_path):
                digest.update(record.encode("utf-8"))
                digest.update(b"\n")

            print(digest.hexdigest())
    finally:
        subprocess.run(
            ["docker", "rm", "-f", container_id],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
