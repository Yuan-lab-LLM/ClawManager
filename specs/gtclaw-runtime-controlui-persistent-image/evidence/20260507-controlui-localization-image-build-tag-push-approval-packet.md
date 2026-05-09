# Control-ui localization image build/tag/push approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH
- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE

## Verdict

CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE

This document is an approval packet only. It is not a build/tag/push implementation, does not build or publish an image, and does not pull, run, deploy, or verify a runtime image.

## Proposed next gate

Recommended next gate name:

`CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE`

The next build/tag/push gate must not proceed without that explicit approval token.

## Future build/tag/push input

Source assembly artifact path:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/`

Build context:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/`

Dockerfile:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile`

Platform:

`linux/arm64`

Runtime target covered:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

Dockerfile parent image:

- `localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`

Dockerfile parent digest:

- image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- linux/arm64 digest from assembly manifest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`

## Future build/tag/push output recommendation

Use a timestamped localization tag suffix generated at the future approved build gate, for example:

`gtclaw-controlui-localization-<YYYYMMDDHHMMSS>`

Recommended tag variables for the future approved gate:

- `TAG_SUFFIX=gtclaw-controlui-localization-<YYYYMMDDHHMMSS>`
- host tag: `localhost:5001/clawmanager-openclaw/openclaw:<same-tag>`
- in-cluster tag reference: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:<same-tag>`

The future build/tag/push evidence must record:

- exact tag suffix used
- host tag
- in-cluster tag reference
- image index digest
- linux/arm64 digest
- Dockerfile path and content summary
- build context path
- overlay file hashes

## Overlay file hashes

The future build/tag/push gate must use the reviewed assembly overlay files exactly as listed below:

| Assembly overlay file | Runtime target | SHA-256 | Status |
| --- | --- | --- | --- |
| `control-ui/index.html` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | copied unchanged |
| `control-ui/assets/index-M4TNVXB3.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | copied unchanged to preserve persistence fix |
| `control-ui/assets/i18n-B06L7jQN.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | copied unchanged |
| `control-ui/assets/zh-CN-B26mMdbY.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | localized zh-CN locale chunk |

## Future gate boundary

The future approved gate may only build, tag, push, and record registry-image publication evidence for the repo-owned assembly build context above.

The future build gate must still prohibit:

- no trustedProxy patch
- no runtime auth contract patch
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no UnifiedFramework modification
- no existing artifact modification
- no existing evidence modification
- no plugin
- no skill distribution
- no browser E2E
- no DevTools
- no Playwright
- no kubectl
- no k3d
- no Helm deployment
- no instance mutation
- no database access or mutation
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Risk statement

- The proposed build/tag/push gate only produces a registry image. It must not deploy that image and must not verify runtime behavior.
- Fresh instance deployment is a later independent approval gate.
- Browser/manual E2E is a later independent approval gate.
- trustedProxy/runtime auth contract work is outside this localization build chain.
- This gate does not alter the reviewed static localization overlay; it only requests approval for future image publication from that build context.

## Readonly evidence checked

This approval packet was prepared after readonly review of:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-approval-packet-controlui-persistence.md`

## Verification commands

Readonly dependency and artifact checks:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md
find specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/index.html specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/*
```

Packet hygiene checks:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image
rg -n "CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE|CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_GATE|20260507-controlui-localization|linux/arm64|/usr/local/lib/node_modules/openclaw/dist/control-ui|no browser E2E|no kubectl|no database|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push-approval-packet.md
```

## Verification result summary

- Assembly manifest and Dockerfile readback confirmed build context `20260507-controlui-localization`, platform `linux/arm64`, digest-pinned parent `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`, and destination root `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Assembly implementation evidence readback confirmed `CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE`.
- Assembly file listing showed `Dockerfile`, `MANIFEST.md`, `control-ui/index.html`, `control-ui/assets/i18n-B06L7jQN.js`, `control-ui/assets/index-M4TNVXB3.js`, and `control-ui/assets/zh-CN-B26mMdbY.js`.
- Overlay hashes matched the assembly manifest, including localized `control-ui/assets/zh-CN-B26mMdbY.js` SHA-256 `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`.
- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image` exited 0.
- Required marker scan exited 0.
- Scoped git status showed only this new approval packet file in the requested path scope.

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically: no build/tag/push/pull image, no container run, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance mutation, no database access or mutation, no trustedProxy patch, no runtime auth contract patch, no plugin, no skill distribution, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing artifact modification, no existing evidence modification, no Mem0 write, no passes:true, no Close, no longterm write-back, and no git stage/commit/push.

No token, password, key, cookie, bearer material, authorization header plaintext, private key, or access URL plaintext was recorded.
