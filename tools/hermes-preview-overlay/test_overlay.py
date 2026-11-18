import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('overlay.py')
REPO = SCRIPT.parents[2]
BRIDGE = REPO / 'backend/internal/services/native_preview_bridge.js'
SOURCE_SHA = '1' * 40


class OverlayTest(unittest.TestCase):
    def test_preserves_pinned_assets_and_installs_bridge_before_module_once(self):
        with tempfile.TemporaryDirectory() as temporary:
            renderer = Path(temporary)
            html = '<html><head><meta http-equiv="Content-Security-Policy" content="script-src \'self\'"><script type="module" crossorigin src="./assets/index-pinned.js"></script></head></html>\n'
            (renderer / 'index.html').write_text(html)
            (renderer / 'build-info.json').write_text(json.dumps({'hermes_commit': 'pinned', 'acceptance': 'not-asserted-by-build'}))
            (renderer / 'assets').mkdir()
            asset = renderer / 'assets/index-pinned.js'
            asset.write_bytes(b'original pinned module bytes\x00')
            pinned = hashlib.sha256(asset.read_bytes()).hexdigest()
            command = [sys.executable, str(SCRIPT), '--renderer-dir', str(renderer), '--bridge', str(BRIDGE), '--source-sha', SOURCE_SHA]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            tag = '<script src="./native-preview-bridge.js"></script>'
            self.assertEqual((renderer / 'index.html').read_text(), html.replace('<script type="module"', tag + '<script type="module"', 1))
            self.assertEqual((renderer / 'native-preview-bridge.js').read_bytes(), BRIDGE.read_bytes())
            self.assertEqual(hashlib.sha256(asset.read_bytes()).hexdigest(), pinned)
            info = json.loads((renderer / 'build-info.json').read_text())
            self.assertEqual(info['hermes_commit'], 'pinned')
            self.assertEqual(info['acceptance'], 'not-asserted-by-build')
            self.assertFalse(info['managed_overlay']['renderer_rebuilt'])
            self.assertEqual(info['managed_overlay']['sourceSHA'], SOURCE_SHA)
            self.assertEqual(info['managed_overlay']['bridge_sha256'], hashlib.sha256(BRIDGE.read_bytes()).hexdigest())
            first = {str(p.relative_to(renderer)): p.read_bytes() for p in renderer.rglob('*') if p.is_file()}
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(first, {str(p.relative_to(renderer)): p.read_bytes() for p in renderer.rglob('*') if p.is_file()})

    def test_invalid_artifact_is_rejected_without_changes(self):
        with tempfile.TemporaryDirectory() as temporary:
            renderer = Path(temporary)
            (renderer / 'index.html').write_text('<script src="./native-preview-bridge.js"></script>')
            (renderer / 'build-info.json').write_text('{}')
            before = {p.name: p.read_bytes() for p in renderer.iterdir()}
            result = subprocess.run([sys.executable, str(SCRIPT), '--renderer-dir', str(renderer), '--bridge', str(BRIDGE), '--source-sha', SOURCE_SHA], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(before, {p.name: p.read_bytes() for p in renderer.iterdir()})


if __name__ == '__main__':
    unittest.main()
