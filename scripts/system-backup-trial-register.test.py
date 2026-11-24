import copy
import json
from pathlib import Path
import runpy
import unittest


ROOT = Path(__file__).resolve().parents[1]
REGISTER = runpy.run_path(str(ROOT / "scripts/system-backup-trial-register.py"))
EXAMPLE = ROOT / "deployments/k8s/system-backup-trial-registration.example.json"


class TrialRegistrationTests(unittest.TestCase):
    def setUp(self):
        self.config = json.loads(EXAMPLE.read_text(encoding="utf-8"))
        REGISTER["validate"](self.config)

    def test_catalog_pin_and_workspace_modes_are_fully_rendered(self):
        source = REGISTER["registration_manifests"](self.config, "source")
        target = REGISTER["registration_manifests"](self.config, "target")
        for result in (source, target):
            self.assertNotIn("${", result)
            self.assertIn('value: "' + self.config["migration_catalog_sha256"] + '"', result)
            self.assertIn("suspend: true", result)
            self.assertIn("defaultMode: 288", result)
        self.assertNotIn("/trial/workspace", source)
        self.assertIn("/trial/workspace", target)
        self.assertIn("mountPath: /trial/archive-write\n              readOnly: false", source)
        self.assertIn("mountPath: /trial/archive-write\n              readOnly: true", target)

    def test_same_cluster_delegates_only_named_source_key(self):
        source = REGISTER["registration_manifests"](self.config, "source")
        role, binding = [json.loads(part) for part in source.split("\n---\n")[-2:]]
        self.assertEqual(role["rules"], [{"apiGroups": [""], "resources": ["secrets"], "resourceNames": ["trial-key"], "verbs": ["get"]}])
        self.assertEqual(binding["subjects"][0]["namespace"], self.config["target"]["namespace"])
        self.config["target"]["cluster"] = "another-cluster"
        self.assertNotIn("-target-key", REGISTER["registration_manifests"](self.config, "source"))

    def test_placeholders_do_not_include_sql_owner_or_secret_material(self):
        items = REGISTER["placeholders"](self.config, "target")["items"]
        self.assertEqual({item["metadata"]["name"] for item in items}, {"trial-key", "trial-target-reader", "trial-target-writer", "trial-target-runtime"})
        self.assertTrue(all(item["type"] == "Opaque" and "data" not in item and "stringData" not in item for item in items))

    def test_yaml_like_names_stay_strings(self):
        self.config["source"]["prepare"]["service_account"] = "null"
        self.config["source"]["prepare"]["job_name"] = "true"
        REGISTER["validate"](self.config)
        rendered = REGISTER["registration_manifests"](self.config, "source")
        self.assertIn('name: "true"', rendered)
        self.assertIn('serviceAccountName: "null"', rendered)

    def test_rejects_unresolved_placeholders_and_unsafe_registration(self):
        values = REGISTER["template_inputs"](self.config, "target")
        template = REGISTER["TEMPLATE"].read_text(encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "variables do not match"):
            REGISTER["render_template"](template.replace("MIGRATION_CATALOG_SHA256}", "WRONG_SHA256}"), values, True)
        for path, value in (
            (("migration_catalog_sha256",), "${MIGRATION_CATALOG_SHA256}"),
            (("target", "prepare", "owner_dsn_secret"), "trial-key"),
            (("target", "restore_read_secret"), "trial-target-writer"),
            (("target", "namespace"), "default"),
            (("target", "image"), "registry.example/clawmanager:latest"),
            (("target", "prepare", "server_uuid"), self.config["source"]["prepare"]["server_uuid"]),
            (("source", "prepare", "job_name"), "prepare\n---"),
        ):
            with self.subTest(path=path):
                config = copy.deepcopy(self.config)
                node = config
                for key in path[:-1]:
                    node = node[key]
                node[path[-1]] = value
                with self.assertRaises(ValueError):
                    REGISTER["validate"](config)

    def deployment(self, side):
        placement = self.config[side]
        return {
            "apiVersion": "apps/v1", "kind": "Deployment",
            "metadata": {"name": "clawmanager", "namespace": placement["namespace"],
                         "uid": "77777777-8888-4999-8aaa-bbbbbbbbbbbb", "resourceVersion": "217"},
            "spec": {"template": {"spec": {
                "serviceAccountName": placement["service_account"],
                "containers": [{"name": "sidecar", "image": "sidecar:test"},
                               {"name": "app", "image": placement["image"]}],
            }}},
        }

    def test_app_patch_wires_projected_materials_for_its_side(self):
        for side in ("source", "target"):
            with self.subTest(side=side):
                observed = self.deployment(side)
                patch = REGISTER["app_patch"](self.config, side, observed, "app")
                tests = {op["path"]: op["value"] for op in patch if op["op"] == "test"}
                self.assertEqual(tests["/metadata/resourceVersion"], "217")
                self.assertEqual(tests["/spec/template/spec/containers/1/name"], "app")
                additions = {op["path"].rsplit("/", 1)[-1]: op["value"] for op in patch if op["op"] == "add"}
                volumes = {vol["name"]: vol for vol in additions["volumes"]}
                mounts = {mount["mountPath"]: mount for mount in additions["volumeMounts"]}
                env = {item["name"]: item["value"] for item in additions["env"]}
                root = REGISTER["APP_MATERIALS"]
                prefix = REGISTER["TRIAL_ENV"]
                self.assertTrue(mounts[root + "/archive-read"]["readOnly"])
                self.assertEqual(env[prefix + "ENVIRONMENT_PROFILE_FILE"], root + "/profile/environment.json")
                self.assertEqual(env["SSL_CERT_FILE"], root + "/ca/ca-bundle.crt")
                for vol in volumes.values():
                    if "secret" in vol:
                        self.assertTrue(vol["secret"]["optional"])
                        self.assertNotEqual(vol["secret"]["secretName"], self.config[side]["prepare"]["owner_dsn_secret"])
                self.assertNotIn("subPath", json.dumps(mounts))
                self.assertEqual(env[prefix + "RUNTIME_BACKUP_ID"], "")
                if side == "source":
                    self.assertEqual(env[prefix + "WRITE_DIR"], root + "/archive-write")
                    self.assertFalse(mounts[root + "/archive-write"].get("readOnly", False))
                    self.assertEqual(env[prefix + "RESTORE_DSN_FILE"], "")
                else:
                    self.assertTrue(volumes["trial-app-archive"]["persistentVolumeClaim"]["readOnly"])
                    self.assertNotIn(root + "/archive-write", mounts)
                    self.assertEqual(env[prefix + "SOURCE_DSN_FILE"], "")
                    self.assertEqual(env[prefix + "WORKSPACE_FILES_TARGET_ROOT"], root + "/workspace")
                    self.assertNotEqual(env[prefix + "RESTORE_DSN_FILE"], env[prefix + "RESTORE_READ_DSN_FILE"])

    def test_app_patch_appends_without_echoing_existing_configuration(self):
        observed = self.deployment("source")
        pod = observed["spec"]["template"]["spec"]
        app = pod["containers"][1]
        app["env"] = [{"name": "EXISTING_PASSWORD", "value": "synthetic-must-stay-out-of-patch"}]
        first = REGISTER["app_patch"](self.config, "source", observed, "app")
        self.assertNotIn("synthetic-must-stay-out-of-patch", json.dumps(first))
        self.assertTrue(any(op["path"].endswith("/env/-") for op in first))
        volumes, mounts, env = REGISTER["app_materials"](self.config["source"], "source")
        pod["volumes"], app["volumeMounts"] = volumes, mounts
        app["env"] += [item if item["value"] else {"name": item["name"]} for item in env]
        self.assertTrue(all(op["op"] == "test" for op in REGISTER["app_patch"](self.config, "source", observed, "app")))

    def test_app_patch_rejects_wrong_identity_and_unsafe_target_alias(self):
        for part, key, wrong in (
            ("metadata", "namespace", "default"), ("app", "image", "unverified:latest"),
            ("pod", "automountServiceAccountToken", False), ("pod", "serviceAccountName", "wrong"),
        ):
            with self.subTest(part=part, key=key):
                observed = self.deployment("target")
                pod = observed["spec"]["template"]["spec"]
                {"metadata": observed["metadata"], "pod": pod, "app": pod["containers"][1]}[part][key] = wrong
                with self.assertRaises(ValueError):
                    REGISTER["app_patch"](self.config, "target", observed, "app")
        observed = self.deployment("target")
        pod = observed["spec"]["template"]["spec"]
        pod["volumes"] = [{"name": "alias", "persistentVolumeClaim": {"claimName": self.config["target"]["artifact_claim"]}}]
        pod["containers"][0]["volumeMounts"] = [{"name": "alias", "mountPath": "/alias"}]
        with self.assertRaisesRegex(ValueError, "writable import mount"):
            REGISTER["app_patch"](self.config, "target", observed, "app")
        pod["containers"][0]["volumeMounts"][0]["readOnly"] = True
        pod["containers"][1]["env"] = [{"name": REGISTER["TRIAL_ENV"] + "SOURCE_DSN_FILE", "value": "/private/source-dsn"}]
        with self.assertRaisesRegex(ValueError, "conflicting Deployment env entry"):
            REGISTER["app_patch"](self.config, "target", observed, "app")

    def test_target_delivery_grants_write_access_only_to_the_importer(self):
        observed = self.deployment("target")
        pod = observed["spec"]["template"]["spec"]
        local = self.config["target"]
        with self.assertRaisesRegex(ValueError, "fsGroup"):
            REGISTER["app_patch"](self.config, "target", observed, "app", True)
        pod["securityContext"] = {"fsGroup": local["prepare"]["volume_group"]}
        patch = REGISTER["app_patch"](self.config, "target", observed, "app", True)
        added = {op["path"]: op["value"] for op in patch if op["op"] == "add"}
        template = "/spec/template/spec"
        worker = added[template + "/containers/-"]
        self.assertEqual(worker["command"], ["/usr/local/bin/system-backup-trial-import"])
        self.assertTrue(worker["securityContext"]["runAsNonRoot"])
        self.assertEqual(worker["securityContext"]["capabilities"], {"drop": ["ALL"]})
        self.assertNotIn("KEY_FILE", json.dumps(worker["env"]))
        self.assertNotIn("DSN_FILE", json.dumps(worker["env"]))
        self.assertTrue(any(item["name"] == "trial-import-write" for item in worker["volumeMounts"]))
        app = pod["containers"][1]
        app["volumeMounts"] = added[template + "/containers/1/volumeMounts"]
        self.assertTrue(all(item.get("readOnly") for item in app["volumeMounts"] if "archive" in item["name"] or "import" in item["name"]))
        pod["volumes"] = added[template + "/volumes"]
        app["env"] = added[template + "/containers/1/env"]
        pod["containers"].append(worker)
        self.assertTrue(all(op["op"] == "test" for op in REGISTER["app_patch"](self.config, "target", observed, "app", True)))
        pod["containers"][0]["volumeMounts"] = [{"name": "trial-import-write", "mountPath": "/unsafe"}]
        with self.assertRaisesRegex(ValueError, "another container"):
            REGISTER["app_patch"](self.config, "target", observed, "app", True)

    def test_registered_hermes_source_has_a_read_only_workspace_and_private_socket(self):
        local = self.config["source"]
        local["workspace_claim"] = "hermes-snapshot"
        local["hermes_history"] = {"image": "registry.example/hermes@sha256:" + "a" * 64,
                                   "script_configmap": "hermes-collector-v1", "user_id": 7,
                                   "instance_id": 13, "run_as_user": 200013}
        REGISTER["validate"](self.config)
        script = REGISTER["hermes_source_script"](local)
        self.assertTrue(script["immutable"])
        self.assertEqual(set(script["data"]), {"history.py"})
        observed = self.deployment("source")
        pod = observed["spec"]["template"]["spec"]
        with self.assertRaisesRegex(ValueError, "fsGroup"):
            REGISTER["app_patch"](self.config, "source", observed, "app")
        pod["securityContext"] = {"fsGroup": local["prepare"]["volume_group"]}
        patch = REGISTER["app_patch"](self.config, "source", observed, "app")
        added = {op["path"]: op["value"] for op in patch if op["op"] == "add"}
        template = "/spec/template/spec"
        worker = added[template + "/containers/-"]
        volumes = {item["name"]: item for item in added[template + "/volumes"]}
        self.assertEqual(worker["image"], local["hermes_history"]["image"])
        self.assertIn("serve-source", worker["args"])
        self.assertEqual(worker["securityContext"]["runAsUser"], 200013)
        self.assertTrue(volumes["trial-hermes-workspace"]["persistentVolumeClaim"]["readOnly"])
        self.assertTrue(any(item["name"] == "trial-hermes-no-token" for item in worker["volumeMounts"]))
        app = pod["containers"][1]
        pod["volumes"] = added[template + "/volumes"]
        app["volumeMounts"] = added[template + "/containers/1/volumeMounts"]
        app["env"] = added[template + "/containers/1/env"]
        pod["containers"].append(worker)
        self.assertTrue(all(item.get("readOnly") for item in app["volumeMounts"] if "hermes" in item["name"]))
        self.assertTrue(all(op["op"] == "test" for op in REGISTER["app_patch"](self.config, "source", observed, "app")))
        pod["containers"][0]["volumeMounts"] = [{"name": "trial-hermes-socket", "mountPath": "/unsafe"}]
        with self.assertRaisesRegex(ValueError, "private collector mounts"):
            REGISTER["app_patch"](self.config, "source", observed, "app")

    def test_workspace_selection_mounts_only_the_registered_subtree(self):
        local = self.config["source"]
        local["workspace_claim"] = "workspace-snapshot"
        local["workspace_files"] = {"user_id": 7, "instance_id": 13, "runtime": "opencode", "relative_root": "home/projects"}
        REGISTER["validate"](self.config)
        observed = self.deployment("source")
        patch = REGISTER["app_patch"](self.config, "source", observed, "app")
        added = {op["path"].rsplit("/", 1)[-1]: op["value"] for op in patch if op["op"] == "add"}
        volume = next(item for item in added["volumes"] if item["name"] == "trial-app-workspace-selection")
        mount = next(item for item in added["volumeMounts"] if item["name"] == volume["name"])
        self.assertEqual(volume["persistentVolumeClaim"], {"claimName": "workspace-snapshot", "readOnly": True})
        self.assertTrue(mount["readOnly"])
        self.assertEqual(mount["subPath"], "opencode/user-7/instance-13/home/projects")
        env = {item["name"]: item["value"] for item in added["env"]}
        self.assertEqual(env[REGISTER["TRIAL_ENV"] + "WORKSPACE_FILES_SELECTION_ROOT"], mount["mountPath"])
        self.assertEqual(env[REGISTER["TRIAL_ENV"] + "WRITE_DIR"], REGISTER["APP_MATERIALS"] + "/archive-write")
        pod = observed["spec"]["template"]["spec"]
        pod["volumes"] = [{"name": "alias", "persistentVolumeClaim": {"claimName": "workspace-snapshot"}}]
        pod["containers"][0]["volumeMounts"] = [{"name": "alias", "mountPath": "/mutable"}]
        with self.assertRaisesRegex(ValueError, "writable snapshot"):
            REGISTER["app_patch"](self.config, "source", observed, "app")
        local["workspace_files"]["relative_root"] = "home/../other"
        with self.assertRaisesRegex(ValueError, "approved relative root"):
            REGISTER["validate"](self.config)


if __name__ == "__main__":
    unittest.main()
