# Control UI Device Identity Diagnostic Verifier Blocker Unblock And Live Vector

verdict: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_VERIFIER_BLOCKER_UNBLOCK_AND_LIVE_VECTOR_FAILED: fresh diagnostic runtime pod unschedulable before browser/live vector
date: 2026-05-08
task_type: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_VERIFIER_BLOCKER_UNBLOCK_AND_LIVE_VECTOR_GATE

## Scope And Verdict

- dependency_gate: APPROVE_CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_DELIVERY_AND_LIVE_VECTOR_GATE
- dependency_gate: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_PATCH_APPLICATION_DONE
- dependency_gate: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_DELIVERY_AND_LIVE_VECTOR_BLOCKED
- verifier_blocker_unblocked: true
- runtime_diagnostic_image_delivered: true
- backend_diagnostic_rollout_delivered: true
- exactly_one_fresh_runtime_instance_created: true
- browser_manual_e2e_run: false
- sanitized_live_vector_collected: partial_only
- failure_reason: fresh runtime pod remained Pending with PodScheduled=False / Unschedulable / Insufficient memory

## Root Cause

- failed_marker: JSON.stringify(params)
- patched_runtime_target_context: function toPendingParamsJSON(params) in the OpenClaw runtime bundle returns JSON.stringify(params) for pending node action params.
- marker_source_classification: OpenClaw existing unrelated code, not the GTClaw diagnostic helper.
- GTClaw_helper_context: function logGtManagerControlUiAuthDiagnostic(params, decision) emits only boolean, enum, and path-shape fields.
- required_action_taken: verifier check for JSON.stringify(params), console.error(params), and console.log(params) was scoped to the GTClaw helper block only.
- global_forbidden_retained:
  - trustedProxyMarker
  - connectParams.trustedProxy
  - connectParams.auth.trustedProxy
  - trustedProxyMarker:

## Patch Summary

- modified_source_verifier: specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs
- modified_assembly_verifier: specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs
- helper_modified: false
- patch_scripts_modified: false
- verifier_scoping: global trustedProxy marker checks remain global; full params logging checks scan only the GTClaw helper block.
- scoped_negative_probe: injected JSON.stringify(params) into a temp patched GTClaw helper and both verifiers rejected it.

## Verifier And Build Evidence

| Check | Result |
| --- | --- |
| source verifier before patch against readback runtime | exit=1, verifier_forbidden_marker_present JSON.stringify(params) |
| assembly verifier before patch against readback runtime | exit=1, verifier_forbidden_marker_present JSON.stringify(params) |
| source verifier after patch against readback runtime | exit=0 |
| assembly verifier after patch against readback runtime | exit=0 |
| packaged verifier in runtime image build | exit=0 |
| source verifier against diagnostic image readback server.impl-BbJvXoPb.js | exit=0 |
| assembly verifier against diagnostic image readback server.impl-BbJvXoPb.js | exit=0 |
| packaged verifier against diagnostic image readback server.impl-BbJvXoPb.js | exit=0 |

Runtime diagnostic image:

- host_tag: localhost:5001/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- cluster_tag: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- image_index_digest: sha256:e520d5ce5359cf2b532043a099ba8e637c6b8611c8e7fd81fd659ff5de755afe
- linux_arm64_manifest_digest: sha256:0942d003c9d4339431823829c97412da243910386d8c6bb50050c41d0f888c06
- GTCLAW_CONTROLUI_AUTH_DIAGNOSTICS: enabled_by_image_env
- build_warning: FromPlatformFlagConstDisallowed
- build_warning: SecretsUsedInArgOrEnv for diagnostic env name only; no secret value was used

## Backend Diagnostic Rollout

- backend_image: k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
- backend_image_index_digest: sha256:63084136990d16d1536006260bfe09a910231aef69d5102948d186bc80a3369b
- CONTROLUI_PROXY_AUTH_DIAGNOSTICS: enabled
- rollout_status: successfully_rolled_out
- healthz_http: 200
- backend_pod_ready: true
- backend_restart_count: 0

## Fresh Runtime Instance

- runtime_pods_before_create_count: 3
- runtime_pods_after_create_count: 4
- new_instance_count: 1
- new_instance_id: 19
- new_instance_name: oc2gi-diag-175648
- new_instance_status: creating
- new_instance_type: openclaw
- new_instance_memory_gb: 2
- new_instance_image_registry: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- new_instance_pod: clawmanager-user-1/clawreef-19-oc2gi-diag-175648
- create_response_status: not_recorded_due_sanitized_reporting_shell_glob_error
- second_create_attempt_same_name_created_second_instance: false
- no_second_instance: true

Pod readiness:

- kubectl_wait_ready_exit: 1
- pod_phase: Pending
- pod_ready: false
- pod_scheduled: false
- scheduler_reason: Unschedulable
- scheduler_message_shape: Insufficient memory
- endpointslice_ready: empty
- running_container_verifier_exit: not_run_pod_pending
- runtime_log_readback: not_run_pod_pending

## Sanitized Live Condition Vector

Backend vector:

- ws_upstream_shape: not_collected_no_browser_attempt
- ws_first_connect: not_collected_no_browser_attempt
- ws_first_upstream_frame: not_collected_no_browser_attempt
- first_upstream_error_code: not_collected_no_browser_attempt

Runtime vector:

- missing_device_decision: not_collected_pod_pending
- is_control_ui: not_collected_pod_pending
- role: not_collected_pod_pending
- auth_method: not_collected_pod_pending
- auth_ok: not_collected_pod_pending
- shared_auth_ok: not_collected_pod_pending
- has_shared_auth: not_collected_pod_pending
- forwarded_prefix_shape: not_collected_pod_pending
- cond_is_control_ui: not_collected_pod_pending
- cond_role_operator: not_collected_pod_pending
- cond_shared_auth_proof: not_collected_pod_pending
- cond_auth_method_shared_secret: not_collected_pod_pending
- cond_forwarded_prefix_match: not_collected_pod_pending
- mediated_helper_result: not_collected_pod_pending

Browser vector:

- browser_manual_e2e_run: false
- device_identity_required_visible: not_collected_pod_pending
- device_signature_invalid_visible: not_collected_pod_pending
- connected_state: not_collected_pod_pending
- chat_ready_state: not_collected_pod_pending
- post_connect_reached: not_collected_pod_pending
- mg_internal_reached: not_collected_pod_pending

## Security Boundaries Confirmed

- direct browser/device-less still rejected: true by verifier model cases
- invalid device-signature protection retained: true by required verifier strings
- browser JSON trustedProxy marker trusted as input: false
- insecure auth added: false
- global bypass added: false
- broad origin workaround added: false
- no direct browser device-less allow
- no passes:true
- no Close
- no longterm write-back
- no Mem0 write
- no git stage/commit/push
- no deployments/docs/longterm/AgentTeam/UnifiedFramework/frontend write
- no old session cleanup
- no old asset cleanup
- no old instance cleanup
- no direct SQL/database mutation

## Verification Commands

Credential-bearing command values and header values were omitted from this evidence.

```text
node --check specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs
node --check specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs
node --check specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
node --check specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs
node <source verifier> <diagnostic image readback server.impl-BbJvXoPb.js> <source patch script>
node <assembly verifier> <diagnostic image readback server.impl-BbJvXoPb.js> <assembly patch script>
node <packaged verifier> <diagnostic image readback server.impl-BbJvXoPb.js> <assembly patch script>
docker buildx build --platform linux/arm64 -t <runtime diagnostic host tag> --push <temporary diagnostic build context>
docker buildx imagetools inspect <runtime diagnostic host tag>
kubectl set image deployment/clawmanager-app -n clawmanager-system clawmanager-app=<backend diagnostic cluster tag>
kubectl set env deployment/clawmanager-app -n clawmanager-system CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=180s
curl healthz status-only
POST /api/v1/instances with sanitized payload shape
kubectl wait --for=condition=Ready pod/clawreef-19-oc2gi-diag-175648 -n clawmanager-user-1 --timeout=90s
kubectl get pod clawreef-19-oc2gi-diag-175648 status-only
go test ./internal/services -run Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth|TestProxyWebSocketWithControlUIScopeDiagnosticsAreSanitized -count=1
go test ./internal/services -count=1
```
