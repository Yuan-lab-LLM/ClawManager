# Control-ui localization expanded capacity recovery and 2Gi fresh instance evidence

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation / Expanded Capacity Recovery + Fresh Instance Runtime Deployment

Gate: CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED
- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE

## Verdict

CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE

Not `CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_BLOCKED`: one authorized listed test runtime pod was deleted pod-only to recover capacity, exactly one new standard 2Gi fresh runtime instance was created through the normal ClawManager API path, and runtime-level evidence passed for pod readiness, image digest, Service, EndpointSlice, HTTP smoke, and running-container control-ui file hashes.

No browser E2E, manual E2E, DevTools, Playwright, Close, passes:true, longterm write-back, git stage/commit/push, direct database access, Service deletion, PVC deletion, session cleanup, asset cleanup, registry tag/content modification, backend/frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework modification, existing artifact modification, or existing evidence modification was performed.

## Target image

| Field | Value |
| --- | --- |
| CLUSTER_TAG | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| expected image index digest | `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54` |
| expected linux/arm64 digest | `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` |
| runtime control-ui root | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

Registry/image metadata read-only confirmation:

```text
Name: localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
MediaType: application/vnd.oci.image.index.v1+json
Digest: sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
linux/arm64 manifest digest: sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e
attestation manifest digest: sha256:40a50f65fd60d98e9bfc9f6e860cd9af49eb26eec92a0354b7d0da74b5765045
```

## Fresh capacity and runtime inventory before cleanup

Read-only node capacity:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
allocated_memory_percent=80
remaining_memory_approx_mib=1552
required_new_standard_instance_memory_mib=2048
capacity_result=insufficient_for_one_2Gi_fresh_instance_without_cleanup
```

Runtime pod inventory before cleanup:

```text
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 instance_id=10 phase=Running ready=true memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 node=k3d-clawmanager-server-0 restarts=0
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 instance_id=11 phase=Running ready=true memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 node=k3d-clawmanager-server-0 restarts=0
pod=clawmanager-user-1/clawreef-12-123 instance_id=12 phase=Pending ready=<none> memory=4Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434 node=<none>
pod=clawmanager-user-1/clawreef-13-0506-claw instance_id=13 phase=Pending ready=<none> memory=4Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434 node=<none>
pod=clawmanager-user-1/clawreef-14-oc1gi-cp-143256 instance_id=14 phase=Failed ready=false memory=1Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712 node=k3d-clawmanager-server-0 restarts=0
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 instance_id=15 phase=Running ready=true memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712 node=k3d-clawmanager-server-0 restarts=0
```

Service/PVC inventory before cleanup showed existing runtime Services and PVCs only. All existing Services and PVCs were left untouched.

## Cleanup action

Capacity was insufficient, so cleanup proceeded in the exact authorized order. The first listed pod was enough to recover capacity, so no later listed pod was deleted.

Candidate validation before delete:

```text
candidate=clawmanager-user-1/pod/clawreef-10-oc2gi-185707
listed=true
namespace=clawmanager-user-1
app_label=clawreef
instance_id=10
phase=Running
ready=true
memory_request=2Gi
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
is_target_localization_image=false
runtime_test_pod=true
ownerRefs=<none>
```

Delete command/result:

```text
kubectl delete pod clawreef-10-oc2gi-185707 -n clawmanager-user-1 --wait=true
result=pod "clawreef-10-oc2gi-185707" deleted
```

deleted_pods:

```text
clawmanager-user-1/pod/clawreef-10-oc2gi-185707
```

No unlisted pod was deleted. The following listed pods were not deleted because capacity was sufficient after the first authorized deletion:

```text
clawmanager-user-1/pod/clawreef-11-oc2gi-oa-131301
clawmanager-user-1/pod/clawreef-15-oc2gi-cp-150002
clawmanager-user-1/pod/clawreef-12-123
clawmanager-user-1/pod/clawreef-13-0506-claw
clawmanager-user-1/pod/clawreef-14-oc1gi-cp-143256
```

Objects explicitly not deleted or modified by cleanup:

```text
service_not_deleted=clawreef-10-oc2gi-185707-svc type=ClusterIP cluster_ip=10.43.10.209 ports=3001,18789
pvc_not_deleted=clawreef-10-pvc status=Bound capacity=20Gi storageClass=local-path
database_row_not_deleted=true
instance_record_not_deleted=true
session_not_deleted=true
asset_not_deleted=true
registry_tag_not_deleted=true
registry_content_not_modified=true
```

Post-cleanup capacity:

```text
allocated_cpu_requests=2200m
allocated_memory_requests=4236Mi
allocated_memory_percent=54
remaining_memory_approx_mib=3600
capacity_recovery_result=sufficient_for_one_standard_2Gi_fresh_instance
```

## Fresh 2Gi instance creation

Exactly one new standard 2Gi fresh runtime instance was created through the normal ClawManager API path. Auth material was used transiently and omitted from output.

API instance list before creation:

```text
api_instances_before_count=8
api_instances_before_ids=5,9,10,11,12,13,14,15
api_instances_before_names=gtclaw-fresh-20260504095843,gtclaw-fresh-bind-lan-auth-20260505-175724,oc2gi-185707,oc2gi-oa-131301,123,0506-claw,oc1gi-cp-143256,oc2gi-cp-150002
```

Create request used:

```text
name=oc2gi-loc-221427
type=openclaw
cpu_cores=1
memory_gb=2
disk_gb=20
gpu_enabled=false
gpu_count=0
os_type=openclaw
os_version=local
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

Sanitized API create result:

```text
create_http_status=201
create_success=true
message=Instance created successfully
created_id=16
created_name=oc2gi-loc-221427
created_status=creating
created_type=openclaw
created_memory_gb=2
created_pod_namespace=clawmanager-user-1
created_pod_name=clawreef-16-oc2gi-loc-221427
created_image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

API instance list after creation:

```text
api_instances_after_count=9
api_instances_after_ids=5,9,10,11,12,13,14,15,16
api_new_ids=16
exactly_one_new_instance=true
```

API status after pod readiness:

```text
api_instance id=16 name=oc2gi-loc-221427 status=running type=openclaw memory_gb=2
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
pod_namespace=clawmanager-user-1
pod_name=clawreef-16-oc2gi-loc-221427
runtime_infra_status=ready
runtime_agent_status=online
runtime_openclaw_status=running
runtime_last_error=
```

## Pod readiness, restart, OOM, image digest

`kubectl wait --for=condition=Ready pod/clawreef-16-oc2gi-loc-221427 -n clawmanager-user-1 --timeout=240s` succeeded.

Pod status:

```text
pod_namespace=clawmanager-user-1
pod_name=clawreef-16-oc2gi-loc-221427
phase=Running
Pod Ready=True
pod_ip=10.42.0.95
node=k3d-clawmanager-server-0
qos=Guaranteed
container=desktop
container_ready=true
restart_count=0
oom_killed=false
state={"running":{"startedAt":"2026-05-07T14:14:31Z"}}
last_state={}
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
pod_imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
imageID_digest_matches_expected_index=true
```

Pod events:

```text
Scheduled to k3d-clawmanager-server-0
Kubelet pulled k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942 for the authorized pod creation
Created container desktop
Started container desktop
```

No OOMKilled event or failed container state was present for instance `16`.

Node capacity after the new pod was created:

```text
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
allocated_memory_percent=80
```

## Service, Endpoint, and EndpointSlice

Service:

```text
service_name=clawreef-16-oc2gi-loc-221427-svc
type=ClusterIP
cluster_ip=10.43.192.205
selector={"app":"clawreef","instance-id":"16"}
service_port name=http port=3001 targetPort=3001 protocol=TCP
service_port name=control-ui port=18789 targetPort=18789 protocol=TCP
```

New PVC:

```text
pvc=clawreef-16-pvc status=Bound capacity=20Gi storageClass=local-path
```

Endpoint:

```text
endpoints_name=clawreef-16-oc2gi-loc-221427-svc
endpoint_address ip=10.42.0.95 targetRef=Pod/clawreef-16-oc2gi-loc-221427
endpoint_port name=http port=3001 protocol=TCP
endpoint_port name=control-ui port=18789 protocol=TCP
```

EndpointSlice:

```text
endpointslice_name=clawreef-16-oc2gi-loc-221427-svc-cbqdr
addressType=IPv4
endpoint_addresses=10.42.0.95 ready=true serving=true targetRef=Pod/clawreef-16-oc2gi-loc-221427
endpointslice_port name=http port=3001 protocol=TCP
endpointslice_port name=control-ui port=18789 protocol=TCP
```

## Runtime HTTP smoke

Checks were run from inside the new running `desktop` container with `curl --noproxy '*'`.

```text
target=127.0.0.1:18789 curl_exit=0 http_code=200 size=3398
target=10.42.0.95:18789 curl_exit=0 http_code=200 size=3398
target=10.43.192.205:18789 curl_exit=0 http_code=200 size=3398
target=127.0.0.1:3001 curl_exit=0 http_code=400 size=264
target=10.42.0.95:3001 curl_exit=0 http_code=400 size=264
target=10.43.192.205:3001 curl_exit=0 http_code=400 size=264
```

18789 HTTP 200 summary: loopback `127.0.0.1:18789` HTTP 200, PodIP `10.42.0.95:18789` HTTP 200, ServiceIP `10.43.192.205:18789` HTTP 200.

Runtime `3001` listener smoke summary: loopback, PodIP, and ServiceIP were reachable with curl exit `0` and HTTP `400`; the test only requires recording the HTTP code.

## Running-container control-ui path and hashes

The running container contains `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

```text
control_ui_path_exists=true
  3398 /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
708145 /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
 42617 /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
 23258 /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
777418 total
```

Running-container file hashes:

```text
b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec  /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

All four running-container hashes match the expected localization image hashes.

## Secret hygiene

No token value, password value, key value, cookie value, credential, bearer value, auth header value, access URL plaintext, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content is recorded in this evidence.

## Explicit forbidden-actions confirmation

- no browser E2E
- no manual E2E
- no DevTools
- no Playwright
- no creation of a second new instance
- no deletion of any unlisted pod
- no deletion of any Service
- no deletion of any PVC
- no deletion or direct mutation of any database row
- no direct database access
- no deletion or direct mutation of any pre-existing instance record
- no session cleanup
- no asset cleanup
- no registry tag/content modification
- no manual image build/tag/push/pull command
- no container run/docker run/docker compose
- no k3d
- no Helm
- no trustedProxy/runtime auth patch
- no plugin/skill distribution
- no backend/frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework modification
- no existing artifact modification
- no existing evidence modification
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Verification commands

```bash
sed -n '1,360p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
rg -n "CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE|CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE|gtclaw-controlui-localization-20260507211942|sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54|sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e|deleted_pods|exactly_one_new_instance=true|Running|Ready=True|restart_count=0|oom_killed=false|18789|HTTP 200|/usr/local/lib/node_modules/openclaw/dist/control-ui|cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f|no browser E2E|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `sed -n '1,360p' ...` | `0` | Evidence content rendered for review through line 360. |
| `git diff --check -- ...` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdict markers, approval token, target tag, both expected digests, `deleted_pods`, `exactly_one_new_instance=true`, `Running`, `Ready=True`, `restart_count=0`, `oom_killed=false`, `18789`, `HTTP 200`, control-ui path, zh-CN hash, `no browser E2E`, `no passes:true`, `no Close`, and `no git stage/commit/push`. |
| secret-shape `rg` scan | `1` | No matches; `secret_shape_match_count=0`. |
| `git status --short -- ...` | `0` | Shows only this allowed new evidence file as untracked in the requested path scope. |
