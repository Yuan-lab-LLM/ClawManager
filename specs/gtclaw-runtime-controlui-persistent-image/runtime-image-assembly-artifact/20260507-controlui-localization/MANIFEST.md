# Runtime Image Assembly Artifact Manifest

Date/timezone: 2026-05-07, Asia/Shanghai

Artifact: `20260507-controlui-localization`

Gate: CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET_DONE
- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH

Verdict scope: assembly artifact only; no build/tag/push/pull.

## Parent Image

- Parent host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656`
- Parent image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- linux/arm64 digest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`

## Source Artifact

Source localization artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/`

Readonly assembly reference:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`

## Assembly Policy

- Dockerfile parent is digest-pinned to the prior origin-allowlist image index.
- Dockerfile platform is `linux/arm64`.
- Files are copied with `COPY --chmod=0644`.
- Destination root is `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- This artifact is only a repo-owned image assembly build context.
- This gate did not build, tag, push, pull, deploy, restart, or mutate runtime state.
- This gate did not patch trustedProxy, runtime auth contracts, plugins, or skill distribution.

## File Manifest

| Source path | Destination path | Mode | Size | SHA-256 | Assembly status |
|---|---|---:|---:|---|---|
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `0644` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | copied unchanged |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `0644` | `708145` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | copied unchanged to preserve persistence fix |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/i18n-B06L7jQN.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `0644` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | copied unchanged |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/zh-CN-B26mMdbY.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `0644` | `23258` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | changed zh-CN locale chunk |

## Localization Delta

Copied unchanged from the reviewed localization artifact:

- `index.html`
- `assets/i18n-B06L7jQN.js`
- `assets/index-M4TNVXB3.js`

Localized static control-ui overlay file:

- `assets/zh-CN-B26mMdbY.js`
- Prior persistence assembly SHA-256: `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809`
- New localization assembly SHA-256: `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`

The localization patch remains limited to the reviewed static control-ui artifact overlay. It does not change backend auth, runtime auth, trustedProxy handling, API contracts, routes, or runtime startup materialization.

## Protected Literal Policy

This assembly artifact preserves the protected literal policy from the localization artifact. Technical compatibility literals and identifiers must remain unchanged unless a separate gate proves they are display-only:

- lowercase `openclaw` package, config, path, API, and command identifiers
- `.openclaw`
- `docs/openclaw.json`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `--no-open`
- `control-ui`
- route/path/API field names
- WebSocket/API/RPC/JSON/CSV/UTC/IANA/Cron/Webhook/model placeholders where they are protocol, format, or ecosystem terms

## Explicit Non-actions

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
