# CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology

Approval:

```text
APPROVE_CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_GATE
```

## Verdict

```text
CONTROLUI_SHARED_AUTH_PROOF_RUNTIME_DELIVERY_AND_LIVE_VECTOR_FAILED: patched runtime delivery succeeded and the mediated control-ui shared auth proof reached webchat connected, but post-connect runtime calls failed with missing operator scopes, so chat_ready_state and internal localization were not reached.
```

## Delivered Runtime Image

```text
host_image=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-shared-auth-proof-runtime-20260508192528
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-shared-auth-proof-runtime-20260508192528
image_index_digest=sha256:bb60ea447208f941a2adc20bfe4b4014b4b0e12b072b8e5e4207d819a9e42a6d
build_context=runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
packaged_verifier_result=passed
readback_verifier_result=passed
startup_config_sync_result=passed
zh_CN_hash_preserved=true
zh_CN_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

Runtime startup config proof from the fresh patched pod:

```text
runtime_config_auth_mode=token
runtime_env_token_present=true
runtime_config_token_present=true
runtime_config_env_token_equal=true
credential_values_recorded=false
```

## Capacity And Fresh Instance

Approved capacity action handled only the failed old diagnostic instance 20. Instances 17 and 18 were not touched.

```text
instance20_before_status=running
instance20_stop_http_code=200
instance20_stop_success=true
instance20_pod_after=NotFound
instance20_pvc_after=Bound retained
instance17_after=running untouched
instance18_after=running untouched
```

Exactly one fresh patched diagnostic runtime instance was created after instance 20 was stopped.

```text
exactly_one_fresh_patched_diagnostic_runtime_instance=true
created_instance_id=21
created_instance_name=oc2gi-sap-r-192528
created_status=running
created_pod=clawmanager-user-1/clawreef-21-oc2gi-sap-r-192528
created_pod_phase=Running
created_pod_ready=true
created_restart_count=0
created_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-shared-auth-proof-runtime-20260508192528
created_image_digest=sha256:bb60ea447208f941a2adc20bfe4b4014b4b0e12b072b8e5e4207d819a9e42a6d
endpoint_ready=true
endpoint_serving=true
endpoint_ports=3001,18789
node_memory_requests_after_ready=6284Mi
```

Current runtime instances after the action:

```text
instance17=running
instance18=running
instance20=stopped
instance21=running
```

## Runtime Reachability

```text
loopback_18789_http_code=200
podip_18789_http_code=200
serviceip_18789_http_code=200
direct_browser_device_less_control_ui_http_code=400
invalid_device_signature_protection_present=true
trustedProxyMarker_global_forbidden=true
connectParams_trustedProxy_global_forbidden=true
```

## Browser/Manual E2E

Browser Use Node REPL was unavailable through tool discovery, so this gate used the existing Chrome DevTools Protocol fallback pattern. One setup script failed before issuing an access request or browser navigation because Node started in ESM mode; that was not counted as the browser/manual E2E attempt. The following executed attempt is the single browser/manual E2E run for this gate.

```text
browser_engine=Google Chrome headless
automation_surface=Chrome DevTools Protocol fallback
login_status=200
access_status=200
access_mode=control-ui
access_target_port=18789
opened_path_shape=/api/v1/instances/21/control-ui/chat
opened_query_keys=session
url_has_token=false
control_ui_document_http_code=200
control_ui_asset_http_codes=200
websocket_handshake_status_code=not_captured_by_browser_cdp
page_title=GTClaw 控制台
document_ready_state=complete
visible_text_category=unknown
credential_material_recorded=false
```

Browser live vector:

```text
device_identity_required_visible=false
device_signature_invalid_visible=false
connected_state=true
chat_ready_state=false
post_connect_reached=false
mg_internal_reached=false
internal_localization_observed=false
chat_ready_blocker=runtime_missing_operator_scopes
```

The browser CDP network listener did not capture the WebSocket handshake status, but backend and runtime sanitized logs for the same browser attempt did capture the mediated WebSocket path and runtime connection outcome.

## Backend Live Vector

Sanitized backend diagnostics for instance 21:

```text
ws_upstream_shape=observed
ws_upstream_shape.instance_id=21
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
ws_first_connect.instance_id=21
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
ws_first_upstream_frame.instance_id=21
ws_first_upstream_frame.message_type=text
first_upstream_error_code=none
```

## Runtime Live Vector

Runtime diagnostic helper code is present, but the runtime diagnostic environment flag was not present in the fresh patched instance, so missing-device helper logs were not emitted. The runtime connection result and post-connect errors were still visible in sanitized runtime logs.

Observed runtime outcome:

```text
runtime_diagnostic_helper_present=true
GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS_present=false
runtime_connection_result=webchat_connected
runtime_post_connect_error_code=INVALID_REQUEST
runtime_post_connect_error_shape=missing_scope
runtime_missing_scope_names=operator.read,operator.pairing
runtime_disconnect_code=1006
```

Shared auth proof condition vector:

```text
missing_device_decision=not_emitted_after_auth_success
is_control_ui=true
role=operator
auth_method=token
auth_ok=true
shared_auth_ok=true
has_shared_auth=true
forwarded_prefix_shape=backend_control_ui_prefix_match
cond_is_control_ui=true
cond_role_operator=true
cond_shared_auth_proof=true
cond_auth_method_shared_secret=true
cond_forwarded_prefix_match=true
mediated_helper_result=true
```

Inference basis:
- backend rewrote the first connect frame with server-owned upstream shared auth.
- runtime startup config now synchronizes gateway.auth.mode/token from OPENCLAW_GATEWAY_TOKEN.
- runtime reached webchat connected instead of device identity required or device signature invalid.
- post-connect failures are missing scope errors, not shared auth proof errors.

## Interpretation

The original shared auth proof blocker is resolved in the delivered patched runtime:

```text
previous_shared_auth_ok=false
current_shared_auth_ok=true
previous_cond_shared_auth_proof=false
current_cond_shared_auth_proof=true
previous_mediated_helper_result=false
current_mediated_helper_result=true
```

The remaining blocker is after authentication:

```text
post_connect_scope_blocker=missing operator.read and operator.pairing scopes
chat_ready_state=false
post_connect_reached=false
mg_internal_reached=false
internal_localization_observed=false
```

This gate does not claim internal localization, because the post-connect / mg internal UI was not actually reached.

## Non-Actions

- no backend rebuild
- no backend rollout
- no frontend modification
- no deployments modification
- no docs modification
- no longterm write-back
- no AgentTeam write-back
- no UnifiedFramework write-back
- no old evidence cleanup
- no old asset cleanup
- no old session cleanup
- no old image cleanup
- no handling of instance 17
- no handling of instance 18
- no handling of any old instance except approved stop of instance 20
- no database mutation
- no direct SQL mutation
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no prefix-as-credential
- no secret/header/access URL logging
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Results

```text
docker_build_result=passed
docker_push_result=passed
packaged_verifier_result=passed
readback_verifier_result=passed
node_check_source_patch=passed
node_check_source_verifier=passed
node_check_assembly_patch=passed
node_check_assembly_verifier=passed
go_test_targeted_internal_services=passed
go_test_all_internal_services=passed
git_diff_check_artifacts_before_evidence=passed
```
