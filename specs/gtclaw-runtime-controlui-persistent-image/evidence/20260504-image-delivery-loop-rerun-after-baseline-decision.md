# Image Delivery Loop Rerun After Baseline Decision Evidence - 2026-05-04

## Verdict

IMAGE_DELIVERY_LOOP_RERUN_DONE

The T017-T021 local k3d image delivery loop completed after Commander/user selected `OPTION_A_KEEP_APPROVED_BASE_26BC` and revised the active before-hash gate for approved base `26bc8e3a...`.

This packet proves source artifact hash = built image hash for the four allowlist files only. It does not prove fresh deployed pod hash, fresh instance behavior, or browser E2E.

## Gate Statement

T017-T021 only; no fresh instance, no deployed pod hash read, no browser E2E, no runtime/K8S/database mutation, no passes:true, no Close.

No runtime pod/container files were read or modified. No Kubernetes resources, Secrets, ConfigMaps, namespaces, database rows, deployment defaults, backend, frontend, docs, longterm, AgentTeam, `spec.md`, `plan.md`, `tasks.md`, or existing evidence files were modified.

## Approval And Baseline Reference

| Item | Value |
| --- | --- |
| Baseline decision | `OPTION_A_KEEP_APPROVED_BASE_26BC` |
| Source artifact | `/tmp/gtclaw-runtime-patch` |
| Base image index digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Base linux/arm64 manifest digest | `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` |
| Copy target | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Forbidden copy target | `/opt/opensparrow/runtime/openclaw/dist/control-ui` |
| Host registry | `localhost:5001` |
| In-cluster alias label | `k3d-clawmanager-registry:5000` |

The forbidden `/opt/opensparrow/runtime/openclaw/dist/control-ui` path was not used.

## Source Artifact Verification

Command:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sort
```

Result:

```text
/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js
/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js
/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js
/tmp/gtclaw-runtime-patch/index.html
```

Allowlist-only comparison returned empty output. No file outside the four allowlist files was present.

Source artifact after-hash table:

| File | Source SHA-256 | Source size | Required after-hash result |
| --- | --- | ---: | --- |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | match |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` | match |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` | match |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` | match |

## Base Digest And Revised Before-Hash Verification

Base reference used:

`localhost:5001/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

Digest inspection classified the approved base digest as an image index digest:

| Digest class | Digest |
| --- | --- |
| image index digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| linux/arm64 manifest digest | `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` |

Base files were extracted from the digest-pinned base into:

`/tmp/gtclaw-controlui-persistent-base-00X0KT`

Revised before-hash gate result:

| File | Observed base SHA-256 | Observed size | Revised expected before-hash result |
| --- | --- | ---: | --- |
| `index.html` | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` | `3395` | match |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` | match |
| `assets/zh-CN-B26mMdbY.js` | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` | `23247` | match |
| `assets/index-M4TNVXB3.js` | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` | `707545` | match |

## Target Tag Generated

Host registry tag:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506`

Expected in-cluster alias label for later approved fresh-instance work:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506`

No `latest` tag was overwritten.

## Build Context And Copy Summary

Build context:

`/tmp/gtclaw-controlui-persistent-build-uutWzi`

Build context file list:

```text
/tmp/gtclaw-controlui-persistent-build-uutWzi/assets/i18n-B06L7jQN.js
/tmp/gtclaw-controlui-persistent-build-uutWzi/assets/index-M4TNVXB3.js
/tmp/gtclaw-controlui-persistent-build-uutWzi/assets/zh-CN-B26mMdbY.js
/tmp/gtclaw-controlui-persistent-build-uutWzi/index.html
```

Dockerfile summary, supplied to `docker buildx build` through standard input:

```Dockerfile
FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8
COPY index.html /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
COPY assets/i18n-B06L7jQN.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
COPY assets/zh-CN-B26mMdbY.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
COPY assets/index-M4TNVXB3.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
```

Only the four allowlist files were copied, and only into `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

## Build / Tag / Push / Pull / Inspect Summary

Redacted commands:

```bash
docker buildx build --platform linux/arm64 --push -t localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506 -f- /tmp/gtclaw-controlui-persistent-build-uutWzi
docker pull --platform linux/arm64 localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506
docker buildx imagetools inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506
curl -sSI -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' http://localhost:5001/v2/clawmanager-openclaw/openclaw/manifests/gtclaw-controlui-persistent-20260504005506
```

Buildx result summary:

| Step | Result |
| --- | --- |
| Build digest-pinned OpenClaw runtime image | done |
| Copy four files to `/usr/local/lib/node_modules/openclaw/dist/control-ui` | done |
| Tag `gtclaw-controlui-persistent-20260504005506` | done through build `-t` |
| Push to `localhost:5001` | done through build `--push` |
| Pull target tag from `localhost:5001` | done |
| Inspect pushed/pulled target artifact | done |
| Extract built artifact hashes | done |

Build warning recorded:

```text
FromPlatformFlagConstDisallowed: FROM --platform flag should not use constant value "linux/arm64"
```

This warning did not change the platform result; the build command and inspected manifest both classify the runtime artifact as `linux/arm64`.

## Pushed / Pulled Digest Evidence

`docker pull --platform linux/arm64` reported:

```text
Digest: sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
Status: Image is up to date for localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506
```

Registry HEAD against `localhost:5001` reported:

```text
HTTP/1.1 200 OK
Content-Type: application/vnd.oci.image.index.v1+json
Docker-Content-Digest: sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
```

Local image inspect after pull recorded repo digest:

```text
localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
```

Platform metadata from local image inspect:

`arm64/linux`

## Image Index Digest / linux/arm64 Manifest Digest Classification

Target image index digest:

`sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`

Target linux/arm64 manifest digest:

`sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e`

`docker buildx imagetools inspect` classified the pushed tag as:

| Digest | Platform | Media type | Classification |
| --- | --- | --- | --- |
| `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` | index | `application/vnd.oci.image.index.v1+json` | image index digest |
| `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` | `linux/arm64` | `application/vnd.oci.image.manifest.v1+json` | linux/arm64 manifest digest |
| `sha256:457e49e93fbebc855a78e94f06c0d221073f3a35b453925480eac2dc45c4397c` | `unknown/unknown` | `application/vnd.oci.image.manifest.v1+json` | attestation manifest |

The image index digest and linux/arm64 manifest digest are not conflated.

## Built Artifact Hash Verification

Built artifact files were extracted from the pulled image into:

`/tmp/gtclaw-controlui-persistent-built-UQunwy`

| File | Built image SHA-256 | Built image size | Required after-hash result |
| --- | --- | ---: | --- |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | match |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` | match |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` | match |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` | match |

## source artifact hash = built image hash Result

`source artifact hash = built image hash` for all four allowlist files.

Byte-for-byte comparisons:

```text
index.html source artifact hash = built image hash
assets/i18n-B06L7jQN.js source artifact hash = built image hash
assets/zh-CN-B26mMdbY.js source artifact hash = built image hash
assets/index-M4TNVXB3.js source artifact hash = built image hash
```

This satisfies the two-surface artifact gate. T014/T015 fresh deployed pod hash remains a later gate.

## Rollback Target

No runtime/K8S/database environment mutation was performed in this loop.

For local image delivery rollback, use the previously approved base image identity or another explicitly approved prior digest/tag:

| Rollback class | Reference |
| --- | --- |
| Prior approved base image index digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Prior approved base linux/arm64 manifest digest | `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` |

Secret-redacted rollback guidance for a later approved environment gate:

```bash
# Do not rely on mutable tag-only rollback.
# Set the runtime image/resource reference back to the prior approved digest/tag through an explicitly approved control-plane path.
# Then verify the fresh runtime artifact hash set under /usr/local/lib/node_modules/openclaw/dist/control-ui.
```

No rollback command was executed.

## Secret Hygiene

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, or token-bearing URL was recorded.

No command printed registry credentials or secret-bearing environment content.

## Remaining Gates

- T014/T015 fresh deployed pod hash alignment is not done.
- T022 fresh instance creation is not authorized and was not done.
- Browser E2E is not authorized and was not done.
- Runtime/K8S/database mutation is not authorized and was not done.
- `passes:true` and Close remain unauthorized.

## Scope Statement

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md`

Temporary local workspaces written:

| Workspace | Purpose |
| --- | --- |
| `/tmp/gtclaw-controlui-persistent-base-00X0KT` | base before-hash extraction |
| `/tmp/gtclaw-controlui-persistent-build-uutWzi` | four-file build context |
| `/tmp/gtclaw-controlui-persistent-built-UQunwy` | built artifact extraction |

Local image builder cache and pulled/inspected local artifact cache were updated for the generated tag only.

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, `.codex/auth.json`, `.codex/config.toml`, and `/opt/opensparrow/runtime/openclaw/dist/control-ui` were not modified.

## Verification Commands

Commands to verify this evidence packet:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sort
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md
rg -n "IMAGE_DELIVERY_LOOP_RERUN_DONE|BLOCKED|OPTION_A_KEEP_APPROVED_BASE_26BC|26bc8e3a|e1b75327|gtclaw-controlui-persistent|localhost:5001|k3d-clawmanager-registry:5000|source artifact hash = built image hash|linux/arm64|image index digest|before-hash|after-hash|rollback|no fresh instance|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-base-before-hash-baseline-decision.md
```
