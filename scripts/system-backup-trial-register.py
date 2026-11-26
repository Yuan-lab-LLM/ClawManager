#!/usr/bin/env python3
"""Render the registered trial resources and wire an observed application Deployment."""

import argparse
import ipaddress
import json
from pathlib import Path
import re
import sys


TEMPLATE = Path(__file__).resolve().parents[1] / "deployments/k8s/system-backup-trial-prepare.yaml.template"
REVISION_ANNOTATION = "clawmanager.io/trial-environment-revision"
MATERIAL_ANNOTATION = "clawmanager.io/trial-material-namespace"
COMMAND = "/usr/local/bin/system-backup-trial-prepare"
APP_MATERIALS = "/var/run/clawmanager-trial"
IMPORTER_CONTAINER = "trial-artifact-import"
HERMES_SOURCE_CONTAINER = "trial-hermes-source"
HERMES_SOURCE_WORKSPACE = "trial-hermes-workspace"
HERMES_SOURCE_SOCKET_DIR = "/run/clawmanager-trial/hermes-source"
SOURCE_WORKSPACE_SELECTION = "trial-app-workspace-selection"
TRIAL_ENV = "SYSTEM_BACKUP_TRIAL_"
PLACEHOLDER = re.compile(r"\$\{([A-Z][A-Z0-9_]*)\}")
WORKSPACE_BLOCK = re.compile(r"^ *# trial-workspace-begin\r?\n.*?^ *# trial-workspace-end\r?\n", re.MULTILINE | re.DOTALL)
LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?")
IMAGE = re.compile(r"[a-z0-9][a-z0-9./:_-]{0,254}@sha256:[a-f0-9]{64}")
UUID = re.compile(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}")
SHA256 = re.compile(r"[a-f0-9]{64}")
DATABASE = re.compile(r"[a-z][a-z0-9_]{0,63}")

PLACEMENT = {"cluster", "namespace", "image", "service_account", "artifact_claim", "key_secret"}
OPTIONAL_PLACEMENT = {"artifact_storage_class", "workspace_claim", "workspace_storage_class", "instance_storage_class"}
ACCOUNT_FIELDS = {
    "source": {"source_read_secret"},
    "target": {"restore_write_secret", "restore_read_secret"},
}
PREPARATION = {
    "revision", "job_name", "service_account", "profile_configmap", "database", "server_uuid",
    "account_host", "owner_dsn_secret", "ca_configmap", "run_as_user", "run_as_group", "volume_group",
}


def unique_object(pairs):
    result = dict(pairs)
    if len(result) != len(pairs):
        raise ValueError("duplicate JSON field")
    return result


def load_json(path):
    limit = 1 << 20
    if path == "-":
        raw = sys.stdin.buffer.read(limit + 1)
    else:
        with Path(path).open("rb") as stream:
            raw = stream.read(limit + 1)
    if len(raw) > limit:
        raise ValueError("registration input exceeds the size limit")
    try:
        return json.loads(raw.decode("utf-8-sig"), object_pairs_hook=unique_object)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError("registration input must be UTF-8 JSON") from exc


def require_fields(obj, required, optional, label):
    if not isinstance(obj, dict) or not required <= obj.keys() or obj.keys() - required - optional:
        raise ValueError(f"missing or unexpected fields in {label}")


def matches(value, pattern):
    return isinstance(value, str) and pattern.fullmatch(value) is not None


def dns_name(value):
    return isinstance(value, str) and len(value) <= 253 and all(matches(part, LABEL) for part in value.split("."))


def mysql_host(value):
    if value in ("%", "localhost"):
        return True
    if not isinstance(value, str) or "%" in value:
        return False
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return False
    return True


def output_secrets(placement, side):
    roles = ["source_read_secret"] if side == "source" else ["restore_write_secret", "restore_read_secret", "runtime_secret"]
    return [placement["key_secret"]] + [placement[role] for role in roles if role in placement]


def valid_workspace_root(value):
    return (isinstance(value, str) and 0 < len(value.encode("utf-8")) <= 240 and value != "home" and
            not any(ch in value for ch in "\\:\x00\r\n") and
            all(part and not part.startswith(".") for part in value.split("/")))


def validate(config):
    require_fields(config, {"migration_catalog_sha256", "target_isolation_confirmed", "source", "target"}, set(), "registration")
    if not matches(config["migration_catalog_sha256"], SHA256) or config["target_isolation_confirmed"] is not True:
        raise ValueError("a pinned migration catalog and confirmed target isolation are required")
    for side, accounts in ACCOUNT_FIELDS.items():
        placement = config[side]
        required = PLACEMENT | accounts | {"prepare"}
        optional = OPTIONAL_PLACEMENT.copy()
        if side == "source":
            optional.update({"hermes_history", "workspace_files"})
        if side == "target":
            required.add("instance_storage_class")
            optional.add("runtime_secret")
        require_fields(placement, required, optional, side)
        if not matches(placement["namespace"], LABEL) or not matches(placement["image"], IMAGE):
            raise ValueError(f"invalid namespace or unpinned image in {side}")
        if any(not dns_name(value) for key, value in placement.items() if key not in {"namespace", "image", "prepare", "hermes_history", "workspace_files"}):
            raise ValueError(f"invalid resource name in {side}")
        if ("workspace_storage_class" in placement and "workspace_claim" not in placement) or placement.get("workspace_claim") == placement["artifact_claim"]:
            raise ValueError(f"invalid workspace and artifact claims in {side}")
        if "hermes_history" in placement:
            hermes = placement["hermes_history"]
            require_fields(hermes, {"image", "script_configmap", "user_id", "instance_id", "run_as_user"}, set(), "source.hermes_history")
            if (not placement.get("workspace_claim") or not matches(hermes["image"], IMAGE) or
                    not dns_name(hermes["script_configmap"]) or
                    any(type(hermes[key]) is not int or not 0 < hermes[key] < 2**31 for key in ("user_id", "instance_id", "run_as_user"))):
                raise ValueError("Hermes source requires a registered workspace snapshot, pinned image, and positive identities")
        if "workspace_files" in placement:
            workspace = placement["workspace_files"]
            require_fields(workspace, {"user_id", "instance_id", "runtime", "relative_root"}, set(), "source.workspace_files")
            if (not placement.get("workspace_claim") or not valid_workspace_root(workspace["relative_root"]) or
                    workspace["runtime"] not in ("hermes", "openclaw", "opencode", "deepseek-harness") or
                    any(type(workspace[key]) is not int or not 0 < workspace[key] < 2**31 for key in ("user_id", "instance_id"))):
                raise ValueError("workspace file selection requires a registered snapshot, instance and approved relative root")
        job = placement["prepare"]
        require_fields(job, PREPARATION, set(), f"{side}.prepare")
        if not matches(job["revision"], LABEL) or not matches(job["job_name"], LABEL):
            raise ValueError(f"invalid Job name or revision in {side}")
        if any(not dns_name(job[key]) for key in ("service_account", "profile_configmap", "owner_dsn_secret", "ca_configmap")):
            raise ValueError(f"invalid preparation resource name in {side}")
        if "hermes_history" in placement and placement["hermes_history"]["script_configmap"] in {job["profile_configmap"], job["ca_configmap"]}:
            raise ValueError("the Hermes collector script requires a dedicated ConfigMap")
        if not matches(job["database"], DATABASE) or not matches(job["server_uuid"], UUID) or not mysql_host(job["account_host"]):
            raise ValueError(f"invalid MySQL identity in {side}")
        if any(type(job[key]) is not int or not 0 < job[key] < 2**31 for key in ("run_as_user", "run_as_group", "volume_group")):
            raise ValueError(f"invalid non-root IDs in {side}")
        outputs = output_secrets(placement, side)
        if job["service_account"] == placement["service_account"] or len(set(outputs)) != len(outputs) or job["owner_dsn_secret"] in outputs:
            raise ValueError(f"preparation and application identities or Secrets overlap in {side}")
    source, target = config["source"], config["target"]
    if not target["namespace"].startswith("cm-drill-") or not target["prepare"]["database"].startswith("cm_drill_trial_"):
        raise ValueError("the target namespace and database must be trial-isolated")
    if source["prepare"]["server_uuid"] == target["prepare"]["server_uuid"]:
        raise ValueError("the source and target MySQL UUIDs must differ")
    if source["cluster"] == target["cluster"] and source["namespace"] == target["namespace"]:
        raise ValueError("source and target namespaces must differ on the same cluster")


def template_inputs(config, side):
    local, source, target = config[side], config["source"], config["target"]
    job = local["prepare"]
    claims = [local["artifact_claim"]]
    if "workspace_claim" in local:
        claims.append(local["workspace_claim"])
    return {
        "NAMESPACE": local["namespace"], "APP_IMAGE_DIGEST": local["image"],
        "APP_SERVICE_ACCOUNT": local["service_account"], "PREPARE_SERVICE_ACCOUNT": job["service_account"],
        "PREPARE_JOB_NAME": job["job_name"], "PROFILE_REVISION": job["revision"],
        "LOCAL_OUTPUT_SECRET_NAMES_JSON": json.dumps(output_secrets(local, side)),
        "LOCAL_PVC_NAMES_JSON": json.dumps(claims),
        "PREPARE_UID": str(job["run_as_user"]), "PREPARE_GID": str(job["run_as_group"]),
        "TRIAL_VOLUME_GID": str(job["volume_group"]), "MYSQL_DATABASE": job["database"],
        "MYSQL_ACCOUNT_HOST": job["account_host"], "MIGRATION_CATALOG_SHA256": config["migration_catalog_sha256"],
        "SOURCE_MYSQL_UUID": source["prepare"]["server_uuid"], "TARGET_MYSQL_UUID": target["prepare"]["server_uuid"],
        "TARGET_NAMESPACE": target["namespace"], "TARGET_INSTANCE_STORAGE_CLASS": target["instance_storage_class"],
        "TARGET_ISOLATION_CONFIRMED": "true", "ARCHIVE_WRITE_MOUNT_READ_ONLY": "false" if side == "source" else "true",
        "WORKSPACE_MOUNT_READ_ONLY": "true" if side == "source" else "false",
        "ENVIRONMENT_PROFILE_CONFIGMAP": job["profile_configmap"], "MYSQL_OWNER_DSN_SECRET": job["owner_dsn_secret"],
        "MYSQL_CA_CONFIGMAP": job["ca_configmap"], "ARTIFACT_PVC": local["artifact_claim"],
        "WORKSPACE_PVC": local.get("workspace_claim", ""),
    }


def render_template(template, values, workspace):
    if len(WORKSPACE_BLOCK.findall(template)) != 3:
        raise ValueError("unexpected workspace blocks in preparation template")
    if not workspace:
        template = WORKSPACE_BLOCK.sub("", template)
    names = set(PLACEHOLDER.findall(template))
    expected = values.keys() if workspace else values.keys() - {"WORKSPACE_MOUNT_READ_ONLY", "WORKSPACE_PVC"}
    if names != expected:
        raise ValueError("preparation template and renderer variables do not match")
    rendered = PLACEHOLDER.sub(lambda match: values[match[1]], template)
    if "${" in rendered:
        raise ValueError("unresolved preparation template placeholder")
    return rendered.rstrip() + "\n"


def resource(api_version, kind, namespace, name, **fields):
    return {"apiVersion": api_version, "kind": kind, "metadata": {"namespace": namespace, "name": name}, **fields}


def registration_manifests(config, side):
    source, target = config["source"], config["target"]
    values = template_inputs(config, side)
    rendered = render_template(TEMPLATE.read_text(encoding="utf-8"), values, "workspace_claim" in config[side])
    if side == "source" and source["cluster"] == target["cluster"]:
        api = "rbac.authorization.k8s.io/v1"
        name = source["prepare"]["job_name"] + "-target-key"
        role = resource(api, "Role", source["namespace"], name, rules=[{
            "apiGroups": [""], "resources": ["secrets"], "resourceNames": [source["key_secret"]], "verbs": ["get"],
        }])
        binding = resource(api, "RoleBinding", source["namespace"], name,
                           roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": name},
                           subjects=[{"kind": "ServiceAccount", "name": target["prepare"]["service_account"], "namespace": target["namespace"]}])
        rendered += "".join("---\n" + json.dumps(obj, indent=2) + "\n" for obj in (role, binding))
    return rendered


def placeholders(config, side):
    local = config[side]
    items = []
    for name in output_secrets(local, side):
        secret = resource("v1", "Secret", local["namespace"], name, type="Opaque")
        secret["metadata"]["annotations"] = {MATERIAL_ANNOTATION: local["namespace"]}
        items.append(secret)
    return {"apiVersion": "v1", "kind": "List", "items": items}


def bind_profile(config, side, observed):
    local = config[side]
    job = local["prepare"]
    values = template_inputs(config, side)
    try:
        metadata = observed["metadata"]
        pod = observed["spec"]["template"]["spec"]
        container, = pod["containers"]
        security = pod["securityContext"]
        worker_security = container["securityContext"]
        env = {item["name"]: item.get("value") for item in container["env"]}
        volumes = {item["name"]: item for item in pod["volumes"]}
        mounts = {item["mountPath"]: (item["name"], item.get("readOnly", False)) for item in container["volumeMounts"]}
        uid = metadata["uid"]
        env_variables = {
            "PREPARE_DATABASE": "MYSQL_DATABASE", "PREPARE_ACCOUNT_HOST": "MYSQL_ACCOUNT_HOST",
            "PREPARE_MIGRATION_CATALOG_SHA256": "MIGRATION_CATALOG_SHA256", "SOURCE_UUID": "SOURCE_MYSQL_UUID",
            "RESTORE_UUID": "TARGET_MYSQL_UUID", "RESTORE_NAMESPACE": "TARGET_NAMESPACE",
            "RESTORE_STORAGE_CLASS": "TARGET_INSTANCE_STORAGE_CLASS", "RESTORE_ISOLATED": "TARGET_ISOLATION_CONFIRMED",
        }
        expected_env = {"SYSTEM_BACKUP_TRIAL_" + name: values[key] for name, key in env_variables.items()}
        expected_env.update({
            "SYSTEM_BACKUP_TRIAL_ENVIRONMENT_PROFILE_FILE": "/trial/profile/environment.json",
            "SYSTEM_BACKUP_TRIAL_PREPARE_OWNER_DSN_FILE": "/trial/owner/dsn",
            "SYSTEM_BACKUP_TRIAL_READ_DIR": "/trial/archive-read",
            "SYSTEM_BACKUP_TRIAL_WRITE_DIR": "/trial/archive-write",
            "SSL_CERT_FILE": "/trial/ca/ca-bundle.crt",
        })
        expected_mounts = {
            "/trial/profile": ("profile", True), "/trial/owner": ("owner", True), "/trial/ca": ("database-ca", True),
            "/trial/archive-read": ("archive", True), "/trial/archive-write": ("archive", side == "target"),
        }
        if "workspace_claim" in local:
            expected_env["SYSTEM_BACKUP_TRIAL_PREPARE_WORKSPACE_DIR"] = "/trial/workspace"
            expected_mounts["/trial/workspace"] = ("workspace", side == "source")
            if volumes["workspace"]["persistentVolumeClaim"]["claimName"] != local["workspace_claim"]:
                raise ValueError("unregistered workspace claim")
        if (observed["apiVersion"] != "batch/v1" or observed["kind"] != "Job" or metadata["name"] != job["job_name"]
                or metadata["namespace"] != local["namespace"] or metadata["annotations"][REVISION_ANNOTATION] != job["revision"]
                or not matches(uid, UUID) or metadata.get("deletionTimestamp")):
            raise ValueError("unregistered Job identity")
        if (pod["serviceAccountName"] != job["service_account"] or pod.get("initContainers")
                or security.get("runAsNonRoot") is not True or security.get("runAsUser") != job["run_as_user"]
                or security.get("runAsGroup") != job["run_as_group"] or security.get("fsGroup") != job["volume_group"]
                or worker_security.get("allowPrivilegeEscalation") is not False or worker_security.get("readOnlyRootFilesystem") is not True
                or container["image"] != local["image"] or container["command"] != [COMMAND] or container.get("args")
                or env != expected_env or len(env) != len(container["env"])):
            raise ValueError("unregistered Job execution")
        if (set(volumes) != {name for name, _ in expected_mounts.values()} or len(volumes) != len(pod["volumes"])
                or mounts != expected_mounts or len(mounts) != len(container["volumeMounts"])
                or any(item.get("subPath") or item.get("subPathExpr") for item in container["volumeMounts"])
                or volumes["profile"]["configMap"]["name"] != job["profile_configmap"]
                or volumes["owner"]["secret"]["secretName"] != job["owner_dsn_secret"]
                or volumes["owner"]["secret"]["defaultMode"] != 0o440
                or volumes["database-ca"]["configMap"]["name"] != job["ca_configmap"]
                or volumes["archive"]["persistentVolumeClaim"]["claimName"] != local["artifact_claim"]):
            raise ValueError("unregistered Job mounts")
    except (AttributeError, KeyError, TypeError, ValueError) as exc:
        raise ValueError("observed Job does not match the registered preparation") from exc
    result = {"revision": job["revision"], "side": side, "prepare_job": {"name": job["job_name"], "uid": uid}}
    for endpoint in ("source", "target"):
        result[endpoint] = {key: value for key, value in config[endpoint].items() if key != "prepare"}
    return resource("v1", "ConfigMap", local["namespace"], job["profile_configmap"], data={"environment.json": json.dumps(result, indent=2) + "\n"})


def app_materials(local, side, hermes_workspace=None, artifact_import=False):
    preparation = local["prepare"]
    volumes, mounts = [], []

    def attach(name, location, volume, read_only=True):
        volumes.append({"name": name, **volume})
        mounts.append({"name": name, "mountPath": APP_MATERIALS + "/" + location, **({"readOnly": True} if read_only else {})})

    def config_map(name, location, config_name, key):
        attach(name, location, {"configMap": {
            "name": config_name, "defaultMode": 0o644, "items": [{"key": key, "path": key}],
        }})
        return APP_MATERIALS + "/" + location + "/" + key

    def secret(name, location, secret_name, key):
        # Empty placeholders let the application start and activate the Job.
        # Whole directory mounts receive subsequent kubelet Secret updates.
        attach(name, location, {"secret": {
            "secretName": secret_name, "optional": True, "defaultMode": 0o440, "items": [{"key": key, "path": key}],
        }})
        return APP_MATERIALS + "/" + location + "/" + key

    profile = config_map("trial-app-profile", "profile", preparation["profile_configmap"], "environment.json")
    ca = config_map("trial-app-ca", "ca", preparation["ca_configmap"], "ca-bundle.crt")
    key = secret("trial-app-key", "key", local["key_secret"], "key")
    claim = {"claimName": local["artifact_claim"]}
    if side == "target" and not artifact_import:
        claim["readOnly"] = True
    archive_volume = "trial-import-write" if artifact_import else "trial-app-archive"
    attach(archive_volume, "archive-read", {"persistentVolumeClaim": claim})

    settings = {
        "ENVIRONMENT_PROFILE_FILE": profile, "KEY_FILE": key, "READ_DIR": APP_MATERIALS + "/archive-read",
        "SOURCE_DSN_FILE": "", "SOURCE_UUID": "", "WRITE_DIR": "",
        "RESTORE_DSN_FILE": "", "RESTORE_READ_DSN_FILE": "", "RESTORE_UUID": "",
        "RESTORE_ISOLATED": "", "RESTORE_NAMESPACE": "", "RESTORE_STORAGE_CLASS": "",
        "RUNTIME_BACKUP_ID": "", "RUNTIME_DSN_FILE": "", "PREPARE_OWNER_DSN_FILE": "",
    }
    if side == "source":
        mounts.append({"name": "trial-app-archive", "mountPath": APP_MATERIALS + "/archive-write"})
        settings.update(
            SOURCE_UUID=preparation["server_uuid"], WRITE_DIR=APP_MATERIALS + "/archive-write",
            SOURCE_DSN_FILE=secret("trial-app-source-reader", "source-reader", local["source_read_secret"], "dsn"),
        )
        if "workspace_files" in local:
            scope = local["workspace_files"]
            workspace = hermes_workspace or SOURCE_WORKSPACE_SELECTION
            if hermes_workspace is None:
                volumes.append({"name": workspace, "persistentVolumeClaim": {
                    "claimName": local["workspace_claim"], "readOnly": True,
                }})
            mounts.append({
                "name": workspace, "mountPath": APP_MATERIALS + "/workspace-selection", "readOnly": True,
                "subPath": f'{scope["runtime"]}/user-{scope["user_id"]}/instance-{scope["instance_id"]}/{scope["relative_root"]}',
            })
            settings["WORKSPACE_FILES_SELECTION_ROOT"] = APP_MATERIALS + "/workspace-selection"
    else:
        settings.update(
            RESTORE_UUID=preparation["server_uuid"], RESTORE_ISOLATED="true",
            RESTORE_NAMESPACE=local["namespace"], RESTORE_STORAGE_CLASS=local["instance_storage_class"],
            RESTORE_DSN_FILE=secret("trial-app-writer", "writer", local["restore_write_secret"], "dsn"),
            RESTORE_READ_DSN_FILE=secret("trial-app-reader", "reader", local["restore_read_secret"], "dsn"),
        )
        if "workspace_claim" in local:
            attach("trial-app-workspace", "workspace", {
                "persistentVolumeClaim": {"claimName": local["workspace_claim"]},
            }, read_only=False)
            settings["WORKSPACE_FILES_TARGET_ROOT"] = APP_MATERIALS + "/workspace"
    env = [{"name": TRIAL_ENV + name, "value": value} for name, value in settings.items()]
    env.append({"name": "SSL_CERT_FILE", "value": ca})
    return volumes, mounts, env


def patch_list(patch, parent, field, path, entries, key):
    observed = parent.get(field, [])
    if not isinstance(observed, list):
        raise ValueError(f"invalid observed Deployment {field}")
    additions = []
    for entry in entries:
        candidates = [item for item in observed if item.get(key) == entry[key]]
        if not candidates:
            additions.append(entry)
        elif candidates != [entry] and not (
            field == "env" and entry.get("value") == "" and candidates == [{"name": entry["name"]}]
        ):
            # The Kubernetes API omits the value field for explicitly empty environment values.
            raise ValueError(f"conflicting Deployment {field} entry {entry[key]}")
    if field not in parent and additions:
        patch.append({"op": "add", "path": path, "value": additions})
    else:
        patch.extend({"op": "add", "path": path + "/-", "value": item} for item in additions)


def artifact_import_materials(local):
    preparation = local["prepare"]
    socket_dir = APP_MATERIALS + "/import-socket"
    socket = socket_dir + "/delivery.sock"
    write_dir = APP_MATERIALS + "/import-write"
    volumes = [
        {"name": "trial-import-socket", "emptyDir": {}},
        {"name": "trial-import-no-token", "emptyDir": {}},
    ]
    socket_mount = {"name": "trial-import-socket", "mountPath": socket_dir}
    settings = {
        "ENVIRONMENT_PROFILE_FILE": APP_MATERIALS + "/profile/environment.json",
        "RESTORE_NAMESPACE": local["namespace"],
        "RESTORE_STORAGE_CLASS": local["instance_storage_class"],
        "IMPORT_SOCKET": socket,
        "IMPORT_WRITE_DIR": write_dir,
    }
    importer = {
        "name": IMPORTER_CONTAINER, "image": local["image"], "imagePullPolicy": "IfNotPresent",
        "command": ["/usr/local/bin/system-backup-trial-import"],
        "terminationMessagePath": "/dev/termination-log", "terminationMessagePolicy": "File", "resources": {},
        "securityContext": {
            "runAsNonRoot": True, "runAsUser": preparation["run_as_user"],
            "runAsGroup": preparation["volume_group"], "allowPrivilegeEscalation": False,
            "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]},
            "seccompProfile": {"type": "RuntimeDefault"},
        },
        "volumeMounts": [
            socket_mount,
            {"name": "trial-app-profile", "mountPath": APP_MATERIALS + "/profile", "readOnly": True},
            {"name": "trial-import-write", "mountPath": write_dir},
            {"name": "trial-import-no-token", "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount", "readOnly": True},
        ],
        "env": [{"name": TRIAL_ENV + name, "value": value} for name, value in settings.items()],
    }
    return volumes, {**socket_mount, "readOnly": True}, {"name": TRIAL_ENV + "IMPORT_SOCKET", "value": socket}, importer


def hermes_source_script(local):
    hermes = local.get("hermes_history")
    if hermes is None:
        raise ValueError("Hermes source is not registered")
    script = Path(__file__).with_name("system-backup-trial-hermes-history.py").read_text(encoding="utf-8")
    return resource("v1", "ConfigMap", local["namespace"], hermes["script_configmap"],
                    immutable=True, data={"history.py": script})


def hermes_source_materials(local, workspace, socket_dir):
    hermes = local["hermes_history"]
    root, scratch = "/workspaces", "/trial-snapshot"
    socket_mount = {"name": "trial-hermes-socket", "mountPath": socket_dir}
    address = socket_mount["mountPath"] + "/history.sock"
    volumes = [
        {"name": "trial-hermes-socket", "emptyDir": {}},
        {"name": "trial-hermes-scratch", "emptyDir": {"sizeLimit": "1100Mi"}},
        {"name": "trial-hermes-no-token", "emptyDir": {}},
        {"name": "trial-hermes-script", "configMap": {"name": hermes["script_configmap"], "defaultMode": 0o444,
                                                      "items": [{"key": "history.py", "path": "history.py"}]}},
        {"name": workspace, "persistentVolumeClaim": {"claimName": local["workspace_claim"], "readOnly": True}},
    ]
    container = {
        "name": HERMES_SOURCE_CONTAINER, "image": hermes["image"], "imagePullPolicy": "IfNotPresent",
        "command": ["/opt/hermes-agent/.venv/bin/python", "-B", "/trial-script/history.py"],
        "args": ["serve-source", "--workspace-root", root, "--socket", address, "--snapshot-dir", scratch,
                 "--user-id", str(hermes["user_id"]), "--instance-id", str(hermes["instance_id"])],
        "env": [{"name": "PYTHONPATH", "value": "/opt/hermes-agent"}],
        "terminationMessagePath": "/dev/termination-log", "terminationMessagePolicy": "File", "resources": {},
        "securityContext": {
            "runAsNonRoot": True, "runAsUser": hermes["run_as_user"], "runAsGroup": local["prepare"]["volume_group"],
            "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
            "capabilities": {"drop": ["ALL"]}, "seccompProfile": {"type": "RuntimeDefault"},
        },
        "volumeMounts": [socket_mount,
            {"name": "trial-hermes-scratch", "mountPath": scratch},
            {"name": workspace, "mountPath": root, "readOnly": True},
            {"name": "trial-hermes-script", "mountPath": "/trial-script", "readOnly": True},
            {"name": "trial-hermes-no-token", "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount", "readOnly": True},
        ],
    }
    extras = {"HERMES_HISTORY_SOURCE_SOCKET": address, "HERMES_HISTORY_SOURCE_ROOT": root,
              "HERMES_HISTORY_SOURCE_USER_ID": str(hermes["user_id"]), "HERMES_HISTORY_SOURCE_INSTANCE_ID": str(hermes["instance_id"])}
    return volumes, {**socket_mount, "readOnly": True}, [{"name": TRIAL_ENV + key, "value": value} for key, value in extras.items()], container


def replace_legacy_target_archive(patch, pod, app, template, selected, claim):
    # Older targets declared a second, read-only volume for the same claim.
    legacy = {"name": "trial-app-archive", "persistentVolumeClaim": {"claimName": claim, "readOnly": True}}
    archive_mount = {"name": legacy["name"], "mountPath": APP_MATERIALS + "/archive-read", "readOnly": True}
    volumes = pod["volumes"].copy()
    mounts = app.get("volumeMounts", []).copy()
    volume_matches = [(i, v) for i, v in enumerate(volumes) if v["name"] == legacy["name"]]
    mount_matches = [(i, m) for i, m in enumerate(mounts) if m["name"] == legacy["name"]]
    if (len(volume_matches) != 1 or volume_matches[0][1] != legacy
            or len(mount_matches) != 1 or mount_matches[0][1] != archive_mount):
        raise ValueError("observed target Deployment has a conflicting legacy archive layout")
    volume_index = volume_matches[0][0]
    mount_index = mount_matches[0][0]
    volumes.pop(volume_index)
    mounts[mount_index] = {**archive_mount, "name": "trial-import-write"}
    patch.extend([
        {"op": "remove", "path": f"{template}/volumes/{volume_index}"},
        {"op": "replace", "path": f"{selected}/volumeMounts/{mount_index}/name", "value": "trial-import-write"},
    ])
    return {**pod, "volumes": volumes}, {**app, "volumeMounts": mounts}


def app_patch(config, side, observed, container_name, artifact_import=False):
    local = config[side]
    if artifact_import and (side != "target" or container_name == IMPORTER_CONTAINER):
        raise ValueError("artifact import requires a separate container in the target Deployment")
    try:
        metadata = observed["metadata"]
        pod = observed["spec"]["template"]["spec"]
        containers = pod["containers"]
        matches_container = [index for index, item in enumerate(containers) if item["name"] == container_name]
        if len(matches_container) != 1:
            raise ValueError("expected exactly one application container with the selected name")
        index = matches_container[0]
        container = containers[index]
        if (observed["apiVersion"] != "apps/v1" or observed["kind"] != "Deployment"
                or metadata["namespace"] != local["namespace"] or not dns_name(metadata["name"])
                or not matches(metadata["uid"], UUID) or not isinstance(metadata["resourceVersion"], str)
                or not metadata["resourceVersion"] or metadata.get("deletionTimestamp")
                or pod["serviceAccountName"] != local["service_account"]
                or pod.get("automountServiceAccountToken") is False or container["image"] != local["image"]):
            raise ValueError("observed Deployment namespace, service account or pinned image differs")
        receiver = None
        collector = None
        hermes_workspace = None
        history_mount = None
        if artifact_import:
            if pod.get("securityContext", {}).get("fsGroup") != local["prepare"]["volume_group"] or (
                pod.get("hostPID") or pod.get("shareProcessNamespace")
            ):
                raise ValueError("artifact import expects the registered Pod fsGroup and separate process namespaces")
            import_volumes, socket_mount, socket_env, receiver = artifact_import_materials(local)
        if side == "source" and "hermes_history" in local:
            if (container_name == HERMES_SOURCE_CONTAINER or pod.get("securityContext", {}).get("fsGroup") != local["prepare"]["volume_group"] or
                    pod.get("hostPID") or pod.get("shareProcessNamespace")):
                raise ValueError("Hermes capture expects the registered Pod fsGroup and a separate process namespace")
            hermes_workspace = HERMES_SOURCE_WORKSPACE
            workspace_volumes = {v["name"] for v in pod.get("volumes", [])
                                 if v.get("persistentVolumeClaim", {}).get("claimName") == local["workspace_claim"]}
            if "workspace_files" in local and workspace_volumes == {SOURCE_WORKSPACE_SELECTION}:
                hermes_workspace = SOURCE_WORKSPACE_SELECTION
            legacy_socket = {"name": "trial-hermes-socket", "mountPath": APP_MATERIALS + "/hermes-source", "readOnly": True}
            socket_dir = legacy_socket["mountPath"] if legacy_socket in container.get("volumeMounts", []) else HERMES_SOURCE_SOCKET_DIR
            history_volumes, history_mount, history_env, collector = hermes_source_materials(local, hermes_workspace, socket_dir)
        volumes, mounts, env = app_materials(local, side, hermes_workspace, artifact_import)
        if side == "source" and (collector is not None or "workspace_files" in local):
            restricted = {item["name"] for item in history_volumes} if collector is not None else set()
            workspace_names = {item["name"] for item in pod.get("volumes", [])
                               if item.get("persistentVolumeClaim", {}).get("claimName") == local["workspace_claim"]}
            for item in containers + pod.get("initContainers", []):
                if collector is not None and item == collector and any(item is candidate for candidate in containers):
                    continue
                for mount in item.get("volumeMounts", []):
                    allowed_app_mount = item is container and (mount == history_mount or mount in mounts)
                    if (mount["name"] in restricted and not allowed_app_mount or
                            mount["name"] in workspace_names and mount.get("readOnly") is not True):
                        raise ValueError("observed source Deployment exposes writable snapshot or private collector mounts")
                if any(device["name"] in restricted | workspace_names for device in item.get("volumeDevices", [])):
                    raise ValueError("observed source Deployment exposes the workspace or collector volume as a block device")
        if side == "target":
            archive_names = {item["name"] for item in pod.get("volumes", [])
                             if item.get("persistentVolumeClaim", {}).get("claimName") == local["artifact_claim"]}
            if artifact_import and archive_names - {"trial-app-archive", "trial-import-write"}:
                raise ValueError("observed target Deployment declares the import claim under another volume name")
            for item in containers + pod.get("initContainers", []):
                if receiver is not None and item == receiver and any(item is candidate for candidate in containers):
                    continue
                if artifact_import and any(mount["name"] in {"trial-import-socket", "trial-import-write", "trial-app-archive"}
                                           for mount in item.get("volumeMounts", []) if item is not container):
                    raise ValueError("observed target Deployment exposes import materials to another container")
                if any(mount["name"] in archive_names and mount.get("readOnly") is not True
                       for mount in item.get("volumeMounts", [])):
                    raise ValueError("observed target Deployment has a writable import mount")
                if any(device["name"] in archive_names for device in item.get("volumeDevices", [])):
                    raise ValueError("observed target Deployment exposes the import claim as a block device")
        if artifact_import:
            volumes.extend(import_volumes)
            mounts.append(socket_mount)
            env.append(socket_env)
        if collector is not None:
            volumes.extend(history_volumes)
            mounts.append(history_mount)
            env.extend(history_env)
        template = "/spec/template/spec"
        selected = f"{template}/containers/{index}"
        identities = {
            "/metadata/name": metadata["name"], "/metadata/namespace": metadata["namespace"],
            "/metadata/uid": metadata["uid"], "/metadata/resourceVersion": metadata["resourceVersion"],
            template + "/serviceAccountName": local["service_account"],
            selected + "/name": container_name, selected + "/image": local["image"],
        }
        patch = [{"op": "test", "path": path, "value": value} for path, value in identities.items()]
        if artifact_import and "trial-app-archive" in archive_names:
            pod, container = replace_legacy_target_archive(patch, pod, container, template, selected, local["artifact_claim"])
        patch_list(patch, pod, "volumes", template + "/volumes", volumes, "name")
        patch_list(patch, container, "volumeMounts", selected + "/volumeMounts", mounts, "mountPath")
        patch_list(patch, container, "env", selected + "/env", env, "name")
        if receiver is not None:
            patch_list(patch, pod, "containers", template + "/containers", [receiver], "name")
        if collector is not None:
            patch_list(patch, pod, "containers", template + "/containers", [collector], "name")
        return patch
    except (AttributeError, IndexError, KeyError, TypeError) as exc:
        raise ValueError("invalid observed application Deployment") from exc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="approved registration JSON with resource references")
    parser.add_argument("--side", required=True, choices=("source", "target"))
    modes = parser.add_subparsers(dest="operation", required=True)
    modes.add_parser("placeholders", help="render empty named Secrets for first-time creation")
    modes.add_parser("render", help="render the suspended Job and its scoped roles")
    modes.add_parser("hermes-source-script", help="render the immutable ConfigMap for the registered Hermes collector")
    profile = modes.add_parser("profile", help="bind the ConfigMap to the observed Kubernetes Job")
    profile.add_argument("--job", required=True, help="observed Job JSON path or - for stdin")
    app = modes.add_parser("app-patch", help="render a JSON patch for the observed application Deployment")
    app.add_argument("--deployment", required=True, help="observed Deployment JSON path or - for stdin")
    app.add_argument("--container", required=True, help="name of the application container in the Deployment")
    app.add_argument("--artifact-import", action="store_true", help="install the private upload receiver in an isolated target")
    args = parser.parse_args()
    try:
        if args.config == "-":
            raise ValueError("use a file for the registration configuration")
        config = load_json(args.config)
        validate(config)
        if args.operation == "render":
            output = registration_manifests(config, args.side)
        else:
            if args.operation == "placeholders":
                data = placeholders(config, args.side)
            elif args.operation == "hermes-source-script":
                data = hermes_source_script(config[args.side])
            elif args.operation == "profile":
                data = bind_profile(config, args.side, load_json(args.job))
            else:
                data = app_patch(config, args.side, load_json(args.deployment), args.container, args.artifact_import)
            output = json.dumps(data, indent=2) + "\n"
    except (OSError, ValueError) as exc:
        parser.exit(2, f"trial registration: {exc}\n")
    sys.stdout.write(output)


if __name__ == "__main__":
    main()
