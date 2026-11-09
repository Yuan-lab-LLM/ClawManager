#!/usr/bin/env bash
# Resource-bounded local build. Run in an isolated container, never in a runtime.
set -euo pipefail
test "$(bun --version)" = "1.4.2"
revision=1f4d07a4a519185955ae8ab2ea3607805509e386
if [ ! -d /build/source/.git ]; then
  git clone /tmp/opencode-source.bundle /build/source
fi
cd /build/source
test "$(git rev-parse HEAD)" = "$revision"
bun /governance/apply-governance.mjs /build/source
bun install --frozen-lockfile
export GOMAXPROCS="${TYPECHECK_GOMAXPROCS:-2}" GOMEMLIMIT="${TYPECHECK_GOMEMLIMIT:-2GiB}"
cd packages/core
timeout --signal=TERM --kill-after=10s "${TYPECHECK_TIMEOUT_SECONDS:-600}s" bun typecheck
bun test test/clawmanager-governance.test.ts --timeout 30000
bun test test/clawmanager-image-tools.test.ts --timeout 30000
bun test test/model.test.ts test/session-model-request.test.ts test/session-model-request-hooks.test.ts test/session-execution.test.ts test/session-title.test.ts test/session-runner.test.ts test/session-runner-tool-events.test.ts test/session-compaction.test.ts test/generate.test.ts test/session-generate.test.ts test/job.test.ts test/session-prompt.test.ts test/session-shell.test.ts test/session-compact.test.ts test/session-move.test.ts test/session-skill.test.ts --timeout 30000
cd ../cli
export OPENCODE_VERSION=2.0.27 OPENCODE_CHANNEL=latest
bun run script/build.ts --target=opencode-linux-x64
./dist/cli-linux-x64/bin/opencode --version
sha256sum ./dist/cli-linux-x64/bin/opencode
