# Control-ui localization image assembly patch implementation

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation Patch

Gate: CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET_DONE
- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH

## Verdict

CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE

This gate implemented only the approved repo-owned runtime image assembly artifact for the reviewed 20260507 official OpenClaw localization static control-ui artifact. It did not build, tag, push, pull, run, deploy, or verify an image.

## Artifact paths

Source localization artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/`

Readonly assembly reference:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`

New runtime image assembly artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/`

Runtime-served control-ui target covered by this assembly overlay:

- `/usr/local/lib/node_modules/openclaw/dist/control-ui`

## Changed files

New assembly artifact files:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/index.html`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/index-M4TNVXB3.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/i18n-B06L7jQN.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/zh-CN-B26mMdbY.js`

New implementation evidence file:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md`

No existing runtime image assembly artifact, existing control-ui runtime artifact, or existing evidence file was modified by this gate.

## Assembly summary

The new artifact mirrors the readonly 20260506 control-ui persistence assembly shape:

- parent image index digest pinned in `Dockerfile`
- platform set to `linux/arm64`
- overlay root `control-ui/`
- file copies use `COPY --chmod=0644`
- destination root `/usr/local/lib/node_modules/openclaw/dist/control-ui`

The source files are copied from the reviewed localization artifact:

- `index.html` copied unchanged
- `assets/i18n-B06L7jQN.js` copied unchanged
- `assets/index-M4TNVXB3.js` copied unchanged to preserve the persistence fix
- `assets/zh-CN-B26mMdbY.js` copied as the changed zh-CN locale chunk

No additional localization edits were performed in this gate.

## File manifest

| Overlay file | Runtime destination | Size | SHA-256 | Status |
| --- | --- | ---: | --- | --- |
| `control-ui/index.html` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | copied unchanged |
| `control-ui/assets/index-M4TNVXB3.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `708145` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | copied unchanged to preserve persistence fix |
| `control-ui/assets/i18n-B06L7jQN.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | copied unchanged |
| `control-ui/assets/zh-CN-B26mMdbY.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `23258` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | changed zh-CN locale chunk |

Changed zh-CN locale chunk relative to the prior persistence assembly:

- Prior persistence assembly SHA-256: `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809`
- New localization assembly SHA-256: `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`

## Protected literal policy

The assembly artifact preserves the protected literal policy from the localization artifact manifest. Technical compatibility literals and identifiers were not translated or renamed:

- lowercase `openclaw` package, config, path, API, and command identifiers
- `.openclaw`
- `docs/openclaw.json`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `--no-open`
- `control-ui`
- route/path/API field names
- WebSocket/API/RPC/JSON/CSV/UTC/IANA/Cron/Webhook/model placeholders where they are protocol, format, or ecosystem terms

This gate copied already-reviewed static files into the assembly context only.

## Risk statement

- This patch changes only static control-ui artifact overlay assembly.
- It does not touch backend auth, runtime auth, trustedProxy handling, API contracts, routes, or runtime startup materialization.
- The persistence fix is retained by carrying forward `assets/index-M4TNVXB3.js` unchanged.
- Image build/tag/push is a later independent gate.
- Browser/manual E2E is a later independent gate.

## Verification commands

Readonly dependency checks:

```bash
sed -n '1,240p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-approval-packet.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/Dockerfile
find specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/*
wc -c specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/*
```

Assembly artifact checks:

```bash
find specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/index.html specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/*
wc -c specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/index.html specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/control-ui/assets/*
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/Dockerfile
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md
```

Hygiene and marker checks:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image
rg -n "CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE|CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE|20260507-controlui-localization|20260507-official-openclaw-localization|/usr/local/lib/node_modules/openclaw/dist/control-ui|no trustedProxy patch|no plugin|no skill distribution|no build/tag/push|no browser E2E|no kubectl|no Mem0 write|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization/MANIFEST.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260507-controlui-localization specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-assembly-patch.md
```

## Verification result summary

- Source localization artifact file listing showed `MANIFEST.md`, `index.html`, `assets/i18n-B06L7jQN.js`, `assets/index-M4TNVXB3.js`, and `assets/zh-CN-B26mMdbY.js`.
- Source localization artifact hashes matched the reviewed manifest.
- New assembly artifact file listing showed `Dockerfile`, `MANIFEST.md`, `control-ui/index.html`, `control-ui/assets/i18n-B06L7jQN.js`, `control-ui/assets/index-M4TNVXB3.js`, and `control-ui/assets/zh-CN-B26mMdbY.js`.
- New assembly overlay hashes matched the reviewed localization artifact hashes.
- `index.html`, `assets/i18n-B06L7jQN.js`, and `assets/index-M4TNVXB3.js` were copied unchanged; `assets/index-M4TNVXB3.js` therefore preserves the persistence fix.
- `assets/zh-CN-B26mMdbY.js` is the changed zh-CN locale chunk with SHA-256 `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`.
- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image` exited 0.
- Required marker scan exited 0 for this evidence file and the new runtime image assembly manifest.
- Scoped git status showed only this new implementation evidence file and the new `20260507-controlui-localization/` assembly artifact in the requested path scope.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no trustedProxy patch, no runtime auth contract patch, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing artifact modification, no existing evidence modification, no plugin, no skill distribution, no build/tag/push, no image pull, no container run, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance mutation, no database access or mutation, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, authorization header plaintext, private key, or access URL plaintext was recorded.
