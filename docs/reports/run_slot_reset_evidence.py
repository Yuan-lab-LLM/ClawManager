#!/usr/bin/env python3
"""Record one test command with its output and frozen-source identity."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import time

SCOPES = ("backend", "frontend", "sql-redis", "cluster", "browser-fixture", "browser-live", "native-upgrade", "report")
IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]*")
SHA256 = re.compile(r"[0-9a-f]{64}")


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def read_source_manifest(path):
    raw = path.read_bytes()
    entries = json.loads(raw)
    if not isinstance(entries, dict) or not entries:
        raise ValueError("source manifest must be a nonempty JSON path-to-SHA-256 object")
    for name, expected in entries.items():
        relative = Path(name)
        if relative.is_absolute() or ".." in relative.parts or relative == Path("."):
            raise ValueError(f"invalid frozen source path: {name}")
        if not isinstance(expected, str) or not SHA256.fullmatch(expected):
            raise ValueError(f"invalid SHA-256 for frozen source path: {name}")
    return raw, entries


def verify_source(root, manifest):
    if manifest is None:
        return None
    raw, entries = read_source_manifest(manifest)
    root = root.resolve()
    for name, expected in entries.items():
        filename = (root / name).resolve()
        if not filename.is_relative_to(root):
            raise ValueError(f"source path leaves frozen root: {name}")
        if digest(filename.read_bytes()) != expected:
            raise ValueError(f"source differs from freeze: {name}")
    return digest(raw)


def stamp():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def write_record(file, record):
    file.seek(0)
    json.dump(record, file, ensure_ascii=False, indent=2)
    file.write("\n")
    file.truncate()
    file.flush()
    os.fsync(file.fileno())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--record", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--suite-id", required=True)
    parser.add_argument("--name")
    parser.add_argument("--label", required=True)
    parser.add_argument("--phase", required=True, choices=("development", "final"))
    parser.add_argument("--scope", required=True, choices=SCOPES)
    parser.add_argument("--kind", choices=("go-json", "command"), default="command")
    parser.add_argument("--cwd", required=True, type=Path)
    parser.add_argument("--source-manifest", type=Path)
    parser.add_argument("--source-root", type=Path)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or not IDENTIFIER.fullmatch(args.suite_id):
        parser.error("a command and valid suite identifier are required")
    if bool(args.source_root) != bool(args.source_manifest) or (args.phase == "final" and not args.source_manifest):
        parser.error("supply source manifest and root together, both are required in phase final")
    if args.record.resolve() == args.evidence.resolve():
        parser.error("record and evidence must differ")
    try:
        fingerprint = verify_source(args.source_root, args.source_manifest)
        args.record.parent.mkdir(parents=True, exist_ok=True)
        args.evidence.parent.mkdir(parents=True, exist_ok=True)
        # Unique output paths preserve prior attempts, even when the command fails.
        with args.record.open("x") as metadata:
            try:
                output = args.evidence.open("xb")
            except OSError:
                args.record.unlink()
                raise
            with output:
                record = {
                    "suite_id": args.suite_id, "name": args.name or args.suite_id, "scope": args.scope,
                    "kind": args.kind, "phase": args.phase, "run_label": args.label,
                    "argv": command, "command": shlex.join(command), "cwd": str(args.cwd.resolve()),
                    "evidence": os.path.relpath(args.evidence.resolve(), args.record.resolve().parent),
                    "started_at": stamp(), "exit_code": None, "source_manifest_sha256": fingerprint,
                }
                write_record(metadata, record)
                begin = time.monotonic()
                try:
                    record["exit_code"] = subprocess.run(command, cwd=args.cwd, stdout=output,
                                                         stderr=subprocess.STDOUT, check=False).returncode
                except OSError as error:
                    output.write(f"{error}\n".encode())
                    record["exit_code"] = 127
                except KeyboardInterrupt:
                    record["exit_code"] = 130
                record.update(finished_at=stamp(), elapsed_seconds=round(time.monotonic() - begin, 6))
                try:
                    if verify_source(args.source_root, args.source_manifest) != fingerprint:
                        raise ValueError("source manifest changed during execution")
                    record["source_verified_after"] = bool(fingerprint)
                except (OSError, ValueError) as error:
                    record.update(source_verified_after=False, source_verification_error=str(error))
                output.flush()
                os.fsync(output.fileno())
                recorded = args.evidence.read_bytes()
                record.update(evidence_sha256=digest(recorded), evidence_bytes=len(recorded))
                write_record(metadata, record)
    except (OSError, ValueError) as error:
        parser.error(str(error))
    print(json.dumps(record, ensure_ascii=False))
    if fingerprint and not record["source_verified_after"]:
        return 126
    code = record["exit_code"]
    return code if code >= 0 else 128 - code


if __name__ == "__main__":
    raise SystemExit(main())
