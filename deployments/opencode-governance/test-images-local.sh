#!/usr/bin/env bash
# Isolated verification only; /build must be a dedicated build volume.
set -euo pipefail
test "$(bun --version)" = "1.4.2"
bun /governance/apply-governance.mjs /build/source --refresh-patches
cd /build/source/packages/core
bun test test/clawmanager-image-tools.test.ts --timeout 30000
bun test test/clawmanager-governance.test.ts --timeout 30000
