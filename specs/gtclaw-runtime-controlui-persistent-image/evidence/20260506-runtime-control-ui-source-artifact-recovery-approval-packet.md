# Runtime Control UI Source Artifact Recovery Approval Packet - 2026-05-06

Worker: RuntimeControlUISourceArtifactRecoveryApprovalPacketWorker

Verdict: `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`

Not `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED`: the dependency gate is blocked on a missing writable repo-owned runtime control-ui artifact/build context, and the read-only `/tmp/gtclaw-runtime-patch/**` reference artifact has enough metadata to request a narrow future recovery approval. This packet does not execute recovery.

This packet is an approval request only. It performed no implementation, no recovery, no copy, no modification of `/tmp/gtclaw-runtime-patch/**`, no build/tag/push/pull, no deploy, no backend restart, no fresh instance creation/deletion/modification, no K8S/runtime/database/registry mutation, no browser E2E, no browser storage/cache/cookie cleanup, no manual WebSocket URL edit, no gateway token/password/key entry, no Mem0 write, no longterm write-back, no `passes:true`, no Close, and no git stage/commit/push.

## Approval Request

Please approve or reject whether a future worker may execute:

`Runtime Control UI Source Artifact Recovery Gate`

Recommended response options:

- `APPROVE_RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_GATE`: authorize the future recovery gate with only the byte-for-byte copy scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not recover the runtime control-ui artifact; provide the blocking concern or revised target.

No approval is implied by this packet. The future recovery gate must not start unless the user explicitly approves it.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Control UI Runtime Persistence Fix Implementation Gate | `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_BLOCKED` |
| Control UI Runtime Persistence Fix Approval Packet | `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_DONE` |
| Control UI Stale Route Root Cause Investigation | `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE` |
| Browser/Manual E2E Origin Allowlist Gate | `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED` |
| Runtime Startup Artifact Origin Allowlist Implementation Rerun | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE` |
| Runtime Image Build/Tag/Push Origin Allowlist Gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |

Current blocker carried forward:

- The implementation gate has no writable, approved repo-owned runtime control-ui artifact/build context containing the persisted gatewayUrl logic.
- The only located bundle is `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js`.
- `/tmp/gtclaw-runtime-patch/**` is currently read-only reference material.
- The current repo feature runtime-startup-artifact contains startup/config wrapper files, not the control-ui JS bundle that needs the persisted gatewayUrl precedence fix.

Root cause carried forward:

- A control-ui bundle persisted gatewayUrl can override the current GTManager-mediated page-derived WebSocket URL.
- The observed stale route shape was an instance `10` WebSocket route on an instance `11` page.
- The intended later implementation must fix bundle precedence, not ask the user to clear storage, edit a URL, or enter a token/password/key.

## Recommended Future Recovery Target

Recommended repo-owned artifact path:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/`

The future recovery gate may create this new repo-owned artifact/build-context directory and copy only the approved files below into it. The purpose is to make a writable source/build-context artifact available for a later Control UI Runtime Persistence Fix Implementation Gate rerun.

Required destination layout:

| Source path | Destination path |
| --- | --- |
| `/tmp/gtclaw-runtime-patch/index.html` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` |
| `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` |
| `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` |
| `/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` |

Future recovery allowed files are limited to exactly these four files:

- `/tmp/gtclaw-runtime-patch/index.html`
- `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js`
- `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js`
- `/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js`

No directory-wide or glob copy is approved except as a future command that still proves the resulting files are exactly the four allowed paths. The safer expected shape is four explicit file copies plus directory creation for `assets/`.

## Read-only Source Metadata

Read-only metadata collected for future recovery planning:

| Source path | Mode | Size | source sha256 |
| --- | ---: | ---: | --- |
| `/tmp/gtclaw-runtime-patch/index.html` | `0644` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | `0644` | `707959` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` |
| `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js` | `0644` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js` | `0644` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

## Future Recovery Scope

If approved, the future Runtime Control UI Source Artifact Recovery Gate may only:

- create the recommended repo-owned artifact directory under `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/`
- byte-for-byte copy the four approved `/tmp/gtclaw-runtime-patch/**` files into that new repo-owned artifact path
- preserve copied file content without modification during recovery
- record source path, destination path, source sha256, destination sha256, source size, destination size, mode, exact copy command, and proof destination is repo-owned
- write one new recovery evidence file for that future gate
- run metadata, hash, status, whitespace, and secret-shape scans with matched values suppressed

The future recovery gate must stop and write a blocked evidence packet if the four source files are missing, their source sha256 or size differs from this packet, the destination path is not repo-owned, or byte-for-byte copy cannot be proven.

## Future Recovery Evidence Requirements

The future recovery evidence must include:

| Required field | Required content |
| --- | --- |
| source path | Absolute source path for each copied file under `/tmp/gtclaw-runtime-patch/`. |
| destination path | Repo-relative destination path under the approved repo-owned artifact target. |
| source sha256 / destination sha256 | SHA-256 before and after copy for every file, with exact equality required. |
| source size / destination size | Byte size before and after copy for every file, with exact equality required. |
| mode | Source and destination mode for every file. Any mode difference must be explained and must not change file content. |
| exact copy command | The exact future command(s) used, preferably explicit `mkdir -p` plus four explicit `cp -p` commands. |
| proof destination is repo-owned | `realpath` proof that the destination is inside the repository plus `git status --short -- <destination-root>` showing the new repo path. |
| no modifications to copied content during recovery | Hash equality proof and a statement that recovery performed byte-for-byte copy only. |

The future recovery evidence must use the literal conclusion: `no modifications to copied content during recovery`.

## Future Recovery Explicitly Forbidden

Approval of this packet must not authorize the future recovery gate to:

- modify copied files content
- patch persisted gatewayUrl bug
- implement the control-ui runtime persistence fix
- build/tag/push/pull
- deploy/restart
- create/delete/modify a fresh instance
- run browser E2E
- use Chrome DevTools MCP
- use Playwright
- mutate K8S/runtime/database/registry state
- mutate backend/frontend/runtime-startup-artifact/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence
- clear localStorage
- clear sessionStorage
- clear IndexedDB
- clear cache
- clear cookies
- edit WebSocket URL
- enter token/password/key
- output secrets, token values, cookie values, credentials, or token-bearing access URLs
- write Mem0
- write longterm
- set `passes:true`
- Close
- git stage/commit/push

Required shorthand for downstream checks:

- no implementation
- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Follow-up Gate Order

1. If the user approves: `Runtime Control UI Source Artifact Recovery Gate`.
2. Then `Control UI Runtime Persistence Fix Implementation Gate` rerun.
3. Then `Runtime Image Build/Tag/Push Approval Packet`.
4. Then fresh instance/runtime deployment approval.
5. Then Browser/Manual E2E approval.
6. Then commit/write-back only after passing evidence exists and the user explicitly approves commit, `passes:true`, Close, and longterm write-back scope.

The future recovery gate must not skip directly to implementation, image build, deploy, fresh instance work, browser E2E, commit, write-back, `passes:true`, or Close.

## Current Packet Explicit Negatives

- no recovery
- no copy
- no implementation
- no runtime control-ui artifact modification
- no `/tmp/gtclaw-runtime-patch/**` modification
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
- no fresh instance
- no fresh instance creation/deletion/modification
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no browser storage/cache/cookie cleanup
- no manual URL edit
- no gateway token/password/key entry
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery-approval-packet.md
rg -n "RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE|RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED|/tmp/gtclaw-runtime-patch|index-M4TNVXB3.js|i18n-B06L7jQN.js|zh-CN-B26mMdbY.js|byte-for-byte copy|repo-owned artifact|source sha256|destination sha256|no implementation|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery-approval-packet.md
```

Also required:

- secret-shape scan with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery-approval-packet.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `/tmp/gtclaw-runtime-patch`, `index-M4TNVXB3.js`, `i18n-B06L7jQN.js`, `zh-CN-B26mMdbY.js`, `byte-for-byte copy`, `repo-owned artifact`, `source sha256`, `destination sha256`, `no implementation`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
