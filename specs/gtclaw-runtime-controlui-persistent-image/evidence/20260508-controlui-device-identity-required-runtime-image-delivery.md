# CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate: CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_GATE

## Verdict

CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_DONE

Built and pushed a new immutable OpenClaw runtime image containing the `sharedAuthOk` mediated helper patch, created exactly one fresh runtime instance with that new image, and completed readiness/readback evidence only.

No browser E2E, manual E2E, DevTools, Playwright, or GTClaw/OpenClaw internal interface localization acceptance was performed or claimed.

## Approval

Approval phrase used:

APPROVE_CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_GATE

Dependency gates used:

- CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_APPROVAL_PACKET_DONE
- CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_GATE_DONE
- backend auth-contract patch delivered and `/healthz` passed

Root-cause input:

- Current instance 17 blocker is the old runtime image not containing the new `sharedAuthOk` mediated helper patch.
- Runtime helper must consume `sharedAuthOk`.
- Runtime authMethod literals are `token|password`.

## Runtime Image

Build input:

```text
context=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/
platform=linux/arm64
```

Published tags:

```text
tag_suffix=gtclaw-controlui-localization-sharedauth-20260508151137
host_tag=localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
cluster_tag=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
```

Build/push result:

```text
docker_buildx_build_push_exit=0
dockerfile_packaged_verifier_exit=0
build_warning=FromPlatformFlagConstDisallowed
```

Registry inspect:

```text
image_index_digest=sha256:eb3e6423d73f8097ef8a5c890204d88dfa4a2af67d503b1124d8f93bd0ad38a6
linux_arm64_manifest_digest=sha256:848999604a850be221d35e1dea9783d7a6442eb9031da62b07273a09f3dca748
attestation_manifest_digest=sha256:0631ad21faa528cb5bc6b2cb79af3d8233becc06a4be539896c3806885ce06d6
```

## Image Readback

Readback path shape:

```text
readback_dir=/tmp/clawmanager-openclaw-image-readback-sharedauth-<timestamp>
server_impl_path=<readback_dir>/server.impl-BbJvXoPb.js
zh_cn_asset_path=<readback_dir>/control-ui/assets/zh-CN-B26mMdbY.js
```

Readback verifier:

```text
source_verifier_exit=0
packaged_verifier_exit=0
isGtManagerMediatedControlUiAuth_present=true
params.sharedAuthOk===true_present=true
x-forwarded-prefix_present=true
invalid_device_signature_marker_present=true
verifyDeviceSignature_present=true
resolveConnectAuthDecision_present=true
bootstrapTokenCandidate_present=true
verifyDeviceToken_present=true
shouldSkipControlUiPairing_present=true
```

zh-CN asset hash:

```text
zh_CN_B26mMdbY_sha256=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
zh_CN_expected_hash_match=true
```

This hash proof only shows that the packaged zh-CN asset did not revert. It is not browser/manual E2E and does not prove GTClaw/OpenClaw internal interface localization acceptance.

## Fresh Runtime Instance

Creation path:

```text
api_path_shape=/api/v1/instances
runtime_image_setting_mutated=false
create_payload_image_registry_shape=cluster_tag
credential_values_recorded=false
request_secret_values_recorded=false
```

Before create:

```text
api_instances_before_count=2
api_instances_before_ids=16,17
api_instances_before_names=oc2gi-loc-221427,oc2gi-tp-134542
```

Create result:

```text
create_http_status=201
created_id=18
created_name=oc2gi-sa-151137
created_status=creating
created_type=openclaw
created_memory_gb=2
created_image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
created_namespace=clawmanager-user-1
created_pod=clawreef-18-oc2gi-sa-151137
```

After create:

```text
api_instances_after_count=3
api_instances_after_ids=16,17,18
api_instances_after_names=oc2gi-loc-221427,oc2gi-tp-134542,oc2gi-sa-151137
new_instance_ids=18
new_instance_count=1
exactly_one_fresh_runtime_instance=true
no_second_instance=true
```

API readback after readiness:

```text
api_instance_id=18
api_instance_name=oc2gi-sa-151137
api_instance_status=running
api_instance_type=openclaw
api_instance_memory_gb=2
api_instance_image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
api_instance_pod_namespace=clawmanager-user-1
api_instance_pod_name=clawreef-18-oc2gi-sa-151137
```

## Pod Readiness

Wait result:

```text
kubectl_wait_ready_exit=0
```

Pod status:

```text
pod=clawmanager-user-1/clawreef-18-oc2gi-sa-151137
phase=Running
node=k3d-clawmanager-server-0
pod_ip_present=true
ready_condition=True
containers_ready_condition=True
container=desktop
desktop_container_ready=true
restart_count=0
last_reason=none
current_reason=running
oom_killed=false
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:eb3e6423d73f8097ef8a5c890204d88dfa4a2af67d503b1124d8f93bd0ad38a6
imageID_matches_new_image_index_digest=true
```

Raw `kubectl get pod -o json` was run for verification. Its runtime environment values were not copied into this evidence; only the sanitized status fields above are recorded.

## Service And Runtime HTTP

Service readback:

```text
service=clawmanager-user-1/clawreef-18-oc2gi-sa-151137-svc
service_type=ClusterIP
service_ip_present=true
selector_instance_id=18
port_3001_present=true
port_18789_present=true
endpointslice_ready=true
endpointslice_serving=true
```

Runtime `18789` HTTP readback:

```text
loopback_http_code=200
loopback_size=3398
podip_http_code=200
podip_size=3398
serviceip_http_code=200
serviceip_size=3398
```

Only status/code and size were recorded; no access endpoint string was recorded.

## Running Container Readback

Verifier:

```text
running_container_verifier_exit=0
running_container_verifier_result=passed
```

Required markers:

```text
isGtManagerMediatedControlUiAuth_present=true
params.sharedAuthOk===true_present=true
x-forwarded-prefix_present=true
invalid_device_signature_marker_present=true
verifyDeviceSignature_present=true
resolveConnectAuthDecision_present=true
bootstrapTokenCandidate_present=true
verifyDeviceToken_present=true
shouldSkipControlUiPairing_present=true
```

Running-container zh-CN hash:

```text
running_container_zh_CN_B26mMdbY_sha256=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
running_container_zh_CN_expected_hash_match=true
```

## Boundary Confirmation

- no browser E2E
- no manual E2E
- no DevTools
- no Playwright
- no GTClaw/OpenClaw internal interface localization acceptance
- no backend write
- no frontend write
- no deployment manifest write
- no docs write
- no longterm write-back
- no AgentTeam write
- no UnifiedFramework write
- no old evidence write
- no old artifact write
- no direct SQL
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
- no cleanup of instance 17
- no cleanup of old instances
- no cleanup of old images
- no cleanup of old artifacts
- no cache/storage cleanup

## Commands Executed

Credential-bearing command values are omitted. Raw pod JSON was not copied into this evidence because it includes runtime environment values; sanitized status fields are recorded above.

```bash
docker buildx build --platform linux/arm64 -t <host_tag> --push specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract
docker buildx imagetools inspect <host_tag>
docker buildx imagetools inspect <host_tag> --raw
node specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs <image-readback-server.impl.js> specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs
shasum -a 256 <image-readback-control-ui>/assets/zh-CN-B26mMdbY.js
POST /api/v1/instances
kubectl wait --for=condition=Ready pod/<fresh-pod> -n clawmanager-user-1 --timeout=240s
kubectl get pod -n clawmanager-user-1 <fresh-pod> -o json
kubectl get svc -n clawmanager-user-1 <fresh-service> -o yaml
kubectl exec <fresh-pod> -- <18789 status-only readback>
kubectl exec <fresh-pod> -- <running-container verifier>
kubectl exec <fresh-pod> -- <running-container zh-CN hash>
```
