# Image Delivery Loop Rerun After Source Recovery Evidence - 2026-05-03

## Verdict

BLOCKED

`IMAGE_DELIVERY_LOOP_RERUN_DONE` was not reached. The rerun stopped on the required base before-hash drift gate before any build, tag, push, pull of the target tag, target artifact inspect, or built artifact extraction.

The approved source artifact at `/tmp/gtclaw-runtime-patch` was present and matched the required after-hash table. The selected base artifact resolved to the approved image index digest, but the four files at `/usr/local/lib/node_modules/openclaw/dist/control-ui` did not match the expected before-hash table. Per the explicit stop condition, the loop stopped for Commander review.

## Gate Statement

T017-T021 only; no fresh instance, no deployed pod hash read, no browser E2E, no runtime/K8S/database mutation, no passes:true, no Close.

No runtime pod/container files were read or modified. No Kubernetes resources, Secrets, ConfigMaps, namespaces, database rows, deployment defaults, backend, frontend, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence files were modified.

## Approval Reference

Approved values used for this rerun:

| Item | Approved value |
| --- | --- |
| Artifact source directory | `/tmp/gtclaw-runtime-patch` |
| Base image digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Target tag pattern | `gtclaw-controlui-persistent-YYYYMMDDHHMMSS` |
| Host registry | `localhost:5001` |
| In-cluster registry alias label | `k3d-clawmanager-registry:5000` |
| Copy target | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

Forbidden copy target not used:

`/opt/opensparrow/runtime/openclaw/dist/control-ui`

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

## Base Digest Verification

Selected base reference:

`localhost:5001/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

Digest inspection classified the approved digest as an image index digest:

| Digest class | Digest |
| --- | --- |
| image index digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| linux/arm64 manifest digest | `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` |

The selected base artifact also had a local repo digest entry for:

`localhost:5001/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

## Base Before-Hash Verification

Base files were extracted offline from the selected base artifact into:

`/tmp/gtclaw-controlui-persistent-build-base-9NqxFp`

Observed base before-hash table:

| File | Observed base SHA-256 | Observed size | Expected before SHA-256 | Expected size | Result |
| --- | --- | ---: | --- | ---: | --- |
| `index.html` | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` | `3395` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` | drift |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` | match |
| `assets/zh-CN-B26mMdbY.js` | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` | `23247` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` | drift |
| `assets/index-M4TNVXB3.js` | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` | `707545` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` | drift |

Result: BLOCKED on before-hash drift. The build did not continue.

## Target Tag Generated

No target tag was generated because the required base before-hash drift stop fired before build/tag.

Approved target tag pattern retained for a future rerun after Commander review:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-YYYYMMDDHHMMSS`

Expected in-cluster alias label retained for future evidence only:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-YYYYMMDDHHMMSS`

No `latest` tag was overwritten.

## Build/Tag/Push/Pull/Inspect Summary

| Step | Result |
| --- | --- |
| Build digest-pinned OpenClaw runtime image | not run; blocked by before-hash drift |
| Copy four files to `/usr/local/lib/node_modules/openclaw/dist/control-ui` | not run |
| Tag `gtclaw-controlui-persistent-YYYYMMDDHHMMSS` | not run |
| Push to `localhost:5001` | not run |
| Pull approved target tag | not run |
| Inspect pushed/pulled target artifact | not run |
| Extract built artifact hashes | not run |

The only inspect performed was base digest inspection. No pushed target artifact exists from this rerun.

## Image Index Digest

New target image index digest: unavailable because no target image was built or pushed.

Base image index digest observed:

`sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

## linux/arm64 Manifest Digest

New target linux/arm64 manifest digest: unavailable because no target image was built or pushed.

Base linux/arm64 manifest digest observed:

`sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315`

## Built Artifact Hash Verification

Built artifact hashes were not extracted because no image was built.

| File | Built image SHA-256 | Built image size | Result |
| --- | --- | ---: | --- |
| `index.html` | n/a | n/a | BLOCKED before build |
| `assets/i18n-B06L7jQN.js` | n/a | n/a | BLOCKED before build |
| `assets/zh-CN-B26mMdbY.js` | n/a | n/a | BLOCKED before build |
| `assets/index-M4TNVXB3.js` | n/a | n/a | BLOCKED before build |

## source artifact hash = built image hash Result

`source artifact hash = built image hash` was not evaluated because the built image does not exist. The source artifact after-hash side is verified; the built image side remains BLOCKED by base before-hash drift.

## Rollback Target

No runtime/K8S environment mutation occurred and no new target tag was pushed, so no rollback action is needed for this rerun.

Future rollback must not rely on mutable tag-only rollback. The currently approved base image index digest remains the last verified rollback identity for this local loop:

`sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

Future Commander review must decide whether the observed before-hash drift is acceptable, whether the expected before-hash table should be revised, or whether a different base artifact should be selected.

## Secret Hygiene

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, or token-bearing URL was recorded.

## Scope Statement

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md`

Temporary local workspace written:

`/tmp/gtclaw-controlui-persistent-build-base-9NqxFp`

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, `.codex/auth.json`, `.codex/config.toml`, and `/opt/opensparrow/runtime/openclaw/dist/control-ui` were not modified.

## Verification Commands

Commands to verify this evidence packet:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sort
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md
rg -n "IMAGE_DELIVERY_LOOP_RERUN_DONE|BLOCKED|/tmp/gtclaw-runtime-patch|gtclaw-controlui-persistent|localhost:5001|k3d-clawmanager-registry:5000|source artifact hash = built image hash|linux/arm64|image index digest|before-hash|after-hash|rollback|no fresh instance|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-runtime-pod-source-artifact-recovery.md
```
