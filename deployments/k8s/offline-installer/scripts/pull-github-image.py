#!/usr/bin/env python3
"""Optional release catalog -> registry pulls -> archives -> images.lock."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
sys.path.insert(0, str(Path(__file__).resolve().parent))
from ghcr_release import resolve_latest

ROOT = Path(__file__).resolve().parent.parent


def run(*args):
    return subprocess.run(list(args), check=True)


def archive_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def reusable_archive(target, image, pull_digest):
    try:
        checksum = Path(str(target) + ".sha256").read_text().split()[0]
        if pull_digest:
            metadata = json.loads(Path(str(target) + ".source.json").read_text())
            if metadata != {"image": image, "digest": pull_digest, "sha256": checksum}:
                return False
        if archive_hash(target) != checksum:
            return False
        run(sys.executable, str(ROOT / "scripts/verify-image-archive.py"), str(target), image)
        return True
    except (OSError, ValueError, IndexError, subprocess.CalledProcessError):
        return False


def validate_reference(key, image):
    if not re.fullmatch(r"(?:platform|runtime|kubernetes|calico|longhorn)\.[a-z0-9][a-z0-9._-]*", key):
        raise ValueError(f"invalid image key: {key}")
    if not re.fullmatch(r"[a-z0-9][a-z0-9._/-]*(?::[0-9]+/[a-z0-9][a-z0-9._/-]*)?:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", image):
        raise ValueError(f"requires a registry image with explicit VERSION tag: {image}")
    tag = image.rsplit(":", 1)[1]
    if tag.lower() == "latest":
        raise ValueError("use an explicit release version instead of latest")
    return tag


def image_group(key):
    return "clawmanager" if key == "platform.clawmanager" else (
        "runtime" if key.startswith("runtime.") else "platform-base"
    )


def prepare(key, image, lock_path, output_dir, pull_digest=None):
    tag = validate_reference(key, image)

    lock_path = Path(lock_path).resolve()
    original = lock_path.read_bytes()
    lines = original.decode("utf-8-sig").splitlines(keepends=True)
    matches = []
    seen = set()
    for index, line in enumerate(lines):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        fields = stripped.split("|")
        if len(fields) != 2 or fields[0] in seen:
            raise ValueError(f"invalid or duplicate images.lock entry on line {index + 1}")
        seen.add(fields[0])
        if fields[0] == key:
            matches.append(index)
    if len(matches) != 1:
        raise ValueError(f"key does not exist in images.lock: {key}")
    index = matches[0]
    ending = "\r\n" if lines[index].endswith("\r\n") else "\n"
    old_value = lines[index].strip().split("|", 1)[1]
    lines[index] = f"{key}|{image}{ending}"
    updated = "".join(lines).encode("utf-8")
    if original.startswith(b"\xef\xbb\xbf"):
        updated = b"\xef\xbb\xbf" + updated

    group = image_group(key)
    # Include the key and reference hash to distinguish variants and registries.
    ref_hash = hashlib.sha256(image.encode()).hexdigest()[:12]
    name = f"{group}-images-{key}-{tag}-{ref_hash}.tar"
    output_dir = Path(output_dir).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    target = output_dir / name
    print(f"Preparing {key}: {old_value or '(empty)'} -> {image}", flush=True)
    if old_value == image and reusable_archive(target, image, pull_digest):
        if lock_path.read_bytes() != original:
            raise ValueError("images.lock changed during cache verification; rerun")
        print(f"SKIP {key}: version and verified archive unchanged: {target}", flush=True)
        return target
    subprocess.run(["docker", "info"], check=True, stdout=subprocess.DEVNULL)
    if pull_digest:
        # Pull the resolved immutable index to avoid latest/version tag races.
        pinned = image.rsplit(":", 1)[0] + "@" + pull_digest
        run("docker", "pull", "--platform", "linux/amd64", pinned)
        run("docker", "tag", pinned, image)
    else:
        run("docker", "pull", "--platform", "linux/amd64", image)
    result = subprocess.run(
        ["docker", "image", "inspect", image, "--format", "{{.Os}}/{{.Architecture}}"],
        check=True, capture_output=True, text=True,
    )
    if result.stdout.strip() != "linux/amd64":
        raise ValueError(f"image architecture mismatch: {result.stdout.strip()}")

    # A failed pull/save/verification cannot update the user's mapping.
    with tempfile.TemporaryDirectory(prefix=".ghcr-", dir=output_dir) as work:
        temporary_archive = Path(work) / name
        run("docker", "save", "--output", str(temporary_archive), image)
        run(sys.executable, str(ROOT / "scripts/verify-image-archive.py"),
            str(temporary_archive), image)
        digest = hashlib.sha256()
        with temporary_archive.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        checksum = Path(work) / (name + ".sha256")
        checksum.write_text(f"{digest.hexdigest()}  {name}\n", encoding="utf-8")
        if lock_path.read_bytes() != original:
            raise ValueError("images.lock changed during download; rerun with the current file")
        os.replace(temporary_archive, target)
        os.replace(checksum, Path(str(target) + ".sha256"))
        metadata = Path(work) / (name + ".source.json")
        metadata.write_text(json.dumps({"image": image, "digest": pull_digest,
                                        "sha256": digest.hexdigest()}), encoding="utf-8")
        os.replace(metadata, Path(str(target) + ".source.json"))

    # Backup beside the lock; rename the new map only after the archive exists.
    backup = None
    if updated != original:
        with tempfile.NamedTemporaryFile(
            prefix="images.lock.backup-", dir=lock_path.parent, delete=False
        ) as stream:
            backup = Path(stream.name)
            stream.write(original)
        temporary_lock = None
        try:
            with tempfile.NamedTemporaryFile(
                prefix=".images.lock-", dir=lock_path.parent, delete=False
            ) as stream:
                temporary_lock = Path(stream.name)
                stream.write(updated)
            if lock_path.read_bytes() != original:
                raise ValueError("images.lock changed; archive saved but mapping not updated")
            os.replace(temporary_lock, lock_path)
        finally:
            if temporary_lock and temporary_lock.exists():
                temporary_lock.unlink()
    print(f"Archive: {target}")
    print(f"Mapping: {key}|{image}")
    if backup:
        print(f"Previous lock backup: {backup}")
    print("Ready for build-offline-bundle.sh; other lock entries remain selected.")
    return target


def read_mapping(path):
    result = {}
    for number, line in enumerate(Path(path).read_text(encoding="utf-8-sig").splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        fields = line.split("|")
        if len(fields) != 2:
            raise ValueError(f"{path}:{number}: expected key|image")
        key, image = (value.strip() for value in fields)
        if key in result:
            raise ValueError(f"{path}:{number}: duplicate key {key}")
        result[key] = image
    return result


def prepare_catalog(catalog, lock, output, groups="all", dry_run=False):
    sources = read_mapping(catalog)
    existing = read_mapping(lock)
    selected = set(groups.split(","))
    if not selected or selected - {"all", "clawmanager", "runtime", "platform-base"}:
        raise ValueError("--group must be all, clawmanager, runtime, platform-base, or a comma list")
    if "all" in selected and len(selected) != 1:
        raise ValueError("all cannot be combined with another group")
    plan = []
    # Validate the entire selected plan before the first network request/write.
    for key, image in sources.items():
        if key not in existing:
            raise ValueError(f"catalog key does not exist in images.lock: {key}")
        if "all" not in selected and image_group(key) not in selected:
            continue
        if not image:
            print(f"SKIP {key}: no public image selected; lock unchanged")
            continue
        if image.endswith(":latest") and image.startswith("ghcr.io/") and image_group(key) in {"clawmanager", "runtime"}:
            validate_reference(key, image[:-len("latest")] + "version-check")
        else:
            validate_reference(key, image)
        plan.append((key, image))
    if not plan:
        raise ValueError("no images selected in the release catalog")
    print(f"Release catalog: {catalog}; selected images: {len(plan)}", flush=True)
    for key, image in plan:
        print(f"  {key}|{image}")
    if dry_run:
        return plan
    resolved = []
    # Resolve every latest before any pull or lock update.
    for key, image in plan:
        if image.endswith(":latest"):
            concrete, digest = resolve_latest(image)
            print(f"Resolved {key}: {image} -> {concrete} ({digest})", flush=True)
            resolved.append((key, concrete, digest))
        else:
            resolved.append((key, image, None))
    for number, (key, image, digest) in enumerate(resolved, 1):
        print(f"[{number}/{len(plan)}] {key}", flush=True)
        if digest:
            prepare(key, image, lock, output, pull_digest=digest)
        else:
            prepare(key, image, lock, output)
    print("Release catalog completed. All selected mappings are ready.")
    return plan


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog", type=Path, default=ROOT / "images/release-images.conf")
    parser.add_argument("--group", default="all", help="all, clawmanager, runtime, platform-base, or comma list")
    parser.add_argument("--dry-run", action="store_true", help="preview without Docker, downloads, or file changes")
    parser.add_argument("--lock", type=Path, default=ROOT / "images/images.lock")
    parser.add_argument("--output", type=Path, default=ROOT / "images/local")
    args = parser.parse_args()
    try:
        prepare_catalog(args.catalog, args.lock, args.output, args.group, args.dry_run)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"ERROR: {error}\n")


if __name__ == "__main__":
    main()
