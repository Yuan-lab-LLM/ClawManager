# Control UI runtime trusted-proxy expanded capacity recovery fresh-instance approval packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, expanded cleanup approval packet
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE

This is an approval packet only. It converts the user authorization that historical test runtime pods may be released more broadly into an exact pod-only cleanup boundary. It did not perform cleanup, did not create a runtime instance, did not mutate Kubernetes resources, did not run browser E2E, did not build/tag/push/pull images, did not edit source/artifacts/old evidence, and did not write longterm, Mem0, passes:true, Close, or git state.

## Dependency gates

| Gate | Status used |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE | Narrow pod-only capacity recovery approval packet exists; this packet expands candidate scope. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_BLOCKED | Fresh instance was blocked before creation because available request headroom was below one standard 2Gi runtime instance. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Final trusted-proxy runtime image tag and digest evidence exists. |

## Current blocker

```text
node=k3d-clawmanager-server-0
allocatable_memory=8024876Ki
allocatable_memory_mi=floor(8024876Ki / 1024)=7836Mi
allocated_memory_requests=6284Mi
available_request_headroom=1552Mi
required_one_standard_2Gi_runtime_instance=2048Mi
missing_headroom_for_one_2Gi_instance=496Mi
expanded_cleanup_requested=true
```

## Proposed next gate

CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

Requested approval token:

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

The next gate must not proceed without this exact approval token.

## Current runtime pod inventory

Read-only `kubectl` inventory:

```text
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 instance_id=11 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-12-123 instance_id=12 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-13-0506-claw instance_id=13 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 instance_id=15 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427 instance_id=16 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

No current runtime pod is using the final trusted-proxy target image.

## Expanded cleanup scope

Cleanup scope for the next gate is pod-only. If approved, the next gate may delete multiple listed candidate pods, but it must not select any pod that is not listed in this packet.

Authorized candidate list:

| Order | Candidate pod | Instance | Status | Request | Expected current headroom effect | Rationale |
| ---: | --- | --- | --- | ---: | --- | --- |
| 1 | `clawmanager-user-1/pod/clawreef-11-oc2gi-oa-131301` | `11 / oc2gi-oa-131301` | Running, Ready, scheduled | `2Gi` | Should release scheduled node request memory. | Historical origin-allowlist fresh-instance test runtime; non-target image. |
| 2 | `clawmanager-user-1/pod/clawreef-15-oc2gi-cp-150002` | `15 / oc2gi-cp-150002` | Running, Ready, scheduled | `2Gi` | Should release scheduled node request memory. | Historical control-ui persistence fresh-instance test runtime; non-target image. |
| 3 | `clawmanager-user-1/pod/clawreef-16-oc2gi-loc-221427` | `16 / oc2gi-loc-221427` | Running, Ready, scheduled | `2Gi` | Should release scheduled node request memory. | Historical localization/auth-contract test runtime; user expanded authorization allows releasing test runtime pods. |
| 4 | `clawmanager-user-1/pod/clawreef-12-123` | `12 / 123` | Pending, unscheduled | `4Gi` | May not release current node allocated request headroom because `node=<none>`. | Stale test runtime pod; eligible for test residue cleanup only after scheduled candidates. |
| 5 | `clawmanager-user-1/pod/clawreef-13-0506-claw` | `13 / 0506-claw` | Pending, unscheduled | `4Gi` | May not release current node allocated request headroom because `node=<none>`. | Stale test runtime pod; eligible for test residue cleanup only after scheduled candidates. |

Pending pods note:

`clawreef-12-123` and `clawreef-13-0506-claw` are Pending and currently show `node=<none>`. Deleting them may clean stale test residue and remove pending scheduling pressure, but it may not increase current node allocated request headroom. They must be treated as cleanup candidates, not as guaranteed memory-recovery candidates.

No unlisted pod may be deleted under the next gate.

## Recommended cleanup order

The expanded cleanup gate should run a fresh read-only capacity preflight before any cleanup.

Recommended order:

1. Recompute node allocatable/requested memory and runtime pod inventory.
2. Delete Running/scheduled 2Gi historical test pod `clawreef-11-oc2gi-oa-131301`.
3. Recompute node allocated request memory.
4. If enough headroom exists for one standard 2Gi target instance and stale pending cleanup is not explicitly needed, stop cleanup and create exactly one target instance.
5. If expanded cleanup is still desired or capacity remains constrained, delete Running/scheduled 2Gi historical test pod `clawreef-15-oc2gi-cp-150002`.
6. Recompute node allocated request memory.
7. If expanded cleanup is still desired or capacity remains constrained, delete Running/scheduled 2Gi historical test pod `clawreef-16-oc2gi-loc-221427`.
8. Recompute node allocated request memory.
9. If expanded cleanup is still desired for stale test residue, delete Pending stale pod `clawreef-12-123`.
10. If expanded cleanup is still desired for stale test residue, delete Pending stale pod `clawreef-13-0506-claw`.
11. Stop cleanup. Do not choose additional pods.
12. Create exactly one new standard 2Gi target trusted-proxy runtime instance only after the cleanup phase stops and capacity is sufficient.

Stop conditions:

```text
cleanup_scope=pod-only
expanded_cleanup_allowed=true
maximum_candidate_pods_deleted=5
candidate_pods_must_be_listed_in_this_packet=true
delete_unlisted_pods=false
stop_after_listed_candidates=true
create_exactly_one_new_2Gi_instance_after_capacity=true
create_second_instance=false
browser_e2e=false
```

## Do-not-touch list

The next gate must not delete, modify, patch, retag, clean, or otherwise mutate:

- Services;
- PVCs;
- database rows;
- instance records;
- sessions;
- assets;
- registry tags/content;
- system workloads;
- deployments;
- Helm resources;
- k3d cluster resources;
- source files;
- runtime artifacts;
- old evidence files;
- git index/remotes.

The cleanup approval is only for listed pods, not their Services, PVCs, database rows, instance records, sessions, assets, or registry content.

## Fresh instance scope

After cleanup and capacity recheck, the next gate may create exactly one new standard 2Gi OpenClaw runtime through the normal ClawManager instance creation path.

Target runtime image:

```text
cluster_tag=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
image_index_digest=sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010
linux_arm64_digest=sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb
type=openclaw
memory_gb=2
exactly_one_new_standard_2Gi_instance=true
```

Creation constraints:

- use `POST /api/v1/instances` through the normal ClawManager API path;
- use per-instance `image_registry` with the exact target cluster tag;
- record before/after instance lists and prove exactly one new ID;
- do not create a second instance;
- do not run browser E2E;
- do not record token/password/key/cookie/bearer/auth header/access URL plaintext.

## Readiness plan

The next gate must collect:

- Pod Running;
- Ready=True;
- desktop container ready;
- restart_count=0;
- oom_killed=false;
- pod image equals target cluster tag;
- imageID/digest aligns with the target image index digest or linux/arm64 manifest digest;
- Service exposes `3001` and `18789`;
- EndpointSlice ready/serving;
- `18789` HTTP 200 from loopback, PodIP, and ServiceIP where feasible;
- running-container trusted-proxy patch proof;
- startup wrapper/config proof;
- zh-CN Control UI hash proof;
- forbidden-action confirmation and sensitive scan result.

Browser/manual E2E remains out of scope for the next expanded capacity/fresh-instance gate. A later browser gate is appropriate only after readiness and running-container readback pass for the new target image instance.

## Verification commands for this approval packet

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-fresh-instance-approval-packet.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-fresh-instance-approval-packet.md
rg -n "CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE|APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE|expanded cleanup|pod-only|clawreef-11-oc2gi-oa-131301|clawreef-15-oc2gi-cp-150002|clawreef-16-oc2gi-loc-221427|clawreef-12-123|clawreef-13-0506-claw|do-not-touch|Services|PVCs|database rows|instance records|sessions|assets|registry tags/content|exactly one new standard 2Gi|gtclaw-controlui-localization-trusted-proxy-20260508130130|no browser E2E|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-fresh-instance-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-fresh-instance-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-expanded-capacity-recovery-fresh-instance-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- ...expanded-capacity-recovery-fresh-instance-approval-packet.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null ...expanded-capacity-recovery-fresh-instance-approval-packet.md` | `1` | No output and no whitespace diagnostics; exit `1` is expected for a new file compared to `/dev/null`. |
| required marker `rg` scan | `0` | Required markers found, including verdict, approval token, expanded cleanup, pod-only, all five candidate pods, do-not-touch scope, exactly one 2Gi instance, target image, and forbidden actions. |
| sensitive/access URL shape scan | `1` | No matches; no token, password, key, cookie, bearer value, auth header value, or access URL plaintext detected. |
| `git status --short -- ...expanded-capacity-recovery-fresh-instance-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |

## Sensitive scan result

```text
sensitive_shape_scan_exit=1
sensitive_shape_match_count=0
```

## Forbidden actions confirmation

This approval packet performed only read-only review and wrote this one file.

It did not perform:

- no cleanup
- no instance creation, deletion, or modification
- no pod creation, deletion, or modification
- no Service creation, deletion, or modification
- no PVC creation, deletion, or modification
- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no browser E2E
- no DevTools
- no Playwright
- no image build/tag/push/pull
- no source modification
- no artifact modification
- no old evidence modification
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
