#!/usr/bin/env python3
"""Add the canonical preview bridge to an existing pinned Hermes renderer.

Only index.html, build-info.json and native-preview-bridge.js are written.
Run against the renderer directory extracted from the existing manager image;
this is a managed overlay, not a rebuild of the pinned upstream renderer.
"""
import argparse
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re
from urllib.parse import urlsplit


class Scripts(HTMLParser):
    def __init__(self, html):
        super().__init__()
        self.lines = html.splitlines(keepends=True)
        self.scripts = []
        self.feed(html)

    def handle_starttag(self, tag, attributes):
        if tag == 'script':
            line, column = self.getpos()
            self.scripts.append((sum(map(len, self.lines[:line - 1])) + column, dict(attributes)))


def overlay(renderer, bridge, source_sha):
    if not re.fullmatch(r'[0-9a-f]{40}', source_sha):
        raise ValueError('source-sha must be the 40-character source Git commit')
    index = renderer / 'index.html'
    metadata = renderer / 'build-info.json'
    html = index.read_bytes().decode('utf-8')
    info = json.loads(metadata.read_bytes())
    if not isinstance(info, dict):
        raise ValueError('build-info.json must be an object')
    scripts = Scripts(html).scripts
    entries = [(offset, attrs) for offset, attrs in scripts if attrs.get('type', '').lower() == 'module' and attrs.get('src')]
    if not entries:
        raise ValueError('Existing external module entry is required')
    entry_offset = entries[0][0]
    existing = [(offset, attrs) for offset, attrs in scripts if urlsplit(attrs.get('src') or '').path.split('/')[-1] == 'native-preview-bridge.js']
    if existing:
        if len(existing) != 1 or existing[0][0] >= entry_offset:
            raise ValueError('Existing preview bridge must appear once before the module entry')
        attrs = existing[0][1]
        source = urlsplit(attrs.get('src') or '')
        if source.scheme or source.netloc or source.path not in ('./native-preview-bridge.js', 'native-preview-bridge.js', '/hermes-desktop-web/native-preview-bridge.js') or attrs.get('type', '').lower() not in ('', 'text/javascript') or 'async' in attrs or 'defer' in attrs or 'integrity' in attrs:
            raise ValueError('Existing preview bridge must be a synchronous same-origin classic script without integrity')
    else:
        html = html[:entry_offset] + '<script src="./native-preview-bridge.js"></script>' + html[entry_offset:]
    bridge_bytes = bridge.read_bytes()
    info['managed_overlay'] = {
        'kind': 'native-preview-bridge',
        'sourceSHA': source_sha,
        'bridge_sha256': hashlib.sha256(bridge_bytes).hexdigest(),
        'renderer_rebuilt': False,
    }
    # Validate every input before writing; preserve all pinned renderer assets.
    (renderer / 'native-preview-bridge.js').write_bytes(bridge_bytes)
    index.write_bytes(html.encode('utf-8'))
    metadata.write_text(json.dumps(info, indent=2, ensure_ascii=False) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--renderer-dir', type=Path, required=True)
    parser.add_argument('--bridge', type=Path, required=True)
    parser.add_argument('--source-sha', required=True)
    arguments = parser.parse_args()
    overlay(arguments.renderer_dir, arguments.bridge, arguments.source_sha)
