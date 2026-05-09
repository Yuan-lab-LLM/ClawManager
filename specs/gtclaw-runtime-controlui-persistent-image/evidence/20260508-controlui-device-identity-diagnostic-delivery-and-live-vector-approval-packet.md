# Control UI Device Identity Diagnostic Delivery And Live Vector Approval Packet

packet_verdict: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_DELIVERY_AND_LIVE_VECTOR_APPROVAL_PACKET_DONE
date: 2026-05-08
request_gate: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_DELIVERY_AND_LIVE_VECTOR_GATE
approval_phrase: APPROVE_CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_DELIVERY_AND_LIVE_VECTOR_GATE

## Dependency State

- diagnostic_patch_application_done: true
- prior_browser_e2e_failed: true
- commander_read_only_review_passed: true
- current_goal: deliver diagnostic backend and runtime, then collect sanitized condition vector
- current_gate_actions: approval packet only

## Proposed Next Gate Scope

1. Build and push a new backend image that includes the diagnostic backend patch.
2. Deploy the backend diagnostic image with `CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1`.
3. Build and push a new runtime image that includes the runtime diagnostic patch.
4. Confirm a safe existing flow for enabling `GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS=1` in the runtime instance.
5. If the runtime diagnostic setting cannot be enabled safely through existing image or instance flow, the next gate must return BLOCKED and must not make ad hoc deployment edits.
6. Create exactly one fresh runtime instance using the diagnostic runtime image.
7. Execute one browser/manual E2E attempt to path shape `/api/v1/instances/<new-id>/control-ui/chat?session=main`.
8. Collect only the sanitized condition vector from backend, runtime, and browser evidence.
9. Use the live vector to identify the condition that still makes the device identity blocker visible.

## Proposed Next Gate Evidence Sources

- backend_events: ws_upstream_shape, ws_first_connect, ws_first_upstream_frame
- runtime_event: missing_device_decision
- browser_fields: visible blocker booleans, connected_state, chat_ready_state
- image_fields: status, digest, tag shape
- instance_fields: status, id enum or number, pod readiness enum, restart count number

## Required Live Fields

- cond_is_control_ui
- cond_role_operator
- cond_shared_auth_proof
- cond_auth_method_shared_secret
- cond_forwarded_prefix_match
- mediated_helper_result
- auth_method enum
- shared_auth_ok bool
- forwarded_prefix_shape enum
- first_upstream_error_code
- device_identity_required_visible
- connected_state
- chat_ready_state

## Explicit Non Scope For Next Gate

- no claim that the blocker is fixed unless the live vector proves it
- no internal localization acceptance
- no claim that post-connect management UI localization passed
- no cleanup of existing instances, old images, old artifacts, browser state, cache, or storage
- no direct database writes
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no passes:true
- no Close
- no git stage/commit/push
- no Mem0 write

## Sanitized Evidence Rules

- record only boolean, enum, path shape, status/code, digest, and sanitized log line
- credential-bearing values, auth headers, browser storage, network headers, and access links must not be recorded as plaintext
- do not dump browser storage
- do not dump network headers
- do not record raw browser location if it contains credential material
- diagnostic lines must stay shape-only and must not include secret values

## Current Gate Non Actions

- no build/tag/push/pull image
- no backend/runtime/frontend deploy
- no kubectl/k3d/Helm mutation
- no browser/manual E2E, DevTools, or Playwright
- no instance create/delete/update
- no database mutation
- no cleanup
- no old evidence or artifact edits
- no git index mutation
