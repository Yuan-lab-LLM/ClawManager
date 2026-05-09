# CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E

Date/timezone: 2026-05-09, Asia/Shanghai
Role/task: Worker, serial topology
Gate type: CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE
Approval phrase used: APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_DONE
```

The reviewed live residual sweep artifact was delivered in a new runtime image. Exactly one fresh replacement instance was created and reached Ready. Worker did not perform browser/manual E2E final acceptance and does not claim final internal localization acceptance.

## Dependency Gates

```text
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_DONE
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_ROOT_CAUSE_AND_PATCH_DONE
CONTROLUI_INTERNAL_LOCALIZATION_RUNTIME_DELIVERY_DONE
```

## Delivered Runtime Image

```text
tag_suffix=gtclaw-internal-localization-residual-sweep-20260509103428
host_image=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428
image_index_digest=sha256:cf71b997e500427487ac940fa9901ce6377a6cd99035f0adb71337555d3d0635
linux_arm64_manifest_digest=sha256:79a9c18d4e0b6f42848943ffb7b4ca78fe0466eb23a1cf32801dedaf2a881b46
attestation_manifest_digest=sha256:91781abaf49f4304974082832eaf11f3f9bb20b8f88b251acbdd5812e2276a7b
build_context=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker_buildx_build_push_exit=0
docker_build_warning=FromPlatformFlagConstDisallowed
```

## Image And Readback Verification

```text
image/readback verifier passed
source verifier during Dockerfile build=passed
image filesystem verifier=passed
image inspect/readback verifier passed
```

Image filesystem hash readback:

```text
index-M4TNVXB3.js = 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
nodes-BBk4VzkK.js = bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e
skills-BRWdbtpV.js = 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
skills-shared-D6eRDyeb.js = f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
config-form-x_UhxUYO.js remains 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
zh-CN-B26mMdbY.js remains cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

Safety boundary verification:

```text
operator.read + operator.pairing scope patch retained
no operator.admin grant
no missing_scope bypass
device signature invalid protection retained
no insecure auth
no global bypass
no direct browser device-less allow
```

## Capacity Handling

Capacity before replacement did not have enough request headroom for another 2Gi pod.

Before handling:

```text
api_instances_before_count=6
api_instances_before_ids=17,18,20,21,22,23
api_instances_before_statuses=17:running,18:running,20:stopped,21:stopped,22:stopped,23:running
api_instances_before_names=17:oc2gi-tp-134542,18:oc2gi-sa-151137,20:oc2gi-diag-r-183249,21:oc2gi-sap-r-192528,22:oc2gi-scope-r-204419,23:oc2gi-iloc-r-213106
allocated_request_memory_before_capacity_mi=6284
available_request_headroom_before_capacity_mi=1552
required_replacement_memory_mi=2048
```

Approved capacity action:

```text
handled_instance=instance 23
handled_instance_name=oc2gi-iloc-r-213106
instance23_stop_http_status=200
instance23_stop_success=true
instance23_pod_after_stop=NotFound
instance23_api_status_after_stop=stopped
allocated_request_memory_after_capacity_mi=4236
available_request_headroom_after_capacity_mi=3600
only instance 23 handled
instances 17/18 untouched
```

No old asset, session, evidence, image, Service, or PVC cleanup was performed.

## Fresh Replacement Instance

Creation:

```text
create_http_status=201
created_id=24
created_name=oc2gi-iloc-rs-103428
created_status_initial=creating
created_type=openclaw
created_memory_gb=2
created_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428
created_namespace=clawmanager-user-1
created_pod=clawreef-24-oc2gi-iloc-rs-103428
api_instances_after_create_ids=17,18,20,21,22,23,24
api_instances_after_create_statuses=17:running,18:running,20:stopped,21:stopped,22:stopped,23:stopped,24:creating
new_instance_ids=24
new_instance_count=1
exactly_one_fresh_replacement=true
```

API readback after readiness:

```text
api_instance_id=24
api_instance_name=oc2gi-iloc-rs-103428
api_instance_status=running
api_instance_type=openclaw
api_instance_memory_gb=2
api_instance_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428
api_instance_pod_namespace=clawmanager-user-1
api_instance_pod_name=clawreef-24-oc2gi-iloc-rs-103428
```

## Replacement Pod, Service, And Runtime Readiness

```text
replacement pod Ready
kubectl_wait_ready_exit=0
pod_phase=Running
pod_ready_condition=True
containers_ready_condition=True
desktop_container_ready=true
restart_count=0
current_reason=running
last_reason=none
oom_killed=false
pod_ip=10.42.0.119
node=k3d-clawmanager-server-0
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-internal-localization-residual-sweep-20260509103428
pod_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:cf71b997e500427487ac940fa9901ce6377a6cd99035f0adb71337555d3d0635
pod_image_id_matches_new_image_index_digest=true
```

Service and EndpointSlice:

```text
service=clawreef-24-oc2gi-iloc-rs-103428-svc
service_type=ClusterIP
service_ip=10.43.59.159
service_selector_instance_id=24
service_ports=http:3001->3001/TCP,control-ui:18789->18789/TCP
endpointslice_ready=true
endpointslice_serving=true
endpointslice_ports=http:3001/TCP,control-ui:18789/TCP
```

Runtime `18789` status-only checks:

```text
loopback_18789_http_code=200
loopback_18789_size=3398
podip_18789_http_code=200
podip_18789_size=3398
serviceip_18789_http_code=200
serviceip_18789_size=3398
```

Instances after delivery:

```text
pod=clawmanager-user-1/clawreef-17-oc2gi-tp-134542 phase=Running ready=true restarts=0 memory=2Gi
pod=clawmanager-user-1/clawreef-18-oc2gi-sa-151137 phase=Running ready=true restarts=0 memory=2Gi
pod=clawmanager-user-1/clawreef-24-oc2gi-iloc-rs-103428 phase=Running ready=true restarts=0 memory=2Gi
instance 23 pod absent after approved stop
```

## Running Container Verification

```text
running-container verifier passed
running_container_verifier_exit=0
running_container_verifier_result=passed
```

Running-container hash readback:

```text
running_container_index-M4TNVXB3.js = 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
running_container_nodes-BBk4VzkK.js = bec1fee1191691d554a803b09e2bb036ee7cf74d08c0bb54e938107ebc25070e
running_container_skills-BRWdbtpV.js = 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
running_container_skills-shared-D6eRDyeb.js = f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
running_container_config-form-x_UhxUYO.js = 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
running_container_zh-CN-B26mMdbY.js = cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

Running-container safety boundary:

```text
operator.read + operator.pairing scope patch retained
no operator.admin grant
no missing_scope bypass
device signature invalid protection retained
no insecure auth
no global bypass
no direct browser device-less allow
```

## Manual E2E Handoff

manual E2E URL:

```text
https://localhost:30443/api/v1/instances/24/control-ui/chat?session=main
```

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

Worker did not perform browser/manual E2E final acceptance and did not claim final internal localization acceptance.

## Boundary Confirmation

```text
no JS asset modification
no Dockerfile modification
no manifest modification
no backend modification
no runtime auth/scope code modification
no operator.admin grant
no missing_scope bypass
no insecure auth
no global bypass
no direct browser device-less allow
no cleanup old asset
no cleanup old session
no cleanup old evidence
no cleanup old image
no cleanup old PVC
no touch instances 17/18
no browser/manual E2E final acceptance by Worker
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
no sensitive values recorded
```

## Commands Executed

Command shapes are recorded with secret-bearing values omitted.

```bash
shasum -a 256 <assembly-control-ui-assets>
node --check <changed assembly JS bundles>
docker buildx build --platform linux/arm64 -t <host_image> --push <assembly-context>
docker buildx imagetools inspect <host_image>
docker run --rm --platform linux/arm64 --entrypoint /bin/sh <host_image> -lc '<image verifier and hash readback>'
POST /api/v1/instances/23/stop
POST /api/v1/instances
kubectl wait --for=condition=Ready pod/clawreef-24-oc2gi-iloc-rs-103428 -n clawmanager-user-1 --timeout=300s
kubectl get pod/service/endpointslice status-only fields
kubectl exec clawreef-24-oc2gi-iloc-rs-103428 -- 18789 status-only checks
kubectl exec clawreef-24-oc2gi-iloc-rs-103428 -- running-container verifier and hash readback
```

## Verification Summary

```text
image inspect/readback verifier passed
running-container verifier passed
replacement pod Ready
required hashes matched
evidence marker scan passed
evidence sensitive scan clean
evidence trailing whitespace scan clean
git diff --check clean
scoped git status recorded
```
