# Control UI internal localization runtime delivery and manual E2E approval packet

Date/timezone: 2026-05-08, Asia/Shanghai

Gate:
- CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_GATE

Approval phrase:
- APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

Verdict:
- CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_DONE

## Dependency state

- CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_DONE is complete.
- CONTROLUI_INTERNAL_LOCALIZATION_GAPS_MANIFEST_CONSISTENCY_FIX_DONE is complete.
- CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_RUNTIME_DELIVERY_DONE is complete.
- User manual E2E on instance 22 / oc2gi-scope-r-204419 reached the GTClaw/OpenClaw internal UI.
- For instance 22, the user observed device_signature_invalid=false, device_identity_required=false, and missing_scope_absent=true.
- The current remaining blocker is internal UI localization quality, not entrance connection form text.
- The internal localization patch has been applied to artifacts, but no new runtime image has been built or pushed from that patch in this gate.

## Approved next gate scope

The next gate is approved to perform runtime delivery and then hand off user manual E2E. It must:

1. Build/push one new runtime image containing the latest internal localization patch.
2. Run image/readback verifier checks proving these artifact hashes:
   - index-M4TNVXB3.js hash = ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b
   - config-form-x_UhxUYO.js hash = 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
   - zh-CN-B26mMdbY.js hash = cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
3. Prove the scope patch still exists in the delivered runtime:
   - operator.read present
   - operator.pairing present
   - no operator.admin grant
   - no missing_scope bypass
   - invalid device-signature protection retained
4. Use the capacity plan below.
5. Create exactly one fresh replacement runtime instance using the new image.
6. Verify replacement pod/service readiness and required runtime HTTP checks in that next gate.
7. Provide the manual E2E URL for the user in this shape:
   - `https://localhost:30443/api/v1/instances/<new_id>/control-ui/chat?session=main`

## Capacity plan for next gate

- If capacity must be released, only the superseded instance 22 / oc2gi-scope-r-204419 may be handled.
- do not touch 17/18.
- Do not clean up old PVC, asset, session, evidence, or image material.
- exactly one fresh replacement runtime instance is allowed.
- If more than one fresh replacement would be required, the next gate must stop and report a blocked verdict.

## Manual E2E handoff

The Worker in the next gate must deliver the fresh instance and manual E2E URL only. The Worker must not perform final browser/manual E2E acceptance for the user.

The user manual E2E acceptance checklist is:
- device_signature_invalid=false
- device_identity_required=false
- missing_scope_absent=true
- webchat_connected=true
- chat_ready_state=true
- mg_internal_reached=true
- Observed English residuals absent or materially reduced inside internal UI:
  - Message Assistant (Enter to send)
  - Default (Auto)
  - Default (off)
  - Form
  - Raw
  - No changes
  - Raw mode disabled...
  - Open
  - Save
  - Apply
  - Update
  - Search settings...
  - Settings
  - Communication
  - Schema unavailable.
- not entrance connection form: the entrance connection form is not final acceptance.

## Current gate non-actions

- no build/tag/push performed.
- no deploy performed.
- no browser E2E performed.
- no instance mutation performed.
- no JS asset, Dockerfile, or manifest mutation performed.
- no backend auth/scope mutation performed.
- no runtime auth predicate or scope propagation mutation performed.
- no passes:true recorded.
- no Close performed.
- no git stage/commit/push performed.
