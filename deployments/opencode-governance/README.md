# OpenCode 2.0.27 governance adapter

Pinned upstream: `1f4d07a4a519185955ae8ab2ea3607805509e386` (`v2.0.27`).
This replaces the experimental 1.18.18-source patch; old `.mjs`/effect helper files
are historical and are NOT referenced by this installer or Dockerfile.

## Compatibility

- Deploy the compatible ClawManager backend and ALL migrations shipped with it
  FIRST (currently through 073, including durable image jobs). Existing instances are not auto-enrolled.
- Existing unpatched instances remain protocol 0. No migration enrolls them.
- The adapter is active only when `CLAWMANAGER_MODEL_GOVERNANCE_PROTOCOL=1`,
  `CLAWMANAGER_LLM_BASE_URL` and `CLAWMANAGER_LLM_API_KEY` are present.
- Capability negotiation enrolls an instance in protocol 1. Enrollment is sticky:
  dropping task headers or restarting cannot silently disable governance.
- Do not downgrade an enrolled runtime to an unpatched image while reopening
  traffic. Controlled rollback/unenrollment is NOT implemented yet.
- No UI source or OpenCode database schema is modified.
- The optional catalog `vision` flag updates managed model image-input modalities.
  Older servers omitting it keep the runtime's existing input modalities. This
  does not enable image generation or install image tools. A new runtime image
  must be built and verified before existing instances can use this adapter change.
- Adapter `cm-governance-2` adds admission draining and process-local activity
  reports. Automatic in-place upgrade is NOT implemented. Do not roll existing
  busy instances automatically or treat a drain report as a backup certificate.

## Source verification and tests

```sh
git clone --depth 1 --branch v2.0.27 https://github.com/anomalyco/opencode.git source
node opencode/model-governance/apply-governance.mjs source --check
node opencode/model-governance/apply-governance.mjs source
cd source
bun install --frozen-lockfile
cd packages/core
bun typecheck
bun test test/clawmanager-governance.test.ts --timeout 30000
bun test test/model.test.ts test/session-model-request.test.ts test/session-model-request-hooks.test.ts test/session-execution.test.ts test/session-title.test.ts test/session-runner.test.ts test/session-runner-tool-events.test.ts test/session-compaction.test.ts test/generate.test.ts test/session-generate.test.ts --timeout 30000
```

Use the upstream-required Bun 1.4.2 for delivery builds. The adapter tests set
their own environment and must run in a SEPARATE process from upstream tests.
The installer checks the exact Git commit and unmodified target files before
writing. A manifest records patched file hashes; reapplication verifies them.
For local adapter development only, `--refresh-patches` reapplies from the pinned
upstream after verifying that every previously patched file remains unchanged.

## Build a test image (AgentsRuntime repository root)

```sh
docker build -f opencode/model-governance/Dockerfile \
  --build-arg BASE_IMAGE=<existing-opencode-image@sha256:digest> \
  -t clawmanager-local/opencode-governance:2.0.27-cm2 opencode/model-governance
```

The correct build entry is `packages/cli/script/build.ts`, not the old
`packages/opencode` path. The target is `opencode-linux-x64`, while the output
directory is `packages/cli/dist/cli-linux-x64/bin/opencode`.
Build Lite and Pro overlays separately. Do not overwrite the current shared
runtime pool; a separate test pool/instance is required for integration checks.

When Docker Hub is unavailable, `BUN_BUILD_IMAGE` may reference a locally built
Linux toolchain containing the official Bun **1.4.2** binary, Git, Python and a
C/C++ compiler. The Dockerfile verifies the Bun version before installing any
dependencies; this override is not permission to downgrade Bun. Download official
packages from `registry.npmjs.org` and verify their published integrity first.
For example, add `--build-arg BUN_BUILD_IMAGE=<verified-local-toolchain>` to the
command above. For local-only base images, use a unique version tag and record
the resolved digest in the build output; do not substitute a moving `latest`.

The Go-based typechecker defaults to `TYPECHECK_GOMAXPROCS=2` and
`TYPECHECK_GOMEMLIMIT=2GiB` during the build. The latter is a **soft Go memory
limit**, not a container memory cap. These settings are not passed to the final
runtime image and do not guarantee enough host RAM for the full build. Keep
memory headroom for existing Kubernetes workloads; do not restart their shared
Docker engine or stop user instances merely to finish a build. Both values can
be overridden with explicit build arguments on a larger, dedicated builder.

### Resource-bounded local verification

`build-local.sh` supports an isolated Linux build container when the local Docker
engine is shared with Kubernetes. Mount the adapter directory read-only at
`/governance`, a Git bundle of the pinned commit read-only at
`/tmp/opencode-source.bundle`, and a dedicated writable volume at `/build`.
Run it with the verified Bun toolchain, explicit `--memory`, `--memory-swap` and
`--cpus` limits, and `--entrypoint bash ... /governance/build-local.sh`.
Equal memory and memory-swap values disable swap for that container.

The script verifies the commit, applies the adapter, installs locked dependencies,
checks types, runs both test processes, then builds the CLI with the stock web UI.
It stops on any failed step. Type checking has a default 600-second deadline,
configurable through `TYPECHECK_TIMEOUT_SECONDS`; timeout/OOM is **not** a pass.
Keep the dedicated volume for retries. Do not mount a live instance workspace.
The script does not build/tag a final runtime image or deploy anything to K8s.

Local verification on 2026-10-21 completed **11 governance tests** and
**427 upstream regression tests** (one upstream test skipped, zero failures).
These were run separately after full type checking exceeded the 2.25 GiB
container limit. No release runtime image or live-instance acceptance is implied.

## Governed image tools (not yet deployed)

`image-tools.ts` installs five native function tools using the existing core
plugin registry: `clawmanager_image_models`, `clawmanager_generate_image`,
`clawmanager_image_request_status`, `clawmanager_save_image` and
`clawmanager_edit_image`. They are registered only when governance is
enabled and advertised only for the `clawmanager` provider. Both OpenAI Chat and
Anthropic Messages prepared turns use the same tool executor; this does not add
an Anthropic native image-generation API or a Responses hosted tool.

The leaf enforces OpenCode permissions, obtains live task headers from Governance,
and sends all requests to the configured Gateway with its instance credential.
One image is generated per call. Stable idempotency uses session, task, generation
and tool-call identity. Cancellation aborts the HTTP exchange; it cannot undo
provider work already performed. Unknown outcomes must not be automatically
resubmitted as new tool calls. The model catalog and all result reads are filtered
by Gateway authorization, not by trusting the language model.

Generation and editing accept `model: "auto"` for Gateway's separate image
capability/priority/load/latency policy. The tool result preserves only safe
selected-model metadata (alongside artifact IDs); secrets and image pixels are
stripped. Auto never means a chat classifier or provider-side `auto` model.
Gateway pins the winning physical model to the durable idempotency task; retries
must not reselect a new provider when rankings change or access is revoked.
There is no automatic paid fallback, streaming or Responses hosted-tool support.

Generation/edit results are authenticated artifact IDs only. The agent can then
call `clawmanager_save_image` with an artifact ID and relative filename; this uses
the normal file-edit permission, downloads through Gateway authorization, and
rechecks task authorization before writing. Refresh the existing ClawManager
Workspace panel and click the saved image to preview/download it. This does not
add an inline image renderer to OpenCode or automatically save every result.

`clawmanager_edit_image` reads selected workspace files with normal read permission
and submits `image[]` and an optional PNG `mask` as multipart form data to
`/images/edits`. Select a model advertising `edit`; generation support alone is
not sufficient. The provider receives the selected image bytes. File paths,
pixels and upstream credentials are not placed in the tool result/chat context.
The upload format follows the [OpenAI image edit API](https://developers.openai.com/api/docs/guides/image-generation#edit-images);
Anthropic conversations invoke this as a function tool, not a native image API.

Local tool limits: PNG/JPEG/WebP, 5 MiB per input/download, up to 16 input images,
24 MiB total inputs including the optional mask. Gateway additionally validates
image structure, mask dimensions, model capability and budget. Paths must be
relative to the active workspace; parents must exist. Traversal, symbolic-link
components and existing save destinations are rejected. FileMutation serializes
cooperating writers; this is not an OS-exclusive create or crash-atomic file
transaction against concurrent external writers. Completed local copies cannot
be erased by later model revocation. Interrupted downloads do not write partial
files; an interruption during filesystem writing follows FileMutation semantics.

Old/unpatched instances and other runtimes do not gain these tools automatically.
Real-provider and live-instance acceptance, plus image rebuild/deployment, remain
pending. Never claim this source patch upgrades existing instances.

Run `bun test test/clawmanager-image-tools.test.ts --timeout 30000` in a separate
Bun 1.4.2 process from governance/upstream tests; test adapters pin environment at
module load. `test-images-local.sh` verifies both suites in a dedicated `/build`
volume without rebuilding the CLI or touching K8s. No real provider API is called.

2026-10-23 verification: core and test type checking passed; Bun 1.4.2 passed
7 image-tool tests (117 assertions, including Auto input and safe selection
metadata) and 12 governance tests (49 assertions).
OpenAI/Anthropic prepared turns both exercised generation, workspace save and
editing. Real temporary files exercised masked edits, read/write denial, unsafe
paths, size limits, revocation and cancellation during download. These are local
fixture tests, not real-provider or running-instance acceptance.

## Cancellation limits and pending validation

Primary requests, retries, tool-continuation steps and automatic compaction use
the authenticated user-message task identity. Auxiliary title/generate requests
use separate task identities. Cancellation races join scoped execution cleanup;
primary receipts are sent only from execution settlement after job cancellation.
Network failures stop managed work, never downgrade it. Unconfirmed receipts
remain unconfirmed; a crash may lose in-memory pending receipts.

Completed file writes and external side effects cannot be undone. Detached
external processes are not claimed to be safely interruptible. Real Lite/Pro
runtime, background-job/subagent cancellation and data-preserving migration must
pass integration acceptance before this is considered a production upgrade.

## Instance drain check (not an upgrade command)

Admin APIs: `POST /api/v1/instances/:id/runtime-maintenance` with
`{"timeout_seconds":300}`, owner/admin `GET` at the same URL, and admin
`POST .../runtime-maintenance/cancel` with the exact returned `operation_id`.
The management instance page exposes these under its drain-check disclosure.

The adapter polls `/runtime/maintenance` once per second through its authenticated
Gateway endpoint. A request closes new public Session admissions, while previously
accepted work may finish. Reports include active executions, admission operations,
background job scopes (through cleanup) and durable queued inputs. A drain is
acknowledged only with a matching operation/generation, closed gate, all zero
counters and a report younger than five seconds. Queued admit-only work is NOT
dropped or force-run to obtain zero. Timeout (10–600 seconds) or explicit cancel
reopens on the next successful poll. A failed poll cannot reopen a closed gate;
stale control blocks new admissions until connectivity recovers.

Legacy runtimes without fresh `maintenance_drain` capability return 409 when an
admin requests draining; normal legacy use is unaffected. The old OpenCode
shared-pool rolling replacement API is blocked rather than reused for safe
migration. This does not add process-stop confirmation, placement fencing,
consistent backup, data restore, controlled protocol rollback or per-instance
pool migration. No operation in this feature stops a process or changes an image.
