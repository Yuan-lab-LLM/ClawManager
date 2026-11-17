import json
import hashlib
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest

from generate_database_report import coverage_rows, generate, read_cases, verified_metric, REQUIRED_COVERAGE, REQUIRED_PERFORMANCE, REQUIRED_SUITES, PERFORMANCE_EVIDENCE


class ReportIntegrity(unittest.TestCase):
    def test_complete_fixture_passes_but_relaxed_target_cannot_hide_slow_metric(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "complete.jsonl"
            metrics, events = [], []
            for identity, maximum in REQUIRED_PERFORMANCE.items():
                suite_id, marker = PERFORMANCE_EVIDENCE[identity]
                line = f"MEASURED {marker} samples=20 P50=1ms P95=2ms"
                metrics.append(dict(id=identity, name=identity, limit_ms=maximum, samples=20, p50_ms=1, p95_ms=2,
                    evidence_suite_id=suite_id, evidence_test="TestFixture", evidence_output=line))
                events.append(dict(Action="output", Package="fixture", Test="TestFixture", Output=line + "\n"))
            events.extend([dict(Action="pass", Package="fixture", Test="TestFixture"), dict(Action="pass", Package="fixture")])
            evidence.write_text('\n'.join(json.dumps(e) for e in events))
            suites = [dict(name="complete-fixture", suite_id=identity, kind="go-json" if identity in {"query-capacity", "audit-capacity"} else "command",
                category="fixture", evidence=str(evidence), command="fixture only", exit_code=0,
                started_at="2026-11-18T00:01:00Z", finished_at="2026-11-18T00:01:01Z", elapsed_seconds=1,
                evidence_sha256=hashlib.sha256(evidence.read_bytes()).hexdigest(), evidence_bytes=evidence.stat().st_size,
                source_manifest_sha256="a"*64) for identity in REQUIRED_SUITES]
            manifest = dict(development_complete=True, review_passed=True, code_frozen_at="2026-11-18T00:00:00Z",
                created_at="2026-11-18T00:02:00Z", suites=suites, performance=metrics,
                coverage=[dict(id=identity, title=title, criterion="synthetic fixture", selectors=["complete-fixture"]) for identity, title in REQUIRED_COVERAGE.items()],
                source={"manifest_sha256":"a"*64}, environment={}, evidence_bundle="fixture.zip", evidence_bundle_sha256="b"*64)
            self.assertTrue(generate(manifest)[1]["accepted"])
            metric = next(m for m in metrics if m["id"] == "audit_default30")
            old = metric["evidence_output"]
            metric.update(p95_ms=1000, limit_ms=99999, evidence_output=old.replace("P95=2ms", "P95=1s"))
            evidence.write_text(evidence.read_text().replace(old, metric["evidence_output"]))
            for suite in suites:
                suite.update(evidence_sha256=hashlib.sha256(evidence.read_bytes()).hexdigest(), evidence_bytes=evidence.stat().st_size)
            _, data = generate(manifest)
            self.assertFalse(data["accepted"])
            row = next(m for m in data["performance"] if m["id"] == "audit_default30")
            self.assertEqual(row["limit_ms"], 500)
            self.assertEqual(row["status"], "NOT_MET")

    def test_performance_must_match_the_final_test_output(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "metrics.jsonl"
            recorded = "MEASURED scenario=trace-page samples=20 P50=1ms P95=2ms"
            evidence.write_text(json.dumps(dict(Action="output", Test="TestCapacity", Output=recorded + "\n")))
            suite = dict(suite_id="query-capacity", kind="go-json", evidence=str(evidence))
            metric = dict(id="trace_page", evidence_suite_id="query-capacity", evidence_test="TestCapacity", evidence_output=recorded,
                          samples=20, p50_ms=1, p95_ms=2)
            self.assertTrue(verified_metric(metric, [suite]))
            metric["p95_ms"] = 1
            self.assertFalse(verified_metric(metric, [suite]))
            metric["p95_ms"] = 2
            metric["evidence_test"] = "TestOther"
            self.assertFalse(verified_metric(metric, [suite]))

    def test_failed_leaf_cannot_be_hidden_by_parent_and_html_is_safe(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "test.jsonl"
            evidence.write_text("\n".join(json.dumps(item) for item in [
                {"Action": "start", "Package": "fixture"},
                {"Action": "output", "Package": "fixture", "Test": "TestScope/fault", "Output": "</script><script id='owned'>injected</script>"},
                {"Action": "fail", "Package": "fixture", "Test": "TestScope/fault"},
                {"Action": "pass", "Package": "fixture", "Test": "TestScope"},
                {"Action": "fail", "Package": "fixture"},
            ]))
            manifest = {"development_complete": True, "review_passed": True,
                "code_frozen_at": "2026-11-18T00:00:00Z", "created_at": "2026-11-18T00:02:00Z",
                "source": {}, "environment": {}, "coverage": [{"title": "fixture", "criterion": "failure visible", "selectors": ["TestScope"]}],
                "suites": [{"kind": "go-json", "name": "integrity", "category": "fixture", "evidence": str(evidence), "command": "test", "exit_code": 1, "started_at": "2026-11-18T00:01:00Z"}]}
            manifest["suites"][0].update(finished_at="2026-11-18T00:01:01Z", elapsed_seconds=1,
                evidence_sha256=hashlib.sha256(evidence.read_bytes()).hexdigest(), evidence_bytes=evidence.stat().st_size)
            page, data = generate(manifest)
            self.assertFalse(data["accepted"])
            self.assertEqual(data["counts"], {"FAIL": 1})
            self.assertNotIn("<script id='owned'>", page)
            self.assertIn("\\u003c/script\\u003e", page)
            manifest["code_frozen_at"] = "2026-11-18T00:03:00Z"
            with self.assertRaisesRegex(ValueError, "predates code freeze"):
                generate(manifest)
            manifest["code_frozen_at"] = "2026-11-18T00:00:00Z"
            manifest["source"]["manifest_sha256"] = "frozen-source"
            with self.assertRaisesRegex(ValueError, "source differs from freeze"):
                generate(manifest)

    def test_successful_command_cannot_hide_a_source_change(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            source = root / "source.txt"
            source.write_text("reviewed")
            manifest = root / "source-manifest.json"
            manifest.write_text(json.dumps({source.name: hashlib.sha256(source.read_bytes()).hexdigest()}))
            record, output = root / "result.json", root / "result.log"
            command = [sys.executable, str(Path(__file__).with_name("run_database_evidence.py")),
                "--record", str(record), "--evidence", str(output), "--name", "source-integrity",
                "--category", "fixture", "--cwd", str(root), "--source-root", str(root),
                "--source-manifest", str(manifest), "--", sys.executable, "-c",
                "from pathlib import Path; import sys; Path(sys.argv[1]).write_text('changed')", str(source)]
            result = subprocess.run(command, capture_output=True, text=True)
            data = json.loads(record.read_text())
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(data["command_exit_code"], 0)
            self.assertEqual(data["exit_code"], 126)
            self.assertTrue(data["source_integrity_failed"])
            self.assertIn("SOURCE INTEGRITY FAILED", output.read_text())

    def test_truncated_package_stream_is_not_a_pass(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "partial.jsonl"
            evidence.write_text('\n'.join(json.dumps(item) for item in [
                {"Action": "start", "Package": "unfinished"},
                {"Action": "pass", "Package": "unfinished", "Test": "TestVisible"},
                {"Action": "pass", "Package": "other"},
            ]))
            cases = read_cases({"kind": "go-json", "evidence": str(evidence), "name": "fixture", "category": "fixture", "command": "test", "exit_code": 0})
            self.assertTrue(any(c["status"] == "INCOMPLETE" for c in cases))

    def test_corrupted_json_line_is_not_ignored(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "corrupt.jsonl"
            evidence.write_text('\n'.join([json.dumps({"Action": "pass", "Package": "fixture", "Test": "TestVisible"}),
                'truncated or corrupted JSON', json.dumps({"Action": "pass", "Package": "fixture"})]))
            cases = read_cases(dict(kind="go-json", evidence=str(evidence), name="fixture", category="fixture", command="test", exit_code=0))
            self.assertTrue(any(c["status"] == "INCOMPLETE" for c in cases))

    def test_empty_matrix_and_skipped_cases_cannot_be_accepted(self):
        with tempfile.TemporaryDirectory() as folder:
            evidence = Path(folder) / "skips.jsonl"
            evidence.write_text('\n'.join(json.dumps(item) for item in [
                {"Action": "skip", "Package": "fixture", "Test": "TestOptional"}, {"Action": "pass", "Package": "fixture"}]))
            suite = dict(kind="go-json", evidence=str(evidence), name="fixture", category="fixture", command="test", exit_code=0,
                started_at="2026-11-18T00:01:00Z", finished_at="2026-11-18T00:01:01Z", elapsed_seconds=1,
                evidence_sha256=hashlib.sha256(evidence.read_bytes()).hexdigest(), evidence_bytes=evidence.stat().st_size)
            manifest = dict(development_complete=True, review_passed=True, code_frozen_at="2026-11-18T00:00:00Z",
                created_at="2026-11-18T00:02:00Z", suites=[suite], coverage=[], source={}, environment={})
            _, data = generate(manifest)
            self.assertFalse(data["accepted"])
            self.assertTrue(data["missing_suites"])
            self.assertTrue(any(r["status"] == "NOT_COVERED" for r in data["coverage"]))
            self.assertTrue(any(r["status"] == "NOT_MET" for r in data["performance"]))
            del suite["finished_at"]
            with self.assertRaisesRegex(ValueError, "completion metadata"):
                generate(manifest)

    def test_dedicated_fixture_covers_opt_in_skip_without_hiding_other_skip(self):
        cases = [{"name": "TestOptIn", "suite": "broad", "status": "SKIP"}, {"name": "TestOptIn", "suite": "real fixture", "status": "PASS"}]
        requirement = [{"title": "fixture", "selectors": ["TestOptIn"]}]
        self.assertEqual(coverage_rows(requirement, cases)[0]["status"], "PASS")
        cases.append({"name": "TestOptIn/Other", "suite": "broad", "status": "SKIP"})
        self.assertEqual(coverage_rows(requirement, cases)[0]["status"], "NOT_COVERED")


if __name__ == "__main__":
    unittest.main()
