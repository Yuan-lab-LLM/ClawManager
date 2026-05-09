# Runtime Image Build/Tag/Push Gate Evidence - Control UI Persistence

Verdict: `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE`

Gate date: 2026-05-06

## Build Approval Dependency Record

This gate was executed only after the user-provided approval and dependency record:

- `APPROVE_RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_GATE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`
- `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_DONE`
- `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE`

Read-only dependency packet checked:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-approval-packet-controlui-persistence.md`

## Build Context

- Build context path: `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`
- Dockerfile path: `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/Dockerfile`
- Platform: `linux/arm64`

## Dockerfile Content Summary

The Dockerfile preflight confirmed exactly five non-empty instructions:

- `FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- Four `COPY --chmod=0644` instructions into `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

No `RUN`, `ENV`, `ENTRYPOINT`, `CMD`, startup script mutation, build-time mutation, or runtime configuration mutation exists in the assembly Dockerfile.

## Published Image

- Tag suffix: `gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- In-cluster tag reference: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Image index digest: `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908`
- linux/arm64 digest: `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a`
- Attestation manifest digest observed in image index: `sha256:1d11896788510318113414d72ed9eb7b08b3b8fdb2ae054656c1c12adb8c1169`

## Assembly File Source Hashes

| Assembly file | SHA-256 |
|---|---|
| `control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `control-ui/assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

Patched JS preflight: `control-ui/assets/index-M4TNVXB3.js` matched required SHA-256 `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`.

## Build/Tag/Push Commands

Tag setup:

```bash
TS=$(TZ=Asia/Shanghai date +%Y%m%d%H%M%S)
TAG_SUFFIX=gtclaw-controlui-persistent-origin-allowlist-persistence-${TS}
HOST_TAG=localhost:5001/clawmanager-openclaw/openclaw:${TAG_SUFFIX}
CLUSTER_TAG=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:${TAG_SUFFIX}
```

Build/tag/push:

```bash
docker buildx build --platform linux/arm64 -t "$HOST_TAG" --push specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence
```

Command exit codes:

| Command | Exit code | Result |
|---|---:|---|
| Approval packet marker preflight | `0` | Approved dependency markers found. |
| Dockerfile marker preflight | `0` | Parent digest, `COPY --chmod=0644`, and target path markers found. |
| Dockerfile exact five-instruction preflight | `0` | Exactly digest-pinned `FROM` plus four `COPY --chmod=0644` instructions. |
| Assembly source hash preflight | `0` | Patched JS and all assembly source hashes recorded. |
| `docker buildx build --platform linux/arm64 -t "$HOST_TAG" --push ...` | `0` | Built and pushed the host tag. |
| `docker buildx imagetools inspect "$HOST_TAG"` | `0` | Published OCI image index found. |
| `docker buildx imagetools inspect "$HOST_TAG" --raw` | `0` | Raw OCI index returned linux/arm64 manifest. |
| `docker manifest inspect "$HOST_TAG"` | `1` | Plain inspect did not resolve local HTTP registry. Non-blocking; retried with `--insecure`. |
| `docker manifest inspect --insecure "$HOST_TAG"` | `0` | Published OCI image index found in registry. |
| Registry arm64 manifest read via `curl` | `0` | linux/arm64 layer list retrieved from local registry. |
| Registry blob path proof via `curl | tar -tzf -` | `0` | Four target paths found in final four arm64 layers. |
| Registry blob hash proof via `curl | tar -xOzf - | shasum -a 256` | `0` | Extracted file hashes match assembly source hashes. |

Build warning observed: `FromPlatformFlagConstDisallowed: FROM --platform flag should not use constant value "linux/arm64"`. This warning did not change the requested platform or the published linux/arm64 manifest result.

## Proof Pushed Image Exists In Registry

`docker buildx imagetools inspect` returned:

- Name: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- MediaType: `application/vnd.oci.image.index.v1+json`
- Digest: `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908`
- linux/arm64 manifest: `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a`

`docker manifest inspect --insecure` also returned the same OCI index and linux/arm64 manifest digest from `localhost:5001`.

## Proof Control UI Files Exist In Image Target Path

The pushed linux/arm64 manifest was read from the local registry. The final four layer blobs contained:

| Layer digest | Target path |
|---|---|
| `sha256:fefe77408c57f50ec8aacaec17c9a5015dded17f929f63bb9c68111dad722c38` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` |
| `sha256:0ab268e1fcd4a9afb13dc48c9ebd61a809797cbc5b58cbee4a21719267c3380d` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` |
| `sha256:90bbc50a2b6e9691d2bb0149bdf0a65210fc339a3272c075d08cd76709a3233a` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` |
| `sha256:2b556f5cf47e6578a5c80c049ec1409df936124abaf9f2699ea8fd388ece89d0` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` |

The same registry blobs were extracted to stdout and hashed; all extracted hashes matched the assembly source hashes above.

## Explicit Negatives

- no deploy/restart was executed.
- no fresh instance create/delete/modify was executed.
- no K8S mutation was executed.
- no runtime/database mutation was executed.
- no registry configuration mutation, registry cleanup, or old tag deletion was executed.
- no browser E2E, Chrome DevTools, Playwright, browser storage cleanup, cache cleanup, or cookie cleanup was executed.
- no gateway token/password/key was requested or entered.
- no assembly artifact, control-ui source artifact, patched JS semantics, runtime-startup-artifact, backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence was modified.
- no Mem0 write or longterm write-back was performed.
- no passes:true update was made.
- no Close action was taken.
- no git stage, commit, or push was performed.

## Next Gate Recommendation

Next gate recommendation: Fresh Instance / Runtime Deployment Approval Packet.
