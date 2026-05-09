# CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology

## Verdict

```text
CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE
```

## Approval Requested

```text
APPROVE_CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH_GATE
```

## Current Fixed Scope

The previous delivery/live-vector gate proved that the earlier auth blockers are no longer the active failure:

```text
device_signature_invalid_visible=false
device_identity_required_visible=false
auth_ok=true
shared_auth_ok=true
cond_shared_auth_proof=true
mediated_helper_result=true
runtime_connection_result=webchat_connected
```

Current runtime image and instance:

```text
instance_id=21
instance_name=oc2gi-sap-r-192528
runtime_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-shared-auth-proof-runtime-20260508192528
runtime_image_digest=sha256:bb60ea447208f941a2adc20bfe4b4014b4b0e12b072b8e5e4207d819a9e42a6d
```

Safety signals still held:

```text
direct_browser_device_less_control_ui_http_code=400
invalid_device_signature_protection_present=true
trustedProxyMarker_global_forbidden=true
connectParams_trustedProxy_global_forbidden=true
```

## Current New Blocker

The only current blocker is after the mediated Control UI connection succeeds:

```text
chat_ready_state=false
post_connect_reached=false
mg_internal_reached=false
internal_localization_observed=false
runtime_post_connect_error_shape=missing_scope
runtime_missing_scope_names=operator.read,operator.pairing
```

Observed post-connect runtime methods/actions that failed in sanitized logs:

```text
sessions.subscribe=missing_scope:operator.read
agent.identity.get=missing_scope:operator.read
agents.list=missing_scope:operator.read
node.list=missing_scope:operator.read
device.pair.list=missing_scope:operator.pairing
chat.history=missing_scope:operator.read
sessions.list=missing_scope:operator.read
models.list=missing_scope:operator.read
commands.list=missing_scope:operator.read
```

The browser did not reach post-connect / mg internal UI, so no internal localization acceptance is claimed.

## Read-Only Clues For Next Gate

Existing backend first-connect rewrite behavior:

```text
backend_first_connect_rewrite_removes_browser_auth=true
backend_first_connect_rewrite_removes_browser_device=true
backend_first_connect_rewrite_adds_server_owned_auth=true
backend_first_connect_rewrite_preserves_known_param_scopes=true
backend_first_connect_diagnostic_preserved_known_param_keys=caps,client,locale,maxProtocol,minProtocol,role,scopes,userAgent
```

Existing backend tests use a client scopes fixture containing:

```text
backend_test_scope_fixture=operator.read,operator.write
backend_test_scope_fixture_missing_operator_pairing=true
```

This is not yet root cause. It only narrows the next gate's diagnostic focus.

## Requested Next Gate

The next gate must be diagnostic-first, then minimal patch only after root cause is proven.

Required diagnostics:

```text
diagnose_browser_first_connect_params_scopes=true
diagnose_backend_first_connect_rewrite_preserves_drops_or_normalizes_scopes=true
diagnose_runtime_session_scope_propagation=true
diagnose_runtime_post_connect_method_scope_requirements=true
diagnose_operator_pairing_grant_source=true
diagnose_scope_normalization_read_without_pairing_path=true
```

Specific questions for the next gate:

- What scope names are actually present in browser first connect params.scopes?
- Does the backend first-connect rewrite preserve, drop, or fail to normalize those scope names?
- Does runtime session/auth state propagate operator scopes after shared auth succeeds?
- Which runtime methods/actions require operator.read and operator.pairing?
- Should operator.pairing be granted by backend-mediated Control UI server-side policy, or must runtime/client request it explicitly?
- Is there a path that normalizes only operator.read while omitting operator.pairing?

## Recommended Patch Boundaries

No patch is authorized by this approval packet. The next gate may patch only after the diagnostics above prove the root cause.

Recommended narrow patch policy:

- If first-connect scopes are missing and backend-mediated Control UI requires operator pairing, prefer backend first-connect rewrite normalization under all of these conditions: strict control-ui route, server-owned upstream auth, role=operator, and backend control-ui prefix match.
- If scopes already exist in first connect but runtime session lacks them, patch runtime scope propagation.
- If backend rewrite drops scopes, patch backend rewrite to preserve the safe scope set.
- If runtime post-connect scope requirement is wrong, patch only with source evidence proving the requirement is too broad.
- If normalization is needed, grant only the minimum required operator scope names for backend-mediated Control UI.

Forbidden patch shapes:

- no bypass
- no missing_scope bypass
- no operator.admin
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no prefix-as-credential

## Proposed Next Gate Allowed Actions

- Read backend first-connect rewrite code and tests.
- Read runtime source/readback scope parser and method/action scope requirements.
- Run secret-safe diagnostics that record only boolean, enum, path shape, query key names, status/code, method/action name, and scope-name enum.
- Patch only the proven minimum backend or runtime scope path.
- Run focused backend/runtime verifier/tests relevant to the patched path.
- Write new evidence for root cause, patch, and verification.

## Current Gate Non-Actions

- no backend/runtime/frontend/control-ui code modification
- no build/tag/push image
- no pull image
- no deploy
- no rollout
- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no instance create/stop/delete
- no browser E2E
- no DevTools
- no Playwright
- no cleanup of old instance, pod, PVC, asset, session, evidence, or image
- no database mutation
- no direct SQL
- no insecure auth
- no global bypass
- no missing_scope bypass
- no operator.admin
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no secret/header/access URL logging
- no passes:true
- no Close
- no longterm write-back
- no Mem0 write
- no git stage/commit/push

## Verification Plan For This Packet

```text
sed_packet=required
required_marker_scan=required
sensitive_scan=allowed_rule_text_only
trailing_whitespace_scan=required
git_diff_check=required
git_status_target=required
```
