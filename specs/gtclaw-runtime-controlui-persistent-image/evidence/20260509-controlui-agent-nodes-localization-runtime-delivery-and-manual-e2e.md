# CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E

Date/timezone: 2026-05-09, Asia/Shanghai
Role/task: Worker, serial topology
Gate type: CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE
Approval phrase used: APPROVE_CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

## Verdict

```text
CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_DONE
```

The reviewed agents and nodes localization artifacts were delivered in a new runtime image. Exactly one fresh replacement instance was created and reached Ready. Worker did not perform browser/manual E2E final acceptance and does not claim final localization acceptance.

## Dependency Gates

```text
CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_DONE
CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_DONE
CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_BLOCKED
CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_DONE
```

## Delivered Runtime Image

```text
tag_suffix=gtclaw-agent-nodes-localization-20260509121909
host_image=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-agent-nodes-localization-20260509121909
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-agent-nodes-localization-20260509121909
image_index_digest=sha256:1d7ecdd6bc4250132d26110c9d29e76bb246da0e354f2ec02e13cb647861d2b3
linux_arm64_manifest_digest=sha256:93d1fd4baaf85c50d307763e72b6c3832e2d412695f591b1aa1415641ced0e54
attestation_manifest_digest=sha256:4c24b44d84cad468c5966eee2f3478345e6a83ba0c0fe8f18df049fa6a356f4a
build_context=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker_buildx_build_push_exit=0
docker_buildx_imagetools_inspect=passed
docker_build_warning=FromPlatformFlagConstDisallowed
```

## Image And Readback Verification

```text
image/readback verifier passed
trusted proxy contract verifier passed
```

Image filesystem hash readback:

```text
agents-_34Q844e.js = 1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7
nodes-BBk4VzkK.js = 25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4
index-M4TNVXB3.js = 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
skills-BRWdbtpV.js = 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
skills-shared-D6eRDyeb.js = f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
config-form-x_UhxUYO.js = 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
zh-CN-B26mMdbY.js = cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

Image safety boundary verification:

```text
operator.read + operator.pairing retained
no operator.admin grant
no missing_scope bypass
no auth/scope modification
no insecure auth
no global bypass
no direct browser device-less allow
device signature invalid protection retained
```

## Capacity Handling

Capacity required processing the superseded instance 24.

Before handling:

```text
running_instances_before=17,18,24
instance17=oc2gi-tp-134542 running
instance18=oc2gi-sa-151137 running
instance24=oc2gi-iloc-rs-103428 running
```

Approved capacity action:

```text
handled_instance=instance 24
handled_instance_name=oc2gi-iloc-rs-103428
instance24_stop_http_status=200
instance24_pod_after_stop=NotFound
instance24_api_status_after_stop=stopped
only instance 24 handled
instances 17/18 untouched
instance24_service_retained=true
instance24_pvc_retained=true
```

No old asset, session, evidence, image, Service, or PVC cleanup was performed.

## Fresh Replacement Instance

Creation:

```text
create_http_status=201
created_id=25
created_name=oc2gi-anloc-121909
created_status_initial=creating
created_type=openclaw
created_memory_gb=2
created_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-agent-nodes-localization-20260509121909
created_namespace=clawmanager-user-1
created_pod=clawreef-25-oc2gi-anloc-121909
new_instance_ids=25
new_instance_count=1
exactly_one_fresh_replacement=true
```

API readback after readiness:

```text
api_instance_id=25
api_instance_name=oc2gi-anloc-121909
api_instance_status=running
api_instance_type=openclaw
api_instance_memory_gb=2
api_instance_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-agent-nodes-localization-20260509121909
api_instance_pod_namespace=clawmanager-user-1
api_instance_pod_name=clawreef-25-oc2gi-anloc-121909
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
pod_ip=10.42.0.121
node=k3d-clawmanager-server-0
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-agent-nodes-localization-20260509121909
pod_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:1d7ecdd6bc4250132d26110c9d29e76bb246da0e354f2ec02e13cb647861d2b3
pod_image_id_matches_new_image_index_digest=true
```

Service and EndpointSlice:

```text
service=clawreef-25-oc2gi-anloc-121909-svc
service_type=ClusterIP
service_ip=10.43.246.11
service_selector_instance_id=25
service_ports=http:3001->3001/TCP,control-ui:18789->18789/TCP
endpointslice_ready=true
endpointslice_ports=http:3001/TCP,control-ui:18789/TCP
```

Runtime `18789` status-only checks:

```text
loopback_18789_http_code=200
podip_18789_http_code=200
serviceip_18789_http_code=200
```

Instances after delivery:

```text
pod=clawmanager-user-1/clawreef-17-oc2gi-tp-134542 phase=Running ready=true restarts=0 memory=2Gi
pod=clawmanager-user-1/clawreef-18-oc2gi-sa-151137 phase=Running ready=true restarts=0 memory=2Gi
pod=clawmanager-user-1/clawreef-25-oc2gi-anloc-121909 phase=Running ready=true restarts=0 memory=2Gi
instance 24 pod absent after approved stop
```

## Running Container Verification

```text
running-container verifier passed
trusted_proxy_contract_verifier=passed
```

Running-container hash readback:

```text
running_container_agents-_34Q844e.js = 1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7
running_container_nodes-BBk4VzkK.js = 25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4
running_container_index-M4TNVXB3.js = 6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6
running_container_skills-BRWdbtpV.js = 36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22
running_container_skills-shared-D6eRDyeb.js = f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b
running_container_config-form-x_UhxUYO.js = 8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245
running_container_zh-CN-B26mMdbY.js = cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

Running-container safety boundary:

```text
operator.read + operator.pairing retained
mediated_window_has_operator_admin=false
missing_scope_literal=false
no operator.admin grant
no missing_scope bypass
no auth/scope modification
no insecure auth
no global bypass
no direct browser device-less allow
device signature invalid protection retained
device identity required protection retained
device.nonce retained
connect.challenge retained
```

## Manual E2E Handoff

Manual E2E URL:

```text
https://localhost:30443/api/v1/instances/25/control-ui/chat?session=main
```

Worker did not execute browser/manual E2E final acceptance. User manual E2E should verify internal UI entry, chat ready, agents page localization, and nodes page localization. Code and file literals such as `main`, `AGENTS`, and `SOUL` may remain. Dynamic metadata may be recorded as deferred residual.

## Operations Performed

```text
docker buildx build --platform linux/arm64 --push
docker buildx imagetools inspect
image/readback verifier
POST /api/v1/instances/24/stop
POST /api/v1/instances
kubectl wait for replacement pod Ready
running-container verifier
18789 status-only checks
```

## Guardrails

```text
no touch instances 17/18
no old asset/session/evidence/image/PVC cleanup
no JS/Dockerfile/MANIFEST/backend/runtime auth/scope modification
no operator.admin grant
no missing_scope bypass
no browser/manual E2E final acceptance
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
```
