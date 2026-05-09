# Runtime Image Build/Tag/Push Approval Packet - Control UI Persistence

Verdict: `RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`

Packet date: 2026-05-06

## User Approval Options

To approve the next gate, reply with exactly:

`APPROVE_RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_GATE`

To reject or block the next gate, reply with:

`REJECT_OR_BLOCK: <reason>`

## Dependency Evidence Checked

This approval packet was prepared after read-only review of:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact.md`

Confirmed dependency gates:

- `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_DONE`
- `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_DONE`
- `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE`

## Why This Gate Is Next

The runtime image assembly artifact now exists as a repo-owned build context with a digest-pinned parent and byte-for-byte copied control-ui files. That makes it appropriate to request approval for the next image publication step: build/tag/push.

This packet must not jump to deploy, fresh instance, or browser E2E because there is not yet a newly built and pushed runtime image tag, image index digest, or linux/arm64 digest for the control-ui persistence artifact. Deployment and E2E require a published image and separate approval after the build/tag/push evidence exists.

## Future Build Scope

- Future build context: `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`
- Future build parent: `localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- Future platform: `linux/arm64`

Recommended tag prefixes for the future gate:

- `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-<timestamp>`
- `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-<timestamp>`

## Future Post-Build Evidence Required

The future build/tag/push gate must record:

- Image tag.
- Image index digest.
- linux/arm64 digest.
- Dockerfile used.
- Assembly source hashes:
  - `index.html`: `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec`
  - `index-M4TNVXB3.js`: `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`
  - `i18n-B06L7jQN.js`: `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63`
  - `zh-CN-B26mMdbY.js`: `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809`
- Proof control-ui files are present in the image target path if inspected:
  - `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html`
  - `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js`
  - `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js`
  - `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js`

## Explicit Boundary

Future approval authorizes only image build/tag/push and registry image publication for the assembly context above.

It does not authorize:

- no deploy or restart.
- no fresh instance create/delete/modify.
- no browser E2E.
- no K8S/runtime/database mutation.
- no deployment evidence, browser evidence, or feature closure.
- no passes:true.
- no Close.

## Current Packet Negative Proofs

This packet-writing gate performed only read-only dependency checks and wrote this approval packet.

- No assembly artifact was modified.
- No control-ui source artifact was modified.
- No patched JS semantics were modified.
- No runtime-startup-artifact, backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence was modified.
- No build/tag/push/pull was executed.
- No deploy/restart was executed.
- No fresh instance create/delete/modify was executed.
- No K8S/runtime/database/registry mutation was executed.
- No browser E2E, Chrome DevTools, Playwright, browser storage cleanup, cache cleanup, or cookie cleanup was executed.
- No gateway token/password/key was requested or entered.
- No Mem0 write or longterm write-back was performed.
- No passes:true update was made.
- No Close action was taken.
- No git stage, commit, or push was performed.
