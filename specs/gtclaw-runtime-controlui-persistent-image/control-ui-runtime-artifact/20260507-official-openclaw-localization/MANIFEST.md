# Official OpenClaw localization static artifact manifest

Date/timezone: 2026-05-07, Asia/Shanghai

Artifact: `20260507-official-openclaw-localization`

Source artifact path:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/`

## Scope

This artifact is a repo-owned static control-ui artifact overlay for GTClaw control-ui localization.

It preserves the existing persistence fix by carrying forward the source artifact's control-ui runtime files. It carries forward the existing i18n loader chunk `assets/i18n-B06L7jQN.js` unchanged. It changes the Simplified Chinese locale chunk and the minimal compiled control-ui chunks needed for hardcoded internal UI copy that bypasses the locale bundle.

No trustedProxy patch was performed. No runtime auth contract patch was performed. No plugin work was performed. No skill distribution work was performed. No build/tag/push was performed.

## Changed files

Changed relative to source artifact:

- `assets/index-M4TNVXB3.js`
- `assets/agents-_34Q844e.js`
- `assets/config-form-x_UhxUYO.js`
- `assets/nodes-BBk4VzkK.js`
- `assets/skills-BRWdbtpV.js`
- `assets/skills-shared-D6eRDyeb.js`
- `assets/zh-CN-B26mMdbY.js`
- `MANIFEST.md` (new manifest in this artifact only)

Copied unchanged relative to source artifact:

- `index.html`
- `assets/i18n-B06L7jQN.js`

## File manifest

| File | Source size | Source SHA-256 | Artifact size | Artifact SHA-256 | Status |
| --- | ---: | --- | ---: | --- | --- |
| `index.html` | 3398 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | 3398 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | copied unchanged |
| `assets/i18n-B06L7jQN.js` | 42617 | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | 42617 | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | copied unchanged |
| `assets/index-M4TNVXB3.js` | 708145 | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | 708269 | `6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6` | localized hardcoded internal UI copy |
| `assets/agents-_34Q844e.js` | 87666 | `5064e99963a255ab72a5c9df17f359cf679a4dc4ad276102b56ed9ff4d36f40d` | 87715 | `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7` | recovered runtime lazy chunk and localized agent page display copy |
| `assets/config-form-x_UhxUYO.js` | 47378 | `2b26c4c3e7b9ca76350dca7a6fc67253e6b3c04c7e907a0ee98384e8c34cefd1` | 47447 | `8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245` | localized schema form fallback copy |
| `assets/nodes-BBk4VzkK.js` | 21966 | `bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e` | 22063 | `25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4` | localized nodes page hardcoded display copy |
| `assets/skills-BRWdbtpV.js` | 14048 | `36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22` | 14048 | `36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22` | localized skills page hardcoded display copy |
| `assets/skills-shared-D6eRDyeb.js` | 1505 | `f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b` | 1505 | `f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b` | localized static packaged skills metadata copy |
| `assets/zh-CN-B26mMdbY.js` | 23255 | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | 23258 | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | localized |

## Localization summary

The localization patch covers user-visible Simplified Chinese control-ui strings in `assets/zh-CN-B26mMdbY.js`, hardcoded internal UI copy in `assets/index-M4TNVXB3.js`, agent page hardcoded internal UI copy in `assets/agents-_34Q844e.js`, nodes page hardcoded internal UI copy in `assets/nodes-BBk4VzkK.js`, skills page hardcoded internal UI copy in `assets/skills-BRWdbtpV.js`, static packaged skills metadata copy in `assets/skills-shared-D6eRDyeb.js`, and schema form fallback copy in `assets/config-form-x_UhxUYO.js`.

Representative changes:

- Gateway-facing text now uses `网关` instead of the English `Gateway` in Chinese sentences.
- Auth label `网关 token` is now `网关令牌`.
- Control-ui connection copy now refers to a `控制台 URL` instead of an `仪表盘 URL`.
- Dreaming status labels are localized as `梦境模式`.
- A residual usage-detail label `Skills` is localized to `技能`.
- Language option descriptors for Japanese, Korean, French, Turkish, Indonesian, and Polish are localized.
- Internal chat placeholder, model/session defaults, settings/config editor controls, and schema fallback messages are localized where the compiled bundle hardcoded English text outside the locale chunk.
- Agent page labels for `main (default)`, `Copy ID`, `Overview`, `Files`, `Tools`, `Skills`, `Channels`, `Cron Jobs`, `Core Files`, `Workspace:`, and the core-file empty state are localized in the recovered lazy chunk.

## Protected literal policy

The patch preserves technical compatibility literals and identifiers. Do not translate or rename these unless a separate gate proves they are display-only:

- lowercase `openclaw` package, config, path, API, and command identifiers
- `.openclaw`
- `docs/openclaw.json`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `--no-open`
- `control-ui`
- route/path/API field names
- WebSocket/API/RPC/JSON/CSV/UTC/IANA/Cron/Webhook/model placeholders where they are protocol, format, or ecosystem terms
- agent or session id values such as `main`
- core file names and internal ids such as `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, and `MISSING`

Observed protected literal counts in the changed locale chunk after patch:

- `openclaw`: 2
- `--no-open`: 1
- `docs/openclaw.json`: 0
- `.openclaw`: 0
- `control-ui`: 0

## Explicit non-actions

- no trustedProxy patch
- no runtime auth contract patch
- no backend modification
- no frontend modification
- no deployment modification
- no runtime startup artifact modification
- no runtime image build or deployment from this artifact
- no plugin packaging
- no skill distribution
- no build/tag/push
- no image pull
- no container run
- no browser E2E
- no kubectl/k3d/Helm mutation
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
