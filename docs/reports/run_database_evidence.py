#!/usr/bin/env python3
"""Record one real test command and its completion metadata, without shell evaluation."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--record", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--name", required=True)
    parser.add_argument("--suite-id")
    parser.add_argument("--category", required=True)
    parser.add_argument("--kind", choices=("go-json", "command"), default="command")
    parser.add_argument("--cwd", required=True, type=Path)
    parser.add_argument("--source-manifest", type=Path)
    parser.add_argument("--source-root", type=Path)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("a command is required")
    if bool(args.source_manifest) != bool(args.source_root):
        parser.error("source manifest and root must be supplied together")
    def verify_source():
        if args.source_manifest is None:
            return None
        root = args.source_root.resolve()
        raw = args.source_manifest.read_bytes()
        entries = json.loads(raw)
        if not entries:
            raise ValueError("empty source manifest")
        for relative, expected in entries.items():
            filename = (root / relative).resolve()
            if not filename.is_relative_to(root):
                raise ValueError("source path leaves frozen root")
            if hashlib.sha256(filename.read_bytes()).hexdigest() != expected:
                raise ValueError("source differs from freeze: " + relative)
        return hashlib.sha256(raw).hexdigest()
    fingerprint = verify_source()
    args.record.parent.mkdir(parents=True, exist_ok=True)
    args.evidence.parent.mkdir(parents=True, exist_ok=True)
    # Reserve metadata first: a rerun must use a new name and retain old evidence.
    with args.record.open("x") as metadata, args.evidence.open("x") as output:
        stamp = lambda: datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
        record = dict(name=args.name, suite_id=args.suite_id or args.name, category=args.category, kind=args.kind,
                      command=shlex.join(command), cwd=str(args.cwd.resolve()),
                      evidence=str(args.evidence.resolve()), started_at=stamp(), exit_code=None,
                      source_manifest_sha256=fingerprint)
        json.dump(record, metadata, ensure_ascii=False, indent=2)
        metadata.flush()
        os.fsync(metadata.fileno())
        begin = time.monotonic()
        try:
            record["exit_code"] = subprocess.run(command, cwd=args.cwd, stdout=output,
                                                  stderr=subprocess.STDOUT, check=False).returncode
        except OSError as error:
            output.write(str(error) + "\n")
            record["exit_code"] = 127
        record["command_exit_code"] = record["exit_code"]
        try:
            if verify_source() != fingerprint:
                raise ValueError("source manifest changed during execution")
        except (OSError, ValueError) as error:
            output.write("\nSOURCE INTEGRITY FAILED: " + str(error) + "\n")
            record["exit_code"] = 126
            record["source_integrity_failed"] = True
        output.flush()
        os.fsync(output.fileno())
        raw_evidence = args.evidence.read_bytes()
        record.update(evidence_sha256=hashlib.sha256(raw_evidence).hexdigest(), evidence_bytes=len(raw_evidence))
        record.update(finished_at=stamp(), elapsed_seconds=round(time.monotonic() - begin, 6))
        metadata.seek(0)
        json.dump(record, metadata, ensure_ascii=False, indent=2)
        metadata.truncate()
        metadata.flush()
        os.fsync(metadata.fileno())
    print(json.dumps(record, ensure_ascii=False))
    raise SystemExit(0 if record["exit_code"] == 0 else 1)


if __name__ == "__main__":
    main()
