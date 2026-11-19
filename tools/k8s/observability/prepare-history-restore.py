#!/usr/bin/env python3
"""Prepare a dedicated restore schema/account and Secret; does not execute SQL or deploy."""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import secrets


def write_private(path, content):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as target:
        target.write(content)
        target.flush()
        os.fsync(target.fileno())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--host", default="mysql.clawmanager-system.svc.cluster.local")
    parser.add_argument("--port", default=3306, type=int)
    parser.add_argument("--user", default="cm_history_restore_v1")
    parser.add_argument("--tls", choices=("true", "skip-verify", "false"), default="true")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9_.-]{1,253}", args.host) or not re.fullmatch(r"[A-Za-z0-9_]{1,32}", args.user) or not 1 <= args.port <= 65535:
        parser.error("bounded hostname, account identifier and TCP port required")
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    database = "cm_history_restore_" + secrets.token_hex(16)
    password = secrets.token_urlsafe(32)
    dsn = f"{args.user}:{password}@tcp({args.host}:{args.port})/{database}?parseTime=true&loc=UTC&tls={args.tls}\n"
    ssl = " REQUIRE SSL" if args.tls != "false" else ""
    sql = (f"CREATE DATABASE `{database}`;\n"
           f"CREATE USER '{args.user}'@'%' IDENTIFIED BY '{password}'{ssl};\n"
           f"GRANT SELECT,INSERT,UPDATE,DELETE,CREATE ON `{database}`.* TO '{args.user}'@'%';\n")
    secret = {"apiVersion": "v1", "kind": "Secret", "immutable": True,
              "metadata": {"name": "clawmanager-history-restore-v1", "namespace": "clawmanager-system"},
              "type": "Opaque", "data": {"dsn": base64.b64encode(dsn.encode()).decode()}}
    patch = {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "clawmanager-app", "namespace": "clawmanager-system"},
             "spec": {"template": {"spec": {
                 "containers": [{"name": "clawmanager-app", "env": [{"name": "CLAWMANAGER_HISTORY_RESTORE_DSN_FILE", "value": "/var/run/secrets/clawmanager-history-restore/dsn"}],
                                 "volumeMounts": [{"name": "history-restore-credential", "mountPath": "/var/run/secrets/clawmanager-history-restore", "readOnly": True}]}],
                 "volumes": [{"name": "history-restore-credential", "secret": {"secretName": "clawmanager-history-restore-v1", "defaultMode": 0o440}}]}}}}
    write_private(args.output / "bootstrap.sql", sql)
    write_private(args.output / "secret.json", json.dumps(secret, indent=2) + "\n")
    write_private(args.output / "deployment-patch.json", json.dumps(patch, indent=2) + "\n")
    write_private(args.output / "dsn.private", dsn)
    write_private(args.output / "README.txt", "No SQL or Kubernetes resources were changed.\nReview/bootstrap the exact isolated schema with a separate administrator.\nThe generated account has no privileges on the application schema or any other database.\nStartup validates this isolation and rejects root/source credentials.\nApply the immutable Secret. Merge the deployment patch only if the app manifest lacks the restore credential mount.\nChoose a TLS profile supported by the existing MySQL service; true validates server certificates.\nThe two-connection restore pool contains only temporary, authenticated proof rows, never application migrations.\nKeep these owner-only files out of source control.\n")
    print("Prepared private restore bootstrap and deployment manifests; no SQL or Kubernetes changes were made.")


if __name__ == "__main__":
    main()
