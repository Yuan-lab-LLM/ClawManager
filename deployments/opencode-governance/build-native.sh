#!/usr/bin/env bash
# Native release builder shared by ordinary Lite/Pro Docker builds.
set -euo pipefail
test "$(bun --version)" = 1.4.2
revision=1f4d07a4a519185955ae8ab2ea3607805509e386
case "${TARGETARCH:-$(uname -m)}" in
 amd64|x86_64) target=opencode-linux-x64; binary=cli-linux-x64 ;;
 arm64|aarch64) target=opencode-linux-arm64; binary=cli-linux-arm64 ;;
 *) echo 'Unsupported native OpenCode release architecture' >&2; exit 64 ;;
esac
mkdir -p /src
cd /src
git init
git remote add origin https://github.com/anomalyco/opencode.git
git fetch --depth 1 origin "$revision"
git checkout --detach FETCH_HEAD
bun /governance/apply-governance.mjs /src
bun install --frozen-lockfile
cd packages/core
GOMAXPROCS=2 GOMEMLIMIT=2GiB bun typecheck
bun test test/clawmanager-governance.test.ts test/clawmanager-image-tools.test.ts test/clawmanager-team-tools.test.ts --timeout 30000
TEAM_TEST_GENERATION=0 bun test test/clawmanager-team-tools.test.ts --timeout 30000
cd ../cli
OPENCODE_VERSION=2.0.27 OPENCODE_CHANNEL=latest bun run script/build.ts --target="$target"
mkdir -p /out
cp "dist/$binary/bin/opencode" /out/opencode
cp /src/.clawmanager-governance-manifest.json /out/governance-manifest.json
/out/opencode --version | grep -Fx 'opencode v2.0.27'
