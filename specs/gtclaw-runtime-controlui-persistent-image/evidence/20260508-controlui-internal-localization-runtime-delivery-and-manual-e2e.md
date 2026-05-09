# CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Approval phrase: APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_DONE
```

The internal localization patch was delivered in a new runtime image, exactly one fresh replacement instance was created, and replacement pod/service/readiness checks passed. Worker did not perform browser/manual E2E final acceptance.

## Delivered Runtime Image

```text
host_image=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-internal-localization-runtime-20260508213106
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-runtime-20260508213106
image_index_digest=sha256:e6700f459ffa1a183bf678accb02b6b4d8ceb360474f416e03ecca6a8d9c57a9
linux_arm64_manifest_digest=sha256:9c2629dc96959fed946f4d5728ccb82092f8c730fd860214a3156b8bce66d8f1
attestation_manifest_digest=sha256:d43f20f811316dc6709c9c2709da219c19aa689565a0741b1da428cb3974f0a3
build_context=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker_buildx_build_push_exit=0
docker_build_warning=FromPlatformFlagConstDisallowed
```

## Image And Readback Verification

Dockerfile packaged verifier ran during build and returned success.

Delivered image readback:

```text
readback_dir=/tmp/clawmanager-openclaw-image-readback-internal-localization-20260508213106
source_verifier_against_image_readback=passed
packaged_verifier_against_image_readback=passed
index-M4TNVXB3.js hash=ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b
config-form-x_UhxUYO.js hash=8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
zh-CN-B26mMdbY.js hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
sharedAuthOk marker present=true
mediated helper markers present=true
scope patch still present=true
operator.read present in mediated scope normalizer=true
operator.pairing present in mediated scope normalizer=true
operator_admin_in_mediated_scope_normalizer=false
no operator.admin
missing_scope_bypass_detected=false
no missing_scope bypass
invalid device-signature protection retained=true
trustedProxy_json_marker_trust_detected=false
```

Running-container verification:

```text
running_container_verifier=passed
running_container_index-M4TNVXB3.js_hash=ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b
running_container_config-form-x_UhxUYO.js_hash=8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
running_container_zh-CN-B26mMdbY.js_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

## Capacity And Replacement Instance

Before capacity handling:

```text
api_instances_before_ids=17,18,20,21,22
api_instances_before_statuses=17:running,18:running,20:stopped,21:stopped,22:running
managed_pod_instance_ids=17,18,22
allocatable_memory_mi=7836
allocated_request_memory_mi=6284
available_request_headroom_mi=1552
required_replacement_memory_mi=2048
```

Approved capacity handling:

```text
handled_instance=instance 22
handled_instance_name=oc2gi-scope-r-204419
instance22_stop_http_status=200
instance22_pod_after_stop=NotFound
instance22_api_status_after_stop=stopped
instance22_pvc_retained=Bound 20Gi
instance22_service_retained=ClusterIP ports 3001,18789
do not touch 17/18
instance17_after_capacity=Running
instance18_after_capacity=Running
allocated_request_memory_after_capacity_mi=4236
available_request_headroom_after_capacity_mi=3600
```

Fresh replacement creation:

```text
create_http_status=201
created_id=23
created_name=oc2gi-iloc-r-213106
created_status_initial=creating
created_type=openclaw
created_memory_gb=2
created_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-runtime-20260508213106
created_namespace=clawmanager-user-1
created_pod=clawreef-23-oc2gi-iloc-r-213106
api_instances_after_create_ids=17,18,20,21,22,23
api_instances_after_create_statuses=17:running,18:running,20:stopped,21:stopped,22:stopped,23:creating
new_instance_ids=23
new_instance_count=1
exactly one fresh replacement=true
```

API readback after readiness:

```text
api_instance_id=23
api_instance_name=oc2gi-iloc-r-213106
api_instance_status=running
api_instance_type=openclaw
api_instance_memory_gb=2
api_instance_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-runtime-20260508213106
api_instance_pod_namespace=clawmanager-user-1
api_instance_pod_name=clawreef-23-oc2gi-iloc-r-213106
```

## Pod Service And 18789 Readiness

Pod readiness:

```text
kubectl_wait_ready_exit=0
pod_phase=Running
pod_ready_condition=True
containers_ready_condition=True
desktop_container_ready=true
restart_count=0
current_reason=running
last_reason=none
oom_killed=false
pod_ip=10.42.0.117
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-runtime-20260508213106
pod_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:e6700f459ffa1a183bf678accb02b6b4d8ceb360474f416e03ecca6a8d9c57a9
```

Service and EndpointSlice:

```text
service=clawreef-23-oc2gi-iloc-r-213106-svc
service_type=ClusterIP
service_ip=10.43.61.67
service_selector_instance_id=23
service_ports=http:3001->3001/TCP,control-ui:18789->18789/TCP
endpointslice_ready=true
endpointslice_serving=true
endpointslice_ports=http:3001/TCP,control-ui:18789/TCP
```

Direct no-proxy 18789 checks:

```text
loopback_18789_http_code=200
loopback_18789_size=3398
podip_18789_http_code=200
podip_18789_size=3398
serviceip_18789_http_code=200
serviceip_18789_size=3398
HTTP 200
```

## Manual E2E Handoff

manual E2E URL:

```text
https://localhost:30443/api/v1/instances/23/control-ui/chat?session=main
```

User manual E2E should report back this vector:

```text
device_signature_invalid=false
device_identity_required=false
missing_scope_absent=true
webchat_connected=true
chat_ready_state=true
mg_internal_reached=true
internal_ui_english_residuals_absent_or_materially_reduced=true
entrance_connection_form_is_not_final_acceptance=true
```

Residual strings for user check inside internal UI:

```text
Message Assistant (Enter to send)
Default (Auto)
Default (off)
Form
Raw
No changes
Raw mode disabled...
Open
Save
Apply
Update
Search settings...
Settings
Communication
Schema unavailable.
```

Worker did not perform browser/manual E2E final acceptance and did not claim internal localization acceptance.

## Boundary Confirmation

```text
no cleanup old PVC
no cleanup old asset
no cleanup old session
no cleanup old evidence
no cleanup old image
no frontend modification
no deployments modification
no docs modification
no longterm write-back
no AgentTeam modification
no UnifiedFramework modification
no database mutation
no direct SQL
no insecure auth
no global bypass
no direct browser device-less allow
no trustedProxy JSON marker trust
no browser/manual E2E final acceptance by Worker
no Mem0 write
no passes:true
no Close
no git stage/commit/push
sensitive values recorded=false
```

## Commands Executed

Commands were run with sensitive values held only in process memory; sensitive values are not copied here.

```bash
docker buildx build --platform linux/arm64 -t <host_image> --push specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker buildx imagetools inspect <host_image>
docker buildx imagetools inspect <host_image> --raw
node specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs <image-readback-server.impl.js> specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs
node <image-readback-packaged-verifier> <image-readback-server.impl.js> <image-readback-packaged-patch-script>
shasum -a 256 <image-readback-control-ui-assets>
POST /api/v1/instances/22/stop
POST /api/v1/instances
kubectl wait --for=condition=Ready pod/clawreef-23-oc2gi-iloc-r-213106 -n clawmanager-user-1 --timeout=240s
kubectl get pod/service/endpointslice status-only fields
kubectl exec clawreef-23-oc2gi-iloc-r-213106 -- 18789 status-only readback
kubectl exec clawreef-23-oc2gi-iloc-r-213106 -- running-container verifier and hash readback
```
