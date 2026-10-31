#!/usr/bin/env python3
"""Install explicit operator-owned files; never generate or rotate existing keys.

Use --check first. Credentials, DSN and source Redis URL must be prepared in
--private-dir using config.example.json. The restore reader must only GET
cm-restore-* objects. Keep independent escrow of encryption/signing keys.
Existing source NFS is mounted read-only twice (volume and container).
The namespace and Deployment UID guards are mandatory for every invocation.
"""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

MOUNT = "/etc/clawmanager/team-data-backup"
WORK = "/var/lib/clawmanager-team-data"
FILES = {"redis_url_file": "source-redis.url", "object_credentials_file": "objects.json",
         "verifier_object_credentials_file": "reader-objects.json", "restore_admin_dsn_file": "restore-admin.dsn",
         "encryption_key_file": "encryption.key", "signing_key_file": "signing.key"}


def kubectl(*args, obj=None, missing=False):
    command = ["kubectl", *args]
    if missing:
        command.append("--ignore-not-found")
    r = subprocess.run(command, input=None if obj is None else json.dumps(obj).encode(),
                       capture_output=True, timeout=45)
    if r.returncode:
        raise RuntimeError("Kubernetes operation failed; credential-bearing output withheld")
    return json.loads(r.stdout) if r.stdout.strip() else None


def prepare(private):
    cfg = json.loads((private / "config.json").read_text())
    assert cfg["source_root"] == "/run/team-data-source"
    assert cfg["stage_root"] == WORK + "/stage" and cfg["restore_root"] == WORK + "/restores"
    assert cfg["source_bucket"] != cfg["backup_bucket"]
    assert re.fullmatch(r"[^\s]+@sha256:[a-f0-9]{64}", cfg["restore_redis_image"]), "pin Redis by digest"
    data = {"config.json": (private / "config.json").read_bytes()}
    for field, name in FILES.items():
        assert cfg[field] == MOUNT + "/" + name, "private file mount must match config"
        path = private / name
        assert path.is_file() and not path.is_symlink() and path.stat().st_size <= 16384
        data[name] = path.read_bytes()
    assert len(data["encryption.key"]) == 32 and len(data["signing.key"]) == 64
    admin = json.loads(data["objects.json"])
    reader = json.loads(data["reader-objects.json"])
    assert admin["access_key"] != reader["access_key"], "restore verifier must use independent read credentials"
    return cfg, data


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--private-dir", required=True, type=Path)
    p.add_argument("--namespace", required=True)
    p.add_argument("--namespace-uid", required=True)
    p.add_argument("--deployment", default="clawmanager-app")
    p.add_argument("--deployment-uid", required=True)
    p.add_argument("--container", default="clawmanager-app")
    p.add_argument("--source-nfs-server", required=True)
    p.add_argument("--source-nfs-path", required=True)
    p.add_argument("--work-pvc", default="team-data-backup-work")
    p.add_argument("--storage-class", default=None)
    p.add_argument("--work-size", default="100Gi")
    p.add_argument("--check", action="store_true", help="read-only preflight, print no credentials")
    a = p.parse_args()
    cfg, files = prepare(a.private_dir)
    assert cfg["namespace"] == a.namespace and cfg["namespace_uid"] == a.namespace_uid
    ns = kubectl("get", "namespace", a.namespace, "-o", "json")
    assert ns["metadata"]["uid"] == a.namespace_uid, "namespace changed"
    dep = kubectl("get", "deployment", a.deployment, "-n", a.namespace, "-o", "json")
    assert dep["metadata"]["uid"] == a.deployment_uid, "deployment changed"
    spec = dep["spec"]["template"]["spec"]
    assert any(c["name"] == a.container for c in spec["containers"]), "container missing"
    secret = kubectl("get", "secret", "team-data-backup-bootstrap", "-n", a.namespace, "-o", "json", missing=True)
    encoded = {name: base64.b64encode(raw).decode() for name, raw in files.items()}
    if secret:
        for name in ("encryption.key", "signing.key"):
            assert secret["data"].get(name) == encoded[name], "existing key differs: no automatic rotation"
    pvc = kubectl("get", "pvc", a.work_pvc, "-n", a.namespace, "-o", "json", missing=True)
    if pvc:
        assert pvc["status"].get("phase") == "Bound", "existing work PVC not bound"
        assert "ReadWriteOnce" in pvc["spec"]["accessModes"] or "ReadWriteMany" in pvc["spec"]["accessModes"]
    sa = spec.get("serviceAccountName", "default")
    # Resource names and SA are fixed to this explicitly guarded namespace.
    metadata = {"name": "team-data-backup-resources", "namespace": a.namespace}
    role = {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role", "metadata": metadata,
            "rules": [{"apiGroups": [""], "resources": ["pods", "services", "secrets"], "verbs": ["get", "list", "create", "delete"]},
                      {"apiGroups": ["networking.k8s.io"], "resources": ["networkpolicies"], "verbs": ["get", "create", "delete"]}]}
    binding = {"apiVersion": role["apiVersion"], "kind": "RoleBinding", "metadata": metadata,
               "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": metadata["name"]},
               "subjects": [{"kind": "ServiceAccount", "name": sa, "namespace": a.namespace}]}
    patch = {"metadata": {"resourceVersion": dep["metadata"]["resourceVersion"]}, "spec": {"template": {
        "metadata": {"annotations": {"clawmanager.io/team-data-config": hashlib.sha256(json.dumps(encoded, sort_keys=True).encode()).hexdigest()}},
        "spec": {"volumes": [
            {"name": "team-data-bootstrap", "secret": {"secretName": "team-data-backup-bootstrap", "defaultMode": 256}},
            {"name": "team-data-source", "nfs": {"server": a.source_nfs_server, "path": a.source_nfs_path, "readOnly": True}},
            {"name": "team-data-work", "persistentVolumeClaim": {"claimName": a.work_pvc}}],
            "containers": [{"name": a.container, "env": [{"name": "SYSTEM_TEAM_DATA_BACKUP_CONFIG", "value": MOUNT + "/config.json"}],
                            "volumeMounts": [{"name": "team-data-bootstrap", "mountPath": MOUNT, "readOnly": True},
                                             {"name": "team-data-source", "mountPath": cfg["source_root"], "readOnly": True},
                                             {"name": "team-data-work", "mountPath": WORK}]}]}}}}
    if not a.check:
        if not pvc:
            claim = {"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": {"name": a.work_pvc, "namespace": a.namespace},
                     "spec": {"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": a.work_size}}}}
            if a.storage_class is not None: claim["spec"]["storageClassName"] = a.storage_class
            kubectl("create", "-f", "-", "-o", "json", obj=claim)
        if secret:
            # Replace guarded by UID/resourceVersion; retain labels and unrelated data.
            secret["data"].update(encoded)
            kubectl("replace", "-f", "-", "-o", "json", obj=secret)
        else:
            kubectl("create", "-f", "-", "-o", "json", obj={"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "team-data-backup-bootstrap", "namespace": a.namespace}, "type": "Opaque", "data": encoded})
        for resource in (role, binding): kubectl("apply", "-f", "-", "-o", "json", obj=resource)
        kubectl("patch", "deployment", a.deployment, "-n", a.namespace, "--type=strategic", "--patch-file=/dev/stdin", "-o", "json", obj=patch)
    print(json.dumps({"namespace": a.namespace, "checked": True, "applied": not a.check, "existing_keys_preserved": bool(secret), "source_read_only": True, "work_pvc": a.work_pvc}))


if __name__ == "__main__":
    try: main()
    except Exception:
        sys.exit("Team backup installation failed; check identities, paths and private configuration. No secrets printed.")
