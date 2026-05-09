# CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_RUNTIME_DELIVERY_AND_INTERNAL_LOCALIZATION_BROWSER_E2E

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Approval phrase: APPROVE_CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_RUNTIME_DELIVERY_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_GATE

## Verdict

```text
CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_RUNTIME_DELIVERY_DONE
```

The runtime scope propagation patch was delivered in a new image, exactly one fresh replacement instance was created, and the replacement pod/service/readiness checks passed. Browser/manual E2E final acceptance was not performed by Worker and remains for the user.

## Delivered Runtime Image

```text
host_image=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-scope-propagation-runtime-20260508204419
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-scope-propagation-runtime-20260508204419
image_index_digest=sha256:623be801e9c0337a9d8d935a3c31827a7c2c1cbe3fe5c690c3d9994fcc69f8ff
linux_arm64_manifest_digest=sha256:5eede22b22363293fd22841b0281a099103113402dbb3264d761ea01ea5bc351
attestation_manifest_digest=sha256:1c225065740c8133b751f62479ea75dbaf20d22304546c332c73c058cd51d7e9
build_context=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker_buildx_build_push_exit=0
docker_build_warning=FromPlatformFlagConstDisallowed
```

## Image And Readback Verifier

The Dockerfile packaged verifier ran during build and returned success.

Delivered image readback verifier:

```text
source_verifier_against_image_readback=passed
packaged_verifier_against_image_readback=passed
sharedAuthOk marker present=true
mediated helper markers present=true
scope normalizer markers present=true
operator.read present in mediated scope normalizer=true
operator.pairing present in mediated scope normalizer=true
operator_admin_in_mediated_scope_normalizer=false
no operator.admin
missing_scope_bypass_detected=false
no missing_scope bypass
invalid device-signature protection retained=true
trustedProxy_json_marker_trust_detected=false
zh-CN hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
zh_CN_expected_hash_match=true
```

Running-container verifier:

```text
running_container_verifier=passed
running_container_zh_CN_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
running_container_zh_CN_hash_match=true
```

## Capacity And Replacement Instance

Capacity preflight before mutation:

```text
api_instances_before_ids=17,18,20,21
api_instances_before_statuses=17:running,18:running,20:stopped,21:running
managed_pod_instance_ids=17,18,21
managed_pvc_instance_ids=17,18,20,21
orphan_pod_instance_ids=none
orphan_pvc_instance_ids=none
allocatable_memory_mi=7836
allocated_request_memory_mi=6284
available_request_headroom_mi=1552
required_replacement_memory_mi=2048
```

Approved capacity action:

```text
handled_instance=instance 21
handled_instance_name=oc2gi-sap-r-192528
instance21_stop_http_status=200
instance21_pod_after_stop=NotFound
instance21_pvc_retained=Bound 20Gi
instance21_service_retained=ClusterIP ports 3001,18789
do not touch 17/18
instance17_after_capacity=Running
instance18_after_capacity=Running
available_request_headroom_after_capacity_mi=3600
```

Fresh replacement creation:

```text
create_http_status=201
created_id=22
created_name=oc2gi-scope-r-204419
created_status_initial=creating
created_type=openclaw
created_memory_gb=2
created_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-scope-propagation-runtime-20260508204419
created_namespace=clawmanager-user-1
created_pod=clawreef-22-oc2gi-scope-r-204419
api_instances_after_ids=17,18,20,21,22
api_instances_after_statuses=17:running,18:running,20:stopped,21:stopped,22:creating
new_instance_ids=22
new_instance_count=1
exactly one fresh replacement=true
```

API readback after readiness:

```text
api_instance_id=22
api_instance_name=oc2gi-scope-r-204419
api_instance_status=running
api_instance_type=openclaw
api_instance_memory_gb=2
api_instance_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-scope-propagation-runtime-20260508204419
api_instance_pod_namespace=clawmanager-user-1
api_instance_pod_name=clawreef-22-oc2gi-scope-r-204419
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
pod_ip=10.42.0.115
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-scope-propagation-runtime-20260508204419
pod_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:623be801e9c0337a9d8d935a3c31827a7c2c1cbe3fe5c690c3d9994fcc69f8ff
```

Service and EndpointSlice:

```text
service=clawreef-22-oc2gi-scope-r-204419-svc
service_type=ClusterIP
service_ip=10.43.5.58
service_selector_instance_id=22
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
https://localhost:30443/api/v1/instances/22/control-ui/chat?session=main
```

User manual E2E should report back this vector:

```text
device_signature_invalid=false
device_identity_required=false
missing_scope_absent=true
webchat_connected=true
chat_ready_state=true
post_connect_reached=true
mg_internal_reached=true
internal_localization_observed_inside_GTClaw_OpenClaw_internal_interface=true
entrance_connection_form_is_not_final_acceptance=true
```

Worker did not perform browser/manual E2E and did not claim internal localization acceptance.

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
```
