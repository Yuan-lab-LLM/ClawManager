"""Exercise the actual embedded bootstrap Python without launching a runtime."""
import ast
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

RUNTIME_ROOT = Path(os.environ.get("RUNTIME_SOURCE_ROOT", "D:/AgentsRuntime"))


def embedded_python(relative):
    script = (RUNTIME_ROOT / relative).read_text(encoding="utf-8")
    return script.split("<<'PY'\n", 1)[1].rsplit("\nPY", 1)[0]


class BootstrapVisionTests(unittest.TestCase):
    def run_opencode(self, env):
        code = embedded_python("opencode/rootfs/usr/local/bin/opencode-apply-runtime-config")
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "opencode.json"
            with patch.dict(os.environ, env, clear=True), patch.object(sys, "argv", ["test", str(output), "https://gateway.example/llm", "auto"]):
                exec(compile(code, "opencode-bootstrap", "exec"), {})
            return json.loads(output.read_text(encoding="utf-8"))

    def test_opencode_flags_catalogue_and_safe_json(self):
        name = '视觉模型 "quoted"'
        doc = self.run_opencode({"CLAWMANAGER_LLM_MODEL": json.dumps([name, "text"]),
                                 "CLAWMANAGER_LLM_VISION": json.dumps({"auto": True, name: True, "text": False}),
                                 "CLAWMANAGER_LLM_API_KEY": "never-write-this-secret"})
        models = doc["provider"]["clawmanager"]["models"]
        self.assertEqual(models[name]["capabilities"]["input"], ["text", "image"])
        self.assertEqual(models["text"]["capabilities"]["input"], ["text"])
        self.assertEqual(models[name]["capabilities"]["output"], ["text"])
        self.assertNotIn("never-write-this-secret", json.dumps(doc))

    def test_opencode_legacy_and_invalid_flags(self):
        self.assertNotIn("capabilities", self.run_opencode({})["provider"]["clawmanager"]["models"]["auto"])
        for value in ['null', '[]', '{"auto":null}', '{"auto":"true"}']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.run_opencode({"CLAWMANAGER_LLM_VISION": value})

    def hermes_block(self):
        code = embedded_python("hermes/rootfs/usr/local/bin/hermes-apply-runtime-config")
        tree = ast.parse(code)
        selected = [node for node in tree.body if isinstance(node, ast.If)
                    and isinstance(node.test, ast.Compare)
                    and isinstance(node.test.left, ast.Constant)
                    and node.test.left.value == "CLAWMANAGER_LLM_VISION"]
        self.assertEqual(len(selected), 1)
        return compile(ast.Module(body=selected, type_ignores=[]), "hermes-vision", "exec")

    def test_hermes_preserves_settings_and_pins_auxiliary(self):
        data = {"auxiliary": {"compression": {"model": "keep"}, "vision": {"provider": "openrouter", "api_key": "old", "timeout": 42}}}
        scope = {"os": os, "json": json, "data": data, "base_url": "https://gateway.example/llm", "api_key": "instance-token"}
        with patch.dict(os.environ, {"CLAWMANAGER_LLM_VISION": '{"auto":true}'}, clear=True):
            exec(self.hermes_block(), scope)
        vision = data["auxiliary"]["vision"]
        self.assertEqual(vision["provider"], "custom")
        self.assertEqual(vision["base_url"], scope["base_url"])
        self.assertEqual(vision["model"], "auto")
        self.assertEqual(vision["api_key"], "")
        self.assertEqual(vision["timeout"], 42)
        self.assertEqual(data["auxiliary"]["compression"], {"model": "keep"})

    def test_hermes_legacy_no_mutation(self):
        data = {"auxiliary": {"vision": {"provider": "existing"}}}
        before = json.dumps(data)
        with patch.dict(os.environ, {}, clear=True):
            exec(self.hermes_block(), {"os": os, "json": json, "data": data})
        self.assertEqual(json.dumps(data), before)

    def test_hermes_invalid_flags_fail_closed(self):
        for value in ['null', '[]', '{"auto":1}']:
            with self.subTest(value=value), patch.dict(os.environ, {"CLAWMANAGER_LLM_VISION": value}, clear=True), self.assertRaises(ValueError):
                exec(self.hermes_block(), {"os": os, "json": json, "data": {}, "base_url": "https://gateway.example", "api_key": "token"})


if __name__ == "__main__":
    unittest.main()
