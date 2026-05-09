# Runtime Image Assembly Artifact Gate Evidence

Verdict: `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_DONE`

Gate date: 2026-05-06

## Approval Dependency Record

This gate was executed only after the user-provided dependency gate record:

- `APPROVE_RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_GATE`
- `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_APPROVAL_PACKET_DONE`
- `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE`
- `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_DONE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE`

## Files Created

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/control-ui/index.html`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/control-ui/assets/index-M4TNVXB3.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/control-ui/assets/i18n-B06L7jQN.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/control-ui/assets/zh-CN-B26mMdbY.js`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact.md`

## Source Artifact Metadata

| Source path | Mode | Size | SHA-256 |
|---|---:|---:|---|
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` | `0644` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` | `0644` | `708145` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` | `0644` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` | `0644` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

Patched JS gate check: `index-M4TNVXB3.js` matched required SHA-256 `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`.

## Destination Target Paths

- `control-ui/index.html` -> `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html`
- `control-ui/assets/index-M4TNVXB3.js` -> `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js`
- `control-ui/assets/i18n-B06L7jQN.js` -> `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js`
- `control-ui/assets/zh-CN-B26mMdbY.js` -> `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js`

All destination copies are file mode `0644` in the repo-owned build context.

## Dockerfile Summary

- Parent: `FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- Parent image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- linux/arm64 digest recorded in manifest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`
- The Dockerfile contains only four `COPY --chmod=0644` instructions after the digest-pinned parent.
- It adds no `RUN`, `ENV`, `ENTRYPOINT`, `CMD`, startup script mutation, build-time mutation, or runtime configuration mutation.

## Negative Proofs

- no build/tag/push/pull: no `docker build`, `docker tag`, `docker push`, `docker pull`, `buildctl`, `nerdctl`, `ctr`, `crane`, or `skopeo` command was executed for this gate.
- no K8S: no `kubectl`, `k3d`, Helm, deployment, restart, rollout, pod, service, namespace, or cluster mutation command was executed.
- no runtime/database/registry mutation: no runtime container, fresh instance, MySQL/database, MinIO/object storage, or registry state mutation command was executed.
- no browser E2E: no browser, Chrome DevTools, Playwright, storage cleanup, cache cleanup, or cookie cleanup command was executed.
- no gateway token/password/key was requested or entered.
- no patched control-ui source artifact was modified.
- no JS semantic change was made; files were copied byte-for-byte from the recovered patched source artifact.
- no `runtime-startup-artifact`, backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence file was modified.
- no passes:true update was made.
- no Close action was taken.
- no git stage, commit, or push was performed.

## Assembly Artifact Boundary

This is a repo-owned runtime image assembly build context only. The artifact is ready for a later approval-controlled build/tag/push gate, but this gate intentionally performed no build/tag/push/pull and no deploy or runtime verification.

## Next Gate Recommendation

Next gate recommendation: Runtime Image Build/Tag/Push Approval Packet.
