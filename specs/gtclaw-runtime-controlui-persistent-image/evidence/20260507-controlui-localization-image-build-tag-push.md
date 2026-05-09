# Control-ui localization image build/tag/push evidence

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation / Build Tag Push

Gate: CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET

## Verdict

CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE

This gate only built, tagged, pushed, and inspected a new timestamped runtime image from the reviewed repo-owned assembly artifact. It did not deploy the image, create a fresh instance, run browser/manual E2E, mutate Kubernetes/database/runtime state, close the feature, write longterm memory, stage, commit, or push git changes.

## Build input

Build context:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/`

Dockerfile:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile`

Platform:

`linux/arm64`

Runtime target covered:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

Dockerfile parent digest:

`sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`

## Published tag

- TAG_SUFFIX: `gtclaw-controlui-localization-20260507211942`
- HOST_TAG: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`
- CLUSTER_TAG: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`

## Build command result

Command executed:

```bash
docker buildx build --platform linux/arm64 -t "$HOST_TAG" --push specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization
```

Build command exit code: `0`

Build output summary:

- Builder: `desktop-linux`
- Exported linux/arm64 manifest: `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e`
- Exported attestation manifest: `sha256:40a50f65fd60d98e9bfc9f6e860cd9af49eb26eec92a0354b7d0da74b5765045`
- Exported image index digest: `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`
- Pushed manifest for HOST_TAG at image index digest `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`
- BuildKit warning observed: `FromPlatformFlagConstDisallowed` for the constant `linux/arm64` Dockerfile parent platform flag. This warning matches the reviewed Dockerfile shape and did not block the build or push.

## Image inspect results

Image index digest:

`sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`

linux/arm64 digest:

`sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e`

Inspect command exit codes:

- `docker buildx imagetools inspect "$HOST_TAG"` exit code: `0`
- `docker buildx imagetools inspect "$HOST_TAG" --raw` exit code: `0`
- `docker manifest inspect --insecure "$HOST_TAG" || true` wrapper exit code: `0`; underlying manifest inspect returned OCI index JSON successfully.
- `docker buildx imagetools inspect "localhost:5001/clawmanager-openclaw/openclaw@sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e" --raw` exit code: `0`

Image index manifest entries:

| Digest | Platform | Media type | Size |
| --- | --- | --- | ---: |
| `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` | `linux/arm64` | `application/vnd.oci.image.manifest.v1+json` | `7701` |
| `sha256:40a50f65fd60d98e9bfc9f6e860cd9af49eb26eec92a0354b7d0da74b5765045` | `unknown/unknown` | `application/vnd.oci.image.manifest.v1+json` | `566` |

linux/arm64 config digest:

`sha256:ae7b15e3c95826de5181e366da0a85198e15ebc2419b1d95ff07b9a993f11e12`

## Overlay source hashes

The reviewed repo-owned assembly artifact source hashes were checked before build:

| Assembly source | SHA-256 |
| --- | --- |
| `control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## Pushed image path/hash proof

Registry manifest/blob inspection was limited to the new tag and its linux/arm64 manifest. Layer blobs were read only, streamed directly for tar path listing and file hash extraction, and not run as containers.

Layer path listing commands exit code: `0` for all four target layers.

Layer file hash extraction commands exit code: `0` for all four target files.

| Runtime target path | linux/arm64 layer digest | Extracted pushed-image SHA-256 | Expected source SHA-256 | Result |
| --- | --- | --- | --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `sha256:fefe77408c57f50ec8aacaec17c9a5015dded17f929f63bb9c68111dad722c38` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | match |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `sha256:0ab268e1fcd4a9afb13dc48c9ebd61a809797cbc5b58cbee4a21719267c3380d` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | match |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `sha256:90bbc50a2b6e9691d2bb0149bdf0a65210fc339a3272c075d08cd76709a3233a` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | match |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `sha256:121ea7a441695e86c0f9e9b4fb8b1e0dbf3964d8a65fe5630f739bc730f55271` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | match |

Note: tar listing showed the pushed layer paths without leading slash, for example `usr/local/lib/node_modules/openclaw/dist/control-ui/index.html`, which maps to the runtime absolute target path above.

## Verification commands run

Readonly dependency and artifact checks:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push-approval-packet.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile
find specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/index.html specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/*
```

Build and image inspect:

```bash
docker buildx build --platform linux/arm64 -t "$HOST_TAG" --push specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization
docker buildx imagetools inspect "$HOST_TAG"
docker buildx imagetools inspect "$HOST_TAG" --raw
docker manifest inspect --insecure "$HOST_TAG" || true
docker buildx imagetools inspect "localhost:5001/clawmanager-openclaw/openclaw@sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e" --raw
```

Pushed-image layer/path/hash readback:

```bash
# Per target layer: registry blob tar path listing and tar content SHA-256 extraction.
# Registry access was read-only and scoped to the newly pushed tag's linux/arm64 layer digests.
```

Final evidence checks to run after writing this file:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md
rg -n "CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE|CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE|gtclaw-controlui-localization-|localhost:5001/clawmanager-openclaw/openclaw|k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw|image index digest|linux/arm64|/usr/local/lib/node_modules/openclaw/dist/control-ui|no browser E2E|no kubectl|no database|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md
```

## Forbidden actions confirmation

Forbidden actions were not executed in this gate. Specifically: no docker pull, no container run, no docker run, no docker compose, no deployment, no restart, no fresh instance, no browser E2E, no manual E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no K8S mutation, no database access or mutation, no runtime instance mutation, no registry cleanup, no registry config mutation, no old tag delete, no trustedProxy patch, no runtime auth patch, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing artifact modification, no existing evidence modification, no Mem0 write, no plugin, no skill distribution, no passes:true, no Close, no longterm write-back, and no git stage/commit/push.

No password, key, cookie, bearer material, authorization header plaintext, private key, or secret access material was recorded.
