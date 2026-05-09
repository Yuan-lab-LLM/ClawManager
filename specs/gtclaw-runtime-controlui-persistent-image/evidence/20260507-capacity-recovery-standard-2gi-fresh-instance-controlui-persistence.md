# Capacity Recovery / Standard 2Gi Fresh Instance Gate - Control UI Persistence

Worker: CapacityRecoveryStandard2GiFreshInstanceControlUIPersistenceWorker

Topology: serial

Verdict: `CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_DONE`

Not `CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_BLOCKED`: capacity was recovered by affecting only the explicitly listed historical disposable pod candidate, exactly one new standard 2Gi fresh disposable instance was created, and the target runtime image stayed Running/Ready with service ports, runtime HTTP/control-ui reachability, imageID digest, and running-container control-ui path verified.

Gate date: 2026-05-07 Asia/Shanghai

This evidence is opened before capacity mutation so the cleanup candidate is recorded before any object is affected.

## Approval And Dependency Gates

| Gate | Status used |
| --- | --- |
| User approval | `APPROVE_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_GATE` |
| Capacity recovery / standard 2Gi approval packet | `CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE` |
| Previous fresh instance runtime deployment gate | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_BLOCKED` |
| Runtime image build/tag/push gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE` |

## Target Image

| Field | Value |
| --- | --- |
| target image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` |
| expected image index digest | `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908` |
| expected linux/arm64 digest | `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a` |

## Initial Capacity State

Read-only node capacity:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
capacity_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
```

Capacity conclusion: available schedulable memory was below a standard 2Gi request. Capacity recovery was required before creating the new 2Gi fresh disposable instance.

Initial runtime pod state:

```text
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 instance_id=9 phase=Running ready=True memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 instance_id=10 phase=Running ready=True memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 instance_id=11 phase=Running ready=True memory=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-12-123 instance_id=12 phase=Pending ready= memory=4Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-13-0506-claw instance_id=13 phase=Pending ready= memory=4Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-14-oc1gi-cp-143256 instance_id=14 phase=Failed ready=False memory=1Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712 terminated_reason=OOMKilled
```

Initial Service state showed existing instance Services and ports only; no Service for the future 2Gi instance existed at this point.

## Cleanup Candidates

Capacity was insufficient, so this gate selected a specific cleanup candidate before taking action:

| Candidate | Namespace | Instance ID | Status | Memory request | Image | Safety reason |
| --- | --- | ---: | --- | --- | --- | --- |
| `pod/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724` | `clawmanager-user-1` | `9` | `Running`, `Ready=True`, restart `0` | `2Gi` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` | Historical disposable fresh-instance test pod from 2026-05-05 evidence, non-target image, not selected as current target verification workload. Deleting only this pod releases the needed 2Gi request while avoiding generalized cleanup. |

Explicit non-candidates:

- `clawreef-10-oc2gi-185707`: old disposable but left untouched because one 2Gi pod candidate is enough.
- `clawreef-11-oc2gi-oa-131301`: old disposable but left untouched because one 2Gi pod candidate is enough.
- `clawreef-12-123` and `clawreef-13-0506-claw`: Pending and not consuming node allocatable memory, so cleanup is not needed for the 2Gi scheduling target.
- `clawreef-14-oc1gi-cp-143256`: Failed/OOM target-image 1Gi pod but not consuming enough active request to solve the 2Gi scheduling need.

Planned capacity action: delete only `pod/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724` in namespace `clawmanager-user-1`. Do not delete its Service, PVC, database row, session, assets, tags, registry content, or any other object.

## Capacity Action Performed

Capacity action:

```text
kubectl delete pod clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -n clawmanager-user-1 --wait=true
result=deleted
```

Objects affected:

```text
deleted=pod/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
namespace=clawmanager-user-1
instance_id=9
```

Objects not affected:

```text
service_not_deleted=clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724-svc
pvc_not_deleted=true
database_row_not_deleted=true
session_not_deleted=true
assets_not_deleted=true
tag_not_deleted=true
registry_content_not_cleaned=true
```

Post-action capacity:

```text
allocated_cpu_requests=2200m
allocated_memory_requests=4236Mi
capacity_recovery_result=available memory request headroom was sufficient for one standard 2Gi fresh instance
```

## Fresh 2Gi Instance Creation

Exactly one new standard 2Gi fresh disposable instance was created through the normal ClawManager API path.

```text
api_instances_before_count=7
api_instances_before_ids=5,9,10,11,12,13,14
create_http_status=201
created_id=15
created_name=oc2gi-cp-150002
status=creating
type=openclaw
memory_gb=2
pod_namespace=clawmanager-user-1
pod_name=clawreef-15-oc2gi-cp-150002
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
api_instances_after_count=8
api_instances_after_ids=5,9,10,11,12,13,14,15
exactly_one_new_instance=true
token_recorded=false
password_recorded=false
key_recorded=false
```

API status after pod readiness:

```text
api_instance id=15 name=oc2gi-cp-150002 status=running type=openclaw memory_gb=2
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod_namespace=clawmanager-user-1
pod_name=clawreef-15-oc2gi-cp-150002
```

Authentication material for the normal backend API calls was used transiently with output suppressed. No token value, password value, key value, cookie value, credential, bearer value, auth header value, or access URL plaintext is recorded in this evidence.

## Pod Ready / Restart / OOM / Digest

`kubectl wait --for=condition=Ready pod/clawreef-15-oc2gi-cp-150002 -n clawmanager-user-1 --timeout=240s` succeeded. A later stability check after the previous 1Gi OOM window still showed the pod Running and Ready.

```text
pod_namespace=clawmanager-user-1
pod_name=clawreef-15-oc2gi-cp-150002
phase=Running
Pod Ready=True
container_ready=true
pod_ip=10.42.0.93
node=k3d-clawmanager-server-0
qos=Guaranteed
container=desktop
restart_count=0
restart=0
oom_killed=false
state={"running":{"startedAt":"2026-05-07T07:00:07Z"}}
last_state={}
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod_imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
imageID_digest_matches_expected_index=true
```

Registry digest read:

```text
registry_content_digest=sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
registry_linux_arm64_digest=sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a
```

Pod events:

```text
Scheduled to k3d-clawmanager-server-0
Container image already present on machine
Created container desktop
Started container desktop
```

No OOMKilled event or failed container state was present for instance `15`.

## Service Ports And Endpoints

Service ports:

```text
service_name=clawreef-15-oc2gi-cp-150002-svc
type=ClusterIP
cluster_ip=10.43.120.202
selector={"app":"clawreef","instance-id":"15"}
service_port name=http port=3001 targetPort=3001 protocol=TCP
service_port name=control-ui port=18789 targetPort=18789 protocol=TCP
```

Endpoint and EndpointSlice:

```text
endpoints_name=clawreef-15-oc2gi-cp-150002-svc
endpoint_address ip=10.42.0.93 targetRef=Pod/clawreef-15-oc2gi-cp-150002
endpoint_port name=http port=3001 protocol=TCP
endpoint_port name=control-ui port=18789 protocol=TCP

endpointslice_name=clawreef-15-oc2gi-cp-150002-svc-cg9zc
addressType=IPv4
endpoints=10.42.0.93 ready=true serving=true
ports=http:3001/TCP,control-ui:18789/TCP
```

## Runtime HTTP / Control UI Reachability

Reachability checks were run from inside the new running container with `curl --noproxy '*'`.

```text
target=127.0.0.1:18789 curl_exit=0 http_code=200 size=3398
target=10.42.0.93:18789 curl_exit=0 http_code=200 size=3398
target=10.43.120.202:18789 curl_exit=0 http_code=200 size=3398
target=127.0.0.1:3001 curl_exit=0 http_code=400 size=264
target=10.42.0.93:3001 curl_exit=0 http_code=400 size=264
target=10.43.120.202:3001 curl_exit=0 http_code=400 size=264
```

Result: runtime control-ui on `18789` is reachable through loopback, PodIP, and ServiceIP with HTTP `200`. Desktop runtime HTTP port `3001` is reachable through loopback, PodIP, and ServiceIP; it returns HTTP `400`, matching prior non-browser listener-smoke behavior for this runtime.

## Running-Container Control UI Path

The running container contains `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

```text
control_ui_path_exists=true
/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html 3398 bytes
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js 708145 bytes
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js 42617 bytes
/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js 23255 bytes
```

Running-container file hashes:

```text
b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec  /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

## Secret Hygiene

No token value, password value, key value, cookie value, credential, bearer value, auth header value, access URL plaintext, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content is recorded in this evidence.

## Explicit Negatives

- no generalized cleanup
- no Ready active user workload cleanup
- no old session cleanup
- no old asset cleanup
- no old tag cleanup
- no registry cleanup
- no build/tag/push/pull
- no deploy/restart
- no browser E2E
- no Chrome DevTools
- no Playwright
- no browser storage/cache/cookie cleanup
- no backend source modification
- no frontend source modification
- no runtime-startup source modification
- no control-ui source modification
- no assembly artifact modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Commands

Required checks for this evidence:

```bash
rg -n "CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_DONE|CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_BLOCKED|sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908|sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a|Pod Ready|restart|service ports|/usr/local/lib/node_modules/openclaw/dist/control-ui|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
secret-shape scan with matched values suppressed
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdict markers, image index digest, linux/arm64 digest, `Pod Ready`, restart, service ports, `/usr/local/lib/node_modules/openclaw/dist/control-ui`, `no browser E2E`, `no passes:true`, and `no Close`. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md` | `0` | No whitespace errors. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |

## Completed Boundary

Completed gate boundaries:

- no browser E2E
- no Chrome DevTools
- no Playwright
- no browser storage/cache/cookie cleanup
- no build/tag/push/pull
- no registry cleanup
- no old session cleanup
- no old asset cleanup
- no old tag cleanup
- no backend/frontend/runtime-startup/control-ui source modification
- no assembly artifact modification
- no deployment/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
