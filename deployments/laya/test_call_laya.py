"""Client regression tests; mock HTTP, no remote model calls or credentials."""
import io
import json
from pathlib import Path
import re
import unittest
import urllib.error
from contextlib import redirect_stdout, redirect_stderr
from unittest.mock import patch

import call_laya as client


class ClientTests(unittest.TestCase):
    def test_base_url(self):
        self.assertEqual(client.normalize_url("http://localhost:18000/"), "http://localhost:18000")
        for url in ("file:///tmp/key", "https://user:password@host", "http://host/v1/systemone", "http://host?key=x"):
            with self.assertRaises(ValueError):
                client.normalize_url(url)

    def test_utf8_and_auth(self):
        with patch.object(client.urllib.request, "build_opener") as build:
            build.return_value.open.return_value.__enter__.return_value = io.BytesIO(b'{"answers":{}}')
            result, elapsed = client.call_api("http://localhost", "/v1/systemone", "test-key", {"state": "你好"})
            request = build.return_value.open.call_args.args[0]
            self.assertEqual(json.loads(request.data), {"state": "你好"})
            self.assertEqual(request.get_header("Authorization"), "Bearer test-key")
            self.assertEqual(build.call_args.args[0].proxies, {})
            self.assertIsInstance(build.call_args.args[1], client.NoRedirect)
            self.assertEqual(result, {"answers": {}})
            self.assertGreaterEqual(elapsed, 0)

    def test_http_error_redacts_body(self):
        with patch.object(client.urllib.request, "build_opener") as build:
            build.return_value.open.side_effect = urllib.error.HTTPError("http://localhost", 401, "bad", {}, io.BytesIO(b'secret-value'))
            with self.assertRaises(client.APIError) as failure:
                client.call_api("http://localhost", "/v1/systemone", "secret-value", {})
            self.assertIn("401", str(failure.exception))
            self.assertNotIn("secret-value", str(failure.exception))

    def test_invalid_json(self):
        with patch.object(client.urllib.request, "build_opener") as build:
            build.return_value.open.return_value.__enter__.return_value = io.BytesIO(b'<html>not laya</html>')
            with self.assertRaises(client.APIError):
                client.call_api("http://localhost", "/health")

    def test_health_without_key(self):
        with patch.object(client, "call_api", return_value=({"status": "ok", "loaded": ["multilingual"]}, 0.1)), patch.object(client.getpass, "getpass") as prompt, redirect_stdout(io.StringIO()):
            self.assertEqual(client.main(["--health"]), 0)
            prompt.assert_not_called()

    def test_single_tier(self):
        calls = []

        def fake_api(*args, **kwargs):
            calls.append((args, kwargs))
            if args[1] == "/health":
                return {"status": "ok", "loaded": ["multilingual"]}, 0.01
            return {"answers": {"tier": {"choice": "medium", "confidence": 0.7}}}, 0.2

        with patch.object(client, "call_api", side_effect=fake_api), patch.dict(client.os.environ, {"LAYA_API_KEY": "test-key"}), redirect_stdout(io.StringIO()):
            self.assertEqual(client.main(["--text", "写代码", "--task", "tier"]), 0)
        payload = calls[1][0][3]
        self.assertEqual(payload["model"], "multilingual")
        self.assertEqual(json.loads(payload["state"]), [{"text": "写代码", "has_non_text": False, "truncated": False}])
        self.assertEqual(payload["max_len"], 2048)
        self.assertEqual(payload["head_max_len"], client.HEAD_MAX_LEN)
        self.assertEqual(set(payload["questions"]), {"tier"})

    def test_tier_rubric_matches_gateway(self):
        source = (Path(__file__).resolve().parents[2] / "backend/internal/aigateway/laya_prompt.go").read_text(encoding="utf-8")
        constants = dict(re.findall(r"const (\w+) = `([^`]+)`", source))
        self.assertEqual(client.QUESTIONS["tier"]["instructions"], constants["layaTierInstructions"])
        for name in ("simple", "medium", "complex", "reasoning"):
            self.assertEqual(client.QUESTIONS["tier"]["criteria"][name], constants[f"laya{name.title()}Criterion"])
        self.assertEqual(list(client.QUESTIONS["tier"]["criteria"]), ["simple", "medium", "complex", "reasoning"])
        self.assertIn(f'const layaPromptVersion = "{client.TIER_PROMPT_VERSION}"', source)
        self.assertIn(f"const layaHeadMaxLen = {client.HEAD_MAX_LEN}", source)

    def test_tier_rejects_oversized_input_before_inference(self):
        args = client.argparse.Namespace(task="tier")
        with patch.object(client, "call_api") as api:
            with self.assertRaises(client.APIError):
                client.run_one(args, "test-key", "中" * 500)
            api.assert_not_called()

    def test_invalid_input(self):
        with patch.object(client, "call_api") as api, redirect_stderr(io.StringIO()):
            for arguments in (["--timeout", "0"], ["--timeout", "nan"], ["--text", " "]):
                self.assertEqual(client.main(arguments), 1)
            api.assert_not_called()

    def test_no_redirect(self):
        self.assertIsNone(client.NoRedirect().redirect_request(None, None, 302, "redirect", {}, "http://another-host"))

    def test_malformed_response(self):
        with patch.object(client, "call_api", return_value=({"status": "ok", "loaded": None}, 0.01)), redirect_stderr(io.StringIO()):
            self.assertEqual(client.main(["--health"]), 1)
        health = ({"status": "ok", "loaded": ["multilingual"]}, 0.01)
        with patch.object(client, "call_api", side_effect=[health, ({"answers": None}, 0.01)]), patch.dict(client.os.environ, {"LAYA_API_KEY": "test-key"}), redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            self.assertEqual(client.main(["--text", "你好"]), 1)


if __name__ == "__main__":
    unittest.main()
