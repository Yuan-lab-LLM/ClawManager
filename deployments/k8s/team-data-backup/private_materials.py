#!/usr/bin/env python3
"""Prepare keys or independent recovery files without reading source Kubernetes.

init-keys --output NEW_DIR creates only new keys (never overwrites an escrow).
recovery --escrow PRIVATE_DIR --output NEW_DIR --namespace N --namespace-uid UID
 --object-endpoint HOST:PORT [--object-tls] --credentials FILE
 --reader-credentials FILE --target-admin-dsn FILE --control-dsn FILE
 --redis-image REGISTRY/redis@sha256:DIGEST

The operator supplies disaster-side DB/object credentials and namespace. Use a
trusted TLS endpoint or an independently managed loopback tunnel. The output
directory is private; preserve it outside the application's failure domain.
"""
import argparse
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
from pathlib import PurePosixPath


def write(root, name, data):
    with os.fdopen(os.open(root / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as f:
        f.write(data if isinstance(data, bytes) else json.dumps(data, indent=2).encode())


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("operation", choices=["init-keys", "recovery"])
    p.add_argument("--output", required=True, type=Path)
    p.add_argument("--escrow", type=Path)
    p.add_argument("--namespace")
    p.add_argument("--namespace-uid")
    p.add_argument("--object-endpoint")
    p.add_argument("--object-tls", action="store_true")
    p.add_argument("--credentials", type=Path)
    p.add_argument("--reader-credentials", type=Path)
    p.add_argument("--target-admin-dsn", type=Path)
    p.add_argument("--control-dsn", type=Path)
    p.add_argument("--redis-image")
    p.add_argument("--bundle-path", help="absolute mount path inside the recovery runner")
    a = p.parse_args()
    files = {}
    if a.operation == "init-keys":
        # DER encodings of RFC8410 Ed25519 contain the final 32-byte seed/public.
        private = subprocess.run(["openssl", "genpkey", "-algorithm", "Ed25519", "-outform", "DER"], capture_output=True, check=True).stdout
        public = subprocess.run(["openssl", "pkey", "-inform", "DER", "-pubout", "-outform", "DER"], input=private, capture_output=True, check=True).stdout
        assert len(private) == 48 and len(public) == 44
        files = {"encryption.key": secrets.token_bytes(32), "signing.key": private[-32:] + public[-32:], "verify.pub": public[-32:]}
    else:
        assert all((a.escrow, a.namespace, a.namespace_uid, a.object_endpoint, a.credentials,
                    a.reader_credentials, a.target_admin_dsn, a.control_dsn, a.redis_image))
        assert re.fullmatch(r"[^\s]+@sha256:[a-f0-9]{64}", a.redis_image)
        if not a.object_tls:
            assert a.object_endpoint.startswith(("127.0.0.1:", "localhost:", "[::1]:")), "TLS or managed loopback tunnel required"
        cfg = json.loads((a.escrow / "config.json").read_text())
        for name in ("encryption.key", "signing.key"):
            files[name] = (a.escrow / name).read_bytes()
        assert len(files["encryption.key"]) == 32 and len(files["signing.key"]) == 64
        files["verify.pub"] = files["signing.key"][32:]
        for name, path in (("objects.json", a.credentials), ("reader-objects.json", a.reader_credentials),
                           ("admin.dsn", a.target_admin_dsn), ("control.dsn", a.control_dsn)):
            assert path.is_file() and not path.is_symlink() and path.stat().st_size <= 16384
            files[name] = path.read_bytes()
        assert json.loads(files["objects.json"])["access_key"] != json.loads(files["reader-objects.json"])["access_key"]
        # Absolute paths refer to the destination runner where this directory is
        # mounted. Move/mount the bundle at the same path, or explicitly edit it.
        root = a.bundle_path or a.output.absolute().as_posix()
        assert PurePosixPath(root).is_absolute() and str(PurePosixPath(root)) == root and root != "/"
        cfg.update(disabled=False, namespace=a.namespace, namespace_uid=a.namespace_uid,
                   source_root="/unmounted-origin/source", runtime_root="/unmounted-origin/runtime",
                   stage_root=root + "/stage", restore_root=root + "/copies", redis_url_file="",
                   object_endpoint=a.object_endpoint, object_secure=a.object_tls,
                   object_credentials_file=root + "/objects.json", verifier_object_credentials_file=root + "/reader-objects.json",
                   restore_admin_dsn_file=root + "/admin.dsn", encryption_key_file=root + "/encryption.key",
                   signing_key_file=root + "/signing.key", restore_redis_image=a.redis_image)
        files["service.json"] = cfg
        files["bootstrap.json"] = {"endpoint": a.object_endpoint, "bucket": cfg["backup_bucket"], "root": "team-data",
            "secure": a.object_tls, "credentials_file": root + "/objects.json", "verification_key_file": root + "/verify.pub",
            "verification_key_version": cfg["key_version"], "encryption_key_file": root + "/encryption.key",
            "encryption_reference": {"provider": "kubernetes", "name": "team-data-backup-bootstrap", "version": cfg["key_version"], "purpose": "encryption"}}
        files["operation.json"] = {"control_dsn_file": root + "/control.dsn", "service_config_file": root + "/service.json", "prepare_empty_control": True}
    # Exclusive directory creation makes reruns harmless to existing key material.
    a.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    for name, data in files.items(): write(a.output, name, data)
    print(json.dumps({"operation": a.operation, "files": sorted(files), "created": True, "source_secret_access": False}))


if __name__ == "__main__":
    try: main()
    except Exception:
        sys.exit("Private material preparation failed. Existing directories and keys are never overwritten; credential details withheld.")
