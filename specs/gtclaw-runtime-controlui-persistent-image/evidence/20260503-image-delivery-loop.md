# Image Delivery Loop Evidence - 2026-05-03

## Verdict

BLOCKED

The T017-T021 local k3d image delivery implementation/evidence loop stopped at preflight because the approved artifact source directory was missing:

`/tmp/gtclaw-runtime-patch`

No image build, tag, push, pull, inspect, built artifact extraction, fresh instance creation, browser E2E, runtime mutation, Kubernetes mutation, database mutation, `passes:true`, or Close action was performed.

## Gate Statement

This packet covers T017-T021 only: no fresh instance, no browser E2E, no runtime/K8S/database mutation, no passes:true, no Close.

Approved scope was image delivery / built artifact evidence loop only. The loop stopped before any artifact delivery action because a hard stop condition was met.

## Approval Reference

Commander/user explicitly approved T017-T021 local k3d image delivery implementation/evidence loop with these approved values and constraints:

| Item | Approved value |
| --- | --- |
| Artifact source directory | `/tmp/gtclaw-runtime-patch` |
| Base image digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Target tag pattern | `gtclaw-controlui-persistent-YYYYMMDDHHMMSS` |
| Host registry | `localhost:5001` |
| In-cluster registry alias | `k3d-clawmanager-registry:5000` |
| Copy target | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

Rejected copy target remains:

`/opt/opensparrow/runtime/openclaw/dist/control-ui`

## Preflight Result

| Check | Result |
| --- | --- |
| Current gate | T017-T021 implementation/evidence loop only |
| Source directory exists | no |
| Allowlist files present | no, because source directory is missing |
| Allowlist expansion | none evaluated; no artifact files were present |
| Stop condition | artifact source dir missing |

Expected four-file allowlist:

| File |
| --- |
| `index.html` |
| `assets/i18n-B06L7jQN.js` |
| `assets/zh-CN-B26mMdbY.js` |
| `assets/index-M4TNVXB3.js` |

Observed command summary:

```text
ls: /tmp/gtclaw-runtime-patch: No such file or directory
ls: /tmp/gtclaw-runtime-patch/assets: No such file or directory
find: /tmp/gtclaw-runtime-patch: No such file or directory
```

## Source Artifact Hashes

Source artifact hash check was not reached because `/tmp/gtclaw-runtime-patch` was missing.

Expected after-hash table carried from T012:

| File | Expected after SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

Source artifact hash result: BLOCKED before after-hash verification.

## Base Image Digest And Before-Hash Proof

Approved base image digest:

`sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

Base digest verification was not reached because preflight stopped on the missing source artifact directory. The loop did not fall back to a mutable tag, did not extract base files, and did not build from any base image.

Expected before-hash table carried from T012:

| File | Expected before SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` |

Base before-hash result: BLOCKED before before-hash verification.

## Build Context And Dockerfile Summary

Build context path: not created.

Dockerfile/copy summary: no Dockerfile was written. No file was copied into `/usr/local/lib/node_modules/openclaw/dist/control-ui`. The forbidden `/opt/opensparrow/runtime/openclaw/dist/control-ui` path was not used.

The intended build would have copied only the four allowlist files into `/usr/local/lib/node_modules/openclaw/dist/control-ui`, but this was not attempted because preflight failed.

## Target Tag And Registry References

No timestamp tag was generated because the loop stopped before build/tag.

Approved target tag pattern, not instantiated:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-YYYYMMDDHHMMSS`

Expected in-cluster alias pattern, not instantiated:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-YYYYMMDDHHMMSS`

No `latest` tag was overwritten and no mutable tag-only rollback reference was created.

## Built Artifact Hashes

Built artifact extraction was not reached. No built image exists from this loop.

Two-surface hash alignment result:

`source artifact hash = built image hash` was not evaluated because the source artifact was missing and no image was built.

## Pushed/Pulled Artifact Digest Evidence

Push/publish was not reached.

Pull/inspect was not reached.

No local registry reference was pushed to `localhost:5001`.

No in-cluster alias reference was pushed or verified at `k3d-clawmanager-registry:5000`.

## Image Index Digest / linux/arm64 Manifest Digest Classification

No new image index digest is available because no image was built or pushed.

No new `linux/arm64` manifest digest is available because no image was built or pushed.

Digest classification rule remains: if a later loop exposes only one digest, classify it as image index digest or `linux/arm64` manifest digest according to tool output and do not conflate them.

## Protected Literal Preservation Summary

Protected literal scanning was not reached because no source artifact files or built artifact files were available for this loop.

The scope rule remains: no broad OpenClaw -> GTClaw replacement is authorized. Technical identity/literals such as `openclaw`, `OpenClaw Image`, `.openclaw`, `openclaw.json`, `openclaw dashboard --no-open`, `dist/control-ui`, package identity, paths, protocols, API/DB/K8S/runtime identifiers, image names, image tags, and image digests must remain preserved.

## T014 / T015 Remaining Gap

T014/T015 remain open. There is no fresh deployed pod hash yet.

This evidence packet does not prove persistent delivery in a fresh disposable instance. Fresh instance creation, deployed pod hash extraction, and browser E2E remain unauthorized under this loop.

## Rollback Reference

No runtime/K8S environment mutation was performed in this loop.

No rollback action is needed for this blocked run because no image, registry, runtime, Kubernetes, Secret, ConfigMap, namespace, database, or deployment default state was changed.

Future rollback target should be the prior approved digest/tag or prior file hash set. Do not rely on mutable tag-only rollback.

## Secret Hygiene Statement

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, or token-bearing URL is recorded in this evidence.

No command printed registry credentials or secret-bearing environment content.

## Scope Statement

Only this new evidence file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md`

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, runtime/app deployment, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, `.codex/auth.json`, `.codex/config.toml`, and `/opt/opensparrow/runtime/openclaw/dist/control-ui` were not modified.

## Verification Commands

To be run after this evidence write:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md
rg -n "IMAGE_DELIVERY_LOOP_DONE|BLOCKED|/usr/local/lib/node_modules/openclaw/dist/control-ui|/opt/opensparrow/runtime/openclaw/dist/control-ui|gtclaw-controlui-persistent|localhost:5001|k3d-clawmanager-registry:5000|source artifact hash = built image hash|linux/arm64|image index digest|before-hash|after-hash|rollback|no fresh instance|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-implementation-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-hash-manifest-design.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-delivery-decision.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-path-verification.md specs/gtclaw-runtime-controlui-persistent-image/spec.md specs/gtclaw-runtime-controlui-persistent-image/plan.md specs/gtclaw-runtime-controlui-persistent-image/tasks.md
```
