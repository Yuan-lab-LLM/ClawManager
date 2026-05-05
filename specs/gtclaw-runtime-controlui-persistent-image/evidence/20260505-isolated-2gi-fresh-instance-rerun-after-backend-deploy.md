# Isolated 2Gi Fresh Instance Rerun After Backend Deploy Gate

Date: 2026-05-05

Worker: FreshInstanceRuntimeVerificationWorker

Topology: serial

## Verdict

ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_DONE

This gate created exactly one new isolated 2Gi OpenClaw fresh instance through the existing ClawManager API and verified the backend deploy fixed the prior runtime listener blockers.

No browser E2E, Chrome DevTools MCP, Playwright, build/tag/push/pull, backend deploy/restart, runtime image rebuild, frontend rebuild/deploy, registry mutation, database migration, manual pod patch, manual Service patch, `kubectl cp`, cleanup/delete instance 9, second fresh instance, `passes:true`, Close, longterm write-back, or Mem0 write occurred.

## Dependency Gates

| Gate | Evidence | Status used |
| --- | --- | --- |
| Isolated 2Gi Fresh Instance Rerun After Backend Deploy Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy-approval-packet.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_APPROVAL_PACKET_DONE`; user explicitly approved this gate |
| Control Plane Backend Build/Deploy Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy.md` | `CONTROL_PLANE_BACKEND_BUILD_DEPLOY_DONE`; running backend image `clawmanager:control-plane-backend-gtclaw-20260505183733`; `Deployment/clawmanager-app` Ready `1/1`; `/healthz` `200 ok` |
| Backend Runtime Listener Follow-up Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE`; source fixed OpenClaw short hostname/label and Service `18789` exposure path |
| Runtime Startup Artifact Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`; expected config sha256 `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e`; expected run sha256 `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

## Exact Commands Run

Read rules, evidence, and API shape:

```sh
rg -n "ClawManager|gtclaw|fresh instance|control-plane|runtime" /Users/eduardogan/.codex/memories/MEMORY.md
sed -n '1,220p' AGENTS.md
sed -n '1,220p' .specify/memory/constitution.md
sed -n '1,180p' backend/AGENTS.md
sed -n '1,220p' /Users/eduardogan/.codex/skills/verification-before-completion/SKILL.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy-approval-packet.md
sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md
rg -n "auth/login|instances|CreateInstance|image_registry|memory_gb|quota|system-settings/images|Authorization" backend/internal backend/cmd deployments -g '*.go' -g '*.yaml' -g '*.yml'
sed -n '34,90p' backend/internal/services/instance_service.go
sed -n '66,150p' backend/internal/handlers/instance_handler.go
sed -n '1,140p' backend/internal/handlers/auth_handler.go
sed -n '1,140p' backend/internal/services/auth_service.go
kubectl get secrets -n clawmanager-system -o custom-columns=NAME:.metadata.name,TYPE:.type --no-headers
kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='<env names and secret refs only>'
sed -n '138,260p' backend/internal/handlers/instance_handler.go
sed -n '97,360p' backend/internal/services/instance_service.go
sed -n '1,120p' backend/internal/services/instance_runtime.go
sed -n '1040,1065p' backend/internal/services/instance_service.go
sed -n '58,95p' backend/internal/services/k8s/service_service.go
```

Read-only preflight:

```sh
k3d cluster list && k3d cluster get clawmanager
kubectl get nodes --request-timeout=5s
kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='name=... image=... replicas=... ready=...'
kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide
kubectl get svc -n clawmanager-system
curl -sk --max-time 10 --noproxy '*' https://localhost:30443/healthz -w '\nhealthz_http=%{http_code} err=%{errormsg}\n'
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'host_tag_id=... repo_digests=...'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'cluster_tag_id=... repo_digests=...'
kubectl get pods -A -l app=clawreef -o json | jq -r '<count and sanitized pod summary>'
```

Two read-only formatting attempts failed before corrected preflight output was collected:

```text
kubectl get deploy ... | jq ...: exit 3, jq quoting issue
docker image inspect ... --format '{{join .RepoDigests ","}}': exit 1, Docker template type issue
```

They did not mutate any environment state and were replaced by the corrected commands above before the fresh instance was created.

Create exactly one fresh instance through the ClawManager API:

```sh
PASS="$(awk -F'"' '/DefaultAdminPassword =/ {print $2; exit}' backend/internal/services/default_passwords.go)"
LOGIN_PAYLOAD="$(jq -nc --arg username 'admin' --arg password "$PASS" '{username:$username,password:$password}')"
LOGIN_RESPONSE="$(curl -sk --max-time 15 --noproxy '*' -X POST https://localhost:30443/api/v1/auth/login -H 'Content-Type: application/json' --data-raw "$LOGIN_PAYLOAD")"
TOKEN="$(printf '%s' "$LOGIN_RESPONSE" | jq -r '.data.access_token // empty')"
curl -sk --max-time 15 --noproxy '*' 'https://localhost:30443/api/v1/instances?limit=100' -H 'Authorization: Bearer <omitted>'
PAYLOAD="$(jq -nc --arg name 'oc2gi-185707' --arg image 'k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033' '{name:$name,type:"openclaw",cpu_cores:1,memory_gb:2,disk_gb:20,gpu_enabled:false,gpu_count:0,os_type:"openclaw",os_version:"local",image_registry:$image}')"
curl -sk --max-time 60 --noproxy '*' -X POST https://localhost:30443/api/v1/instances -H 'Authorization: Bearer <omitted>' -H 'Content-Type: application/json' --data-raw "$PAYLOAD"
curl -sk --max-time 15 --noproxy '*' 'https://localhost:30443/api/v1/instances?limit=100' -H 'Authorization: Bearer <omitted>'
```

Post-create verification:

```sh
kubectl wait --for=condition=Ready pod/clawreef-10-oc2gi-185707 -n clawmanager-user-1 --timeout=240s
kubectl get pod clawreef-10-oc2gi-185707 -n clawmanager-user-1 -o json | jq -r '<sanitized pod status fields>'
kubectl get svc -n clawmanager-user-1 -l instance-id=10 -o json
kubectl get endpoints -n clawmanager-user-1 clawreef-10-oc2gi-185707-svc -o json
kubectl get endpointslices.discovery.k8s.io -n clawmanager-user-1 -l kubernetes.io/service-name=clawreef-10-oc2gi-185707-svc -o json
kubectl get events -n clawmanager-user-1 --field-selector involvedObject.name=clawreef-10-oc2gi-185707 --sort-by=.lastTimestamp
kubectl exec -n clawmanager-user-1 clawreef-10-oc2gi-185707 -c desktop -- env POD_IP=10.42.0.80 SVC_IP=10.43.10.209 sh -lc '<curl listener checks with --noproxy *>'
kubectl exec -n clawmanager-user-1 clawreef-10-oc2gi-185707 -c desktop -- sh -lc '<sha256sum startup artifact files and grep command-mode markers without printing secret values>'
kubectl logs -n clawmanager-user-1 clawreef-10-oc2gi-185707 -c desktop --tail=300
kubectl exec -n clawmanager-user-1 clawreef-10-oc2gi-185707 -c desktop -- sh -lc '<hostname and decorated label byte-length check>'
curl -sk --max-time 15 --noproxy '*' 'https://localhost:30443/api/v1/instances/10' -H 'Authorization: Bearer <omitted>'
kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='image=... ready=...'
curl -sk --max-time 10 --noproxy '*' https://localhost:30443/healthz -w '\nhealthz_http=%{http_code} err=%{errormsg}\n'
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'host_tag_id=... repo_digests=...'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'cluster_tag_id=... repo_digests=...'
kubectl get pods -A -l app=clawreef -o json | jq -r '<count and sanitized pod summary>'
```

## Read-only Preflight Results

| Check | Result |
| --- | --- |
| `k3d cluster list` and `k3d cluster get clawmanager` | `clawmanager` `1/1`, load balancer `true` |
| `kubectl get nodes --request-timeout=5s` | `k3d-clawmanager-server-0` Ready |
| backend deployment identity | `Deployment/clawmanager-app`, namespace `clawmanager-system`, image `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| backend deployment readiness | replicas `1`, ready `1`, updated `1`, available `1`, generation `11`, observed generation `11` |
| backend pod | `clawmanager-app-55d7847759-xn8kh`, Ready `1/1`, Running, restarts `0` |
| `/healthz` | body `ok`, HTTP `200` |
| runtime host tag digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| runtime in-cluster tag digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| app=clawreef runtime pods before creation | count `1`: `clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724` |

Preflight passed, so the gate proceeded to create exactly one new fresh instance.

## Fresh Instance Creation Result

API authentication succeeded; token value was omitted and not written to evidence.

API instance list before creation:

```text
api_instances_before_count=5 ids=1,2,3,5,9
```

Create request used:

```text
name=oc2gi-185707
type=openclaw
cpu_cores=1
memory_gb=2
disk_gb=20
gpu_enabled=false
gpu_count=0
os_type=openclaw
os_version=local
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Sanitized API creation result:

```text
create_http_status=201
create_success=true
message=Instance created successfully
id=10
name=oc2gi-185707
status=creating
type=openclaw
memory_gb=2
pod_namespace=clawmanager-user-1
pod_name=clawreef-10-oc2gi-185707
```

API instance list after creation:

```text
api_instances_after_count=6 ids=1,2,3,5,9,10
```

Exactly one new API instance was created: `10`.

## Pod Verification

`kubectl wait` result:

```text
pod/clawreef-10-oc2gi-185707 condition met
```

Pod status:

| Field | Value |
| --- | --- |
| namespace | `clawmanager-user-1` |
| pod | `clawreef-10-oc2gi-185707` |
| phase | `Running` |
| Ready condition | `True` |
| PodIP | `10.42.0.80` |
| node | `k3d-clawmanager-server-0` |
| pod spec hostname | `clawreef-10` |
| QoS | `Guaranteed` |
| container | `desktop` |
| container ready | `true` |
| restart count | `0` |
| OOMKilled | `false` |
| image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |

Events for the new pod were only normal scheduling/pull/create/start events. No OOM event was present.

## Hostname And Label Verification

Runtime-observed hostname:

```text
runtime_hostname=clawreef-10
decorated_label=clawreef-10 (OpenClaw)
decorated_label_bytes=22
within_63=true
```

The hostname/label fix is proven for this fresh instance: `clawreef-10 (OpenClaw)` is `22` bytes, which is within the `63 bytes` limit.

## Service, Endpoint, And EndpointSlice Verification

Service:

| Field | Value |
| --- | --- |
| service | `clawreef-10-oc2gi-185707-svc` |
| type | `ClusterIP` |
| ServiceIP | `10.43.10.209` |
| selector | `app=clawreef`, `instance-id=10` |

Service ports:

| Name | Port | TargetPort | Protocol |
| --- | ---: | ---: | --- |
| `http` | `3001` | `3001` | `TCP` |
| `control-ui` | `18789` | `18789` | `TCP` |

The Service exposes both `3001` and `18789`.

Endpoints:

```text
endpoints_name=clawreef-10-oc2gi-185707-svc
endpoint_address ip=10.42.0.80 targetRef=Pod/clawreef-10-oc2gi-185707
endpoint_port name=http port=3001 protocol=TCP
endpoint_port name=control-ui port=18789 protocol=TCP
```

EndpointSlice:

```text
EndpointSlice name=clawreef-10-oc2gi-185707-svc-qp4bp addressType=IPv4 endpoints=10.42.0.80 ports=http:3001,control-ui:18789
```

## Listener Verification

Listener checks were run from inside the `desktop` container with `curl --noproxy '*'` to avoid proxying PodIP or ServiceIP requests.

| Target | Host | Port | curl exit | HTTP code | Result |
| --- | --- | ---: | ---: | ---: | --- |
| loopback `18789` | `127.0.0.1` | `18789` | `0` | `200` | reachable |
| PodIP `18789` | `10.42.0.80` | `18789` | `0` | `200` | reachable |
| ServiceIP `18789` | `10.43.10.209` | `18789` | `0` | `200` | reachable |
| loopback `3001` | `127.0.0.1` | `3001` | `0` | `400` | reachable |
| PodIP `3001` | `10.42.0.80` | `3001` | `0` | `400` | reachable |
| ServiceIP `3001` | `10.43.10.209` | `3001` | `0` | `400` | reachable |

The required `18789` PodIP and ServiceIP listener checks passed. Port `3001` remained reachable on loopback, PodIP, and ServiceIP.

## Startup Artifact Verification

Inside the new pod:

```text
config_sha=347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e expected=347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e match=true
run_sha=53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda expected=53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda match=true
config_contains_bind_lan=true
config_contains_auth_token_mode=true
token_value_printed=false
```

Startup artifact hashes match the reviewed runtime startup artifact. The config contains `--bind lan` and `--auth token` mode markers. No token value was printed.

## Log Verification

Log checks:

```text
label_63_assertion_count=0
hostname_label_assertion_count=0
bind_18789_line_count=4
```

The OpenClaw log has no `Label cannot be longer than 63 bytes` assertion and no hostname/label assertion. Sanitized bind evidence includes the runtime exposing OpenClaw control UI at `0.0.0.0:18789`.

## API Status Read

Sanitized API status after pod readiness:

```text
api_instance id=10 name=oc2gi-185707 status=running type=openclaw memory_gb=2 image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 pod_namespace=clawmanager-user-1 pod_name=clawreef-10-oc2gi-185707
api_runtime infra_status=ready agent_status=online openclaw_status=running last_error=
```

No token, cookie, credential, secret, or access URL plaintext was printed.

## Exactly One New Fresh Instance Proof

Runtime pods before creation:

```text
count=1
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
```

Runtime pods after creation:

```text
count=2
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running podIP=10.42.0.80 readyContainers=1 restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running podIP=10.42.0.77 readyContainers=1 restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

The count increased from `1` to `2`, and the only new fresh instance/pod is instance `10` / `clawreef-10-oc2gi-185707`.

Instance 9 was not modified, cleaned up, or deleted.

## Backend And Runtime Digest Post-check

Backend after the fresh instance creation:

```text
image=clawmanager:control-plane-backend-gtclaw-20260505183733 replicas=1 ready=1 updated=1 available=1 generation=11 observedGeneration=11
healthz=200 ok
```

Runtime image digest after the fresh instance creation:

```text
host_tag_id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
cluster_tag_id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Runtime image digest remained unchanged:

```text
sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

## Blockers

None.

The previous blockers are resolved for the new isolated 2Gi fresh instance:

- Service missing `18789`: resolved; Service exposes `3001` and `18789`.
- `18789` unreachable on PodIP/ServiceIP: resolved; both PodIP and ServiceIP returned curl exit `0` and HTTP `200`.
- hostname/label assertion persists: resolved; `clawreef-10 (OpenClaw)` is `22` bytes and logs show zero `63 bytes` label assertions.
- startup artifact hash drift: not present; both expected sha256 hashes matched.

## Recommendation

Recommended next gate:

`Browser E2E Approval/Gate`

Listener/hash verification is fully covered by this gate for the new fresh instance. Do not set `passes:true`, Close, or write longterm until browser E2E completes and the user explicitly approves final write-back.

## Explicit Negatives

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no build/tag/push/pull
- no backend deploy
- no backend restart
- no runtime image rebuild
- no frontend rebuild
- no frontend deploy
- no registry mutation
- no database migration
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no cleanup/delete instance 9
- no second fresh instance
- no modification to backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence
- no token value, cookie value, credential, secret, or access URL plaintext
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md` | `0` | Requested path-limited whitespace check reported no errors. |
| `rg -n "ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_DONE\|ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_BLOCKED\|clawmanager:control-plane-backend-gtclaw-20260505183733\|sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9\|2Gi\|fresh instance\|Ready\|OOM\|imageID\|18789\|3001\|PodIP\|ServiceIP\|EndpointSlice\|hostname\|label\|63 bytes\|0.0.0.0:18789\|347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e\|53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda\|no browser E2E\|no passes:true\|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md` | `0` | Required markers found. |
| secret-shape scan on this new evidence | `1` | No matches. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy-approval-packet.md` | `0` | Shows only the new rerun evidence and its approval packet in this status scope. |
| additional no-index whitespace scan for the new untracked evidence file | `0` | No whitespace diagnostics. |
