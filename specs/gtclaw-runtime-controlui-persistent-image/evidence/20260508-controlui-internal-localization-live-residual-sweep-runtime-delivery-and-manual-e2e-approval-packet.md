# CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET

Date/timezone: 2026-05-09, Asia/Shanghai
Role/task: Worker, serial topology
Gate type: CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_GATE
Approval phrase for next gate: APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_DONE
```

This packet requests approval for the next gate only. This gate does not approve or perform build/tag/push, deploy, browser E2E, manual E2E, instance mutation, cleanup, Close, longterm write-back, or git stage/commit/push.

## Dependency State

Completed prerequisite gates:

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_DONE
CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_DONE
```

Manual E2E on instance 23 / `oc2gi-iloc-r-213106` already proved that the user can enter the internal UI. The remaining issue after that delivery was live internal localization residuals. The artifact-only residual sweep patch has now passed read-only Commander review and is ready for a separately approved runtime delivery plus user manual E2E cycle.

## Next Gate Approval Request

Approve only this next gate phrase:

```text
APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE
```

Approval authorizes the next Worker to deliver the already-reviewed residual sweep artifacts into a new runtime image and hand off one new replacement instance for user manual E2E.

## Requested Next-Gate Scope

Allowed after approval:

- Build and push a new runtime image that includes the live residual sweep patch.
- Run image/readback verification against the delivered image.
- Run running-container verification against the replacement runtime.
- If capacity handling is required, only the superseded instance 23 may be handled.
- Create exactly one fresh replacement instance for this delivery.
- Hand off the fresh replacement instance to the user for manual E2E acceptance of the internal UI localization.

Forbidden even after this approval unless separately approved:

- Do not touch instances 17 or 18.
- Do not clean old asset, session, evidence, image, pod, PVC, or other retained delivery material.
- Do not modify JS asset, Dockerfile, manifest, backend, or runtime auth/scope code in the delivery gate.
- Do not grant `operator.admin`.
- Do not add a missing_scope bypass.
- Do not use insecure auth, global bypass, or direct browser device-less allow.
- Do not perform Close, longterm write-back, passes:true, or git stage/commit/push.

## Required Delivery Verification

The next gate must record:

```text
image/readback verifier passed
running-container verifier passed
exactly one fresh replacement instance
scope patch retained: operator.read + operator.pairing only
no operator.admin grant
no missing_scope bypass
```

Required delivered asset hash values:

```text
index-M4TNVXB3.js = 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
nodes-BBk4VzkK.js = bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e
skills-BRWdbtpV.js = 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
skills-shared-D6eRDyeb.js = f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
config-form-x_UhxUYO.js remains 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
zh-CN-B26mMdbY.js remains cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

The delivery evidence must prove the hashes from both image/readback and running-container readback, not only from the local artifact tree.

## Required Manual E2E Acceptance

User manual E2E should check:

```text
device_signature_invalid=false
device_identity_required=false
connected/chat ready reached
mg/internal UI reached
Appearance residuals materially cleared
Dreams residuals materially cleared
Nodes residuals materially cleared
Skills residuals materially cleared
ClawHub may remain as a product name
dynamic skill/plugin descriptions may be recorded as dynamic metadata residuals
dynamic metadata residuals are not this patch blocker unless proven to come from bundled static asset
```

Manual E2E acceptance must inspect the internal UI, not only the outer connection form.

## Delivery Boundaries

The next gate should preserve the existing trusted mediated Control UI contract:

- `operator.read` and `operator.pairing` are the only mediated runtime scopes expected.
- `operator.admin` must remain absent from the mediated scope normalizer.
- `missing_scope` handling must not be bypassed.
- The existing runtime auth predicate and scope propagation code must not be changed.

If user manual E2E still sees English residuals after delivery, the next report must classify them as one of:

```text
delivery did not include expected asset hash
static bundled display text still present
dynamic plugin/skill metadata
product name
protocol or code literal
new page outside this residual sweep
```

## This Gate Non-Actions

```text
packet only
no JS asset modification
no Dockerfile modification
no manifest modification
no backend modification
no runtime auth/scope modification
no build/tag/push
no pull
no deploy
no kubectl/k3d/Helm mutation
no instance/database mutation
no browser E2E
no manual E2E
no DevTools
no Playwright
no cleanup
no operator.admin grant
no missing_scope bypass
no insecure auth
no global bypass
no direct browser device-less allow
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
no sensitive values recorded
```
