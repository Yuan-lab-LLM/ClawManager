# Control-ui localization fresh instance runtime deployment evidence

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation / Fresh Instance Runtime Deployment

Gate: CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET

## Verdict

CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED: read-only capacity preflight showed insufficient schedulable memory for exactly one new standard 2Gi fresh runtime instance without cleanup, and this gate does not authorize deleting or modifying old instance, pod, service, PVC, database row, registry tag, or any generalized cleanup target.

No fresh instance was created. No ClawManager create request was sent.

## Target image

| Field | Value |
| --- | --- |
| CLUSTER_TAG | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| HOST_TAG | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| image index digest | `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54` |
| linux/arm64 digest | `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` |
| runtime target | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

Readonly image metadata check:

```text
Name: localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
MediaType: application/vnd.oci.image.index.v1+json
Digest: sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
linux/arm64 manifest digest: sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e
attestation manifest digest: sha256:40a50f65fd60d98e9bfc9f6e860cd9af49eb26eec92a0354b7d0da74b5765045
```

## Readonly capacity preflight

Command scope: read-only `kubectl describe node` and `kubectl get pods/services/endpoints/endpointslices`.

Node capacity summary:

```text
node=k3d-clawmanager-server-0
allocatable_cpu=14
allocatable_memory=8024876Ki
allocated_cpu_requests=3200m
allocated_memory_requests=6284Mi
allocated_memory_percent=80
events=<none>
```

Capacity arithmetic:

```text
allocatable_memory_approx_mib=7836
allocated_memory_requests_mib=6284
remaining_memory_approx_mib=1552
required_new_standard_instance_memory=2Gi
required_new_standard_instance_memory_mib=2048
capacity_result=insufficient_without_cleanup
```

Active scheduled 2Gi runtime pods already consuming allocatable memory:

```text
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running ready=true memory_request=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 phase=Running ready=true memory_request=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
pod=clawmanager-user-1/clawreef-15-oc2gi-cp-150002 phase=Running ready=true memory_request=2Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
```

Other runtime pods observed:

```text
pod=clawmanager-user-1/clawreef-12-123 phase=Pending ready=<none> memory_request=4Gi node=<none>
pod=clawmanager-user-1/clawreef-13-0506-claw phase=Pending ready=<none> memory_request=4Gi node=<none>
pod=clawmanager-user-1/clawreef-14-oc1gi-cp-143256 phase=Failed ready=false memory_request=1Gi image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
```

Service and endpoint preflight observed existing runtime Services, including prior Services for old runtime instances. This gate did not modify any Service, Endpoint, EndpointSlice, pod, PVC, instance, database row, or registry tag.

## Create decision

Capacity rule applied:

- If no cleanup is needed, continue with exactly one standard 2Gi fresh instance.
- If capacity is insufficient and cleanup, deletion, database mutation, or generalized cleanup would be required, block.

Decision:

```text
create_attempted=false
create_http_status=not_attempted
created_id=none
created_name=none
pod_namespace=none
pod_name=none
service_name=none
exactly_one_new_instance=false
blocker=insufficient schedulable memory for one new 2Gi instance without unauthorized cleanup
```

Because no instance was created, the following runtime-level checks are not available in this gate:

- before/after API instance count and instance IDs for a created instance.
- new instance id/name/type/memory_gb/image_registry/pod_namespace/pod_name.
- Pod phase Running and Ready=True.
- container_ready, restart_count=0, oom_killed=false, last_state, and new-pod events.
- pod image reference and imageID digest comparison against expected image index digest.
- new Service containing ports `3001` and `18789`.
- new Endpoint or EndpointSlice pointing to a new pod and ready/serving.
- control-ui `18789` HTTP 200 from loopback, PodIP, and ServiceIP.
- runtime `3001` listener smoke.
- running container control-ui path and file/hash checks under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

## Expected file hashes for next unblocked gate

The target image build/tag/push evidence already proved the pushed linux/arm64 image contains these target paths and hashes. They still require running-container confirmation in a later unblocked runtime deployment gate.

| Runtime target path | Expected SHA-256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## Evidence commands run

Readonly reference evidence:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment-approval-packet.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
```

Readonly preflight:

```bash
kubectl describe node k3d-clawmanager-server-0 | sed -n '/Allocatable:/,/Events:/p'
kubectl get pods -A -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[*].ready,MEM_REQ:.spec.containers[*].resources.requests.memory,CPU_REQ:.spec.containers[*].resources.requests.cpu,IMAGE:.spec.containers[*].image,NODE:.spec.nodeName'
kubectl get svc,endpoints,endpointslice -A
docker buildx imagetools inspect "localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942"
```

Final checks to run after writing this file:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
rg -n "CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_DONE|CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE|gtclaw-controlui-localization-20260507211942|sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54|sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e|capacity_result=insufficient_without_cleanup|create_attempted=false|/usr/local/lib/node_modules/openclaw/dist/control-ui|cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f|no browser E2E|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
```

## Secret hygiene

No token value, password value, key value, cookie value, credential, bearer value, auth header value, access URL plaintext, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content is recorded in this evidence.

## Forbidden actions confirmation

Forbidden actions were not executed in this gate. Specifically: no browser E2E, no manual E2E, no DevTools, no Playwright, no creation of more than one new instance, no instance creation at all, no cleanup, no deletion or modification of old instance, pod, service, PVC, database row, or registry tag, no direct database access or mutation, no image build/tag/push/pull, no container run, no docker run, no docker compose, no k3d, no Helm, no trustedProxy patch, no runtime auth patch, no plugin, no skill distribution, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing artifact modification, no existing evidence modification, no Mem0 write, no passes:true, no Close, no longterm write-back, and no git stage/commit/push.
