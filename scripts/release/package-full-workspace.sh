#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
STAMP="$(date +%Y%m%d-%H%M%S)"
BUNDLE_NAME="ClawManager-full-workspace-${STAMP}"
TMP_ARCHIVE="/tmp/${BUNDLE_NAME}.tar.gz"
FINAL_ARCHIVE="$DIST_DIR/${BUNDLE_NAME}.tar.gz"

mkdir -p "$DIST_DIR"
rm -f "$TMP_ARCHIVE"

tar \
  --exclude='./.git' \
  --exclude='./.codex/auth.json' \
  --exclude='./.codex/config.toml' \
  --exclude='./dist/ClawManager-full-workspace-*.tar.gz' \
  -czf "$TMP_ARCHIVE" \
  -C "$ROOT_DIR" \
  .

mv "$TMP_ARCHIVE" "$FINAL_ARCHIVE"

echo "Workspace archive: $FINAL_ARCHIVE"
echo "Read first:        00-START-HERE.md"
echo "Extract into:      an empty directory"
du -sh "$FINAL_ARCHIVE"
