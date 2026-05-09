# CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_APPROVAL_PACKET

Verdict: CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_APPROVAL_PACKET_DONE

Gate type:
CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_APPROVAL_PACKET_GATE

Dependency:
- CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_DONE

Approval requested:
APPROVE_CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_GATE

## Current State

Running state accepted for this approval packet:
- instance 16 stopped
- instance 17 running
- instance 18 running
- instance 20 running on the old diagnostic runtime image
- instance 20 live vector still fails at shared_auth_ok=false

This packet is read-only plus evidence write only:
- no build/tag/push/pull image
- no rollout/patch/scale K8S
- no instance create/stop/delete
- no browser/manual E2E, DevTools, or Playwright
- no cleanup of old evidence/assets/sessions/images
- no handling of instance 17, 18, or 20 lifecycle in this gate
- no code modification in this gate
- no database mutation or direct SQL
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Root Cause Summary

Secret-safe root cause facts from the completed patch gate:
- backend_runtime_token_equal=true
- runtime_token_source=OPENCLAW_GATEWAY_TOKEN
- config_env_token_equal=false
- stale config token caused sharedAuthOk=false

Interpretation:
- the backend-owned instance credential and runtime env credential are aligned
- the runtime persisted config retained a different gateway.auth credential branch
- the runtime config-first resolver selected the stale config credential
- first connect carried a shared proof shape, but the runtime compared it against the stale config credential

## Patch Summary

Patched artifact:
- runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-ensure-controlui-origin

Patch behavior:
- startup materialization syncs gateway.auth.mode/token from OPENCLAW_GATEWAY_TOKEN
- the sync uses the server-owned runtime credential source
- the patch does not relax the auth predicate
- the patch does not add prefix-as-credential behavior
- the patch does not trust a browser JSON trustedProxy marker

Safety expectations retained for the next gate:
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- invalid device-signature protection remains required
- direct browser device-less access remains rejected
- no secret/header/access URL logging

## Requested Delivery Scope

The next gate requests permission to:
- build/push one new runtime image from the patched assembly artifact
- run packaged verifier against the built runtime image
- run readback verifier against the delivered runtime server implementation
- preserve zh-CN hash from the current localized runtime assembly
- record only image tag/digest, verifier status, and sanitized status fields

The next gate must not:
- rebuild backend unless separately approved
- weaken runtime auth contracts
- treat X-Forwarded-Prefix as a credential
- trust a browser JSON marker
- record credential values, header values, or access URL values

## Requested Capacity And Live Vector Scope

Because instance 17, instance 18, and instance 20 are running and instance 16 is stopped, the next gate should prefer handling only instance 20 for capacity if a fresh patched diagnostic instance is required.

Capacity request:
- do not touch instance 17
- do not touch instance 18
- do not cleanup old evidence/assets/sessions/images
- handle only instance 20 if capacity must be recovered for the patched diagnostic rerun
- after the approved capacity action, create exactly one fresh patched diagnostic runtime instance
- if capacity still cannot be recovered without touching 17/18 or unrelated assets, return BLOCKED with status-only evidence

Fresh runtime request:
- create exactly one fresh patched diagnostic runtime instance
- use the newly built patched runtime diagnostic image
- preserve backend diagnostic env CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
- record only boolean, enum, path shape, query key names, status/code, digest, and sanitized condition names

## Requested Browser/Manual E2E Scope

The next gate requests permission for:
- one browser/manual E2E
- sanitized backend/runtime/browser live vector collection
- backend vector: ws_upstream_shape, ws_first_connect, ws_first_upstream_frame, first_upstream_error_code
- runtime vector: missing_device_decision, is_control_ui, role, auth_method, auth_ok, shared_auth_ok, has_shared_auth, forwarded_prefix_shape
- runtime condition vector: cond_is_control_ui, cond_role_operator, cond_shared_auth_proof, cond_auth_method_shared_secret, cond_forwarded_prefix_match, mediated_helper_result
- browser vector: device_identity_required_visible, device_signature_invalid_visible, connected_state, chat_ready_state, post_connect_reached, mg_internal_reached, internal_localization_observed

Post-connect rule:
- if connected_state=true and chat_ready_state=true, proceed to observe post-connect / mg internal localization
- do not claim internal localization unless post-connect / mg internal UI is actually reached
- if connection succeeds but post-connect or mg internal UI is not reached, record the sanitized condition vector and return FAILED rather than claiming localization

## Next Gate Guardrails

The next gate must keep:
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no prefix-as-credential
- no secret/header/access URL logging
- no old asset cleanup
- no old session cleanup
- no old evidence cleanup
- no old image cleanup
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
