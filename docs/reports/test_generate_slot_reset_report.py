#!/usr/bin/env python3
"""Exercise the evidence CLIs with subprocesses and temporary evidence."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import zipfile

HERE = Path(__file__).resolve().parent
RECORD = HERE / "run_slot_reset_evidence.py"
REPORT = HERE / "generate_slot_reset_report.py"


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")


class EvidenceReportTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        source_file = self.source / "checked"
        source_file.write_text("frozen", encoding="utf-8")
        source_manifest = self.root / "source.json"
        write_json(source_manifest, {"checked": hashlib.sha256(source_file.read_bytes()).hexdigest()})
        self.manifest = {
            "run_label": "fixture-54", "code_frozen_at": "2026-01-01T00:00:00Z",
            "sample": True,
            "source": {"manifest": "source.json", "manifest_sha256": hashlib.sha256(source_manifest.read_bytes()).hexdigest(),
                       "revision": 'fixture </script><script>alert(1)</script>'},
            "environment": {"host": "test fixture"}, "suites": [], "acceptance": {},
            "required_suites": [], "native_upgrades": {}, "artifacts": {},
        }

    def record(self, suite, code, *, kind="command", scope="backend", phase="final", result=0):
        filename = f"{suite}.json"
        args = [
            sys.executable, str(RECORD), "--suite-id", suite, "--phase", phase,
            "--label", self.manifest["run_label"], "--scope", scope, "--kind", kind,
            "--cwd", str(self.source), "--source-manifest", str(self.root / "source.json"),
            "--source-root", str(self.source), "--record", str(self.root / filename),
            "--evidence", str(self.root / f"{suite}.log"), "--", sys.executable, "-c", code,
        ]
        run = subprocess.run(args, capture_output=True, text=True)
        self.assertEqual(result, run.returncode, run.stderr)
        self.manifest["suites"].append(filename)
        return json.loads((self.root / filename).read_text(encoding="utf-8"))

    def go(self, suite, events, **kwargs):
        payload = "\n".join(json.dumps(event) for event in events)
        return self.record(suite, f"print({payload!r})", kind="go-json", **kwargs)

    def report(self, expected=0):
        manifest = self.root / "manifest.json"
        write_json(manifest, self.manifest)
        output = self.root / "delivery"
        run = subprocess.run([
            sys.executable, str(REPORT), "--manifest", str(manifest),
            "--out-dir", str(output), "--basename", "fixture", "--bundle",
        ], capture_output=True, text=True)
        self.assertEqual(expected, run.returncode, run.stderr)
        if expected:
            return run.stderr
        return json.loads((output / "fixture.json").read_text(encoding="utf-8")), output

    def test_recorder_retains_argv_stdout_stderr_exit_and_checks_source(self):
        row = self.record("command", "import sys; print('out'); print('err', file=sys.stderr); sys.exit(7)", result=7)
        self.assertEqual(7, row["exit_code"])
        self.assertEqual(str(self.source), row["cwd"])
        self.assertNotEqual(row["started_at"], row["finished_at"])
        self.assertEqual("fixture-54", row["run_label"])
        self.assertEqual(self.manifest["source"]["manifest_sha256"], row["source_manifest_sha256"])
        evidence = (self.root / "command.log").read_bytes()
        self.assertIn(b"out\n", evidence)
        self.assertIn(b"err\n", evidence)
        self.assertEqual(hashlib.sha256(evidence).hexdigest(), row["evidence_sha256"])
        changed = self.record("mutation", "from pathlib import Path; Path('checked').write_text('changed')", result=126)
        self.assertEqual(0, changed["exit_code"])
        self.assertFalse(changed["source_verified_after"])

    def test_required_groups_keep_pass_fail_skip_and_not_run(self):
        def event(action, package="pkg", test=None):
            return dict(Action=action, Package=package, **({"Test": test} if test else {}))

        self.go("mixed", [event("start"), event("run", test="TestPass"), event("pass", test="TestPass"),
                          event("skip", test="TestSkip"), event("pass"),
                          event("start", "empty"), event("skip", "empty")])
        self.go("package-failure", [event("start"), event("fail")])
        self.go("empty", [event("start"), event("pass")])
        self.go("truncated", [event("start"), event("run", test="TestInterrupted")])
        self.manifest["required_suites"] = ["mixed", "package-failure", "empty", "truncated", "absent"]
        for number, name in ((1, "TestPass"), (2, "TestSkip"), (3, "TestAbsent")):
            self.manifest["acceptance"][str(number)] = [
                {"expected": name, "actual": name, "evidence": [{"suite": "mixed", "test": name, "package": "pkg"}]},
            ]
        data, _ = self.report()
        self.assertEqual(15, len(data["acceptance"]))
        self.assertEqual(["PASS", "SKIP", "NOT RUN"], [row["status"] for row in data["acceptance"][:3]])
        self.assertEqual(
            {"mixed": "PASS", "package-failure": "FAIL", "empty": "NOT RUN",
             "truncated": "NOT RUN", "absent": "NOT RUN"}, data["required_suites"],
        )
        self.assertEqual("FAIL", data["acceptance_status"])
        suite = next(row for row in data["suites"] if row["suite_id"] == "package-failure")
        self.assertEqual([{"package": "pkg", "status": "FAIL", "elapsed_seconds": None}], suite["packages"])

    def test_fixture_cannot_prove_live_browser_or_native_refusal(self):
        self.record("mock", "print('OpenCode runtime native CLI is unsupported')", scope="browser-fixture")
        self.manifest["acceptance"]["12"] = [
            {"expected": "desktop and narrow", "actual": "mock network used", "evidence": [{"suite": "mock"}]},
        ]
        self.manifest["native_upgrades"]["opencode"] = {
            "status": "REFUSED", "detail": "OpenCode runtime native CLI is unsupported",
            "operation": "prepare", "source_image": "old", "target_image": "new",
            "evidence": [{"suite": "mock", "contains": "native CLI is unsupported"}],
        }
        data, _ = self.report()
        self.assertEqual("NOT RUN", data["acceptance"][11]["status"])
        self.assertEqual("NOT RUN", data["native_upgrades"]["opencode"]["status"])

    def test_refusal_escape_links_and_portable_bundle(self):
        self.record("blocked", "import sys; print('OpenCode native CLI unsupported </script>'); sys.exit(1)",
                    scope="native-upgrade", result=1)
        self.manifest["native_upgrades"]["opencode"] = {
            "status": "REFUSED", "detail": "OpenCode native CLI unsupported", "operation": "prepare",
            "source_image": "source-image", "target_image": "target-image",
            "evidence": [{"suite": "blocked", "contains": "OpenCode native CLI unsupported"}],
        }
        data, output = self.report()
        self.assertFalse(data["accepted"])
        self.assertEqual("REFUSED", data["native_upgrades"]["opencode"]["status"])
        html = (output / "fixture.html").read_text(encoding="utf-8")
        self.assertIn("示例夹具", html)
        self.assertIn("fixture-evidence.zip", html)
        self.assertIn("#suite-blocked", html)
        self.assertNotIn("<script>alert(1)</script>", html)
        self.assertIn("&lt;/script&gt;", html)
        with zipfile.ZipFile(output / "fixture-evidence.zip") as bundle:
            self.assertIn("SHA256SUMS", bundle.namelist())
            self.assertIn("raw/blocked.log", bundle.namelist())
            self.assertIn("source-manifest.json", bundle.namelist())
            self.assertIn("fixture.html", bundle.namelist())
            self.assertIn("OpenCode native CLI", bundle.read("raw/blocked.log").decode())

    def test_supporting_native_source_keeps_original_identity(self):
        evidence = self.root / "native.log"
        evidence.write_text("actual candidate retained neighbors; unsupported CLI", encoding="utf-8")
        self.manifest["supporting_runs"] = [{
            "suite_id": "native-candidate", "scope": "cluster", "status": "PASS",
            "evidence": "native.log", "evidence_sha256": hashlib.sha256(evidence.read_bytes()).hexdigest(),
            "source_revision": "separate-source", "finished_at": "2025-12-31T14:00:00Z",
            "command": "native test",
        }]
        self.manifest["acceptance"]["5"] = [{
            "expected": "traffic remains healthy", "actual": "retained neighbors",
            "evidence": [{"suite": "native-candidate", "contains": "retained neighbors"}],
        }]
        data, _ = self.report()
        self.assertEqual("PASS", data["acceptance"][4]["status"])
        self.assertEqual([], data["suites"])
        self.assertEqual("supporting", data["supporting_runs"][0]["phase"])
        self.assertEqual("2025-12-31T14:00:00Z", data["supporting_runs"][0]["finished_at"])
        self.manifest["required_suites"] = ["native-candidate"]
        data, _ = self.report()
        self.assertEqual("NOT RUN", data["required_suites"]["native-candidate"])

    def test_tampered_or_development_evidence_is_rejected(self):
        self.record("dev", "print('source fixture')", phase="development")
        self.assertIn("not a final recording", self.report(expected=2))
        self.manifest["suites"].clear()
        self.record("tampered", "print('original')")
        (self.root / "tampered.log").write_text("altered", encoding="utf-8")
        self.assertIn("evidence differs from recording", self.report(expected=2))

    def test_export_removes_fixture_credentials_without_changing_the_recorded_digest(self):
        credential = b"stale-fixture-password"
        token = b"eyJfixture01234567.eyJfixture01234567.signature0123456789"
        original = self.record("credentials", f"print('https://test:{credential.decode()}@fixture.invalid {token.decode()}')")
        data, output = self.report()
        for path in output.rglob("*"):
            if path.is_file() and path.suffix != ".zip":
                self.assertNotIn(credential, path.read_bytes(), str(path))
                self.assertNotIn(token, path.read_bytes(), str(path))
        log = (output / "raw/credentials.log").read_bytes()
        row = data["suites"][0]
        self.assertEqual(original["evidence_sha256"], row["evidence_sha256"])
        self.assertEqual(hashlib.sha256(log).hexdigest(), row["published_log_sha256"])
        self.assertNotEqual(row["evidence_sha256"], row["published_log_sha256"])
        self.assertEqual("PASS", row["status"])


if __name__ == "__main__":
    unittest.main()
