# CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET

Date/timezone: 2026-05-09, Asia/Shanghai
Role/task: Worker, serial topology
Gate type: CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_GATE
Approval phrase for next gate: APPROVE_CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_GATE

## Verdict

```text
CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE
```

This packet requests the next root-cause-and-patch gate only. This gate did not patch JS, Dockerfile, manifest, backend, runtime auth/scope, or runtime behavior. This gate did not build/tag/push, deploy, mutate instances or database state, run browser E2E, run manual E2E, perform cleanup, set passes:true, Close, write longterm state, or git stage/commit/push.

## Dependency State

Completed prerequisite delivery:

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_DONE
```

Manual E2E on instance 24 / `oc2gi-iloc-rs-103428` reached the internal UI. User reported that most pages are acceptable, while the 代理 and 节点 surfaces still show display English residuals.

## Next Gate Approval Request

Approve only this next gate phrase:

```text
APPROVE_CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_GATE
```

Approval authorizes the next Worker to locate, classify, and minimally patch the Agent and Nodes localization residuals in reviewed runtime artifacts only.

## Observed Residual Scope

Agent page residuals to classify:

```text
main (default)
Copy ID
Default
Overview
Files
Tools
Skills
Channels
Cron Jobs
Core Files
Bootstrap persona, identity, and tool guidance.
Workspace:
AGENTS / SOUL / TOOLS / IDENTITY / USER / HEARTBEAT / BOOTSTRAP / MEMORY / MISSING
Select a file to edit.
```

Nodes page residuals to classify:

```text
Exec 节点绑定
Binding
default agent · uses default (any)
Use default
Devices
Pairing requests plus role credential display text
No paired devices.
Nodes
Paired devices and live links.
No nodes found.
```

The next gate must preserve technical identifiers. `main`, `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, `MISSING`, `system.run`, `exec host=node`, `gateway`, `node`, method names, file names, route names, and protocol names must be classified before any display change. If any item is a code literal, protocol literal, file name, enum id, or dynamic metadata, it must be retained or deferred rather than blindly translated.

## Required Root-Cause Work

The next gate must first locate each residual source and classify it as one of:

```text
static bundled display text
compiled control-ui bundle hardcoded text
lazy-loaded chunk display text
feature-specific Agent chunk
feature-specific Nodes chunk
config/schema display text
dynamic metadata
user data
code literal
protocol literal
file name
product name
```

Likely static artifact targets include:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/nodes-BBk4VzkK.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/nodes-BBk4VzkK.js
```

Actual patch scope must be based on diagnosis. If another bundled chunk contains the display text, the next gate may patch that chunk only after recording the source path and before/after hash.

## Patch Boundary For Next Gate

Allowed after approval:

- Patch static bundled display text for the observed Agent and Nodes residuals.
- Patch matching copies in the control-ui runtime artifact and runtime image assembly artifact.
- Update artifact manifest/hash records only if required by local artifact conventions.
- Run exact/near-exact residual scans against the changed artifacts.
- Run `node --check` on every changed JS bundle or chunk.
- Record every changed asset hash.

Forbidden after approval unless separately approved:

- Do not modify backend.
- Do not modify runtime auth/scope.
- Do not modify runtime auth predicate or scope propagation.
- Do not grant `operator.admin`.
- Do not add a missing_scope bypass.
- Do not change gateway behavior.
- Do not change runtime behavior outside display text.
- Do not translate code literal, protocol literal, enum id, file name, route name, method name, or product name without classification.
- Do not build/tag/push.
- Do not deploy.
- Do not mutate instances or database state.
- Do not run browser E2E or manual E2E.
- Do not cleanup.
- Do not set passes:true.
- Do not Close.
- Do not write longterm state.
- Do not git stage/commit/push.

## Required Verification For Next Gate

The next root-cause-and-patch gate must record:

```text
initial residual scan for Agent and Nodes strings
source classification per residual
static bundled display text patched only
dynamic metadata deferred
code literal retained
protocol literal retained
file name retained
new asset hash for every changed bundle or chunk
matching artifact and assembly copies
node --check every changed JS bundle or chunk
final residual scan
remaining hits classified
no auth/scope modification
no operator.admin grant
no missing_scope bypass
no build/tag/push
no deploy
no browser E2E
no passes:true
no Close
no git stage/commit/push
```

If any residual comes from dynamic metadata or user data, the next gate must record it as dynamic metadata or user data and defer it unless a separate approval expands the scope.

## This Gate Non-Actions

```text
approval packet only
no patch JS
no Dockerfile modification
no manifest modification
no backend modification
no runtime auth/scope modification
no build/tag/push
no pull
no deploy
no kubectl/k3d/Helm mutation
no instance mutation
no database mutation
no browser E2E
no manual E2E
no cleanup
no auth/scope change
no operator.admin grant
no missing_scope bypass
no code literal blind translation
no protocol literal blind translation
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
no sensitive values recorded
```
