# Control UI Agent Chunk Source Recovery And Localization Patch

Date/timezone: 2026-05-09, Asia/Shanghai

Gate: CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_GATE

Approval phrase:

- APPROVE_CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_GATE

Verdict:

- CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_DONE

## Scope

This gate recovered the exact runtime lazy chunk `agents-_34Q844e.js`, copied it into both writable overlay artifacts, localized the approved static bundled display text for the agent page, and recorded the changed asset hash.

No build/tag/push was performed. no deploy was performed. no browser E2E was performed.

## Recovery Source

Proven runtime path:

- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js`

The requested delivered image tag was not present under the requested host tag in local Docker, so the approved fallback was used:

- instance 24 / oc2gi-iloc-rs-103428 running container readback

Recovered exact chunk before localization:

- source path: `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js`
- recovered size: 87666 bytes
- recovered hash: `5064e99963a255ab72a5c9df17f359cf679a4dc4ad276102b56ed9ff4d36f40d`

Copied targets:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/agents-_34Q844e.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/agents-_34Q844e.js`

## Root Cause Classification

The agent page residual was in a lazy-loaded static Control UI chunk:

- static bundled display text: patched in `agents-_34Q844e.js`
- lazy-loaded chunk: recovered because it was absent from the writable overlay artifacts
- dynamic metadata deferred: no plugin, skill, or user-provided metadata was translated in this gate
- code literal retained: route ids, panel ids, enum ids, method names, and runtime values remain unchanged
- file name retained: core file names and internal ids remain unchanged

`main (default)` was not a literal string in the chunk. The `main` value remains the agent id. Only the display wrapper was localized so the rendered option becomes `main（默认）` when the imported helper returns the default marker.

## Patched Display Text

| Residual | Result | Classification |
| --- | --- | --- |
| `main (default)` | `main（默认）` | display wrapper localized; `main` id retained |
| `Copy ID` | `复制 ID` | static bundled display text |
| `Default` | `默认` | static bundled display text |
| `Overview` | `概览` | static bundled display text |
| `Files` | `文件` | tab display text localized; route id retained |
| `Tools` | `工具` | tab display text localized; route id retained |
| `Skills` | `技能` | tab and card display text localized; route id retained |
| `Channels` | `频道` | tab and card display text localized; route id retained |
| `Cron Jobs` | `定时任务` | tab display text localized |
| `Core Files` | `核心文件` | static bundled display text |
| `Bootstrap persona, identity, and tool guidance.` | `引导人格、身份和工具指南。` | static bundled display text |
| `Workspace:` | `工作区：` | static bundled display text |
| `Select a file to edit.` | `选择一个文件进行编辑。` | static bundled display text |

## Retained And Deferred

- code literal retained: `main` remains the agent id and is not renamed.
- file name retained: `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, and `MISSING` are treated as file names or internal ids when present.
- method names, route names, file names, enum ids, and protocol literals are retained.
- dynamic metadata deferred: runtime plugin, skill, and user-provided metadata remain out of scope.

## Artifact Changes

Changed files:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/agents-_34Q844e.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/agents-_34Q844e.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-agent-chunk-source-recovery-and-localization-patch.md`

Dockerfile COPY added:

- `control-ui/assets/agents-_34Q844e.js` to `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js`

New asset hash:

| Artifact copy | Size | Hash |
| --- | ---: | --- |
| source artifact `assets/agents-_34Q844e.js` | 87715 | `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7` |
| assembly artifact `control-ui/assets/agents-_34Q844e.js` | 87715 | `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7` |

The source and assembly copies compare identical.

## Verification Results

- `test -f` passed for both recovered artifact copies.
- `node --check` passed for both changed JS bundle copies.
- `shasum -a 256` produced identical `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7` hashes.
- `cmp -s` passed for the source and assembly copies.
- Dockerfile and both MANIFEST files now reference `agents-_34Q844e.js`.
- Chinese display string scan found the localized target labels. `main（默认）` is runtime-composed from the retained `main` id plus the localized `（默认）` wrapper.

## Remaining Hits

The broad English scan can still match code identifiers and non-display wiring because it includes generic terms such as `Files`, `Tools`, `Skills`, and `Channels`.

Remaining hits are classified as:

- code literal retained: `agentFilesList`, `agentFilesLoading`, `agentSkills`, `agentTools`, `activePanel`, and route ids such as `files`, `tools`, `skills`, `channels`, and `cron`.
- dynamic metadata deferred: labels and descriptions loaded from runtime plugin, skill, or user data remain out of scope.
- file name retained: core file names and internal ids remain unchanged if they are returned by runtime data.

No remaining hit is an approved exact display residual for `Copy ID`, `Overview`, `Cron Jobs`, `Core Files`, `Bootstrap persona`, `Workspace:`, or `Select a file to edit`.

## Guardrails

- no auth/scope modification
- no operator.admin grant
- no missing_scope bypass
- no runtime auth predicate modification
- no backend modification
- no build/tag/push
- no deploy
- no browser E2E
- no instance/database mutation
- no cleanup
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
