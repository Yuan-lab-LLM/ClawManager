#!/usr/bin/env python3
"""Install the single-node HostPath profile into a new namespace."""

import argparse
from contextlib import contextmanager, suppress
from functools import partial
import json
import re
from pathlib import Path, PurePosixPath
from shutil import rmtree
import signal
import subprocess
import sys
from urllib.parse import urlsplit


RUNTIME_IMAGES = {
    "openclaw-runtime": "OPENCLAW_RUNTIME_IMAGE",
    "hermes-runtime": "HERMES_RUNTIME_IMAGE",
    "opencode-runtime": "OPENCODE_RUNTIME_IMAGE",
    "deepseek-harness-runtime": "DEEPSEEK_HARNESS_RUNTIME_IMAGE",
}
TOOLS = Path(__file__).resolve().parent


def manifest_documents(output):
    decoder = json.JSONDecoder()
    while output.strip():
        document, end = decoder.raw_decode(output.lstrip())
        if document["kind"] == "List":
            yield from document["items"]
        else:
            yield document
        output = output.lstrip()[end:]


def single_node_resources(documents, images, slots):
    namespaces = [item["metadata"]["name"] for item in documents if item["kind"] == "Namespace"]
    if len(namespaces) != 1:
        raise ValueError("single-node manifest must declare exactly one namespace")
    namespace = namespaces[0]
    if any(item["metadata"].get("namespace", namespace) != namespace for item in documents):
        raise ValueError("all namespaced install resources must use the manifest namespace")

    deployments = {item["metadata"]["name"]: item for item in documents if item["kind"] == "Deployment"}
    required = {"clawmanager-app", "mysql", "minio", "clawmanager-team-redis", *RUNTIME_IMAGES}
    if missing := required - deployments.keys():
        raise ValueError("missing deployments: " + ", ".join(sorted(missing)))
    if unknown := images.keys() - deployments.keys():
        raise ValueError("unknown image overrides: " + ", ".join(sorted(unknown)))
    containers = {name: item["spec"]["template"]["spec"]["containers"][0] for name, item in deployments.items()}
    for name, image in images.items():
        containers[name]["image"] = image

    app = deployments["clawmanager-app"]
    env = {item["name"]: item for item in containers["clawmanager-app"]["env"]}
    if env.get("CLAWMANAGER_STORAGE_PROFILE", {}).get("value") != "single-node":
        raise ValueError("this installer requires the single-node HostPath profile")
    port_start = int(env.get("RUNTIME_GATEWAY_PORT_START", {}).get("value", "20000"))
    port_end = int(env.get("RUNTIME_GATEWAY_PORT_END", {}).get("value", "20299"))
    if not 1024 <= port_start <= port_end <= 65535 or slots * 3 > port_end - port_start + 1:
        raise ValueError("the Runtime gateway port range must provide three ports per slot")
    db_host = env.get("DB_HOST", {}).get("value")
    if db_host not in {"mysql", f"mysql.{namespace}", f"mysql.{namespace}.svc", f"mysql.{namespace}.svc.cluster.local"}:
        raise ValueError("fresh installation must use its bundled MySQL service")
    for name in ("OBJECT_STORAGE_ENDPOINT", "TEAM_REDIS_URL", "PLATFORM_REDIS_URL", "CLAWMANAGER_TEAM_REDIS_URL",
                 "CLAWMANAGER_TEAM_MANAGER_BASE_URL", "CLAWMANAGER_CONTROL_UI_ORIGIN", "SKILL_SCANNER_BASE_URL"):
        value = env.get(name, {}).get("value", "")
        if any(other != namespace for other in re.findall(r"\.([a-z0-9-]+)\.svc(?:[.:/]|$)", value)):
            raise ValueError(f"{name} points outside {namespace}; update bundled service DNS names")
    for name, value in {
        "RUNTIME_CONTAINER_SLOTS": str(slots),
        "RUNTIME_POD_USER_NAMESPACES": "true",
        "RUNTIME_NAMESPACE": namespace,
        "CLAWMANAGER_SYSTEM_NAMESPACE": namespace,
        "K8S_NAMESPACE": namespace if namespace != "clawmanager-system" else "clawmanager",
        "SKILL_SCANNER_NAMESPACE": namespace,
        **{env_name: containers[name]["image"] for name, env_name in RUNTIME_IMAGES.items()},
    }.items():
        env[name] = {"name": name, "value": value}
    containers["clawmanager-app"]["env"] = list(env.values())
    app["spec"]["replicas"] = 0

    claims = [item for item in documents if item["kind"] == "PersistentVolumeClaim"]
    workspace = env["RUNTIME_WORKSPACE_PVC_CLAIM"]["value"]
    storage = next((item["spec"].get("storageClassName") for item in claims
                    if item["metadata"]["name"] == workspace), None)
    if not storage:
        raise ValueError("the Runtime workspace must use a static storage class")
    claim = next(item for item in claims if item["metadata"]["name"] == workspace)
    volume = next((item for item in documents if item["kind"] == "PersistentVolume"
                   and item["metadata"]["name"] == claim["spec"].get("volumeName")), {})
    if "hostPath" not in volume.get("spec", {}) or not volume["spec"].get("nodeAffinity", {}).get("required"):
        raise ValueError("the Runtime workspace requires a static HostPath volume with node affinity")
    resources = [item for item in documents if item["kind"] != "Deployment"
                 or item["metadata"]["name"] not in RUNTIME_IMAGES]
    return namespace, storage, workspace, resources


def configure_browser_origins(resources, namespace, origin, port_range):
    parsed = urlsplit(origin)
    low, high = (int(port) for port in port_range.split("-", 1))
    if (parsed.scheme != "https" or not parsed.hostname or not parsed.port or parsed.path
            or parsed.query or parsed.fragment or parsed.username or parsed.password):
        raise ValueError("browser origin must be an HTTPS origin with an explicit manager port")
    if not 30000 <= low <= high <= 32767 or high - low >= 512 or low <= parsed.port <= high:
        raise ValueError("browser port range must use at most 512 distinct NodePorts outside the manager port")
    app = next(item for item in resources if item["kind"] == "Deployment"
               and item["metadata"]["name"] == "clawmanager-app")
    container = app["spec"]["template"]["spec"]["containers"][0]
    config = {"CLAWMANAGER_BROWSER_ORIGIN": origin, "CLAWMANAGER_BROWSER_PORT_RANGE": port_range}
    container["env"] = [item for item in container["env"] if item["name"] not in config]
    container["env"].extend({"name": name, "value": value} for name, value in config.items())
    resources.append({
        "apiVersion": "v1", "kind": "Service",
        "metadata": {"name": "clawmanager-browser-origins", "namespace": namespace},
        "spec": {
            "type": "NodePort", "selector": {"app": "clawmanager-app"},
            "ports": [{"name": f"browser-{port}", "port": port, "targetPort": 8444, "nodePort": port}
                      for port in range(low, high + 1)],
        },
    })


@contextmanager
def migration_window(mysql):
    original = mysql("SELECT @@GLOBAL.log_bin_trust_function_creators;").strip()
    if original not in {"0", "1"}:
        raise ValueError("could not read original log_bin_trust_function_creators")
    try:
        if original == "0":
            mysql("SET GLOBAL log_bin_trust_function_creators = ON;")
        yield
    finally:
        if original == "0":
            mysql("SET GLOBAL log_bin_trust_function_creators = OFF;")
            if mysql("SELECT @@GLOBAL.log_bin_trust_function_creators;").strip() != original:
                raise RuntimeError("failed to restore log_bin_trust_function_creators")


def start_clawmanager(kube, mysql, bootstrap_sql, timeout):
    with migration_window(mysql):
        mysql(bootstrap_sql)
        kube("scale", "deployment/clawmanager-app", "--replicas=1")
        kube("rollout", "status", "deployment/clawmanager-app", f"--timeout={timeout}s")


def kubectl(context, *command, payload=None, capture=False, quiet_error=False):
    base = ["kubectl", "--context", context] if context else ["kubectl"]
    result = subprocess.run([*base, *command], check=True, input=payload, text=True,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if quiet_error else None)
    return result.stdout


def mysql_admin(kube, sql):
    try:
        return kube("exec", "-i", "deployment/mysql", "--", "sh", "-c",
                    'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --protocol=TCP -h127.0.0.1 -uroot -N -B',
                    payload=sql, capture=True, quiet_error=True)
    except subprocess.CalledProcessError as error:
        # Bootstrap SQL contains the new restore credential.
        raise RuntimeError("MySQL administrative step failed") from error


@contextmanager
def hostpath_preflight(resources, kube, namespace, timeout):
    app = next(item for item in resources if item["kind"] == "Deployment"
               and item["metadata"]["name"] == "clawmanager-app")
    env = {item["name"]: item.get("value") for item in app["spec"]["template"]["spec"]["containers"][0]["env"]}
    paths = [item["spec"]["hostPath"]["path"] for item in resources
             if item["kind"] == "PersistentVolume" and "hostPath" in item["spec"]]
    paths.append(env["K8S_PV_HOST_PATH_PREFIX"])
    clean_paths = [PurePosixPath(path) for path in paths]
    if any(str(clean) != original or not clean.is_absolute() or clean == PurePosixPath("/") or ".." in clean.parts
           for clean, original in zip(clean_paths, paths)):
        raise ValueError("HostPath install directories must be canonical absolute paths")
    storage = json.loads(kube("get", "pv", "-o", "json", capture=True))["items"]
    existing = [PurePosixPath(volume["spec"][source]["path"])
                for volume in storage for source in ("hostPath", "local") if source in volume["spec"]]
    for index, path in enumerate(clean_paths):
        if any(path == used or path in used.parents or used in path.parents
               for used in [*existing, *clean_paths[:index]]):
            raise ValueError(f"HostPath {path} overlaps another persistent volume or install path")

    redis = next(item for item in resources if item["kind"] == "Deployment"
                 and item["metadata"]["name"] == "clawmanager-team-redis")
    image = redis["spec"]["template"]["spec"]["containers"][0]["image"]
    volumes = [{"name": f"data-{index}", "hostPath": {"path": path, "type": "DirectoryOrCreate"}}
               for index, path in enumerate(paths)]
    pod = {
        "apiVersion": "v1", "kind": "Pod", "metadata": {"name": "clawmanager-install-storage-check"},
        "spec": {
            "restartPolicy": "Never", "automountServiceAccountToken": False, "hostUsers": False,
            "nodeSelector": {"clawmanager.io/storage-node": "true"},
            "volumes": volumes,
            "containers": [{
                "name": "check", "image": image, "imagePullPolicy": "IfNotPresent",
                "command": ["sh", "-ec",
                            'for dir in /check/*; do entries=$(ls -A "$dir"); '
                            'if [ -n "$entries" ]; then echo "Nonempty install path: $dir"; exit 1; fi; done'],
                "volumeMounts": [{"name": volume["name"], "mountPath": f"/check/{index}", "readOnly": True}
                                 for index, volume in enumerate(volumes)],
            }],
        },
    }
    resource = next(item for item in resources if item["kind"] == "Namespace")
    created = json.loads(kube("create", "-f", "-", "-o", "json", payload=json.dumps(resource), capture=True))
    scoped = partial(kube, "-n", namespace)
    try:
        try:
            scoped("create", "-f", "-", payload=json.dumps(pod))
            scoped("wait", "pod/clawmanager-install-storage-check", "--for=jsonpath={.status.phase}=Succeeded",
                   f"--timeout={timeout}s")
        except subprocess.CalledProcessError:
            print("Storage check paths: " + ", ".join(f"/check/{index}={path}" for index, path in enumerate(paths)),
                  file=sys.stderr)
            for command in (
                ("get", "pod/clawmanager-install-storage-check", "-o", "wide"),
                ("get", "events", "--field-selector", "involvedObject.name=clawmanager-install-storage-check"),
                ("logs", "pod/clawmanager-install-storage-check"),
            ):
                with suppress(subprocess.CalledProcessError):
                    scoped(*command, "--request-timeout=10s")
            raise
        scoped("delete", "pod/clawmanager-install-storage-check", "--ignore-not-found", "--timeout=30s")
        yield
    except (Exception, KeyboardInterrupt):
        options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": created["metadata"]["uid"]}}
        try:
            kube("delete", "--raw", f"/api/v1/namespaces/{namespace}", "-f", "-", "--request-timeout=30s",
                 payload=json.dumps(options), capture=True)
            kube("wait", "--for=delete", f"namespace/{namespace}", "--timeout=30s")
        except (OSError, subprocess.CalledProcessError) as error:
            print(f"Could not finish cleaning up preflight namespace {namespace}: {error}", file=sys.stderr)
        raise


def install(args):
    kube = partial(kubectl, args.context)
    documents = list(manifest_documents(kube("create", "--dry-run=client", "--validate=false", "-o", "json",
                                            "-f", str(args.manifest), capture=True)))
    images = json.loads(args.images_file.read_text()) if args.images_file else {}
    if not isinstance(images, dict) or any(not isinstance(ref, str) or not ref for ref in images.values()):
        raise ValueError("--images-file must contain a JSON object mapping deployments to image references")
    images.update(item.split("=", 1) for item in args.image)
    namespace, storage, workspace, resources = single_node_resources(documents, images, args.slots)
    if args.browser_origin:
        configure_browser_origins(resources, namespace, args.browser_origin, args.browser_ports)
    if kube("get", "namespace", namespace, "--ignore-not-found", "-o", "name", capture=True).strip():
        raise ValueError(f"namespace {namespace} already exists; select a fresh installation")
    private = args.credentials_dir
    if private.exists() or private.is_symlink():
        raise ValueError(f"credentials directory {private} already exists; select a new directory")
    nodes = json.loads(kube("get", "nodes", "-o", "json", capture=True))["items"]
    if (len(nodes) != 1 or nodes[0]["metadata"].get("labels", {}).get("clawmanager.io/storage-node") != "true"
            or not any(item["type"] == "Ready" and item["status"] == "True"
                       for item in nodes[0].get("status", {}).get("conditions", []))):
        raise ValueError("single-node install needs one Ready node labelled clawmanager.io/storage-node=true")
    version = json.loads(kube("version", "-o", "json", capture=True))["serverVersion"]["gitVersion"]
    release = re.match(r"v(\d+)\.(\d+)", version)
    if release is None or tuple(map(int, release.groups())) < (1, 33):
        raise ValueError(f"HostPath slots with Pod user namespaces require Kubernetes >= v1.33 (server {version})")
    node = nodes[0]["metadata"]["name"]
    config = json.loads(kube("get", "--raw", f"/api/v1/nodes/{node}/proxy/configz", capture=True))["kubeletconfig"]
    ids = config.get("userNamespaces", {}).get("idsPerPod", 65536)
    if ids < 262144:
        raise ValueError("kubelet userNamespaces.idsPerPod must cover Runtime IDs starting at 200000; set 1048576")

    for item in resources:
        if item["kind"] in {"PersistentVolume", "ClusterRole", "ClusterRoleBinding"}:
            name = item["metadata"]["name"]
            if kube("get", item["kind"], name, "--ignore-not-found", "-o", "name", capture=True).strip():
                raise ValueError(f"{item['kind']} {name} already exists; use isolated resource names")
    existing = kube("get", "storageclass", storage, "--ignore-not-found", "-o", "json", capture=True)
    if existing:
        if json.loads(existing)["provisioner"] != "kubernetes.io/no-provisioner":
            raise ValueError(f"storage class {storage} must use kubernetes.io/no-provisioner")
    else:
        resources.append({
            "apiVersion": "storage.k8s.io/v1", "kind": "StorageClass", "metadata": {"name": storage},
            "provisioner": "kubernetes.io/no-provisioner", "reclaimPolicy": "Retain",
        })

    audit = private / "audit.json"
    history = private / "history"
    with hostpath_preflight(resources, kube, namespace, args.timeout):
        private.mkdir(mode=0o700)
        try:
            subprocess.run([sys.executable, str(TOOLS / "observability/prepare-observability-secret.py"),
                            "--initialize", "--key-file", str(private / "audit.key"), "--output", str(audit)], check=True)
            subprocess.run([sys.executable, str(TOOLS / "observability/prepare-history-restore.py"),
                            "--output", str(history), "--host", f"mysql.{namespace}.svc.cluster.local",
                            "--tls", "skip-verify"], check=True)
            for path in (audit, history / "secret.json"):
                secret = json.loads(path.read_text())
                secret["metadata"]["namespace"] = namespace
                path.write_text(json.dumps(secret) + "\n")
                resources.append(secret)

            resources = [item for item in resources if item["kind"] != "Namespace"]
            payload = json.dumps({"apiVersion": "v1", "kind": "List", "items": resources})
            kube("create", "--dry-run=server", "-f", "-", payload=payload)
        except (Exception, KeyboardInterrupt):
            try:
                rmtree(private)
            except OSError as error:
                print(f"Could not finish removing staged credentials at {private}: {error}", file=sys.stderr)
            raise

    # A failed live create may still have persisted Secrets or other resources.
    kube("create", "-f", "-", payload=payload)

    ns = partial(kube, "-n", namespace)
    mysql = partial(mysql_admin, ns)

    for dependency in ("mysql", "minio", "clawmanager-team-redis"):
        ns("rollout", "status", f"deployment/{dependency}", f"--timeout={args.timeout}s")
    ns("wait", f"pvc/{workspace}", "--for=jsonpath={.status.phase}=Bound", f"--timeout={args.timeout}s")
    start_clawmanager(ns, mysql, (history / "bootstrap.sql").read_text(), args.timeout)
    for runtime in RUNTIME_IMAGES:
        ns("rollout", "status", f"deployment/{runtime}", f"--timeout={args.timeout}s")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--credentials-dir", type=Path, required=True, help="new directory for private recovery credentials")
    parser.add_argument("--context", help="kubectl context (defaults to current context)")
    parser.add_argument("--slots", type=int, default=1, choices=range(1, 257), metavar="1..256")
    parser.add_argument("--timeout", type=int, default=300, help="seconds per rollout or wait")
    parser.add_argument("--images-file", type=Path, help="JSON object mapping deployments to image references")
    parser.add_argument("--image", action="append", default=[], metavar="DEPLOYMENT=IMAGE")
    parser.add_argument("--browser-origin", help="manager HTTPS origin when using dedicated browser NodePorts")
    parser.add_argument("--browser-ports", metavar="FIRST-LAST")
    args = parser.parse_args()
    if args.timeout <= 0 or any("=" not in item or not all(item.split("=", 1)) for item in args.image):
        parser.error("positive timeout and DEPLOYMENT=IMAGE overrides are required")

    if bool(args.browser_origin) != bool(args.browser_ports):
        parser.error("--browser-origin and --browser-ports are required together")

    def interrupt(_signal, _frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupt)
    try:
        install(args)
    except (ValueError, RuntimeError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Installation failed: {error}\n")
    except KeyboardInterrupt:
        parser.exit(130, "Installation interrupted. Inspect the namespace before retrying.\n")


if __name__ == "__main__":
    main()
