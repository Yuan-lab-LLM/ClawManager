# Build context: opencode/model-governance directory in AgentsRuntime.
# BASE_IMAGE must be an immutable existing Lite/Pro image, not a moving latest.
ARG BASE_IMAGE
ARG BUN_BUILD_IMAGE=oven/bun:1.4.2
FROM ${BUN_BUILD_IMAGE} AS builder
USER root
RUN test "$(bun --version)" = "1.4.2" \
 && (command -v git && command -v python3 && command -v gcc && command -v g++ && command -v make \
     || (apt-get update && apt-get install -y --no-install-recommends git python3 build-essential ca-certificates))
WORKDIR /src
ARG OPENCODE_REVISION=1f4d07a4a519185955ae8ab2ea3607805509e386
RUN git init && git remote add origin https://github.com/anomalyco/opencode.git && git fetch --depth 1 origin ${OPENCODE_REVISION} && git checkout --detach FETCH_HEAD
RUN bun install --frozen-lockfile
# Keep the locked dependency layer reusable when only the adapter/docs change.
COPY apply-governance.mjs governance.ts governance.test.ts image-tools.ts image-tools.test.ts /governance/
RUN bun /governance/apply-governance.mjs /src
# Limit the Go-based typechecker while local Kubernetes workloads are running.
# Build-only settings; these are not inherited by the final runtime image.
ARG TYPECHECK_GOMAXPROCS=2
ARG TYPECHECK_GOMEMLIMIT=2GiB
RUN cd packages/core && GOMAXPROCS=${TYPECHECK_GOMAXPROCS} GOMEMLIMIT=${TYPECHECK_GOMEMLIMIT} bun typecheck && bun test test/clawmanager-governance.test.ts --timeout 30000
RUN cd packages/core && bun test test/clawmanager-image-tools.test.ts --timeout 30000
RUN cd packages/core && bun test test/model.test.ts test/session-model-request.test.ts test/session-model-request-hooks.test.ts test/session-execution.test.ts test/session-title.test.ts test/session-runner.test.ts test/session-runner-tool-events.test.ts test/session-compaction.test.ts test/generate.test.ts test/session-generate.test.ts test/job.test.ts test/session-prompt.test.ts test/session-shell.test.ts test/session-compact.test.ts test/session-move.test.ts test/session-skill.test.ts --timeout 30000
ENV OPENCODE_VERSION=2.0.27 OPENCODE_CHANNEL=latest
RUN cd packages/cli && bun run script/build.ts --target=opencode-linux-x64
RUN /src/packages/cli/dist/cli-linux-x64/bin/opencode --version

FROM ${BASE_IMAGE}
COPY --chmod=755 --from=builder /src/packages/cli/dist/cli-linux-x64/bin/opencode /usr/local/bin/opencode
ENV CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL=1
LABEL io.clawmanager.opencode-revision="1f4d07a4a519185955ae8ab2ea3607805509e386" \
      io.clawmanager.governance-adapter="cm-governance-2" \
      io.clawmanager.governance-protocol="1"
# Existing base entrypoint, credentials, paths and stock UI are preserved.
# Drain observation is supported; automatic data-safe upgrade is NOT implemented.
