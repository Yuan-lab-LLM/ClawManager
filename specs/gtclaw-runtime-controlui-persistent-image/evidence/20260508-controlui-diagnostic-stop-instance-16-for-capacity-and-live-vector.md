# CONTROLUI_DIAGNOSTIC_STOP_INSTANCE_16_FOR_CAPACITY_AND_LIVE_VECTOR

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Approval gate:

```text
APPROVE_CONTROLUI_DIAGNOSTIC_STOP_INSTANCE_16_FOR_CAPACITY_AND_LIVE_VECTOR_GATE
```

## Verdict

```text
CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_FAILED: capacity recovered through the approved stop of instance 16 and exactly one replacement diagnostic runtime instance reached Ready, but the runtime live vector still rejects the mediated control-ui connection because cond_shared_auth_proof=false and mediated_helper_result=false.
CONTROLUI_DIAGNOSTIC_STOP_INSTANCE_16_FOR_CAPACITY_AND_LIVE_VECTOR_FAILED
```

## Scope

Approved action was limited to stopping instance 16 / oc2gi-loc-221427 to release capacity, then creating exactly one replacement diagnostic runtime instance and running one browser/manual E2E.

```text
approved_stop_instance_id=16
approved_stop_instance_name=oc2gi-loc-221427
untouched_old_instances=17,18
previous_failed_instance_19=already_removed_before_this_gate
runtime_diagnostic_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
backend_diagnostic_image=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
CONTROLUI_PROXY_AUTH_DIAGNOSTICS=enabled
GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS=enabled_by_runtime_image
```

## Capacity Recovery

Instance 16 was stopped through the ClawManager API. No old evidence, asset, session, image, or unrelated instance was cleaned.

```text
instance_16_api_stop_http_code=200
instance_16_api_status_after=stopped
instance_16_pod_after=NotFound
instance_16_pvc_after=Bound retained
instance_17_status_after=running
instance_18_status_after=running
memory_requests_after_instance_16_stop=4236Mi (54%)
memory_requests_after_replacement_ready=6284Mi (80%)
```

Exactly one replacement diagnostic runtime instance was created after instance 16 was stopped.

```text
exactly_one_replacement_fresh_runtime_instance=true
replacement_instance_id=20
replacement_instance_name=oc2gi-diag-r-183249
replacement_pod=clawmanager-user-1/clawreef-20-oc2gi-diag-r-183249
replacement_status=running
replacement_pod_phase=Running
replacement_ready=true
replacement_restart_count=0
replacement_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
replacement_image_digest=sha256:e520d5ce5359cf2b532043a099ba8e637c6b8611c8e7fd81fd659ff5de755afe
endpoint_ready=true
endpoint_serving=true
endpoint_ports=3001,18789
```

Current allowed instances after the action:

```text
instance_16=stopped
instance_17=running
instance_18=running
instance_20=running
```

## Runtime Reachability

Direct runtime checks from inside the replacement pod:

```text
loopback_18789_http_code=200
podip_18789_http_code=400
serviceip_18789_http_code=400
```

The direct PodIP and ServiceIP checks did not return HTTP 200. The backend-mediated browser route still reached the runtime through the control-ui proxy and loaded the document/assets with HTTP 200.

Packaged verifier against the running replacement container:

```text
packaged_verifier=passed
verifier_target=/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
trustedProxyMarker_global_forbidden=preserved
connectParams.trustedProxy_global_forbidden=preserved
connectParams.auth.trustedProxy_global_forbidden=preserved
trustedProxyMarker_colon_global_forbidden=preserved
```

## Browser Manual E2E

Browser/manual E2E used a temporary Google Chrome headless profile through Chrome DevTools Protocol fallback. The Browser Use Node REPL surface was unavailable in this session, and Playwright was not installed. Credential material, route cookie values, header values, and full access URLs were not printed or recorded.

```text
browser_engine=Google Chrome headless
automation_surface=Chrome DevTools Protocol fallback
profile_is_temporary=true
login_status=200
access_status=200
access_mode=control-ui
access_target_port=18789
access_path_shape=/api/v1/instances/20/control-ui/
access_query_keys=none
opened_path_shape=/api/v1/instances/20/control-ui/chat
opened_query_keys=session
control_ui_document_http_code=200
control_ui_asset_http_codes=200
credential_material_recorded=false
```

Browser live vector:

```text
device_identity_required_visible=true
device_signature_invalid_visible=false
connected_state=false
chat_ready_state=false
post_connect_reached=false
mg_internal_reached=false
internal_localization_observed=false
```

## Backend Live Vector

Sanitized backend diagnostics for the browser attempt:

```text
ws_upstream_shape=observed
ws_upstream_shape.instance_id=20
ws_upstream_shape.target_port=18789
ws_upstream_shape.upstream_path=/
ws_upstream_shape.upstream_query_shape=known:none,unknown:0
ws_upstream_shape.source_query_had_token=false
ws_upstream_shape.source_query_had_password=false
ws_upstream_shape.forwarded_prefix_shape=backend_control_ui_prefix_match
ws_upstream_shape.browser_auth_header_seen=false
ws_upstream_shape.browser_cookie_seen=true
ws_upstream_shape.browser_x_openclaw_token_seen=false
ws_upstream_shape.upstream_auth_header_present=true
ws_upstream_shape.upstream_cookie_present=false
ws_upstream_shape.upstream_x_openclaw_token_present=false

ws_first_connect=observed
ws_first_connect.instance_id=20
ws_first_connect.message_type=text
ws_first_connect.json_valid=true
ws_first_connect.method=connect
ws_first_connect.has_params=true
ws_first_connect.browser_auth_present=false
ws_first_connect.browser_device_present=true
ws_first_connect.rewritten_auth_token_present=true
ws_first_connect.rewritten_device_present=false
ws_first_connect.preserved_known_param_keys=caps,client,locale,maxProtocol,minProtocol,role,scopes,userAgent
ws_first_connect.preserved_extra_param_count=0
ws_first_connect.bridge_result=rewritten_forwarded

ws_first_upstream_frame=observed
ws_first_upstream_frame.instance_id=20
ws_first_upstream_frame.message_type=text
first_upstream_error_code=none
```

## Runtime Live Vector

Sanitized runtime diagnostics for the same browser attempt:

```text
missing_device_decision=observed
is_control_ui=true
role=operator
auth_method=token
auth_ok=false
shared_auth_ok=false
has_shared_auth=true
forwarded_prefix_shape=backend_control_ui_prefix_match
cond_is_control_ui=true
cond_role_operator=true
cond_shared_auth_proof=false
cond_auth_method_shared_secret=true
cond_forwarded_prefix_match=true
mediated_helper_result=false
runtime_close_code=1008
```

## Interpretation

Capacity is no longer the blocker for this run. The browser route reached the replacement runtime through the backend-mediated control-ui path, and the backend bridge rewrote the first connect frame with an upstream auth token while removing the browser device field.

The remaining blocker is runtime-side auth proof evaluation:

```text
cond_is_control_ui=true
cond_role_operator=true
cond_auth_method_shared_secret=true
cond_forwarded_prefix_match=true
cond_shared_auth_proof=false
mediated_helper_result=false
```

This preserves the safety boundary:

```text
direct_browser_device_less_allow=false
no direct browser device-less allow
invalid_device_signature_protection=not_relaxed
no insecure auth
no global bypass
no broad origin workaround
no trustedProxy JSON marker trust
browser JSON trustedProxy marker not trusted
no secret/header/access URL logging
```

## Non-Actions

- no handling of old instances other than approved stop of instance 16
- no changes to instances 17 or 18
- no old asset cleanup
- no old session cleanup
- no old evidence cleanup
- no old image cleanup
- no build/tag/push/pull backend image
- no build/tag/push/pull runtime image
- no source code modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm write-back
- no AgentTeam write-back
- no UnifiedFramework write-back
- no database mutation
- no direct SQL
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no token value logging
- no password value logging
- no key value logging
- no cookie value logging
- no header value logging
- no access URL value logging
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Commands

```bash
kubectl get pod clawreef-16-oc2gi-loc-221427 -n clawmanager-user-1 -o name
kubectl get pods -n clawmanager-user-1 status-only
kubectl get pod clawreef-20-oc2gi-diag-r-183249 -n clawmanager-user-1 status-only
kubectl exec -n clawmanager-user-1 clawreef-20-oc2gi-diag-r-183249 -- curl status-only for loopback/PodIP/ServiceIP 18789
kubectl exec -i -n clawmanager-user-1 clawreef-20-oc2gi-diag-r-183249 -- node - /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js < specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs
browser/manual E2E once using temporary Chrome DevTools Protocol fallback
sanitized backend/runtime log scan
sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md
rg -n "CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_FAILED|CONTROLUI_DIAGNOSTIC_STOP_INSTANCE_16_FOR_CAPACITY_AND_LIVE_VECTOR_FAILED|instance_16|oc2gi-loc-221427|exactly_one_replacement_fresh_runtime_instance=true|missing_device_decision|cond_is_control_ui|cond_role_operator|cond_shared_auth_proof|cond_auth_method_shared_secret|cond_forwarded_prefix_match|mediated_helper_result|device_identity_required_visible|device_signature_invalid_visible|connected_state|chat_ready_state|post_connect_reached|mg_internal_reached|internal_localization_observed|no insecure auth|no global bypass|no direct browser device-less allow|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md
rg -n "(token|password|key|cookie|bearer|authorization|access URL)" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md || true
grep -n '[[:blank:]]$' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md || true
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-diagnostic-stop-instance-16-for-capacity-and-live-vector.md
```

## Verification Results

```text
packaged_verifier_result=passed
node_check_source_verifier=passed
node_check_assembly_verifier=passed
node_check_source_patch=passed
node_check_assembly_patch=passed
go_test_targeted_internal_services=passed
go_test_all_internal_services=passed
required_marker_scan=passed
sensitive_scan_result=only field names, enum values, boolean flags, and rule text; no secret values
trailing_whitespace_scan=passed
git_diff_check=passed
git_status_new_evidence=untracked
```
