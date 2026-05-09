# Runtime Image Assembly Artifact Approval Packet - 2026-05-06

Worker: RuntimeImageAssemblyArtifactApprovalPacketWorker

Verdict: `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_DONE`

Not `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_BLOCKED`: the control-ui runtime persistence fix rerun completed in a repo-owned recovered artifact, and the prior origin-allowlist runtime image exists as the correct parent for a future assembly context. This packet requests approval only; it does not create an assembly artifact.

This packet performed no implementation, no runtime image assembly, no Dockerfile/build-context modification, no patched control-ui file modification, no build/tag/push/pull, no deploy, no backend restart, no fresh instance creation/deletion/modification, no K8S/runtime/database/registry mutation, no browser E2E, no browser storage/cache/cookie cleanup, no gateway token/password/key entry, no Mem0 write, no longterm write-back, no `passes:true`, no Close, and no git stage/commit/push.

## Approval Request

Please approve or reject whether a future worker may execute:

`Runtime Image Assembly Artifact Gate`

Recommended response options:

- `APPROVE_RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_GATE`: authorize the future assembly artifact gate with only the scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not create the runtime image assembly artifact; provide the blocking concern or revised assembly target.

No approval is implied by this packet. The future assembly gate must not start unless the user explicitly approves it.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Control UI Runtime Persistence Fix Implementation Rerun | `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE` |
| Runtime Control UI Source Artifact Recovery | `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_DONE` |
| Runtime Image Build/Tag/Push Origin Allowlist Gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |

Patched control-ui artifact:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`

Patched `assets/index-M4TNVXB3.js` after sha256:

`d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`

Prior origin-allowlist runtime image identity:

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| image index digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` |
| linux/arm64 digest | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` |

## Why Direct Build/Tag/Push Is Not Approved

Do not go directly to build/tag/push from the current startup artifact.

Reason:

- The current startup Dockerfile only contains startup/config/helper `COPY` instructions.
- It copies `/defaults/openclaw-agent/config.yaml`, `/usr/local/bin/openclaw-ensure-controlui-origin`, `/usr/local/bin/openclaw-gateway-with-origin-allowlist`, and `/etc/services.d/openclaw-agent/run`.
- It does not include any `COPY` instruction for the patched control-ui artifact.
- A direct build from that startup context could produce an image that keeps the origin allowlist startup behavior but does not contain the persisted gatewayUrl fix.

The missing assembly step is therefore required before a future Runtime Image Build/Tag/Push Approval Packet.

## Recommended Future Assembly Target

Recommended repo-owned build context:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`

Recommended contents:

- `Dockerfile`
- `control-ui/index.html`
- `control-ui/assets/index-M4TNVXB3.js`
- `control-ui/assets/i18n-B06L7jQN.js`
- `control-ui/assets/zh-CN-B26mMdbY.js`
- optional `MANIFEST.md` recording source hashes and target paths

The future assembly context should be a repo-owned artifact that combines:

- parent image: prior origin-allowlist runtime image
- patched control-ui runtime files: recovered repo-owned control-ui artifact

The future assembly gate should create only this assembly context and its evidence. It must not build, tag, push, pull, deploy, or mutate any runtime/environment state.

## Recommended Future Dockerfile Semantics

Recommended parent:

```Dockerfile
FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45
```

Recommended target runtime path:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

Path rationale:

- Prior image delivery evidence used `/usr/local/lib/node_modules/openclaw/dist/control-ui` as the control-ui copy target.
- The same prior evidence explicitly did not use `/opt/opensparrow/runtime/openclaw/dist/control-ui`.

Recommended `COPY` semantics:

```Dockerfile
COPY --chmod=0644 control-ui/index.html /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
COPY --chmod=0644 control-ui/assets/index-M4TNVXB3.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
COPY --chmod=0644 control-ui/assets/i18n-B06L7jQN.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
COPY --chmod=0644 control-ui/assets/zh-CN-B26mMdbY.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

Future assembly must specifically include:

- `index.html`
- `assets/index-M4TNVXB3.js`
- `assets/i18n-B06L7jQN.js`
- `assets/zh-CN-B26mMdbY.js`

Modes must be preserved or explicitly set to `0644`.

## Future Assembly Source Manifest

Current patched source artifact metadata for the future assembly gate:

| Source artifact path | Mode | Size | sha256 |
| --- | ---: | ---: | --- |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` | `0644` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` | `0644` | `708145` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` | `0644` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` | `0644` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

## Future Assembly Evidence Requirements

The future Runtime Image Assembly Artifact Gate evidence must record:

| Required field | Required content |
| --- | --- |
| parent image tag/digest | Host tag plus digest-pinned parent `localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`; include linux/arm64 digest `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` as platform evidence. |
| target runtime path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` and each destination file path below it. |
| source artifact paths | The four repo-owned recovered/patched control-ui source paths. |
| source sha256/size/mode | SHA-256, byte size, and mode for each source file before assembly copy. |
| destination target paths | The four image target paths in the future Dockerfile. |
| Dockerfile content summary | Parent image, four `COPY --chmod=0644` statements, and no other semantic changes. |
| proof no build/tag/push/pull was run | Command log and explicit negative statement for build/tag/push/pull. |
| no implementation beyond assembly context creation | Evidence that patched JS semantics, startup helper/config semantics, backend/frontend, and environment state were not changed. |

The future assembly evidence must stop with `BLOCKED` if the patched JS after sha256 is not `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`, if the parent image digest is not the prior origin-allowlist image index digest, or if the copy target cannot remain `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

## Future Assembly Explicitly Forbidden

Approval of this packet must not authorize the future assembly gate to:

- modify patched JS semantics
- modify startup helper/config semantics
- modify backend source
- modify frontend GTManager source
- modify unrelated runtime artifacts
- modify deployments, docs, longterm, AgentTeam, spec/plan/tasks, or existing evidence
- build/tag/push/pull
- deploy/restart
- create/delete/modify a fresh instance
- run browser E2E
- use Chrome DevTools MCP
- use Playwright
- mutate K8S/runtime/database/registry state
- clear localStorage/sessionStorage/IndexedDB/cache/cookies
- require manual WebSocket URL editing
- enter gateway token/password/key
- output secrets, token values, cookie values, credentials, or access URL plaintext
- write Mem0
- write longterm
- set `passes:true`
- Close
- git stage/commit/push

Required shorthand for downstream checks:

- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Follow-up Gate Order

1. If the user approves: `Runtime Image Assembly Artifact Gate`.
2. Then `Runtime Image Build/Tag/Push Approval Packet`.
3. Then `Runtime Image Build/Tag/Push Gate`.
4. Then `Fresh Instance / Runtime Deployment Approval Packet`.
5. Then `Browser/Manual E2E Approval Packet`.
6. Then commit/write-back only after passing evidence exists and the user explicitly approves commit, `passes:true`, Close, and longterm write-back scope.

The future assembly gate must not skip directly to build/tag/push, deploy, fresh instance work, browser E2E, commit, write-back, `passes:true`, or Close.

## Current Packet Explicit Negatives

- no runtime image assembly artifact creation
- no Dockerfile modification
- no build context modification
- no patched control-ui file modification
- no backend modification
- no frontend modification
- no runtime-startup-artifact modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance create/delete/modify
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no localStorage cleanup
- no sessionStorage cleanup
- no IndexedDB cleanup
- no cache cleanup
- no cookies cleanup
- no gateway token/password/key entry
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact-approval-packet.md
rg -n "RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_DONE|RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_BLOCKED|runtime image assembly|patched control-ui|index-M4TNVXB3.js|/usr/local/lib/node_modules/openclaw/dist/control-ui|sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact-approval-packet.md
```

Also required:

- secret-shape scan with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact-approval-packet.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `runtime image assembly`, `patched control-ui`, `index-M4TNVXB3.js`, `/usr/local/lib/node_modules/openclaw/dist/control-ui`, the prior origin-allowlist image index digest, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
