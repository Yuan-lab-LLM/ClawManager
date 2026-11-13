"""Isolated stdlib HTTP contract tests; run on the test cluster only."""
import http.server
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest

SCRIPT = Path(__file__).with_name("upgrade-runtime.py")
IMAGE = "registry.example/runtime@sha256:" + "a" * 64
TOKEN = "fixture-secret-do-not-print"


class DeliveryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.state = Path(self.tmp.name) / "operation.json"
        self.calls = []
        self.statuses = ["finished"]
        self.strategy = "legacy_rolling"
        self.passed = True
        self.conflict = False
        self.empty = False
        self.drop_create = False
        self.drop_preflight = False
        self.drop_preparation = False
        self.redirect = False
        self.preparation_status = "completed"
        test = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.handle_request()

            def do_POST(self):
                self.handle_request()

            def handle_request(self):
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or "{}")
                test.calls.append((self.command, self.path, body))
                if self.headers.get("Authorization") != "Bearer " + TOKEN:
                    self.send_error(401)
                    return
                rollout = {"id": 41, "runtime_type": test.runtime, "target_image_ref": IMAGE, "status": test.statuses[0], "phase": "verify"}
                if len(test.statuses) > 1 and self.command == "GET":
                    test.statuses.pop(0)
                data = {"rollout": rollout}
                status = 200
                preparation = {"id": "prep-31", "kind": "rollout_wake", "runtime_type": test.runtime, "status": test.preparation_status, "phase": "wake"}
                if self.path.endswith("/prepare"):
                    if test.drop_preparation:
                        self.close_connection = True
                        return
                    data = {"operation": preparation}
                elif self.path == "/api/v1/admin/runtime-scheduling":
                    data = {"items": [preparation]}
                elif self.path.endswith("/preflight"):
                    if test.drop_preflight:
                        self.close_connection = True
                        return
                    data = {"passed": test.passed, "blockers": [] if test.passed else [TOKEN], "strategy": test.strategy, "target_image_ref": IMAGE, "empty_pool_reset": test.empty}
                    if test.strategy == "openclaw_8plus_data_safe":
                        data["rollout"] = dict(rollout, status="preflight_passed", preflight_id="proof-41")
                elif self.command == "POST":
                    if test.drop_create:
                        self.close_connection = True
                        return
                    status = 409 if test.conflict else 201
                elif "?" in self.path:
                    data = {"items": [rollout]}
                if test.redirect:
                    self.send_response(302)
                    self.send_header("Location", "/stolen")
                    self.end_headers()
                    return
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"success": status < 400, "data": data, "error": TOKEN}).encode())

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.api = "http://127.0.0.1:%d/api/v1" % self.server.server_port
        self.runtime = "hermes"

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.tmp.cleanup()

    def run_cli(self, *extra):
        result = subprocess.run([sys.executable, str(SCRIPT), "--api-url", self.api, "--runtime-type", self.runtime, "--target-image", IMAGE, "--state-file", str(self.state), "--poll-interval", "0.01", "--timeout", "0.12", *extra], env=dict(os.environ, CM_TOKEN=TOKEN), capture_output=True, text=True)
        self.assertNotIn(TOKEN, result.stdout + result.stderr)
        return result

    def test_submit_once_and_resume_polls_same_operation(self):
        self.statuses = ["running", "finished"]
        self.assertEqual(self.run_cli().returncode, 0)
        self.assertEqual(self.run_cli().returncode, 0)
        self.assertEqual(sum(method == "POST" for method, _, _ in self.calls), 2)
        self.assertEqual(json.loads(self.state.read_text())["operation_id"], 41)
        self.assertEqual(self.state.stat().st_mode & 0o777, 0o600)
        self.assertTrue(all(path.startswith("/api/v1/admin/runtime-rollouts") or path == "/api/v1/admin/runtime-scheduling" for _, path, _ in self.calls))

    def test_terminal_failure_states_stop(self):
        for state in ("blocked", "preflight_blocked", "error", "failed", "cancelled", "needs_attention", "unknown"):
            with self.subTest(state=state):
                self.statuses = [state]
                result = self.run_cli("--operation-id", "41")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("41", result.stdout)
        self.assertTrue(all(method == "GET" for method, _, _ in self.calls))

    def test_timeout_preserves_operation(self):
        self.statuses = ["running"]
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("timeout", result.stderr)
        self.assertEqual(json.loads(self.state.read_text())["operation_id"], 41)

    def test_conflict_never_reposts(self):
        self.conflict = True
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(len(self.calls), 3)

    def test_lost_create_response_requires_history_recovery(self):
        self.drop_create = True
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(len(self.calls), 3)
        self.assertEqual(self.run_cli("--history").returncode, 0)
        self.assertEqual(self.run_cli("--operation-id", "41").returncode, 0)
        self.assertEqual(sum(method == "POST" for method, _, _ in self.calls), 2)

    def test_data_safe_preflight_confirmation(self):
        self.runtime = "openclaw"
        self.strategy = "openclaw_8plus_data_safe"
        self.assertEqual(self.run_cli().returncode, 0)
        self.assertEqual([path for method, path, _ in self.calls if method == "POST"], ["/api/v1/admin/runtime-rollouts/prepare", "/api/v1/admin/runtime-rollouts/preflight", "/api/v1/admin/runtime-rollouts"])
        self.assertEqual(self.calls[3][2]["preflight_id"], "proof-41")
        self.assertEqual(self.calls[3][2]["max_unavailable"], 0)
        self.assertEqual(self.calls[2][2]["preparation_id"], "prep-31")
        self.assertEqual(self.calls[3][2]["preparation_id"], "prep-31")

    def test_blocked_preflight_does_not_submit(self):
        self.runtime = "openclaw"
        self.passed = False
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(len(self.calls), 3)

    def test_legacy_empty_reset_uses_handler_classification(self):
        self.runtime = "openclaw"
        self.empty = True
        result = self.run_cli()
        self.assertEqual(result.returncode, 0)
        self.assertIn("legacy_rolling", result.stdout)
        self.assertIn("empty_pool_reset", result.stdout)
        self.assertNotIn("preflight_id", self.calls[3][2])

    def test_opencode_rejected_without_mutation(self):
        self.runtime = "opencode"
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(self.calls, [])

    def test_tag_rejected_before_network(self):
        self.assertNotEqual(self.run_cli("--target-image", "registry.example/runtime:latest").returncode, 0)
        self.assertEqual(self.calls, [])

    def test_redirect_not_followed(self):
        self.redirect = True
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(len(self.calls), 1)

    def test_state_identity_mismatch_does_not_poll(self):
        self.assertEqual(self.run_cli().returncode, 0)
        self.calls.clear()
        self.assertNotEqual(self.run_cli("--target-image", "registry.example/runtime@sha256:" + "b" * 64).returncode, 0)
        self.assertEqual(self.calls, [])

    def test_failed_preparation_stops_before_upgrade(self):
        self.preparation_status = "needs_attention"
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual([path for method, path, _ in self.calls if method == "POST"], ["/api/v1/admin/runtime-rollouts/prepare"])

    def test_preparation_timeout_resume_does_not_prepare_twice(self):
        self.preparation_status = "starting"
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.preparation_status = "completed"
        self.assertEqual(self.run_cli().returncode, 0)
        self.assertEqual(sum(path.endswith("/prepare") for _, path, _ in self.calls), 1)

    def test_unknown_status_is_not_echoed(self):
        self.statuses = [TOKEN]
        self.assertNotEqual(self.run_cli("--operation-id", "41").returncode, 0)

    def test_processing_preparation_is_polled_until_timeout(self):
        self.preparation_status = "processing"
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("timeout", result.stderr)

    def test_preparation_id_cannot_replay_unknown_rollout_submission(self):
        self.drop_create = True
        for runtime in ("hermes", "deepseek-harness"):
            with self.subTest(runtime=runtime):
                self.runtime = runtime
                self.state = Path(self.tmp.name) / (runtime + ".json")
                self.assertNotEqual(self.run_cli().returncode, 0)
                original_calls = list(self.calls)
                self.assertEqual(json.loads(self.state.read_text())["stage"], "submitting")
                self.assertNotEqual(self.run_cli("--preparation-id", "prep-31").returncode, 0)
                self.assertEqual(self.calls, original_calls)
                self.assertEqual(json.loads(self.state.read_text())["stage"], "submitting")

    def test_preparation_id_cannot_replay_unknown_preflight_submission(self):
        self.runtime = "openclaw"
        self.strategy = "openclaw_8plus_data_safe"
        self.drop_preflight = True
        self.assertNotEqual(self.run_cli().returncode, 0)
        original_calls = list(self.calls)
        self.assertEqual(json.loads(self.state.read_text())["stage"], "preflighting")
        self.assertNotEqual(self.run_cli("--preparation-id", "prep-31").returncode, 0)
        self.assertEqual(self.calls, original_calls)
        self.assertEqual(json.loads(self.state.read_text())["stage"], "preflighting")

    def test_preparation_id_recovers_only_unknown_initial_preparation(self):
        self.drop_preparation = True
        self.assertNotEqual(self.run_cli().returncode, 0)
        self.assertEqual(json.loads(self.state.read_text())["stage"], "preparing")
        self.drop_preparation = False
        self.assertEqual(self.run_cli("--preparation-id", "prep-31").returncode, 0)
        self.assertEqual(sum(path.endswith("/prepare") for _, path, _ in self.calls), 1)


if __name__ == "__main__":
    unittest.main()
