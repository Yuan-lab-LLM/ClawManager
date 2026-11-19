#!/usr/bin/env python3
"""Build on server54 and prepare an immutable helper ConfigMap; never deploys."""
import argparse
import base64
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    addresses = subprocess.check_output(["hostname", "-I"], text=True).split()
    if "10.130.81.54" not in addresses:
        parser.error("helper tests and binary builds must run on test cluster 10.130.81.54")
    repo = Path(__file__).resolve().parents[4]
    backend = repo / "backend"
    env = dict(os.environ, GOTOOLCHAIN="local", CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
    env.pop("GOROOT", None)
    subprocess.run([args.go, "test", "./cmd/backup-flock", "-count=1"], cwd=backend, env=env, check=True)
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix="cm-backup-lock-") as temporary:
        binary = Path(temporary) / "flock"
        subprocess.run([args.go, "build", "-trimpath", "-ldflags=-s -w -buildid=", "-o", str(binary), "./cmd/backup-flock"], cwd=backend, env=env, check=True)
        raw = binary.read_bytes()
    encoded = base64.b64encode(gzip.compress(raw, compresslevel=9, mtime=0)).decode()
    digest = hashlib.sha256(raw).hexdigest()
    manifest = {
        "apiVersion": "v1", "kind": "ConfigMap", "immutable": True,
        "metadata": {"name": "mysql-backup-lock-helper-v1", "namespace": "clawmanager-system", "annotations": {"clawmanager.io/binary-sha256": digest}},
        "data": {"SHA256SUMS": digest + "  /opt/cm-backup-tools/flock\n"},
        "binaryData": {"flock.gz": encoded},
    }
    output = json.dumps(manifest, indent=2) + "\n"
    if len(output.encode()) > 950_000:
        raise RuntimeError("compressed helper exceeds the bounded ConfigMap budget")
    path = args.output / "mysql-backup-lock-helper.json"
    with path.open("x") as target:
        target.write(output)
    path.chmod(0o600)
    (args.output / "README.txt").write_text(
        "Prepared on server54; no Kubernetes changes were made.\n"
        "Review the immutable ConfigMap and the backup CronJob together.\n"
        "Apply this ConfigMap before scheduling the updated CronJob.\n"
        "Do not replace an existing v1 with different bytes; version the helper and manifest together.\n"
        "Binary SHA256: " + digest + "\n")
    print(path)


if __name__ == "__main__":
    main()
