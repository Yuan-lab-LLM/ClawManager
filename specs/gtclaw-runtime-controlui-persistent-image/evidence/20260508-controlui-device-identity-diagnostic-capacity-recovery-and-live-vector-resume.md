# Control UI Device Identity Diagnostic Capacity Recovery And Live Vector Resume

verdict: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_BLOCKED: insufficient memory after approved instance 19 recovery
date: 2026-05-08
task_type: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_GATE

## Approval And Scope

- approval_phrase_used: APPROVE_CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_GATE
- dependency_gate: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_APPROVAL_PACKET_DONE
- approved_capacity_action: process only failed diagnostic instance 19 / oc2gi-diag-175648 and its Pending pod/PVC
- backend diagnostic image retained: k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
- backend diagnostic env retained: CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
- backend healthz after recovery: 200
- runtime diagnostic image retained: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- replacement_create_allowed_only_after_instance_19_recovery: true
- replacement_create_attempted: false
- replacement_create_blocked_reason: insufficient memory after approved instance 19 recovery
- exactly_one_replacement_fresh_runtime_instance: false
- success_path_marker_not_claimed: exactly_one_replacement_fresh_runtime_instance=true

## Instance 19 Recovery

- instance 19 name: oc2gi-diag-175648
- pre_recovery_instance_19_status: creating
- pre_recovery_pod: clawmanager-user-1/clawreef-19-oc2gi-diag-175648
- pre_recovery_pod_phase: Pending
- pre_recovery_scheduler_reason: Unschedulable
- pre_recovery_scheduler_message_shape: Insufficient memory
- pre_recovery_pvc: clawmanager-user-1/clawreef-19-pvc
- recovery_method: ClawManager API delete for instance 19 only
- delete_http_code: 200
- delete_message_shape: Instance deleted successfully
- api_instance_ids_before_delete: 16,17,18,19
- api_instance_ids_after_delete: 16,17,18
- post_recovery_instance_19_present_in_api: false
- post_recovery_pod_status_only_check: NotFound
- post_recovery_pvc_status_only_check: NotFound
- old_instance_ids_after_recovery: 16,17,18
- old_instance_names_after_recovery: oc2gi-loc-221427, oc2gi-tp-134542, oc2gi-sa-151137
- other_old_instance_processed: false
- old_pod_cleanup_outside_instance_19: false
- old_pvc_cleanup_outside_instance_19: false

## Capacity Check After Instance 19 Recovery

- node_count: 1
- node_ready: true
- running_openclaw_pods_after_recovery: 3
- running_openclaw_memory_requests: 2Gi each
- node_allocatable_memory_shape: 8024876Ki
- node_allocated_memory_requests_shape: 6284Mi
- replacement_required_memory_request_shape: 2Gi
- scheduler_capacity_verdict: insufficient_for_replacement_2Gi_request
- blocked_rule_applied: do not stop other old instances, do not expand K3D/Docker, do not lower memory request
- replacement_runtime_instance_created: false
- replacement_pod_ready_check: not_run_no_replacement_created
- http_18789_loopback_code: not_run_no_replacement_created
- http_18789_podip_code: not_run_no_replacement_created
- http_18789_serviceip_code: not_run_no_replacement_created
- packaged_verifier_against_replacement_running_container: not_run_no_replacement_created

## Sanitized Live Condition Vector

Backend vector:

- ws_upstream_shape: not_collected_no_replacement_runtime
- ws_first_connect: not_collected_no_replacement_runtime
- ws_first_upstream_frame: not_collected_no_replacement_runtime
- first_upstream_error_code: not_collected_no_replacement_runtime

Runtime vector:

- missing_device_decision: not_collected_no_replacement_runtime
- is_control_ui: not_collected_no_replacement_runtime
- role: not_collected_no_replacement_runtime
- auth_method: not_collected_no_replacement_runtime
- auth_ok: not_collected_no_replacement_runtime
- shared_auth_ok: not_collected_no_replacement_runtime
- has_shared_auth: not_collected_no_replacement_runtime
- forwarded_prefix_shape: not_collected_no_replacement_runtime
- cond_is_control_ui: not_collected_no_replacement_runtime
- cond_role_operator: not_collected_no_replacement_runtime
- cond_shared_auth_proof: not_collected_no_replacement_runtime
- cond_auth_method_shared_secret: not_collected_no_replacement_runtime
- cond_forwarded_prefix_match: not_collected_no_replacement_runtime
- mediated_helper_result: not_collected_no_replacement_runtime

Browser vector:

- browser_manual_e2e_run: false
- device_identity_required_visible: not_collected_no_replacement_runtime
- device_signature_invalid_visible: not_collected_no_replacement_runtime
- connected_state: not_collected_no_replacement_runtime
- chat_ready_state: not_collected_no_replacement_runtime
- post_connect_reached: not_collected_no_replacement_runtime
- mg_internal_reached: not_collected_no_replacement_runtime
- internal_localization_observed: not_collected_no_replacement_runtime

## Security And Non Actions

- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no secret/header/access URL logging
- no old asset cleanup
- no old session cleanup
- no old evidence cleanup
- no old image cleanup
- no instance cleanup outside instance 19
- no replacement instance create after insufficient capacity verdict
- no backend image build/tag/push/pull
- no runtime image build/tag/push/pull
- no backend rebuild
- no runtime rebuild
- no code change
- no frontend change
- no deployments change
- no docs change
- no longterm change
- no AgentTeam change
- no UnifiedFramework change
- no direct SQL
- no database mutation outside approved instance 19 API delete
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Verification Status

- kubectl get pod clawreef-19-oc2gi-diag-175648 status-only: NotFound
- kubectl get pods namespace status-only after recovery: old instances 16,17,18 Running only
- replacement create before_count: 3
- replacement create after_count: 3
- replacement pod Ready check: not_run_no_replacement_created
- 18789 loopback/PodIP/ServiceIP HTTP 200 check: not_run_no_replacement_created
- packaged verifier against replacement running container: not_run_no_replacement_created
- browser/manual E2E: not_run_no_replacement_created
- sanitized backend/runtime log scan: not_run_no_replacement_runtime
