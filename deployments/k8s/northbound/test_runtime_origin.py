import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("runtime_origin", Path(__file__).with_name("merge-runtime-origin.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class RuntimeOriginTest(unittest.TestCase):
    def test_existing_tenant_patch_preserves_rendered_origin_and_other_env(self):
        origin = {"name": module.SETTING, "value": "https://opencode-{instance_id}.example.test:32543/"}
        app = {"kind": "Deployment", "metadata": {"name": "clawmanager-app", "namespace": "tenant"},
               "spec": {"template": {"spec": {"containers": [{"name": "clawmanager-app", "env": [origin]}]}}}}
        patch = {"spec": {"template": {"spec": {"containers": [{"name": "clawmanager-app", "image": "release", "env": [{"name": "OTHER", "value": "preserved"}]}]}}}}
        module.merge([app], patch, "tenant")
        module.merge([app], patch, "tenant")
        container = patch["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["env"], [{"name": "OTHER", "value": "preserved"}, origin])
        self.assertEqual(container["image"], "release")
        with self.assertRaises(ValueError):
            module.merge([app], patch, "different-tenant")
        for value in ["https://example.test/", "http://opencode-{instance_id}.example.test/", "https://u:p@opencode-{instance_id}.example.test/", "https://opencode-{instance_id}.example.test/?token=private"]:
            origin["value"] = value
            with self.assertRaises(ValueError):
                module.merge([app], patch, "tenant")


if __name__ == "__main__":
    unittest.main()
