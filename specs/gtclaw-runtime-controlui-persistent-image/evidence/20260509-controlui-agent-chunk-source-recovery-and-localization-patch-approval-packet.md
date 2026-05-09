# Control UI Agent Chunk Source Recovery And Localization Patch Approval Packet

Date/timezone: 2026-05-09, Asia/Shanghai

Gate:
CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_APPROVAL_PACKET_GATE

Requested next approval phrase:
APPROVE_CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_GATE

## Dependency State

- `CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_BLOCKED` is the current upstream result.
- The 节点 page `nodes-BBk4VzkK.js` static bundled display text has already been patched and synchronized between source artifact and assembly artifact.
- Current blocker: 代理 page residual text is in lazy chunk `agents-_34Q844e.js`, but that chunk is absent from the current writable overlay artifacts.
- Current packet only requests the next gate. It does not recover a chunk, patch JS, edit Dockerfile, build, deploy, mutate an instance, or run browser E2E.

## Current Root Cause

- `index-M4TNVXB3.js` references `./agents-_34Q844e.js` from the compiled lazy loader.
- The expected runtime path is:
  `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js`
- The writable overlay target paths do not currently contain the chunk:
  - `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/agents-_34Q844e.js`
  - `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/agents-_34Q844e.js`
- Because the exact chunk is absent, patching `index-M4TNVXB3.js`, locale data, or config-form/schema chunks would be unsafe and incomplete.

## Requested Next Gate Scope

Allow the next gate to recover the exact runtime chunk from a proven Control UI source:

- Preferred image source:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428`
- Alternate readback source:
  instance 24 / `oc2gi-iloc-rs-103428` running container readback
- Required proven path:
  `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js`

Allow the next gate to copy the exact recovered chunk to:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/agents-_34Q844e.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/agents-_34Q844e.js`

Allow the next gate to:

- add the corresponding `COPY` line to `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile`;
- patch only 代理 page static bundled display text in `agents-_34Q844e.js`;
- update manifest and hash records for the recovered chunk and any changed artifact;
- run syntax, hash, source/assembly byte match, and residual scans;
- produce evidence for source path, destination paths, and hash values.

## Target Display Strings For Next Gate

The next gate should scan the recovered `agents-_34Q844e.js` for exact or near-exact display text:

- `main (default)`
- `Copy ID`
- `Default`
- `Overview`
- `Files`
- `Tools`
- `Skills`
- `Channels`
- `Cron Jobs`
- `Core Files`
- `Bootstrap persona, identity, and tool guidance.`
- `Workspace:`
- `Select a file to edit.`

Expected display-only translations may include:

- `Copy ID` -> `复制 ID`
- `Default` -> `默认`
- `Overview` -> `概览`
- `Files` -> `文件`
- `Tools` -> `工具`
- `Skills` -> `技能`
- `Channels` -> `频道`
- `Cron Jobs` -> `定时任务`
- `Core Files` -> `核心文件`
- `Bootstrap persona, identity, and tool guidance.` -> `引导人格、身份和工具指南。`
- `Workspace:` -> `工作区：`
- `Select a file to edit.` -> `选择一个文件进行编辑。`

## Literal And Data Classification Required

- main code literal retained: keep `main` as the agent/session id; only a display wrapper may show `main（默认）`.
- `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, `MISSING`: file name retained when they are file names, internal ids, or code literals.
- method names, route names, enum ids, protocol literals, command names, and file names must remain unchanged.
- dynamic metadata deferred: runtime user data, agent file lists, dynamic file content, and remote registry/plugin data are recorded but not translated unless proven to be static bundled display text.
- Product or project names may be retained if they are stable names rather than display labels.

## Next Gate Verification Requirements

- Confirm recovered source path and destination paths.
- Record original recovered chunk hash before patch.
- Record changed source and assembly asset hash after patch.
- Confirm source and assembly recovered chunk are byte-identical after sync.
- Run `node --check` on every changed JS bundle/chunk in source artifact.
- Run `node --check` on every changed JS bundle/chunk in assembly artifact.
- Scan changed assets for remaining target English strings.
- Scan changed assets for expected Chinese display strings.
- Explain remaining hits as static bundled display text, dynamic metadata deferred, code literal retained, protocol literal retained, or file name retained.
- If Dockerfile changes, scan it for the added `agents-_34Q844e.js` `COPY` line.
- Confirm no backend, runtime auth/scope, scope predicate, or scope propagation change.

## Current Gate Non-Actions

- no chunk recovery
- no JS patch
- no Dockerfile patch
- no manifest patch
- no auth/scope modification
- no operator.admin grant
- no missing_scope bypass
- no backend modification
- no runtime auth predicate modification
- no runtime scope propagation modification
- no build/tag/push
- no pull
- no deploy
- no kubectl/k3d/Helm mutation
- no instance/database mutation
- no browser E2E
- no cleanup
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Approval Request

Request approval for next gate:

APPROVE_CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_GATE

Expected next-gate success marker:

CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_DONE

Current approval packet marker:

CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_APPROVAL_PACKET_DONE
