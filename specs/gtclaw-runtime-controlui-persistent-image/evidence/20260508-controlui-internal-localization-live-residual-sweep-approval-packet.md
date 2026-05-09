# CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_APPROVAL_PACKET

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate type: CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_APPROVAL_PACKET_GATE
Approval phrase for next gate: APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_GATE

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_APPROVAL_PACKET_DONE
```

This packet requests approval for the next gate only: root-cause and patch planning/execution for live instance 23 internal Control UI residual English observed by the user. This packet does not approve build, image push/pull, deploy, rollout, browser E2E, manual E2E, instance changes, cleanup, Close, longterm write-back, or git stage/commit/push.

## Current State

Prerequisite gate `CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_DONE` is complete. User manual E2E has reached instance 23 / `oc2gi-iloc-r-213106` internal UI.

Connection and base scope are no longer the current blocker:

```text
device signature invalid not observed
device identity required not observed
missing operator.read not observed
missing_scope not observed for the previously blocking base connection path
```

The current failure point is internal Control UI residual English. The `GatewayRequestError: missing scope: operator.admin` text observed on privileged pages is not approval to add the scope. It is an error-display localization target only, with `no operator.admin grant` and no backend or runtime scope predicate change.

## Live Residuals Reported By User

### Appearance / Settings

Exact or near-exact residual strings:

```text
Theme
Choose a theme family.
Claw
Knot
Dash
Roundness
Adjust corner radius across the UI.
None
Slight
Default
Round
Full
Connection
```

### Dreams / Diary

Exact or near-exact residual strings:

```text
Dreams
Imported Insights
Memory Palace
This is the raw dream diary the system writes while replaying and consolidating memory; use it to inspect what the memory system is noticing, and where it still looks noisy or thin.
```

### Nodes / Exec approvals

Exact or near-exact residual strings:

```text
GatewayRequestError: missing scope: operator.admin
Exec approvals
Allowlist and approval policy for exec hosts=gateway/node.
Target
Host
Gateway
Gateway edits local approvals; node edits the selected node.
Load exec approvals to edit allowlists.
Save
Any node
No nodes with system.run available.
```

### Skills

Exact or near-exact residual strings:

```text
GatewayRequestError: missing scope: operator.admin
Skills
Installed skills and their status.
All
Ready
Needs Setup
Disabled
Filter installed skills
52 shown
ClawHub
Search and install skills from the registry
Search ClawHub skills...
BUILT-IN SKILLS
Set up and use 1Password CLI...
Manage Apple Notes...
```

## Next Gate Objective

Locate and patch residual English visible in the internal Control UI from the live instance 23 screenshot set. The next gate must classify every targeted string before editing, then apply the smallest runtime-Control-UI-only artifact changes needed to localize static UI copy without changing connection, identity, admin scope, or runtime trust behavior.

## Required Source Classification

The next gate must classify each residual string into one of these buckets before patching:

```text
compiled control-ui bundle hardcoded text
locale/i18n key missing
lazy-loaded chunk
config-form/schema UI chunk
feature-specific chunk: dreams, skills, nodes, approvals
dynamic plugin/skill metadata/user data
product name / theme name / protocol name / code literal
```

Classification notes:

- Appearance strings such as `Theme`, `Choose a theme family.`, `Roundness`, and `Adjust corner radius across the UI.` are likely static UI copy, but the next gate must prove whether they live in `index-M4TNVXB3.js`, `config-form-x_UhxUYO.js`, `i18n-B06L7jQN.js`, `zh-CN-B26mMdbY.js`, or another lazy chunk.
- Theme values `Claw`, `Knot`, and `Dash` may be product/theme name literals. Do not blindly translate product name values unless the source proves they are user-facing labels rather than stable identifiers.
- Values such as `None`, `Slight`, `Default`, `Round`, and `Full` may be enum labels, schema UI labels, or code literal values. Translate only the display label, not the underlying code literal.
- `Dreams`, `Imported Insights`, and `Memory Palace` are feature-specific strings. The next gate must determine whether they are static UI copy, locale entries, or dynamic data from runtime memory content.
- `Exec approvals`, `Allowlist and approval policy for exec hosts=gateway/node.`, and related host/node approval text are feature-specific approvals UI strings. Scope literals such as `operator.admin`, `operator.read`, `system.run`, `gateway`, and `node` remain technical literals unless displayed inside a localized sentence.
- Skill descriptions may be localized only if they are built-in static metadata packaged in the Control UI/runtime artifact. If they come from dynamic plugin metadata, user data, or registry data, record them as dynamic plugin/skill metadata/user data for separate approval.

## Approved Patch Boundary For Next Gate

Allowed next-gate write scope after approval:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/
```

Allowed next-gate patch targets:

- Compiled Control UI bundle assets and lazy chunks under the control-ui runtime artifact.
- Matching compiled Control UI bundle assets and lazy chunks under the runtime image assembly artifact.
- Runtime image assembly artifact Dockerfile only when copying or adding a missing lazy chunk into the assembly is required.

Required hash handling:

- Record pre-change and post-change SHA-256 for every changed asset.
- If a missing lazy-loaded chunk is newly copied into the assembly artifact, record source path, destination path, and SHA-256.
- If Dockerfile assembly behavior changes, record the exact copied asset list and the asset hashes.

Protected literals and non-translation zones:

- Do not blindly translate skill names, product name values, theme name values, protocol names, scope literals, method names, API key literal text, CLI commands, package names, route names, file names, or code literal values.
- `missing scope: operator.admin` may only be changed as localized error presentation. It must not be fixed by adding, granting, bypassing, or fabricating `operator.admin`.
- `no operator.admin grant`.
- `no missing_scope bypass`.
- `no missing_scope stage/commit/push`: no missing_scope bypass and no git stage/commit/push are approved by this packet.

Forbidden next-gate changes:

- No backend auth/scope modification.
- No runtime auth predicate modification.
- No runtime scope propagation modification.
- No `operator.admin` grant.
- No missing-scope bypass.
- No image build, tag, push, pull, deploy, rollout, instance create/stop/delete, browser E2E, manual E2E, cleanup, Close, longterm write-back, or git stage/commit/push.

## Required Next-Gate Verification

The next gate must provide fresh evidence for:

```text
exact/near-exact string scan for every live screenshot residual listed above
new or changed asset SHA-256 hashes
node --check on every changed JS bundle or lazy chunk
classification of every remaining hit
explicit separation of static UI copy from dynamic plugin/skill metadata/user data
explicit confirmation that product name, theme name, protocol name, scope literal, method name, API key literal, CLI command, and code literal handling was not blind translation
explicit confirmation of no operator.admin grant
explicit confirmation of no missing_scope bypass
```

The next gate may run local read-only scans and syntax checks against the artifact files. Any later build, runtime image assembly validation, deploy, rollout, browser inspection, or manual E2E must go through a separate approval gate.

## Suggested Next-Gate Scan Set

Initial source candidates:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/config-form-x_UhxUYO.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/i18n-B06L7jQN.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/zh-CN-B26mMdbY.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/config-form-x_UhxUYO.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/i18n-B06L7jQN.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/zh-CN-B26mMdbY.js
```

Suggested string scan targets:

```text
Theme|Choose a theme family|Roundness|Adjust corner radius across the UI|Connection
Dreams|Imported Insights|Memory Palace|raw dream diary
Exec approvals|Allowlist and approval policy|GatewayRequestError|operator.admin|No nodes with system.run available
Skills|Installed skills|Needs Setup|BUILT-IN SKILLS|Search ClawHub skills|1Password CLI|Apple Notes
```

## Approval Request

Approve only the next gate named by this phrase:

```text
APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_GATE
```

Approving this phrase authorizes root-cause classification and artifact-only patching within the boundaries above. It does not authorize build/deploy/live E2E, backend auth/scope work, runtime auth predicate/scope propagation changes, `operator.admin` grant, missing_scope bypass, instance mutation, cleanup, Close, longterm write-back, or git stage/commit/push.

## Boundary Confirmation

```text
packet only
no patch in this gate
no build/tag/push/pull image
no deploy/rollout/kubectl/k3d/Helm mutation
no create/stop/delete instance
no browser/manual E2E/DevTools/Playwright
no cleanup
no backend auth/scope modification
no runtime auth predicate/scope propagation modification
no operator.admin grant
no missing_scope bypass
no frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework modification
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
no sensitive values recorded
```
