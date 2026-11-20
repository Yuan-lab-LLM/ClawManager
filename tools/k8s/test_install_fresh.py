from argparse import Namespace
from contextlib import redirect_stderr
import io
import json
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

from install_fresh import (RUNTIME_IMAGES, configure_browser_origins, hostpath_preflight, install,
                           manifest_documents, single_node_resources, start_clawmanager)


def deployment(name, env=()):
    return {
        "kind": "Deployment",
        "metadata": {"name": name, "namespace": "cm-test"},
        "spec": {"replicas": 1, "template": {"spec": {"containers": [{
            "name": name, "image": "ghcr.io/example/" + name + ":source",
            "env": [{"name": key, "value": value} for key, value in env],
        }]}}},
    }


def manifest_fixture():
    docs = [
        {"kind": "Namespace", "metadata": {"name": "cm-test"}},
        {"kind": "PersistentVolumeClaim", "metadata": {"name": "workspace", "namespace": "cm-test"},
         "spec": {"storageClassName": "manual", "volumeName": "workspace"}},
        {"kind": "PersistentVolume", "metadata": {"name": "workspace"},
         "spec": {"hostPath": {"path": "/data/workspace"},
                  "nodeAffinity": {"required": {"nodeSelectorTerms": [{"matchExpressions": []}]}}}},
        deployment("clawmanager-app", [("CLAWMANAGER_STORAGE_PROFILE", "single-node"),
                                      ("RUNTIME_WORKSPACE_PVC_CLAIM", "workspace"), ("DB_HOST", "mysql"),
                                      ("K8S_PV_HOST_PATH_PREFIX", "/data/users")]),
        *(deployment(name) for name in ("mysql", "minio", "clawmanager-team-redis", *RUNTIME_IMAGES)),
    ]
    return "\n".join(json.dumps(item) for item in docs)


class FreshInstallTests(unittest.TestCase):
    def test_render_hands_off_all_four_runtimes_to_control_plane(self):
        payload = manifest_fixture()
        image = "ghcr.io/example/opencode-lite@sha256:" + "a" * 64
        with self.assertRaisesRegex(ValueError, "three ports per slot"):
            single_node_resources(list(manifest_documents(payload)), {}, 101)
        namespace, storage, claim, rendered = single_node_resources(
            list(manifest_documents(payload)), {"opencode-runtime": image}, 1)
        self.assertEqual((namespace, storage, claim), ("cm-test", "manual", "workspace"))
        self.assertFalse(any(item["metadata"]["name"] in RUNTIME_IMAGES for item in rendered))
        app = next(item for item in rendered if item["metadata"]["name"] == "clawmanager-app")
        self.assertEqual(app["spec"]["replicas"], 0)
        env = {item["name"]: item["value"] for item in app["spec"]["template"]["spec"]["containers"][0]["env"]}
        self.assertEqual(env["OPENCODE_RUNTIME_IMAGE"], image)
        self.assertEqual(env["RUNTIME_POD_USER_NAMESPACES"], "true")
        self.assertEqual(env["RUNTIME_CONTAINER_SLOTS"], "1")

    def test_browser_service_matches_configured_ports(self):
        docs = [deployment("clawmanager-app")]
        configure_browser_origins(docs, "cm-test", "https://127.0.0.1:30443", "32100-32101")
        service = docs[-1]
        self.assertEqual(service["metadata"]["namespace"], "cm-test")
        self.assertEqual(service["spec"]["type"], "NodePort")
        self.assertEqual([(item["nodePort"], item["targetPort"]) for item in service["spec"]["ports"]],
                         [(32100, 8444), (32101, 8444)])

    def test_retained_hostpath_overlap_blocks_before_writes(self):
        docs = [
            deployment("clawmanager-app", [("K8S_PV_HOST_PATH_PREFIX", "/data/test/users")]),
            {"kind": "PersistentVolume", "spec": {"hostPath": {"path": "/data/test/workspace"}}},
        ]

        def kube(*command, **kwargs):
            self.assertEqual(command, ("get", "pv", "-o", "json"))
            return json.dumps({"items": [{"spec": {"local": {"path": "/data/test"}}}]})

        with self.assertRaisesRegex(ValueError, "overlaps another persistent volume"):
            with hostpath_preflight(docs, kube, "cm-test", 30):
                self.fail("overlapping HostPath passed preflight")

    def test_preflight_failure_can_retry_but_partial_apply_preserves_credentials(self):
        for failed_stage in ("probe-create", "probe-wait", "server-dry-run"):
            with self.subTest(failed_stage=failed_stage), TemporaryDirectory() as root:
                credentials = Path(root) / "credentials"
                args = Namespace(context=None, manifest=Path("manifest.yaml"), images_file=None, image=[],
                                 slots=1, browser_origin=None, credentials_dir=credentials, timeout=30)
                stage = failed_stage
                namespace_uid = None

                def kube(_context, *command, payload=None, **_kwargs):
                    nonlocal namespace_uid
                    operation = command[:2]
                    if operation == ("create", "--dry-run=client"):
                        return manifest_fixture()
                    if operation == ("get", "namespace"):
                        return "namespace/cm-test" if namespace_uid else ""
                    if operation == ("get", "nodes"):
                        return json.dumps({"items": [{
                            "metadata": {"name": "one", "labels": {"clawmanager.io/storage-node": "true"}},
                            "status": {"conditions": [{"type": "Ready", "status": "True"}]},
                        }]})
                    if command[0] == "version":
                        return '{"serverVersion":{"gitVersion":"v1.33.0"}}'
                    if operation == ("get", "--raw"):
                        return '{"kubeletconfig":{"userNamespaces":{"idsPerPod":1048576}}}'
                    if command == ("get", "pv", "-o", "json"):
                        return '{"items":[]}'
                    if command[0] == "get":
                        return ""
                    if operation == ("create", "-f"):
                        if json.loads(payload)["kind"] == "Namespace":
                            self.assertIsNone(namespace_uid)
                            namespace_uid = "created-by-this-attempt"
                            return json.dumps({"metadata": {"uid": namespace_uid}})
                        self.assertEqual(stage, "apply")
                        raise subprocess.CalledProcessError(1, "apply")
                    if operation == ("create", "--dry-run=server"):
                        if stage == "server-dry-run":
                            raise subprocess.CalledProcessError(1, stage)
                        return ""
                    if operation == ("delete", "--raw"):
                        self.assertEqual(command[2], "/api/v1/namespaces/cm-test")
                        self.assertEqual(json.loads(payload)["preconditions"], {"uid": namespace_uid})
                        namespace_uid = None
                        return ""
                    if operation == ("wait", "--for=delete"):
                        self.assertIsNone(namespace_uid)
                        return ""
                    if operation == ("-n", "cm-test"):
                        if {"create": "probe-create", "wait": "probe-wait"}.get(command[2]) == stage:
                            raise subprocess.CalledProcessError(1, stage)
                        return ""
                    raise AssertionError(command)

                with patch("install_fresh.kubectl", side_effect=kube), redirect_stderr(io.StringIO()):
                    with self.assertRaises(subprocess.CalledProcessError) as first:
                        install(args)
                    self.assertEqual(first.exception.cmd, failed_stage)
                    self.assertIsNone(namespace_uid)
                    self.assertFalse(credentials.exists())

                    stage = "apply"
                    with self.assertRaises(subprocess.CalledProcessError) as retry:
                        install(args)
                    self.assertEqual(retry.exception.cmd, "apply")
                    self.assertIsNotNone(namespace_uid)
                    self.assertTrue((credentials / "audit.key").is_file())
                    self.assertTrue((credentials / "history" / "bootstrap.sql").is_file())

                    with self.assertRaisesRegex(ValueError, "namespace cm-test already exists"):
                        install(args)
                    self.assertIsNotNone(namespace_uid)
                    self.assertTrue((credentials / "audit.key").is_file())

                    namespace_uid = None
                    with self.assertRaisesRegex(ValueError, "credentials directory .* already exists"):
                        install(args)
                    self.assertIsNone(namespace_uid)
                    self.assertTrue((credentials / "audit.key").is_file())

    def test_mysql_original_value_is_restored_after_rollout_failure(self):
        for original in ("0", "1"):
            with self.subTest(original=original):
                state = original
                calls = []

                def mysql(sql):
                    nonlocal state
                    calls.append(sql)
                    if sql.startswith("SELECT"):
                        return state
                    if sql.startswith("SET GLOBAL"):
                        state = "1" if sql.endswith("ON;") else "0"

                def kube(*command):
                    calls.append(command)
                    self.assertEqual(state, "1")
                    if command[0] == "rollout":
                        raise RuntimeError("rollout failed")

                with self.assertRaisesRegex(RuntimeError, "rollout failed"):
                    start_clawmanager(kube, mysql, "bootstrap", 30)
                self.assertEqual(state, original)
                self.assertLess(calls.index("bootstrap"), calls.index(("scale", "deployment/clawmanager-app", "--replicas=1")))
                if original == "1":
                    self.assertFalse(any(isinstance(call, str) and call.startswith("SET") for call in calls))
