# Isolated 2Gi+ Fresh Instance Gate Evidence

Date: 2026-05-05

Verdict: ISOLATED_2GI_FRESH_INSTANCE_BLOCKED

## Scope

This gate was approved to create and verify one isolated 2Gi+ fresh instance only if the required read-only cluster readiness preflight passed.

The preflight did not pass. The gate stopped before fresh instance creation and before any runtime, database, Kubernetes, or cluster/serverlb mutation.

## Published Image Identity

- Host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- In-cluster tag: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- Expected local image digest: `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`
- Expected linux/arm64 manifest digest: `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7`

## Exact Commands Run

Read-only preflight:

```sh
k3d cluster list
k3d cluster get clawmanager
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nPorts={{json .NetworkSettings.Ports}}'
docker inspect k3d-clawmanager-serverlb --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nPorts={{json .NetworkSettings.Ports}}'
kubectl cluster-info --request-timeout=5s
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
```

Evidence write and verification commands:

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md
rg -n "ISOLATED_2GI_FRESH_INSTANCE_DONE|ISOLATED_2GI_FRESH_INSTANCE_BLOCKED|gtclaw-controlui-persistent-bind-lan-auth-20260505162033|sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9|2Gi|fresh instance|cluster readiness|server-0|serverlb|18789|3001|PodIP|ServiceIP|Ready|OOM|imageID|no browser E2E|no manual pod patch|no manual Service patch|no kubectl cp|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md
rg -n -i '(token|secret|password|passwd|credential|authorization|cookie|access[_-]?token|id[_-]?token|refresh[_-]?token|bearer)[[:space:]"`]*[:=][[:space:]"`]*[A-Za-z0-9_./+=-]{8,}|[?&](token|auth|key|code|access_token|id_token|refresh_token)=[^&[:space:]"`]+|k3d\.cluster\.token=[^[:space:]]+|https?://[^[:space:]"`]*[?&][^[:space:]"`]*(token|auth|key|code)=' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-approval-packet.md
```

## Sanitized Outputs

### `k3d cluster list`

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   0/1       0/0      true
```

### `k3d cluster get clawmanager`

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   0/1       0/0      true
```

### `docker inspect k3d-clawmanager-server-0`

```text
Name=/k3d-clawmanager-server-0
Image=docker.io/rancher/k3s:v1.33.6-k3s1
StateStatus=exited
StateRunning=false
StateExitCode=137
StateStartedAt=2026-05-02T07:37:23.406150209Z
StateFinishedAt=2026-05-05T06:40:36.756563584Z
Ports={}
```

### `docker inspect k3d-clawmanager-serverlb`

```text
Name=/k3d-clawmanager-serverlb
Image=ghcr.io/k3d-io/k3d-proxy:5.8.3
StateStatus=exited
StateRunning=false
StateExitCode=137
StateStartedAt=2026-05-02T07:37:23.40704325Z
StateFinishedAt=2026-05-05T06:40:36.664089376Z
Ports={}
```

### `kubectl cluster-info --request-timeout=5s`

```text
Exit code: 1
Sanitized summary: connection refused to current Kubernetes API endpoint; read-only cluster access failed.
```

### Registry health: `http://localhost:5001/v2/`

```text
curl_exit=0 target=localhost:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

### Host tag image inspect

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

### In-cluster tag image inspect

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

## Cluster Readiness Preflight Result

Result: BLOCKED.

Findings:

- `k3d cluster list` shows `clawmanager` server readiness `0/1`.
- `k3d cluster get clawmanager` shows `clawmanager` server readiness `0/1`.
- `k3d-clawmanager-server-0` is not running: `StateStatus=exited`, `StateRunning=false`, `StateExitCode=137`.
- `k3d-clawmanager-serverlb` is not running: `StateStatus=exited`, `StateRunning=false`, `StateExitCode=137`.
- `kubectl cluster-info --request-timeout=5s` failed read-only access with connection refused.

STOP condition met:

- k3d cluster server is not ready.
- `server-0` is exited/not running.
- `serverlb` is exited/not running.
- Kubernetes read-only cluster access failed.

No isolated 2Gi+ fresh instance was created.

## Registry And Image Preflight Result

Registry health:

- `localhost:5001 /v2/` HEAD returned `status=200`, `remote_ip=127.0.0.1`.

Image digest:

- Host tag still resolves locally to `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.
- In-cluster tag still resolves locally to `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.
- No image digest drift was observed.

Registry and image publication are not the current blocker.

## Fresh Instance Work

Not attempted because cluster readiness preflight failed.

- Previous runtime image/resource setting: not read or changed; stopped before runtime setting flow.
- Temporary runtime image/resource setting: not applied.
- Restore confirmation: not applicable; no setting was changed.
- Fresh instance id/name: not created.
- Namespace: not available.
- Pod name: not available.
- Container name: not available.
- Pod phase: not available.
- Ready condition: not available.
- Restart count: not available.
- OOMKilled or OOM-related status: not available.
- Pod image: not available.
- Pod imageID: not available.
- Service name/type: not available.
- ServiceIP: not available.
- Service ports and target ports: not available.
- PodIP: not available.
- Service exposes `18789`: not verified because no fresh instance was created.
- Service exposes `3001`: not verified because no fresh instance was created.
- `18789` listener reachability from PodIP: not attempted.
- `18789` listener reachability from ServiceIP: not attempted.
- Startup artifact effect proof: not attempted. The required future proof remains to distinguish `127.0.0.1:18789` from PodIP/ServiceIP `18789` reachability and show the OpenClaw gateway is pod-facing, not loopback-only.

## Blocker Classification

Primary blocker: cluster readiness failure.

Specific blockers:

- `clawmanager` k3d cluster server readiness is `0/1`.
- `k3d-clawmanager-server-0` is exited.
- `k3d-clawmanager-serverlb` is exited.
- `kubectl` read-only cluster access fails.

Non-blockers in this preflight:

- Registry health is reachable on `localhost:5001 /v2/`.
- Published local image tags still point to the approved digest.

## Recommendation

Recommended next gate: Cluster Readiness Recovery Approval Packet.

Do not proceed to isolated 2Gi+ fresh instance creation until the user explicitly approves cluster/server/serverlb recovery and read-only readiness confirms:

- k3d `clawmanager` server readiness is `1/1`.
- `k3d-clawmanager-server-0` is running.
- `k3d-clawmanager-serverlb` is running.
- `kubectl` read-only cluster access succeeds.
- Registry health and image digest remain valid.

Registry Recovery Follow-up Approval Packet is not the primary recommendation because registry health is currently reachable and image digest has not drifted.

## Explicit Negatives / No Mutation Proof

- no cluster/serverlb recovery
- no docker start/restart for cluster/server/serverlb
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no kubectl cp write
- no Kubernetes write
- no runtime image/resource setting mutation
- no database write
- no fresh instance creation
- no backend change
- no frontend change
- no deployments change
- no docs change
- no longterm write
- no AgentTeam change
- no spec.md / plan.md / tasks.md change
- no existing evidence change
- no reviewed startup artifact change
- no `/tmp/gtclaw-runtime-patch/**` change
- no `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` change
- no secrets/token/cookie/access URL plaintext
- no Mem0 write
- no passes:true
- no Close
