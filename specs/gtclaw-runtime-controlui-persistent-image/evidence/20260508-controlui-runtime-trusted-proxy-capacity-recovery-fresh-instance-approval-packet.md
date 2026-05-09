# Control UI runtime trusted-proxy capacity recovery fresh-instance approval packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, approval packet
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE

This is an approval packet only. It did not perform cleanup, did not create a runtime instance, did not mutate Kubernetes resources, did not run browser E2E, did not build/tag/push/pull images, did not modify source or artifacts, and did not write longterm, Mem0, passes:true, Close, or git state.

## Dependencies

| Gate | Status used |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_BLOCKED | Blocked before creation because available request headroom was below one standard 2Gi runtime instance. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Final trusted-proxy runtime image exists with digest evidence. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_APPROVAL_PACKET_DONE | Fresh-instance scope and verification requirements are already approved as the target after capacity recovery. |

## Current blocker

```text
node=k3d-clawmanager-server-0
allocatable_memory=8024876Ki
allocatable_memory_mi=floor(8024876Ki / 1024)=7836Mi
allocated_memory_requests=6284Mi
available_request_headroom=1552Mi
required_standard_runtime_instance_memory=2048Mi
missing_headroom_for_one_2Gi_instance=496Mi
fresh_instance_blocked_before_creation=true
```

## Proposed next gate

CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

Requested approval token:

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE

The next gate must not proceed without this exact token.

## Read-only current runtime pod inventory

Current runtime pod inventory from read-only `kubectl`:

```text
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 instance_id=11 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-12-123 instance_id=12 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-13-0506-claw instance_id=13 phase=Pending ready=<none> restarts=<none> memory=4Gi node=<none> image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 instance_id=15 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427 instance_id=16 phase=Running ready=true restarts=0 memory=2Gi node=k3d-clawmanager-server-0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
```

No current runtime pod is using the final trusted-proxy target image.

## Proposed cleanup candidates

Cleanup must be pod-only and must prioritize historical disposable test runtime pods. The next gate may delete only the first eligible candidate needed to make enough request headroom for exactly one new standard 2Gi target instance.

| Order | Candidate | Instance | Request | Current status | Why candidate | Authorized object if approved |
| ---: | --- | --- | ---: | --- | --- | --- |
| 1 | `pod/clawreef-11-oc2gi-oa-131301` in `clawmanager-user-1` | `11 / oc2gi-oa-131301` | `2Gi` | `Running`, `Ready=true`, restart `0` | Historical origin-allowlist fresh-instance test runtime; non-target image; deleting this one pod should release enough request headroom. | This pod only. |
| 2 | `pod/clawreef-15-oc2gi-cp-150002` in `clawmanager-user-1` | `15 / oc2gi-cp-150002` | `2Gi` | `Running`, `Ready=true`, restart `0` | Historical control-ui persistence fresh-instance test runtime; non-target image; fallback only if candidate 1 is absent, not consuming scheduled request, or capacity remains insufficient after recheck. | This pod only. |

Non-candidates:

- `pod/clawreef-16-oc2gi-loc-221427`: do not touch; it is the current localization/auth-contract evidence target and should not be repurposed or cleaned under this approval.
- `pod/clawreef-12-123` and `pod/clawreef-13-0506-claw`: do not touch under this packet; they are Pending with `node=<none>` and do not release current node allocated request headroom in the observed blocker.
- Any system namespace workload: do not touch.

## Exact do-not-touch list

The next gate must not delete, modify, patch, retag, clean, or otherwise mutate:

- Services, including `clawreef-11-oc2gi-oa-131301-svc` and `clawreef-15-oc2gi-cp-150002-svc`;
- PVCs, including `clawreef-11-pvc` and `clawreef-15-pvc`;
- database rows;
- instance records;
- sessions;
- assets;
- registry tags;
- registry content;
- system image settings;
- system workloads;
- deployments;
- Helm releases;
- k3d cluster resources;
- old or current non-candidate runtime pods;
- `pod/clawreef-16-oc2gi-loc-221427`;
- source files or runtime artifacts;
- longterm project memory;
- git index or remote branches.

## Cleanup order and stop condition

The next gate must run a fresh read-only capacity preflight before any cleanup.

Cleanup order:

1. If request headroom is already at least `2048Mi`, do no cleanup and proceed to fresh instance creation.
2. If request headroom is below `2048Mi`, delete only candidate 1 pod: `clawmanager-user-1/pod/clawreef-11-oc2gi-oa-131301`.
3. Recompute node allocated request memory.
4. If request headroom is now at least `2048Mi`, stop cleanup immediately and proceed to fresh instance creation.
5. If candidate 1 is absent, not scheduled, does not release request memory, or the recheck still shows insufficient headroom, delete only candidate 2 pod: `clawmanager-user-1/pod/clawreef-15-oc2gi-cp-150002`.
6. Recompute node allocated request memory again.
7. If request headroom is now at least `2048Mi`, stop cleanup immediately and proceed to fresh instance creation.
8. If request headroom is still insufficient after these approved pod-only candidates, stop and return BLOCKED. Do not choose new candidates ad hoc.

Stop condition:

```text
stop_cleanup_when_available_request_headroom_mi >= 2048
maximum_candidate_pods_deleted=2
preferred_expected_candidate_pods_deleted=1
delete_services=false
delete_pvcs=false
delete_database_rows=false
delete_instance_records=false
delete_sessions=false
delete_assets=false
delete_registry_content=false
```

## Fresh instance scope

After capacity is sufficient, the next gate may create exactly one new standard 2Gi OpenClaw runtime instance through the normal ClawManager instance creation path.

Target runtime image:

```text
cluster_tag=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130
image_index_digest=sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010
linux_arm64_digest=sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb
memory_gb=2
type=openclaw
exactly_one_new_instance_required=true
```

Recommended instance name shape:

```text
oc2gi-tp-HHMMSS
```

Creation constraints:

- use `POST /api/v1/instances` through the normal ClawManager API path;
- use per-instance `image_registry` with the exact target cluster tag;
- record before/after instance lists and prove exactly one new ID;
- do not create a second instance if readiness fails;
- do not reuse old instance `16` as the success target;
- do not run browser E2E.

## Readiness verification plan

The next gate must record:

- Pod `Running`;
- `Ready=True`;
- desktop container ready;
- `restart_count=0`;
- `oom_killed=false`;
- pod image equals the target cluster tag;
- pod imageID/digest aligns with the target image index digest or linux/arm64 manifest digest;
- Service exposes `3001` and `18789`;
- EndpointSlice is ready/serving for the new pod;
- `18789` returns HTTP `200` from loopback, PodIP, and ServiceIP where feasible.

Running-container readback:

- `/usr/local/share/gtclaw/trusted-proxy-auth-contract` exists and is directory-executable;
- packaged verifier exits `0` against `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js`;
- bundle contains `isGtManagerMediatedControlUiAuth`;
- bundle contains `x-forwarded-prefix`;
- direct-client protections remain present: `device signature invalid`, `verifyDeviceSignature`, `resolveConnectAuthDecision`, `bootstrapTokenCandidate`, `verifyDeviceToken`, `shouldSkipControlUiPairing`;
- `/defaults/openclaw-agent/config.yaml` uses `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`;
- wrapper executable exists;
- sanitized config readback prints only keys, modes, booleans, and counts.

zh-CN hash proof:

| File | Expected sha256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

Browser/manual E2E remains out of scope for the next capacity/fresh-instance gate. It becomes appropriate only after readiness and running-container readback pass for the new target image instance.

## Verification commands for the next gate

The next gate must adapt names after creation and keep credential output suppressed.

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,ALLOCATABLE_CPU:.status.allocatable.cpu,ALLOCATABLE_MEMORY:.status.allocatable.memory,CAPACITY_MEMORY:.status.capacity.memory
kubectl describe nodes | sed -n '/Allocated resources:/,/Events:/p'
kubectl -n clawmanager-user-1 get pods -l app=clawreef -o 'custom-columns=NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,IMAGE:.spec.containers[0].image,MEMORY:.spec.containers[0].resources.requests.memory,NODE:.spec.nodeName' --sort-by=.metadata.creationTimestamp
```

If cleanup is required and approved by the exact token:

```bash
kubectl -n clawmanager-user-1 delete pod clawreef-11-oc2gi-oa-131301 --wait=true
kubectl describe nodes | sed -n '/Allocated resources:/,/Events:/p'
# Stop here if available request headroom is now >= 2048Mi.
kubectl -n clawmanager-user-1 delete pod clawreef-15-oc2gi-cp-150002 --wait=true
kubectl describe nodes | sed -n '/Allocated resources:/,/Events:/p'
```

Then create exactly one instance through the ClawManager API with the target `image_registry`, keeping credential values and auth headers out of output/evidence.

After creation:

```bash
kubectl -n clawmanager-user-1 wait --for=condition=Ready "pod/$POD" --timeout=240s
kubectl -n clawmanager-user-1 get pod "$POD" -o jsonpath='{.status.phase}{"\n"}{range .status.conditions[*]}{.type}={.status}{"\n"}{end}{range .status.containerStatuses[*]}{.name} ready={.ready} restarts={.restartCount} imageID={.imageID}{"\n"}{end}'
kubectl -n clawmanager-user-1 get svc "$SVC" -o jsonpath='{range .spec.ports[*]}{.name}{" "}{.port}{"->"}{.targetPort}{"/"}{.protocol}{"\n"}{end}'
kubectl -n clawmanager-user-1 get endpoints "$SVC" -o wide
kubectl -n clawmanager-user-1 get endpointslice -l "kubernetes.io/service-name=$SVC" -o jsonpath='{range .items[*].endpoints[*]}{.addresses} ready={.conditions.ready} serving={.conditions.serving}{"\n"}{end}'
```

Runtime `18789` and running-container readback:

```bash
POD_IP="$(kubectl -n clawmanager-user-1 get pod "$POD" -o jsonpath='{.status.podIP}')"
SVC_IP="$(kubectl -n clawmanager-user-1 get svc "$SVC" -o jsonpath='{.spec.clusterIP}')"
kubectl -n clawmanager-user-1 exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18789/
kubectl -n clawmanager-user-1 exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' "http://$POD_IP:18789/"
kubectl -n clawmanager-user-1 exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' "http://$SVC_IP:18789/"
kubectl -n clawmanager-user-1 exec "$POD" -c desktop -- sh -lc '
set -eu
test -d /usr/local/share/gtclaw/trusted-proxy-auth-contract
test -x /usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract
node /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs \
  /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js \
  /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
grep -q "isGtManagerMediatedControlUiAuth" /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
grep -q "x-forwarded-prefix" /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
grep -q "openclaw-gateway-with-gtmanager-auth-contract" /defaults/openclaw-agent/config.yaml
'
kubectl -n clawmanager-user-1 exec "$POD" -c desktop -- sha256sum \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

## Verification commands for this approval packet

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-capacity-recovery-fresh-instance-approval-packet.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-capacity-recovery-fresh-instance-approval-packet.md
rg -n "CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_FRESH_INSTANCE_APPROVAL_PACKET_DONE|APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_GATE|clawreef-11-oc2gi-oa-131301|clawreef-15-oc2gi-cp-150002|do-not-touch|Services|PVCs|database rows|instance records|sessions|assets|registry tags|stop_cleanup_when_available_request_headroom_mi >= 2048|exactly_one_new_instance_required=true|gtclaw-controlui-localization-trusted-proxy-20260508130130|readiness verification|no browser E2E|no cleanup|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-capacity-recovery-fresh-instance-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-capacity-recovery-fresh-instance-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-capacity-recovery-fresh-instance-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- ...capacity-recovery-fresh-instance-approval-packet.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null ...capacity-recovery-fresh-instance-approval-packet.md` | `1` | No output and no whitespace diagnostics; exit `1` is expected for a new file compared to `/dev/null`. |
| required marker `rg` scan | `0` | Required markers found, including verdict, approval token, cleanup candidates, do-not-touch scope, stop condition, target image, readiness plan, and forbidden actions. |
| sensitive/access URL shape scan | `1` | No matches; no token, password, key, cookie, bearer value, auth header value, or access URL plaintext detected. |
| `git status --short -- ...capacity-recovery-fresh-instance-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |

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
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
