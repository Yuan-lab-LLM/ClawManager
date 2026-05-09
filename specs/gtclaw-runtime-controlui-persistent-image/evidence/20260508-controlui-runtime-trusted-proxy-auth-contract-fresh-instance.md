# CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, fresh-instance gate
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_BLOCKED: insufficient allocatable memory request headroom before creation; no fresh instance was created.

This gate stopped at read-only capacity preflight as required. It did not create a runtime instance, did not run browser E2E, did not run DevTools or Playwright, did not build/tag/push/pull images, did not modify source or artifacts, and did not perform cleanup.

## Approval token used

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE

## Dependency gates

| Gate | Status used |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_APPROVAL_PACKET_DONE | Fresh-instance approval packet written and reviewed. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Target trusted-proxy runtime image built/pushed with digest evidence. |
| CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED | Backend delivery complete, runtime readiness still required. |
| CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE | Backend bridge sanitizer implemented; runtime gate remains required. |
| CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE | Browser localization rerun recorded known auth-contract blocker only. |

## Target runtime image

| Field | Value |
| --- | --- |
| cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130` |
| image index digest | `sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010` |
| linux/arm64 manifest digest | `sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb` |

## Capacity preflight result

Read-only node capacity:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
capacity_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
```

Capacity calculation:

```text
allocatable_memory_mi=floor(8024876Ki / 1024)=7836Mi
allocated_memory_requests=6284Mi
available_request_headroom=1552Mi
required_standard_instance_memory=2048Mi
capacity_sufficient_for_one_standard_2Gi_instance=false
```

Block decision:

```text
create_instance_attempted=false
reason=available request headroom was below one standard 2Gi runtime instance before creation
```

## Current runtime pods

Read-only runtime pod inventory:

```text
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 instance_id=11 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-12-123 instance_id=12 phase=Pending ready= restarts= memory=4Gi node= image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-13-0506-claw instance_id=13 phase=Pending ready= restarts= memory=4Gi node= image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 instance_id=15 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427 instance_id=16 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

Current runtime pods do not include the target trusted-proxy image. Old instance `16 / oc2gi-loc-221427` was not reused as a success target.

## Service and PVC inventory

Read-only Service inventory:

```text
service=clawreef-5-gtclaw-fresh-20260504095843-svc ports=3001/TCP,18789/TCP
service=clawreef-5-instance-5-svc ports=18789/TCP,3001/TCP
service=clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724-svc ports=3001/TCP
service=clawreef-10-oc2gi-185707-svc ports=3001/TCP,18789/TCP
service=clawreef-11-oc2gi-oa-131301-svc ports=3001/TCP,18789/TCP
service=clawreef-12-123-svc ports=3001/TCP,18789/TCP
service=clawreef-13-0506-claw-svc ports=3001/TCP,18789/TCP
service=clawreef-15-oc2gi-cp-150002-svc ports=3001/TCP,18789/TCP
service=clawreef-16-oc2gi-loc-221427-svc ports=3001/TCP,18789/TCP
```

Read-only PVC inventory:

```text
pvc=clawreef-5-pvc status=Bound capacity=20Gi
pvc=clawreef-9-pvc status=Bound capacity=20Gi
pvc=clawreef-10-pvc status=Bound capacity=20Gi
pvc=clawreef-11-pvc status=Bound capacity=20Gi
pvc=clawreef-12-pvc status=Bound capacity=20Gi
pvc=clawreef-13-pvc status=Bound capacity=20Gi
pvc=clawreef-15-pvc status=Bound capacity=20Gi
pvc=clawreef-16-pvc status=Bound capacity=20Gi
```

No Service or PVC was created, deleted, or modified.

## Instance before/after list

Because capacity was insufficient, this gate stopped before API login and before `POST /api/v1/instances`.

Observed runtime-instance list before creation attempt:

```text
before_runtime_instance_ids=11,12,13,15,16
before_running_runtime_instance_ids=11,15,16
target_success_instance_id=none
```

After list:

```text
after_instance_list_not_collected=true
after_reason=no instance creation was attempted after capacity block
exactly_one_new_instance=false
```

The success condition `exactly_one_new_instance=true` is not claimed in this evidence.

## Fresh instance creation

Creation status:

```text
create_http_status=not_attempted
created_id=none
created_name=none
created_memory_gb=none
created_image_registry=none
no second instance=true
```

No ClawManager API create request was sent because capacity preflight failed.

## Readiness

Runtime readiness was not reached because no fresh instance was created.

Required future success markers not claimed in this blocked gate:

```text
Pod Running=not_reached
Ready=True=not_reached
desktop_container_ready=not_reached
restart_count=0 not_reached
oom_killed=false not_reached
imageID_digest_matches_target=not_reached
Service exposes 3001 and 18789=not_reached
EndpointSlice ready/serving=not_reached
18789 loopback/PodIP/ServiceIP HTTP 200=not_reached
```

## Running-container readback

Running-container readback was not reached because no fresh instance was created.

Required future success markers not claimed in this blocked gate:

```text
/usr/local/share/gtclaw/trusted-proxy-auth-contract exists=not_reached
directory_executable=not_reached
packaged_verifier_exit_0=not_reached
isGtManagerMediatedControlUiAuth=not_reached
x-forwarded-prefix=not_reached
device signature invalid=not_reached
verifyDeviceSignature=not_reached
resolveConnectAuthDecision=not_reached
bootstrapTokenCandidate=not_reached
verifyDeviceToken=not_reached
shouldSkipControlUiPairing=not_reached
/defaults/openclaw-agent/config.yaml wrapper proof=not_reached
/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract executable=not_reached
sanitized_config_readback=not_reached
```

No token, password, key, cookie, bearer value, auth header value, or access URL was read or recorded.

## zh-CN hash proof

zh-CN running-container hash proof was not reached because no fresh instance was created.

Required future expected hashes:

```text
zh-CN hash proof=not_reached
index.html expected=b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec
assets/index-M4TNVXB3.js expected=d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648
assets/i18n-B06L7jQN.js expected=3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63
assets/zh-CN-B26mMdbY.js expected=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

## Browser/manual E2E gate decision

Browser/manual E2E is not appropriate yet.

Reason:

- no fresh/current runtime instance is running the final trusted-proxy runtime image;
- no Pod Running/Ready evidence exists for the target image;
- no running-container trusted-proxy patch proof exists for the target image;
- no startup wrapper/config proof exists from a running container;
- no zh-CN running-container hash proof exists for the target image.

The next useful action is a separate capacity/cleanup approval packet naming an exact cleanup candidate or capacity expansion path. This gate does not authorize cleanup.

## Commands executed

Read-only capacity and inventory commands:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,ALLOCATABLE_CPU:.status.allocatable.cpu,ALLOCATABLE_MEMORY:.status.allocatable.memory,CAPACITY_MEMORY:.status.capacity.memory
kubectl describe nodes | sed -n '/Allocated resources:/,/Events:/p'
kubectl -n clawmanager-user-1 get pods -l app=clawreef -o 'custom-columns=NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,IMAGE:.spec.containers[0].image,MEMORY:.spec.containers[0].resources.requests.memory,NODE:.spec.nodeName' --sort-by=.metadata.creationTimestamp
kubectl -n clawmanager-user-1 get svc --sort-by=.metadata.creationTimestamp
kubectl -n clawmanager-user-1 get pvc --sort-by=.metadata.creationTimestamp
kubectl -n clawmanager-user-1 get pods -l app=clawreef -o jsonpath='{range .items[*]}{.metadata.name}{" instance-id="}{.metadata.labels.instance-id}{" phase="}{.status.phase}{" ready="}{.status.containerStatuses[0].ready}{" restarts="}{.status.containerStatuses[0].restartCount}{" image="}{.spec.containers[0].image}{" memory="}{.spec.containers[0].resources.requests.memory}{" node="}{.spec.nodeName}{"\n"}{end}'
```

No mutating command was run.

## Verification commands

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance.md
rg -n "CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE|gtclaw-controlui-localization-trusted-proxy-20260508130130|sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010|exactly_one_new_instance=true|Pod Running|Ready=True|restart_count=0|oom_killed=false|18789|isGtManagerMediatedControlUiAuth|x-forwarded-prefix|zh-CN|no browser E2E|no second instance|no cleanup|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- ...fresh-instance.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null ...fresh-instance.md` | `1` | No output and no whitespace diagnostics; exit `1` is expected for a new file compared to `/dev/null`. |
| required marker `rg` scan | `0` | Required markers found, including gate/verdict, target image tag, index digest, success markers marked not reached, 18789, trusted-proxy patch markers, zh-CN, and forbidden actions. |
| sensitive/access URL shape scan | `1` | No matches; no token, password, key, cookie, bearer value, auth header value, or access URL plaintext was detected. |
| `git status --short -- ...fresh-instance.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |

## Sensitive scan result

```text
sensitive_shape_scan_exit=1
sensitive_shape_match_count=0
```

## Forbidden actions confirmation

- no browser E2E
- no DevTools
- no Playwright
- no second instance
- no old instance modification
- no old pod modification
- no old Service modification
- no old PVC modification
- no database row modification
- no session modification
- no asset modification
- no direct kubectl mutation
- no cleanup
- no image build/tag/push/pull
- no backend source or artifact edits
- no frontend source or artifact edits
- no runtime source or artifact edits
- no database direct access or mutation
- no registry cleanup
- no storage/cache/cookie cleanup
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
