"""Local tests: Docker is mocked; archive verification uses the real verifier."""
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("pull_github", ROOT / "scripts/pull-github-image.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
IMAGE = "ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:v2026.9.13"
REAL_RUN = subprocess.run


class PullTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.lock = self.root / "images.lock"
        self.original = b"# keep\r\nruntime.openclaw.lite|old:1\r\nruntime.hermes.pro|\r\nplatform.mysql|mysql:8.4.8\r\n"
        self.lock.write_bytes(self.original)
        self.fail = None
        self.calls = []

    def fake_run(self, args, **kwargs):
        if args[0] != "docker":
            return REAL_RUN(args, **kwargs)
        self.calls.append(args)
        if args[1] == self.fail:
            raise subprocess.CalledProcessError(1, args)
        if args[1] == "image":
            return subprocess.CompletedProcess(args, 0, stdout=(
                "linux/arm64" if self.fail == "arch" else "linux/amd64"))
        if args[1] == "save":
            payload = json.dumps([{"RepoTags": [IMAGE if self.fail != "tags" else "wrong:1"]}]).encode()
            with tarfile.open(args[3], "w") as archive:
                member = tarfile.TarInfo("manifest.json")
                member.size = len(payload)
                archive.addfile(member, io.BytesIO(payload))
        if args[1] == "pull" and self.fail == "concurrent":
            self.lock.write_bytes(self.original + b"# user edit\n")
        return subprocess.CompletedProcess(args, 0)

    def execute(self, key="runtime.openclaw.lite", image=IMAGE):
        with patch.object(MODULE.subprocess, "run", side_effect=self.fake_run):
            return MODULE.prepare(key, image, self.lock, self.root / "output")

    def test_success_and_retry(self):
        target = self.execute()
        self.assertTrue(target.name.startswith("runtime-images-"))
        self.assertTrue(Path(str(target) + ".sha256").exists())
        expected = self.original.replace(b"old:1", IMAGE.encode())
        self.assertEqual(self.lock.read_bytes(), expected)
        self.assertEqual(next(self.root.glob("images.lock.backup-*")).read_bytes(), self.original)
        self.assertIn(["docker", "pull", "--platform", "linux/amd64", IMAGE], self.calls)
        self.calls.clear()
        self.assertEqual(self.execute(), target)
        self.assertEqual(self.calls, [])
        self.assertEqual(len(list(self.root.glob("images.lock.backup-*"))), 1)

    def test_corrupt_archive_rebuilt(self):
        target = self.execute()
        target.write_bytes(b"broken")
        self.calls.clear()
        self.execute()
        self.assertTrue(any(call[1] == "save" for call in self.calls))

    def test_latest_digest_change_rebuilt(self):
        def prepare(digest):
            with patch.object(MODULE.subprocess, "run", side_effect=self.fake_run):
                return MODULE.prepare("runtime.openclaw.lite", IMAGE, self.lock,
                                      self.root / "output", pull_digest=digest)
        prepare("sha256:" + "a" * 64)
        self.calls.clear()
        prepare("sha256:" + "a" * 64)
        self.assertEqual(self.calls, [])
        prepare("sha256:" + "b" * 64)
        self.assertTrue(any(call[1] == "pull" for call in self.calls))

    def test_failures_preserve_lock(self):
        for failure in ("pull", "save", "tags", "arch"):
            with self.subTest(failure=failure):
                self.fail = failure
                with self.assertRaises((ValueError, subprocess.CalledProcessError)):
                    self.execute()
                self.assertEqual(self.lock.read_bytes(), self.original)
                self.assertFalse(list((self.root / "output").glob("*.tar")))

    def test_invalid_inputs_do_not_call_docker(self):
        for key, image in (("runtime.missing.lite", IMAGE),
                           ("runtime.openclaw.lite", IMAGE.rsplit(":", 1)[0] + ":latest"),
                           ("runtime.openclaw.lite", "https://github.com/package")):
            with self.assertRaises(ValueError):
                self.execute(key, image)
        self.assertEqual(self.calls, [])

    def test_concurrent_edit_preserved(self):
        self.fail = "concurrent"
        with self.assertRaisesRegex(ValueError, "changed"):
            self.execute()
        self.assertEqual(self.lock.read_bytes(), self.original + b"# user edit\n")

    def test_batch_preview_and_groups(self):
        catalog = self.root / "release.conf"
        catalog.write_text(f"runtime.openclaw.lite|{IMAGE}\nplatform.mysql|mysql:8.4.8\nruntime.hermes.pro|\n")
        with patch.object(MODULE, "prepare") as prepare:
            plan = MODULE.prepare_catalog(catalog, self.lock, self.root, dry_run=True)
            self.assertEqual(len(plan), 2)
            prepare.assert_not_called()
            MODULE.prepare_catalog(catalog, self.lock, self.root, groups="platform-base")
            prepare.assert_called_once_with("platform.mysql", "mysql:8.4.8", self.lock, self.root)
        self.assertEqual(self.lock.read_bytes(), self.original)

    def test_bad_later_entry_fails_before_first_download(self):
        catalog = self.root / "release.conf"
        catalog.write_text(f"runtime.openclaw.lite|{IMAGE}\nplatform.mysql|mysql:latest\n")
        with patch.object(MODULE, "prepare") as prepare:
            with self.assertRaises(ValueError):
                MODULE.prepare_catalog(catalog, self.lock, self.root)
            prepare.assert_not_called()

    def test_shipped_catalog_covers_only_application_lock_keys(self):
        sources = MODULE.read_mapping(ROOT / "images/release-images.conf")
        lock = MODULE.read_mapping(ROOT / "images/images.lock")
        self.assertEqual(set(sources), {key for key in lock if key.startswith(("platform.", "runtime."))})
        self.assertEqual(sources["runtime.workbuddy.internal"], "")
        for key, image in sources.items():
            if image:
                MODULE.validate_reference(key, image.replace(":latest", ":version-check"))

    def test_batch_latest_uses_resolved_tag_and_digest(self):
        catalog = self.root / "release.conf"
        latest = IMAGE.rsplit(":", 1)[0] + ":latest"
        catalog.write_text(f"runtime.openclaw.lite|{latest}\n")
        digest = "sha256:" + "a" * 64
        with patch.object(MODULE, "resolve_latest", return_value=(IMAGE, digest)) as resolve, \
                patch.object(MODULE, "prepare") as prepare:
            MODULE.prepare_catalog(catalog, self.lock, self.root, dry_run=True)
            resolve.assert_not_called()
            MODULE.prepare_catalog(catalog, self.lock, self.root)
            prepare.assert_called_once_with("runtime.openclaw.lite", IMAGE, self.lock, self.root, pull_digest=digest)

    def test_unresolved_latest_does_not_pull_or_update(self):
        catalog = self.root / "release.conf"
        catalog.write_text(f"platform.mysql|mysql:8.4.8\nruntime.openclaw.lite|{IMAGE.rsplit(':', 1)[0]}:latest\n")
        with patch.object(MODULE, "resolve_latest", side_effect=ValueError("no matching tag")), \
                patch.object(MODULE, "prepare") as prepare:
            with self.assertRaises(ValueError):
                MODULE.prepare_catalog(catalog, self.lock, self.root)
            prepare.assert_not_called()
        self.assertEqual(self.lock.read_bytes(), self.original)

    def test_registry_matches_digest_not_newest_date(self):
        import ghcr_release
        with patch.object(ghcr_release, "Registry") as registry:
            registry.return_value.tags.return_value = ["latest", "v2026.9.17", "v2026.9.13", "build-123", "v2026.99.99"]
            registry.return_value.digest.side_effect = lambda tag: "same" if tag in {"latest", "v2026.9.13"} else "different"
            self.assertEqual(ghcr_release.resolve_latest(IMAGE.rsplit(':', 1)[0] + ':latest'), (IMAGE, "same"))


if __name__ == "__main__":
    unittest.main()
