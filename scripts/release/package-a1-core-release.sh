#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
STAMP="$(date +%Y%m%d-%H%M%S)"
BUNDLE_NAME="ClawManager-A1-core-chain-${STAMP}"
STAGE_DIR="$DIST_DIR/$BUNDLE_NAME"

FILES=(
  "00-START-HERE.md"
  "deployments/k3s/clawmanager.yaml"
  "docs/k3s-local-setup.md"
  "docs/manual-skill-import.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/README.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/01-release-package.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/02-release-checklist.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/05-external-test-guide.md"
  "docs/superpowers/releases/2026-04-16-a1-core-release/06-live-demo-record.md"
)

OPTIONAL_EVIDENCE=(
  "/tmp/a1-core-release-dryrun.txt"
  "/tmp/a1-package-healthz.txt"
  "/tmp/a1-package-models.json"
  "/tmp/a1-package-instances-raw.json"
  "/tmp/a1-package-runtime-raw.json"
  "/tmp/a1-package-skills-raw.json"
)

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/evidence/live"

for rel_path in "${FILES[@]}"; do
  mkdir -p "$STAGE_DIR/$(dirname "$rel_path")"
  cp "$ROOT_DIR/$rel_path" "$STAGE_DIR/$rel_path"
done

for evidence_path in "${OPTIONAL_EVIDENCE[@]}"; do
  if [[ -f "$evidence_path" ]]; then
    cp "$evidence_path" "$STAGE_DIR/evidence/live/"
  fi
done

cat > "$STAGE_DIR/START-HERE.md" <<'EOF'
# ClawManager A1 Core-Chain External Test Bundle

从这里开始：

1. 先读 `00-START-HERE.md`
2. 外部测试按 `docs/superpowers/releases/2026-04-16-a1-core-release/05-external-test-guide.md`
3. 精确 bootstrap 按 `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md`
4. 精确验收按 `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
5. 可选手动 Skill 验证按 `docs/manual-skill-import.md`

注意：

- 不要把 scanner / FastSkill / U3 混入这次 A1 签收
- 不要把 provider API key 写回 repo 或 manifest
- 当前 live 演示摘要在 `docs/superpowers/releases/2026-04-16-a1-core-release/06-live-demo-record.md`
EOF

(cd "$STAGE_DIR" && find . -type f | sort | sed 's#^\./##' > FILES.txt)
(cd "$STAGE_DIR" && shasum -a 256 $(find . -type f ! -name 'SHA256SUMS.txt' | sort) > SHA256SUMS.txt)

mkdir -p "$DIST_DIR"
tar -czf "$DIST_DIR/${BUNDLE_NAME}.tar.gz" -C "$DIST_DIR" "$BUNDLE_NAME"

echo "Bundle directory: $STAGE_DIR"
echo "Bundle archive:   $DIST_DIR/${BUNDLE_NAME}.tar.gz"
