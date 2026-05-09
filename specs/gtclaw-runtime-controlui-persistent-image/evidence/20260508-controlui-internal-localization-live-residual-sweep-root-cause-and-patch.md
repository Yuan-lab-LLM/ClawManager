# CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Approval phrase used: APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_GATE

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_DONE
```

Artifact-only root-cause and patch completed. No build/tag/push, no deploy, no browser E2E, no manual E2E, no instance mutation, no cleanup, no passes:true, no Close, and no git stage/commit/push were performed.

## Current Blocker

The connection path and base scope are no longer the blocker for this gate. The blocker handled here was live internal Control UI residual English in instance 23 / `oc2gi-iloc-r-213106`.

`GatewayRequestError: missing scope: operator.admin` was handled only as localized error presentation. There was no operator.admin grant and no missing_scope bypass.

## Root-Cause Summary

- `Theme`, `Choose a theme family.`, `Roundness`, `Adjust corner radius across the UI.`, and `Connection`: compiled control-ui bundle hardcoded text in `index-M4TNVXB3.js`; patched.
- `Claw`, `Knot`, `Dash`: product name / theme name display labels in the theme card array; display labels patched while `claw`, `knot`, and `dash` code literal ids were preserved.
- `None`, `Slight`, `Default`, `Round`, `Full`: display labels for numeric roundness values; numeric code literal values were preserved.
- `Dreams`: existing zh-CN locale already provides `梦境`; remaining English hits are default English locale data or source identifiers. The live hardcoded diary residuals were in `index-M4TNVXB3.js`; patched.
- `Imported Insights`, `Memory Palace`, and the raw dream diary paragraph: feature-specific dreams/wiki hardcoded text in `index-M4TNVXB3.js`; patched.
- `Exec approvals`, `Allowlist and approval policy`, `Target`, `Host`, `Gateway`, `Load exec approvals`, `Save`, `Any node`, and `No nodes with system.run available.`: feature-specific nodes/approvals lazy chunk text in `nodes-BBk4VzkK.js`; chunk was copied from the local parent image and patched.
- `GatewayRequestError: missing scope: operator.admin`: compiled request error class and display helper in `index-M4TNVXB3.js`; presentation now renders `缺少权限范围：operator.admin`. The `operator.admin` code literal remains unchanged.
- `Skills`, `Installed skills and their status.`, `All`, `Ready`, `Needs Setup`, `Disabled`, `Filter installed skills`, count shown text, and `Search ClawHub skills...`: feature-specific skills lazy chunk text in `skills-BRWdbtpV.js`; chunk was copied from the local parent image and patched.
- `BUILT-IN SKILLS`: static packaged metadata heading in `skills-shared-D6eRDyeb.js`; dependency chunk was copied from the local parent image and patched to `内置技能`.
- `ClawHub`: product name retained. Surrounding search and registry copy was localized.
- `1Password CLI` and `Apple Notes` descriptions: not found in the patched Control UI artifact scan. Classified as dynamic plugin/skill metadata/user data and deferred.

## Patched Files

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/nodes-BBk4VzkK.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/skills-BRWdbtpV.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/skills-shared-D6eRDyeb.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/nodes-BBk4VzkK.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/skills-BRWdbtpV.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/skills-shared-D6eRDyeb.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile
```

## Copied Chunk Source And Destination

Source image:

```text
localhost:5001/clawmanager-openclaw/openclaw@sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
```

Copied source paths:

```text
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/nodes-BBk4VzkK.js
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-BRWdbtpV.js
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-shared-D6eRDyeb.js
```

Destination paths:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/nodes-BBk4VzkK.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/skills-BRWdbtpV.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/skills-shared-D6eRDyeb.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/nodes-BBk4VzkK.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/skills-BRWdbtpV.js
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/skills-shared-D6eRDyeb.js
```

Dockerfile assembly lines added for the three copied chunks.

## Asset Hashes

Original hashes recorded before patch:

```text
index-M4TNVXB3.js: ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b
nodes-BBk4VzkK.js: 5604489c32c7a5483ec54e50d21b17734d3f2b29561bf63c671946a255866939
skills-BRWdbtpV.js: b50b67150888582b70cec7e23042809d4eef1bec9279179615d46bac43c6e627
skills-shared-D6eRDyeb.js: c7ccf52397a1decd9b1abec098d0b73d8b2889007dcab70d69e899069cb4d19c
```

New asset hash values after patch:

```text
index-M4TNVXB3.js: 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
nodes-BBk4VzkK.js: bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e
skills-BRWdbtpV.js: 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
skills-shared-D6eRDyeb.js: f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
```

The control-ui runtime artifact and runtime image assembly artifact now have matching hashes for each changed asset.

## Verification

Initial residual scan was red: exact/near-exact screenshot strings were present in `index-M4TNVXB3.js`, `nodes-BBk4VzkK.js`, `skills-BRWdbtpV.js`, and `skills-shared-D6eRDyeb.js` after the missing chunks were copied from the local parent image.

Final display-surface scan:

```text
changed index bundle: no targeted display residuals
changed nodes chunk: no targeted display residuals
changed skills chunk: no targeted display residuals
changed skills shared chunk: no targeted display residuals
```

`node --check` was run on every changed JS bundle/chunk in both artifact trees and returned exit 0 with no output.

Dockerfile scan confirmed the added COPY lines:

```text
control-ui/assets/nodes-BBk4VzkK.js
control-ui/assets/skills-BRWdbtpV.js
control-ui/assets/skills-shared-D6eRDyeb.js
```

Static packaged metadata check:

```text
BUILT-IN SKILLS source: skills-shared-D6eRDyeb.js static packaged metadata
1Password CLI source: not present in patched Control UI artifact
Apple Notes source: not present in patched Control UI artifact
dynamic plugin/skill metadata/user data deferred: true
```

## Remaining Hits

remaining hits from the broad final scan are classified as:

- English default locale data in `i18n-B06L7jQN.js`, not the patched zh-CN display path.
- Existing zh-CN text containing English product/protocol names such as `Dreams`, not a newly patched display regression.
- Source identifiers and object property names in `index-M4TNVXB3.js`, such as theme settings and skills state fields.
- Technical literals including `operator.admin`, `operator.read`, `system.run`, `gateway`, and `node`.
- Existing verifier text under `openclaw-runtime/verify-trusted-proxy-contract.mjs`.
- Config-form schema labels in `config-form-x_UhxUYO.js`; those are schema UI metadata and were not blindly translated in this gate.
- `ClawHub` remains a product name.

No remaining hit is being classified as an unpatched target display string in the changed display surfaces.

## Boundary Confirmation

```text
no operator.admin grant
no missing_scope bypass
no backend auth/scope modification
no runtime auth predicate modification
no runtime scope propagation modification
no build/tag/push
no deploy
no kubectl/k3d/Helm mutation
no create/stop/delete instance
no browser E2E
no manual E2E
no cleanup
no frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework modification
no old evidence modification
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
no sensitive values recorded
```
