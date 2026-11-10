#!/usr/bin/env python3

"""Verify that a Docker archive contains exactly the requested image tags."""

from pathlib import Path
import json
import sys
import tarfile


def canonical_image(image: str) -> str:
    name = image.split("@", 1)[0]
    if "/" not in name:
        return f"docker.io/library/{image}"
    first = name.split("/", 1)[0]
    if "." not in first and ":" not in first and first != "localhost":
        return f"docker.io/{image}"
    return image


def main() -> None:
    if len(sys.argv) < 3:
        raise SystemExit(
            "usage: verify-image-archive.py ARCHIVE EXPECTED_IMAGE [EXPECTED_IMAGE ...]"
        )

    archive = Path(sys.argv[1])
    expected = {canonical_image(image) for image in sys.argv[2:]}
    try:
        with tarfile.open(archive, "r") as docker_archive:
            manifest_member = docker_archive.extractfile("manifest.json")
            if manifest_member is None:
                raise SystemExit(f"{archive}: missing manifest.json")
            manifest = json.load(manifest_member)
    except (tarfile.TarError, OSError, json.JSONDecodeError) as error:
        raise SystemExit(f"{archive}: invalid Docker archive: {error}") from error

    actual: set[str] = set()
    for item in manifest:
        repo_tags = item.get("RepoTags")
        if not isinstance(repo_tags, list) or not repo_tags:
            raise SystemExit(f"{archive}: image entry has no RepoTags")
        actual.update(canonical_image(tag) for tag in repo_tags)

    missing = sorted(expected - actual)
    extra = sorted(actual - expected)
    if missing or extra:
        details = []
        if missing:
            details.append("missing=" + ",".join(missing))
        if extra:
            details.append("extra=" + ",".join(extra))
        raise SystemExit(f"{archive}: RepoTags mismatch: {'; '.join(details)}")


if __name__ == "__main__":
    main()
