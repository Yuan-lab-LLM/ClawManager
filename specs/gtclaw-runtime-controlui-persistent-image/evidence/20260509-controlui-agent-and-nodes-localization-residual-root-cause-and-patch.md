# Control UI Agent And Nodes Localization Residual Root Cause And Patch

Date/timezone: 2026-05-09, Asia/Shanghai

Gate:
CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_GATE

Approval phrase:
APPROVE_CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_GATE

## Verdict

CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_BLOCKED:
nodes static bundled display text was patched and synchronized, but the agent page residuals are in lazy chunk `agents-_34Q844e.js`, which is referenced by the compiled index bundle but absent from both writable overlay artifacts. The agent page strings must not be fabricated from unrelated bundles.

## Scope Observed

- Dependency gate `CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE` is treated as satisfied by the requested gate.
- Dependency gate `CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_DONE` is treated as satisfied by the requested gate.
- User manual E2E on instance 24 / oc2gi-iloc-rs-103428 reached the internal UI.
- Current blocker is live residual English on 代理 and 节点 pages.

## Root Cause Classification

### 代理 Page

- `index-M4TNVXB3.js` references `./agents-_34Q844e.js` through the lazy loader and calls `renderAgents`.
- `agents-_34Q844e.js` is absent in:
  - `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/`
  - `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/`
- Exact searches for observed agent strings did not find static patchable display text in the writable overlay assets:
  - `Copy ID`
  - `Bootstrap persona`
  - `Workspace:`
  - `Select a file to edit.`
  - `Core Files`
  - `main (default)`
- Classification:
  - `Overview`, `Files`, `Tools`, `Skills`, `Channels`, `Cron Jobs`, `Core Files`, `Copy ID`, and the bootstrap sentence: lazy-loaded feature chunk, not available in this writable overlay.
  - `main`: code literal retained as the agent/session id. A future patch may render `main（默认）` only inside display-only text.
  - `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, `MISSING`: file name retained or code literal retained unless a future chunk inspection proves a separate display wrapper.
  - Possible `dynamic metadata`: agent file lists and descriptions can arrive from runtime data through `agents.files.list` / `agents.files.get`; those are not safe to translate at this gate.

### 节点 Page

- `nodes-BBk4VzkK.js` is present in both writable overlay artifacts and contains static bundled display text for the observed Nodes residuals.
- Patched static bundled display text in source and assembly copies:
  - `Binding` -> `绑定`
  - `default agent` / `uses default (...)` -> `默认代理` / `使用默认值（...）`
  - `Use default` -> `使用默认值`
  - `Devices` -> `设备`
  - pairing request sentence -> `配对请求和角色凭证。`
  - `No paired devices.` -> `没有已配对设备。`
  - `Nodes` -> `节点`
  - `Paired devices and live links.` -> `已配对设备和实时链接。`
  - `No nodes found.` -> `没有找到节点。`
- Additional adjacent static bundled display text in the same chunk was localized to avoid leaving the same surface partially English.
- `system.run`, `exec host=node/gateway`, `gateway`, `node`, `__default__`, `__defaults__`, method names, route names, enum ids, and internal field names were preserved as protocol literal retained or code literal retained.

## Patch Files

Changed runtime artifact files:

- `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/nodes-BBk4VzkK.js`
- `control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md`

Changed assembly artifact files:

- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/nodes-BBk4VzkK.js`
- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`

No Dockerfile change. No lazy chunk was copied or added because the needed agent chunk is absent from the writable artifacts.

## New Asset Hash

new asset hash:

- source `assets/nodes-BBk4VzkK.js`
  - old SHA-256: `bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e`
  - new SHA-256: `25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4`
  - size: `22063`
- assembly `control-ui/assets/nodes-BBk4VzkK.js`
  - old SHA-256: `bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e`
  - new SHA-256: `25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4`
  - size: `22063`

`cmp -s` confirmed source and assembly `nodes-BBk4VzkK.js` are byte-identical after patch.

## Verification Performed

- `node --check` passed for changed source JS bundle/chunk.
- `node --check` passed for changed assembly JS bundle/chunk.
- `shasum -a 256` recorded the changed source and assembly asset hash above.
- `cmp -s` passed for changed source and assembly asset.
- English residual scan on changed assets no longer shows the target Nodes display strings.
- Remaining hits in changed assets are code literal retained:
  - `autoAllowSkills`
  - `onDevicesRefresh`
  - `defaultBinding`
  - `configForm`
  - `system.run`
  - `main`
- Chinese target scan found:
  - `绑定`
  - `默认代理`
  - `使用默认值`
  - `设备`
  - `配对请求`
  - `没有已配对设备`
  - `实时链接`
  - `没有找到节点`

## Blocker Detail

The agent page blocker remains because the only compiled evidence in the writable overlay is a lazy import reference:

- `./agents-_34Q844e.js`
- `renderAgents(...)`

The target chunk itself is not present in either writable overlay artifact, and no approved source path for that missing chunk was available in this gate. Patching `index-M4TNVXB3.js`, `config-form-x_UhxUYO.js`, or locale fallback data would not address the observed agent page UI safely.

Required next gate:

- supply or approve the exact `agents-_34Q844e.js` runtime chunk source;
- then scan it for `main (default)`, `Copy ID`, `Default`, `Overview`, `Files`, `Tools`, `Skills`, `Channels`, `Cron Jobs`, `Core Files`, `Bootstrap persona`, `Workspace:`, and `Select a file to edit.`;
- patch only static bundled display text;
- keep code literal retained, protocol literal retained, file name retained, and dynamic metadata deferred.

## Guardrails Confirmed

- no auth/scope modification
- no operator.admin grant
- no missing_scope bypass
- no backend modification
- no runtime auth predicate modification
- no runtime scope propagation modification
- no build/tag/push
- no deploy
- no browser E2E
- no instance/database mutation
- no cleanup
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

Final marker:
CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_BLOCKED
