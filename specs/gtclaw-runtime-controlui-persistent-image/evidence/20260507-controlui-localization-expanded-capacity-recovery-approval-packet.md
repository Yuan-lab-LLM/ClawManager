# Control-ui localization expanded capacity recovery approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED
- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE

User authorization background: user explicitly stated that more test runtime memory may be released and that previous runtime images were test images. This packet still performs no cleanup and only requests approval for a later gate.

## Verdict

CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_APPROVAL_PACKET_DONE

This document is an approval packet only. It does not delete or modify any pod, Service, PVC, instance, database row, session, asset, or registry tag. It does not create an instance, run browser/manual E2E, build/pull/push an image, close the feature, write longterm memory, stage, commit, or push git changes.

## Proposed next gate

Recommended next gate name:

`CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE`

The next gate must not proceed without that exact explicit approval token.

## Current blocker and capacity summary

The prior runtime deployment gate blocked because read-only preflight showed insufficient schedulable memory for one new standard 2Gi fresh instance:

```text
blocker=insufficient schedulable memory for one new 2Gi instance without unauthorized cleanup
remaining_memory_approx_mib=1552
required_new_standard_instance_memory_mib=2048
create_attempted=false
```

Fresh read-only node inventory for this expanded packet:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
allocated_memory_percent=80
non_terminated_pods=11
events=<none>
```

Capacity conclusion: current request headroom is still below one additional standard 2Gi runtime instance. At least one scheduled 2Gi runtime pod must be released, or equivalent capacity must be made available, before the localization 2Gi fresh instance can be created safely.

## Target image for retry

| Field | Value |
| --- | --- |
| CLUSTER_TAG | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| HOST_TAG | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| image index digest | `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54` |
| linux/arm64 digest | `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` |
| runtime control-ui root | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

## Current runtime pod and instance inventory

Instance IDs below are from Kubernetes `instance-id` labels and runtime naming. A ClawManager API instance list was not needed for this approval packet, avoiding any auth-bearing output.

| Pod | Instance ID | Phase | Ready | Memory request | Image | Node | Restarts | Service endpoint |
| --- | ---: | --- | --- | --- | --- | --- | ---: | --- |
| `clawreef-10-oc2gi-185707` | `10` | Running | true | `2Gi` | `gtclaw-controlui-persistent-bind-lan-auth-20260505162033` | `k3d-clawmanager-server-0` | `0` | `10.42.0.84:3001,18789` |
| `clawreef-11-oc2gi-oa-131301` | `11` | Running | true | `2Gi` | `gtclaw-controlui-persistent-origin-allowlist-20260506124656` | `k3d-clawmanager-server-0` | `0` | `10.42.0.86:3001,18789` |
| `clawreef-12-123` | `12` | Pending | none | `4Gi` | `dev-arm64-pkt09-20260414170434` | none | none | none |
| `clawreef-13-0506-claw` | `13` | Pending | none | `4Gi` | `dev-arm64-pkt09-20260414170434` | none | none | none |
| `clawreef-14-oc1gi-cp-143256` | `14` | Failed | false | `1Gi` | `gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` | `k3d-clawmanager-server-0` | `0` | not ready |
| `clawreef-15-oc2gi-cp-150002` | `15` | Running | true | `2Gi` | `gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` | `k3d-clawmanager-server-0` | `0` | `10.42.0.93:3001,18789` |

## Current Service and endpoint inventory

All listed Services are ClusterIP Services in namespace `clawmanager-user-1`. This packet does not approve deleting any Service.

| Service | Selector | ClusterIP | Ports | Endpoint status |
| --- | --- | --- | --- | --- |
| `clawreef-10-oc2gi-185707-svc` | `instance-id=10` | `10.43.10.209` | `3001,18789` | ready endpoints `10.42.0.84:3001,18789` |
| `clawreef-11-oc2gi-oa-131301-svc` | `instance-id=11` | `10.43.221.32` | `3001,18789` | ready endpoints `10.42.0.86:3001,18789` |
| `clawreef-12-123-svc` | `instance-id=12` | `10.43.219.118` | `3001,18789` | no endpoints |
| `clawreef-13-0506-claw-svc` | `instance-id=13` | `10.43.206.72` | `3001,18789` | no endpoints |
| `clawreef-14-oc1gi-cp-143256-svc` | `instance-id=14` | `10.43.70.248` | `3001,18789` | EndpointSlice has `10.42.0.91`, not ready |
| `clawreef-15-oc2gi-cp-150002-svc` | `instance-id=15` | `10.43.120.202` | `3001,18789` | ready endpoints `10.42.0.93:3001,18789` |
| `clawreef-5-gtclaw-fresh-20260504095843-svc` | `instance-id=5` | `10.43.47.127` | `3001,18789` | no endpoints |
| `clawreef-5-instance-5-svc` | `instance-id=5` | `10.43.30.23` | `18789,3001` | no endpoints |
| `clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724-svc` | `instance-id=9` | `10.43.36.89` | `3001` | no endpoints |

## Current PVC inventory

PVCs exist for historical runtime instances and must not be deleted in the proposed next gate.

| PVC | Namespace | Status | Capacity | Related instance |
| --- | --- | --- | --- | ---: |
| `clawreef-5-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `5` |
| `clawreef-9-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `9` |
| `clawreef-10-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `10` |
| `clawreef-11-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `11` |
| `clawreef-12-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `12` |
| `clawreef-13-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `13` |
| `clawreef-14-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `14` |
| `clawreef-15-pvc` | `clawmanager-user-1` | Bound | `20Gi` | `15` |

## Cleanup candidates

### Safe old disposable test pods

These are non-localization test runtime pods. The proposed next gate may delete these pod objects only, in this order, stopping as soon as enough capacity is available for one standard 2Gi localization fresh instance.

| Order | Pod | Namespace | Instance ID | Reason | Expected request relief |
| ---: | --- | --- | ---: | --- | --- |
| 1 | `pod/clawreef-10-oc2gi-185707` | `clawmanager-user-1` | `10` | old bind-lan-auth disposable 2Gi test image; not localization target | `2Gi` |
| 2 | `pod/clawreef-11-oc2gi-oa-131301` | `clawmanager-user-1` | `11` | old origin-allowlist disposable 2Gi test image; not localization target | `2Gi` |
| 3 | `pod/clawreef-15-oc2gi-cp-150002` | `clawmanager-user-1` | `15` | prior persistence verification 2Gi test image; non-localization target, keep until after older tests if possible | `2Gi` |

### Stale failed or pending pods

These objects are stale runtime test pods. Pending pods do not currently consume node allocatable request because they are unscheduled, but deleting them may reduce queue noise. Failed pod `14` is not useful for the localization retry and is a stale failed test workload. The proposed next gate may delete these pod objects only after or alongside the safe old disposable test pods if the operator wants expanded cleanup.

| Pod | Namespace | Instance ID | Status | Reason |
| --- | --- | ---: | --- | --- |
| `pod/clawreef-12-123` | `clawmanager-user-1` | `12` | Pending, no node, no endpoints | stale unscheduled 4Gi test pod |
| `pod/clawreef-13-0506-claw` | `clawmanager-user-1` | `13` | Pending, no node, no endpoints | stale unscheduled 4Gi test pod |
| `pod/clawreef-14-oc1gi-cp-143256` | `clawmanager-user-1` | `14` | Failed, not ready | stale failed/OOM 1Gi test pod |

## Proposed exact pod-only delete list

If explicitly approved, the proposed next gate may delete only these pod objects:

```text
clawmanager-user-1/pod/clawreef-10-oc2gi-185707
clawmanager-user-1/pod/clawreef-11-oc2gi-oa-131301
clawmanager-user-1/pod/clawreef-15-oc2gi-cp-150002
clawmanager-user-1/pod/clawreef-12-123
clawmanager-user-1/pod/clawreef-13-0506-claw
clawmanager-user-1/pod/clawreef-14-oc1gi-cp-143256
```

Stale-state guardrails:

- First run a fresh read-only capacity and pod-state check.
- If enough capacity is already available, skip cleanup and create exactly one standard 2Gi fresh instance.
- Do not delete any pod that is absent, no longer a runtime test pod, or now uses the localization target image.
- Stop deleting after enough capacity is recovered unless the explicit expanded approval is interpreted as "clear all listed test pods"; record the interpretation before acting.
- If a Service, PVC, database row, instance record, session, asset, registry tag, or unlisted pod must be removed to proceed, block and request a separate approval packet.

## Resources not to touch

The proposed next gate must not delete or modify:

- any Service, including all `clawreef-*-svc` Services.
- any PVC, including `clawreef-5-pvc`, `clawreef-9-pvc`, and `clawreef-10-pvc` through `clawreef-15-pvc`.
- any database row or ClawManager instance record.
- any session, asset, or runtime user data.
- any registry tag, registry manifest, registry blob, or image content.
- any pod not listed in the proposed exact pod-only delete list.
- any `clawmanager-system`, `kube-system`, or `default` namespace workload.
- backend, frontend, deployments, docs, longterm, AgentTeam, UnifiedFramework, existing artifact, or existing evidence files.

## Proposed next gate target

After capacity is available, the proposed next gate may create exactly one new standard 2Gi fresh runtime instance through the normal ClawManager API/runtime path using:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`

The proposed next gate must verify:

- Pod Running and Ready.
- `restart_count=0`.
- `oom_killed=false`.
- Service ports `3001` and `18789`.
- Endpoint/EndpointSlice ready and serving.
- control-ui `18789` HTTP 200 on loopback, PodIP, and ServiceIP.
- runtime `3001` listener smoke with HTTP code recorded.
- running-container path `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- running-container file hashes matching the reviewed localization image.

Expected control-ui hashes:

| Runtime target path | Expected SHA-256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## E2E boundary

The proposed next gate still must not run browser/manual E2E, DevTools, Playwright, browser storage cleanup, browser cache cleanup, browser cookie cleanup, or manual browser verification.

Browser/manual E2E remains a later independent approval gate after runtime-level readiness, `18789` HTTP 200, and running-container file/hash evidence exist.

## Readonly evidence and inventory checked

Readonly evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-capacity-recovery-standard-2gi-fresh-instance-approval-packet.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md`

Readonly inventory commands:

```bash
kubectl describe node k3d-clawmanager-server-0 | sed -n '/Allocatable:/,/Events:/p'
kubectl get pods -n clawmanager-user-1 -o custom-columns='NAME:.metadata.name,INSTANCE:.metadata.labels.instance-id,PHASE:.status.phase,READY:.status.containerStatuses[*].ready,MEM_REQ:.spec.containers[*].resources.requests.memory,CPU_REQ:.spec.containers[*].resources.requests.cpu,IMAGE:.spec.containers[*].image,NODE:.spec.nodeName,RESTARTS:.status.containerStatuses[*].restartCount'
kubectl get svc -n clawmanager-user-1 -o custom-columns='NAME:.metadata.name,TYPE:.spec.type,CLUSTER_IP:.spec.clusterIP,SELECTOR:.spec.selector,PORTS:.spec.ports[*].port,TARGET_PORTS:.spec.ports[*].targetPort'
kubectl get endpoints -n clawmanager-user-1 -o wide
kubectl get endpointslice -n clawmanager-user-1 -o wide
kubectl get pvc -A -o wide
```

## Verification commands

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-approval-packet.md
rg -n "CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_APPROVAL_PACKET_DONE|CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_APPROVAL_PACKET_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_GATE|cleanup candidates|2Gi|fresh instance|gtclaw-controlui-localization-20260507211942|no browser E2E|no database|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-approval-packet.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-approval-packet.md
```

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically: no pod deletion or modification, no Service deletion or modification, no PVC deletion or modification, no instance creation, deletion, or modification, no database row deletion or modification, no session or asset deletion or modification, no registry tag deletion or modification, no direct database access or mutation, no image build/tag/push/pull, no browser E2E, no DevTools, no Playwright, no k3d, no Helm, no Mem0 write, no passes:true, no Close, no longterm write-back, and no git stage/commit/push.

No token value, password value, key value, cookie value, bearer value, authorization header plaintext, access URL plaintext, private key, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was recorded.
