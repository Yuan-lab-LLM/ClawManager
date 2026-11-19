#!/usr/bin/env python3
"""Prepare a reviewable immutable Secret; never contacts Kubernetes or deploys."""
import argparse
import base64
import json
import os
import secrets
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("--key-file", type=Path, required=True, help="durable owner-only master key file")
parser.add_argument("--output", type=Path, required=True, help="new owner-only Secret JSON file")
parser.add_argument("--initialize", action="store_true", help="explicitly create a previously absent master key")
args = parser.parse_args()
if args.initialize:
    fd = os.open(args.key_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as stream:
        stream.write(base64.b64encode(secrets.token_bytes(32)).decode() + "\n")
        stream.flush()
        os.fsync(stream.fileno())
if args.key_file.is_symlink() or args.key_file.stat().st_mode & 0o077:
    parser.error("master key must be a regular owner-only file")
encoded = args.key_file.read_text().strip()
if len(base64.b64decode(encoded, validate=True)) != 32:
    parser.error("master key must decode to exactly 32 bytes")
manifest = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "clawmanager-observability-key-v1", "namespace": "clawmanager-system"}, "immutable": True, "type": "Opaque", "stringData": {"master-key": encoded}}
fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w") as stream:
    json.dump(manifest, stream, indent=2)
    stream.write("\n")
    stream.flush()
    os.fsync(stream.fileno())
print("Prepared immutable observability Secret; no resources were deployed.")
