# Isolated 2Gi+ Fresh Instance Rerun After Cluster Recovery

Date: 2026-05-05

## Verdict

ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED

The cluster readiness preflight passed after `CLUSTER_READINESS_RECOVERY_DONE`, so this gate created one isolated 2Gi fresh instance through the existing ClawManager API. The pod reached Ready and used the approved image digest, but the required control-ui listener checks failed:

- the Service exposes `3001` but does not expose `18789`;
- `18789` is unreachable on `127.0.0.1`, PodIP, and ServiceIP;
- the runtime log shows OpenClaw gateway initially binding to `0.0.0.0:18789`, then exiting after an assertion that the generated label is longer than 63 bytes.

The gate stopped as BLOCKED. No second fresh instance was created, no Service was patched, and no pod was patched.

## Dependency Gates

| Gate | Evidence | Status used |
| --- | --- | --- |
| Cluster Readiness Recovery Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md` | `CLUSTER_READINESS_RECOVERY_DONE`; `clawmanager` `1/1`; server-0/serverlb running; node Ready; registry `localhost:5001 /v2/` `200` |
| Previous Isolated 2Gi Fresh Instance Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md` | `ISOLATED_2GI_FRESH_INSTANCE_BLOCKED`; no fresh instance created because cluster readiness had failed |
| Isolated 2Gi Fresh Instance Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-approval-packet.md` | `ISOLATED_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE` |
| Registry Recovery Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation.md` | `REGISTRY_RECOVERY_IMPLEMENTATION_DONE`; registry restored and approved image push succeeded |

## Published Image Identity

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| approved image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| approved linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

## Exact Commands Run

Read-only preflight:

```sh
k3d cluster list
k3d cluster get clawmanager
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPorts={{json .NetworkSettings.Ports}}'
docker inspect k3d-clawmanager-serverlb --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPorts={{json .NetworkSettings.Ports}}'
kubectl cluster-info --request-timeout=5s
kubectl get nodes --request-timeout=5s
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
```

API command shapes used for existing ClawManager flow:

```sh
curl -sk -X POST https://localhost:30443/api/v1/auth/login -H 'Content-Type: application/json' --data-raw '<credential values omitted>'
curl -sk https://localhost:30443/api/v1/auth/me -H 'Authorization: Bearer <omitted>'
curl -sk https://localhost:30443/api/v1/system-settings/images -H 'Authorization: Bearer <omitted>'
curl -sk https://localhost:30443/api/v1/instances?limit=100 -H 'Authorization: Bearer <omitted>'
curl -sk https://localhost:30443/api/v1/users/1/quota -H 'Authorization: Bearer <omitted>'
curl -sk -X POST https://localhost:30443/api/v1/instances -H 'Authorization: Bearer <omitted>' -H 'Content-Type: application/json' --data-raw '<sanitized create payload>'
curl -sk https://localhost:30443/api/v1/instances/9 -H 'Authorization: Bearer <omitted>'
```

Fresh instance metadata and read-only verification:

```sh
kubectl wait --for=condition=Ready pod/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -n clawmanager-user-1 --timeout=180s
kubectl get pod clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -n clawmanager-user-1 -o json
kubectl get svc -n clawmanager-user-1 -l instance-id=9 -o json
kubectl get endpoints -n clawmanager-user-1 -l instance-id=9 -o wide
kubectl exec -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -c desktop -- sh -lc '<read-only curl listener checks>'
kubectl exec -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -c desktop -- sh -lc '<read-only process/listener inspection>'
kubectl logs -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -c desktop --tail=120
kubectl exec -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -c desktop -- sh -lc '<read-only startup artifact hash/config check>'
kubectl get events -n clawmanager-user-1 --field-selector involvedObject.name=clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 --sort-by=.lastTimestamp
```

Verification commands:

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md
rg -n "ISOLATED_2GI_FRESH_INSTANCE_RERUN_DONE|ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED|gtclaw-controlui-persistent-bind-lan-auth-20260505162033|sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9|2Gi|fresh instance|cluster readiness|1/1|Ready|OOM|imageID|18789|3001|PodIP|ServiceIP|restore|no browser E2E|no manual pod patch|no manual Service patch|no kubectl cp|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md
rg -n -i '<secret-shape pattern>' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md
```

## Read-only Preflight Result

### Cluster readiness

```text
k3d cluster list:
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   1/1       0/0      true

k3d cluster get clawmanager:
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   1/1       0/0      true
```

### Docker server containers

```text
k3d-clawmanager-server-0:
StateStatus=running
StateRunning=true
StateExitCode=0

k3d-clawmanager-serverlb:
StateStatus=running
StateRunning=true
StateExitCode=0
Ports={"30443/tcp":[{"HostIp":"0.0.0.0","HostPort":"30443"},{"HostIp":"::","HostPort":"30443"}],"6443/tcp":[{"HostIp":"0.0.0.0","HostPort":"58334"}]}
```

### Kubernetes read-only access

```text
kubectl cluster-info --request-timeout=5s: exit 0
kubectl get nodes --request-timeout=5s:
NAME                       STATUS   ROLES                  AGE   VERSION
k3d-clawmanager-server-0   Ready    control-plane,master   19d   v1.33.6+k3s1
```

### Registry health

```text
curl_exit=0 target=localhost:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

### Image digest preflight

Both host and in-cluster tags resolved locally to the approved digest:

```text
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Preflight result: PASS.

## Runtime Image Setting Handling

Previous global `openclaw` runtime image setting was read before creation:

```text
instance_type=openclaw
display_name=OpenClaw ARM Local Registry
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
is_enabled=true
```

Global temporary runtime image setting was not used. The existing ClawManager create-instance API supports a per-instance full image through `image_registry`, so this gate used the approved in-cluster tag directly in the create request:

```text
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Restore action: not applicable because no global setting was changed.

Restore verification: post-create `GET /api/v1/system-settings/images` still showed the same previous `openclaw` setting:

```text
instance_type=openclaw
display_name=OpenClaw ARM Local Registry
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
is_enabled=true
```

## Fresh Instance Creation Result

Create request used:

```text
name=gtclaw-fresh-bind-lan-auth-20260505-175724
type=openclaw
cpu_cores=1
memory_gb=2
disk_gb=20
os_type=openclaw
os_version=local
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Sanitized API result:

```text
create_http_status=201
instance id=9
name=gtclaw-fresh-bind-lan-auth-20260505-175724
status=creating
type=openclaw
memory_gb=2
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
```

After sync:

```text
instance id=9
name=gtclaw-fresh-bind-lan-auth-20260505-175724
status=running
type=openclaw
memory_gb=2
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
```

## Pod Metadata

`kubectl wait` result:

```text
pod/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 condition met
kubectl_wait_ready_exit=0
```

Pod metadata:

```text
namespace=clawmanager-user-1
pod name=clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
container name=desktop
phase=Running
Ready=True
PodIP=10.42.0.77
node=k3d-clawmanager-server-0
restart count=0
OOMKilled=false
pod image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Kubernetes events:

```text
Scheduled to k3d-clawmanager-server-0
Pulled image k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
Created container desktop
Started container desktop
```

## Service Metadata

Service metadata:

```text
service name=clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724-svc
type=ClusterIP
ServiceIP=10.43.36.89
selector={"app":"clawreef","instance-id":"9"}
ports=[{"name":"http","port":3001,"targetPort":3001,"protocol":"TCP"}]
exposes_18789=false
exposes_3001=true
```

Endpoints:

```text
clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724-svc   10.42.0.77:3001
```

Required Service exposure check:

- `3001`: PASS, exposed by Service and endpoint.
- `18789`: FAIL, not exposed by Service and no endpoint.

## Listener Reachability

Read-only listener checks were run from inside the fresh pod with proxy bypass enabled.

Control-ui listener `18789`:

```text
curl_exit=7 target=127.0.0.1:18789 status=000 remote_ip= remote_port=-1 err=Failed to connect
curl_exit=7 target=10.42.0.77:18789 status=000 remote_ip= remote_port=-1 err=Failed to connect
curl_exit=7 target=10.43.36.89:18789 status=000 remote_ip= remote_port=-1 err=Failed to connect
```

Desktop listener `3001`:

```text
curl_exit=0 target=127.0.0.1:3001 status=400 remote_ip=127.0.0.1 remote_port=3001 err=
curl_exit=0 target=10.42.0.77:3001 status=400 remote_ip=10.42.0.77 remote_port=3001 err=
curl_exit=0 target=10.43.36.89:3001 status=400 remote_ip=10.43.36.89 remote_port=3001 err=
```

Interpretation:

- `3001` desktop route basic reachability is not broken at loopback, PodIP, or ServiceIP.
- `18789` is not reachable at loopback, PodIP, or ServiceIP.
- Gateway pod-facing reachability is not proven.

## Startup Artifact Checks

Read-only hash/config check inside the pod:

```text
347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e  /defaults/openclaw-agent/config.yaml
53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda  /etc/services.d/openclaw-agent/run
```

Startup command context:

```yaml
openclaw_command:
  - openclaw
  - gateway
  - run
  - --bind
  - lan
  - --auth
  - token
openclaw_config_path: /config/.openclaw/openclaw.json
```

Sanitized log excerpt:

```text
[gateway] starting HTTP server...
[canvas] host mounted at http://0.0.0.0:18789/__openclaw__/canvas/
[gateway] Gateway is binding to a non-loopback address.
[gateway] ready
[openclaw] Uncaught exception: AssertionError [ERR_ASSERTION]: Label cannot be longer than 63 bytes (clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 (OpenClaw))
```

Interpretation:

- The reviewed startup artifact is present in the pod and includes `--bind lan` / `--auth token`.
- The runtime attempted pod-facing bind on `0.0.0.0:18789`.
- The OpenClaw gateway did not remain available because the process exited after the label-length assertion.
- Because `18789` is down, the startup artifact effect is BLOCKED rather than accepted.

## Blocker Classification

Primary blocker: fresh runtime control-ui listener failure.

Specific blockers:

- Service port exposure incomplete: Service exposes `3001` but not `18789`.
- OpenClaw gateway process exited after startup due label length assertion.
- `18789` listener unreachable on loopback, PodIP, and ServiceIP.

Non-blockers:

- cluster readiness passed: `1/1`.
- pod scheduling passed.
- pod Ready passed.
- 2Gi fresh instance capacity was allocated.
- no OOM was observed.
- imageID matches the approved digest.
- desktop `3001` basic reachability passed on loopback, PodIP, and ServiceIP.
- global runtime image setting did not need restore because it was not changed.

## Recommendation

Recommended next approval packet:

`Fresh Instance / Runtime Listener Follow-up Approval Packet`

That packet should request explicit approval for the minimum next step, likely one of:

1. Create one additional isolated fresh instance with a shorter unique name to avoid the OpenClaw DNS label length crash, while preserving the same approved image and 2Gi capacity.
2. Separately verify whether the live ClawManager backend image/service creation path includes the Service `18789` exposure change before retrying.

Do not proceed to browser E2E until a fresh instance proves:

- Service exposes both `18789` and `3001`;
- `18789` is reachable from PodIP and ServiceIP;
- `3001` remains basically reachable;
- gateway is pod-facing and not loopback-only.

## Explicit Negatives / No Forbidden Action Proof

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no kubectl cp write
- no build/tag/push/pull
- no docker build
- no docker rebuild
- no docker tag
- no docker push
- no docker pull
- no registry mutation
- no cluster/server/serverlb recovery
- no k3d cluster create/delete/start/stop
- no backend change
- no frontend change
- no runtime source change
- no deployments change
- no docs change
- no longterm write
- no AgentTeam change
- no spec.md / plan.md / tasks.md change
- no existing evidence change
- no reviewed startup artifact change
- no `/tmp/gtclaw-runtime-patch/**` change
- no `/tmp/gtclaw-startup-source-inspect-*/**` change
- no secret, cookie, credential, token value, or access URL plaintext
- no Mem0 write
- no passes:true
- no Close
