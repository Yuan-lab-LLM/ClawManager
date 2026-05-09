# Control-ui localization image assembly approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH

## Verdict

CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET_DONE

This document is an approval packet only. It is not an implementation patch, does not create or modify a runtime image assembly artifact, and does not build, tag, push, deploy, or verify a runtime image.

## Proposed next gate

Recommended next gate name:

`CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE`

The next gate should not proceed without that explicit approval token.

## Patch objective for proposed next gate

If approved, the next patch should add the 20260507 official OpenClaw localization static control-ui artifact into a new repo-owned runtime image assembly artifact so the image assembly overlay covers the runtime-served control-ui target:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

The source localization artifact for that proposed assembly patch is:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/`

The current readonly image assembly reference is:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`

That reference shows the expected assembly shape: a digest-pinned Dockerfile and a `control-ui/` overlay copied to `/usr/local/lib/node_modules/openclaw/dist/control-ui` with file mode `0644`.

## Suggested write set for proposed next gate

Allow only:

- New runtime image assembly artifact directory:
  `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/`
- Manifest and overlay files inside that new artifact only.
- New implementation evidence file only, for example:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md`

Do not modify existing runtime image assembly artifacts, existing control-ui runtime artifacts, existing evidence files, backend, frontend, deployments, docs, longterm, AgentTeam, UnifiedFramework, or feature close/write-back state.

## Localization artifact evidence

Source localization artifact path:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/`

Localization implementation evidence:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md`

Localization artifact files:

| File | SHA-256 | Evidence status |
| --- | --- | --- |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | copied unchanged |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | copied unchanged |
| `assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | copied unchanged to preserve persistence fix |
| `assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | changed zh-CN locale chunk |

Copied unchanged from source artifact:

- `index.html`
- `assets/i18n-B06L7jQN.js`
- `assets/index-M4TNVXB3.js`

Changed zh-CN locale chunk:

- `assets/zh-CN-B26mMdbY.js`
- Source SHA-256: `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809`
- Localized artifact SHA-256: `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`

## Protected literal policy

The proposed image assembly patch must preserve the protected literal policy from the localization artifact manifest. Technical compatibility literals and identifiers must not be translated or renamed unless a separate gate proves they are display-only:

- lowercase `openclaw` package, config, path, API, and command identifiers
- `.openclaw`
- `docs/openclaw.json`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `--no-open`
- `control-ui`
- route/path/API field names
- WebSocket/API/RPC/JSON/CSV/UTC/IANA/Cron/Webhook/model placeholders where they are protocol, format, or ecosystem terms

The proposed image assembly patch should copy the already-reviewed static files into the new assembly context; it should not perform additional localization edits.

## Continued prohibitions for proposed next gate

The proposed next gate must continue to prohibit:

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
- no build/tag/push
- no image pull
- no container run
- no browser E2E
- no DevTools
- no Playwright
- no kubectl
- no k3d
- no Helm
- no instance mutation
- no database access or mutation
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Risk statement

- The proposed next patch should only change static control-ui artifact overlay assembly. It must not touch backend auth, runtime auth, trustedProxy handling, API contracts, or route behavior.
- The persistence fix is preserved by carrying forward `assets/index-M4TNVXB3.js` unchanged from the reviewed localization artifact.
- Image build, tag, and push are a later independent gate and are outside the proposed image assembly patch gate.
- Browser/manual E2E is a later independent gate and is outside the proposed image assembly patch gate.
- The proposed next gate should produce repo-owned build context evidence only; it should not mutate runtime state.

## Verification performed for this approval packet

Readonly source and evidence inspection:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md
find specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/*
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/Dockerfile
```

Packet hygiene checks to run after this file is written:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image
rg -n "CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET_DONE|CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE|20260507-official-openclaw-localization|/usr/local/lib/node_modules/openclaw/dist/control-ui|no trustedProxy patch|no plugin|no skill distribution|no build/tag/push|no browser E2E|no kubectl|no Mem0 write|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-approval-packet.md
```

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically: no image assembly patch, no runtime image assembly artifact modification, no control-ui runtime artifact modification, no existing evidence modification, no trustedProxy patch, no runtime auth contract patch, no plugin, no skill distribution, no build/tag/push, no image pull, no container run, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance mutation, no database access or mutation, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, authorization header plaintext, private key, or access URL plaintext was recorded.
