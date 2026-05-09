# CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_DONE

Expanded pod-only capacity recovery released enough request memory, exactly one new standard 2Gi trusted-proxy OpenClaw runtime instance was created through the normal ClawManager API path, and the new running container passed readiness, runtime HTTP, trusted-proxy contract readback, startup wrapper/config proof, and zh-CN Control UI hash proof.

## Approval token used

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

## Dependency gates

| Gate | Status used |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE | Expanded pod-only cleanup approval packet authorized listed candidate pods only. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_BLOCKED | Previous fresh instance gate blocked before creation because request headroom was below 2Gi. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Final trusted-proxy runtime image tag and digest evidence exists. |

## Target runtime image

```text
target_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
expected_index_digest=sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010
expected_linux_arm64_digest=sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb
```

## Initial capacity preflight

Read-only capacity before cleanup:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
capacity_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
available_request_headroom=1552Mi
required_standard_2Gi_instance_memory=2048Mi
capacity_sufficient_before_cleanup=false
```

Initial listed runtime pod inventory:

```text
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-12-123 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-13-0506-claw phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

## Cleanup performed

Expanded cleanup authorization allowed only the five listed candidate pods. This gate deleted only the first listed candidate and stopped cleanup once enough request headroom was available.

```text
deleted_pods=clawmanager-user-1/pod/clawreef-11-oc2gi-oa-131301
deleted_pod_count=1
candidate_order_followed=true
no_unlisted_pod_deletion=true
no_Service/PVC_deletion=true
no_cleanup_beyond_required_headroom=true
```

Mutation command executed:

```bash
kubectl -n clawmanager-user-1 delete pod clawreef-11-oc2gi-oa-131301 --wait=true
```

Result:

```text
pod "clawreef-11-oc2gi-oa-131301" deleted
```

Capacity after deleting candidate 1:

```text
allocated_cpu_requests=2200m
allocated_memory_requests=4236Mi
available_request_headroom=3600Mi
capacity_sufficient_for_one_standard_2Gi_instance=true
cleanup_stopped=true
```

Candidate pods not deleted:

```text
not_deleted=clawmanager-user-1/pod/clawreef-15-oc2gi-cp-150002
not_deleted=clawmanager-user-1/pod/clawreef-16-oc2gi-loc-221427
not_deleted=clawmanager-user-1/pod/clawreef-12-123
not_deleted=clawmanager-user-1/pod/clawreef-13-0506-claw
```

Service/PVC boundary after cleanup:

```text
service_still_exists=clawreef-11-oc2gi-oa-131301-svc
pvc_still_exists=clawreef-11-pvc
services_deleted=false
pvcs_deleted=false
database_rows_deleted=false
instance_records_deleted=false
sessions_deleted=false
assets_deleted=false
registry_tags_or_content_deleted=false
```

## Fresh instance creation

Exactly one new standard 2Gi OpenClaw runtime instance was created through the normal ClawManager API path.

API authentication used transient in-memory values only. Token/password/auth header values were not printed or recorded.

Before create:

```text
api_instances_before_count=8
api_instances_before_ids=5,9,10,11,12,13,15,16
api_instances_before_names=gtclaw-fresh-20260504095843,gtclaw-fresh-bind-lan-auth-20260505-175724,oc2gi-185707,oc2gi-oa-131301,123,0506-claw,oc2gi-cp-150002,oc2gi-loc-221427
```

Create request shape:

```text
POST /api/v1/instances
name=oc2gi-tp-134542
type=openclaw
memory_gb=2
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
credential_values_recorded=false
auth_header_value_recorded=false
```

Create result:

```text
create_http_status=201
created_id=17
created_name=oc2gi-tp-134542
created_status=creating
created_type=openclaw
created_memory_gb=2
created_image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
created_namespace=clawmanager-user-1
created_pod=clawreef-17-oc2gi-tp-134542
```

After create:

```text
api_instances_after_count=9
api_instances_after_ids=5,9,10,11,12,13,15,16,17
api_instances_after_names=gtclaw-fresh-20260504095843,gtclaw-fresh-bind-lan-auth-20260505-175724,oc2gi-185707,oc2gi-oa-131301,123,0506-claw,oc2gi-cp-150002,oc2gi-loc-221427,oc2gi-tp-134542
new_instance_ids=17
new_instance_count=1
exactly_one_new_instance=true
no second instance=true
```

API status after pod readiness:

```text
api_instance id=17 name=oc2gi-tp-134542 status=running type=openclaw memory_gb=2 image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130 pod_namespace=clawmanager-user-1 pod_name=clawreef-17-oc2gi-tp-134542
```

## Pod readiness

`kubectl wait --for=condition=Ready pod/clawreef-17-oc2gi-tp-134542 -n clawmanager-user-1 --timeout=240s` succeeded.

Pod status:

```text
pod=clawmanager-user-1/clawreef-17-oc2gi-tp-134542
phase=Running
PodReadyToStartContainers=True
Initialized=True
Ready=True
ContainersReady=True
PodScheduled=True
pod_ip=10.42.0.99
node=k3d-clawmanager-server-0
container=desktop
desktop_container_ready=true
restart_count=0
current_reason=running
last_reason=none
oom_killed=false
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010
imageID_matches_expected_index_digest=true
```

Pod events:

```text
Successfully assigned clawmanager-user-1/clawreef-17-oc2gi-tp-134542 to k3d-clawmanager-server-0
Successfully pulled image "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130"
Created container: desktop
Started container desktop
```

No `OOMKilled`, `Back-off`, `Failed`, `Killing`, `Warning`, or `Error` event was reported by the filtered pod describe check.

## Service and EndpointSlice

Service:

```text
service=clawreef-17-oc2gi-tp-134542-svc
type=ClusterIP
cluster_ip=10.43.207.170
selector={"app":"clawreef","instance-id":"17"}
service_port=http 3001->3001/TCP
service_port=control-ui 18789->18789/TCP
```

Endpoints:

```text
endpoints=10.42.0.99:3001,10.42.0.99:18789
```

EndpointSlice:

```text
endpointslice=clawreef-17-oc2gi-tp-134542-svc-kgb2h
address=10.42.0.99
ready=true
serving=true
target=Pod/clawreef-17-oc2gi-tp-134542
ports=http:3001/TCP,control-ui:18789/TCP
```

## Runtime HTTP 18789

Runtime `18789` returned HTTP 200 from all required in-cluster paths:

```text
loopback_http_code=200 size=3398
podip_http_code=200 size=3398
serviceip_http_code=200 size=3398
```

Targets:

```text
loopback=http://127.0.0.1:18789/
pod_ip=http://10.42.0.99:18789/
service_ip=http://10.43.207.170:18789/
```

## Running-container trusted-proxy proof

Trusted-proxy proof directory and verifier:

```text
/usr/local/share/gtclaw/trusted-proxy-auth-contract mode=drwxr-xr-x
packaged_verifier_exists=true
verifier_exit=0
verifier_result=trusted-proxy auth contract verifier passed for /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
```

Required bundle markers:

```text
isGtManagerMediatedControlUiAuth present=true
x-forwarded-prefix present=true
device signature invalid present=true
verifyDeviceSignature present=true
resolveConnectAuthDecision present=true
bootstrapTokenCandidate present=true
verifyDeviceToken present=true
shouldSkipControlUiPairing present=true
```

These readbacks prove the running container contains the trusted-proxy mediated Control UI patch and retains direct-client protection markers.

## Startup wrapper and sanitized config proof

Startup wrapper/config proof:

```text
/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract mode=-rwxr-xr-x
/defaults/openclaw-agent/config.yaml references openclaw-gateway-with-gtmanager-auth-contract=true
wrapper_launches=openclaw gateway run --bind lan --auth token
startup_wrapper_config_proof=ok
```

Sanitized runtime config readback:

```text
runtime_config_exists=true
runtime_config_top_keys=agents,browser,channels,commands,gateway,meta,models,plugins,session,tools,update,wizard
gateway_keys=auth,bind,controlUi,mode,nodes,port,tailscale
gateway_mode=local
gateway_bind=loopback
gateway_port=18789
auth_keys=mode,token
auth_mode=none
has_auth_token_key=true
control_ui_keys=allowedOrigins
control_ui_allowed_origins_count=3
trusted_proxy_config_present=false
```

The sanitized config readback printed only keys, modes, booleans, and counts. It did not print secret values.

## zh-CN Control UI hash proof

Running-container hashes:

```text
b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec  /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

Result:

```text
zh-CN Control UI hash proof passed
zh-CN localization did not revert
```

## Final runtime inventory

Runtime pods after cleanup and fresh instance creation:

```text
pod=clawreef-12-123 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none>
pod=clawreef-13-0506-claw phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none>
pod=clawreef-15-oc2gi-cp-150002 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0
pod=clawreef-16-oc2gi-loc-221427 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0
pod=clawreef-17-oc2gi-tp-134542 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0
```

Final allocated resources after the new 2Gi instance was scheduled:

```text
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
```

Services and PVCs after the gate still include the old objects plus the new instance objects. No old Service or PVC was deleted.

## Browser/manual E2E decision

Browser/manual E2E is now appropriate as a separate later gate, because:

- backend delivery was already completed in the dependency gate;
- the final trusted-proxy target image is now running in fresh instance `17 / oc2gi-tp-134542`;
- Pod Running/Ready, Service `18789`, EndpointSlice readiness, runtime HTTP 200, running-container trusted-proxy proof, startup wrapper/config proof, and zh-CN hash proof all passed.

This gate did not run browser E2E.

## Commands executed

Representative command shapes are recorded with credential and auth header values omitted:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,ALLOCATABLE_CPU:.status.allocatable.cpu,ALLOCATABLE_MEMORY:.status.allocatable.memory,CAPACITY_MEMORY:.status.capacity.memory
kubectl describe nodes | sed -n '/Allocated resources:/,/Events:/p'
kubectl -n clawmanager-user-1 get pods -l app=clawreef -o 'custom-columns=NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,IMAGE:.spec.containers[0].image,MEMORY:.spec.containers[0].resources.requests.memory,NODE:.spec.nodeName' --sort-by=.metadata.creationTimestamp
kubectl -n clawmanager-user-1 delete pod clawreef-11-oc2gi-oa-131301 --wait=true
curl -sk --max-time 15 --noproxy '*' -X POST https://localhost:30443/api/v1/auth/login -H 'Content-Type: application/json' --data-raw '<credential values omitted>'
curl -sk --max-time 15 --noproxy '*' 'https://localhost:30443/api/v1/instances?limit=100' -H 'Authorization: Bearer <omitted>'
curl -sk --max-time 60 --noproxy '*' -X POST https://localhost:30443/api/v1/instances -H 'Authorization: Bearer <omitted>' -H 'Content-Type: application/json' --data-raw '<sanitized create payload>'
kubectl -n clawmanager-user-1 wait --for=condition=Ready pod/clawreef-17-oc2gi-tp-134542 --timeout=240s
kubectl -n clawmanager-user-1 exec clawreef-17-oc2gi-tp-134542 -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '<http code only>' http://127.0.0.1:18789/
kubectl -n clawmanager-user-1 exec clawreef-17-oc2gi-tp-134542 -c desktop -- node /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
kubectl -n clawmanager-user-1 exec clawreef-17-oc2gi-tp-134542 -c desktop -- sha256sum /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

## Verification commands

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-and-fresh-instance.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-and-fresh-instance.md
rg -n "CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE|deleted_pods|exactly_one_new_instance=true|gtclaw-controlui-localization-trusted-proxy-20260508130130|isGtManagerMediatedControlUiAuth|x-forwarded-prefix|zh-CN|no browser E2E|no second instance|no unlisted pod deletion|no Service/PVC deletion|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-and-fresh-instance.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-and-fresh-instance.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-and-fresh-instance.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- ...expanded-capacity-recovery-and-fresh-instance.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null ...expanded-capacity-recovery-and-fresh-instance.md` | `1` | No output and no whitespace diagnostics; exit `1` is expected for a new file compared to `/dev/null`. |
| required marker `rg` scan | `0` | Required markers found, including verdict, deleted_pods, exactly_one_new_instance=true, target image, trusted-proxy markers, zh-CN, and forbidden actions. |
| sensitive/access URL shape scan | `1` | No matches; no token, password, key, cookie, bearer value, auth header value, or access URL plaintext detected. |
| `git status --short -- ...expanded-capacity-recovery-and-fresh-instance.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |

## Sensitive scan result

```text
sensitive_shape_scan_exit=1
sensitive_shape_match_count=0
```

## Forbidden actions confirmation

- no browser E2E
- no DevTools
- no Playwright
- no image build/tag/push/pull
- no source/artifact edits
- no direct database access/mutation
- no registry cleanup
- no storage/cache/cookie cleanup
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no second instance
- no unlisted pod deletion
- no Service/PVC deletion
- no database row deletion
- no instance record deletion
- no session deletion
- no asset deletion
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
