# Cluster Readiness Recovery Gate Evidence

Date: 2026-05-05

## Verdict

CLUSTER_READINESS_RECOVERY_DONE

The approved cluster readiness recovery gate started only the two existing stopped k3d cluster containers, then verified k3d, Docker, kubectl, registry, and image digest readiness with read-only commands.

## Scope And Approval

Dependency approval packet:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery-approval-packet.md`
- verdict: `CLUSTER_READINESS_RECOVERY_APPROVAL_PACKET_DONE`

Approved mutation scope:

```sh
docker start k3d-clawmanager-server-0
docker start k3d-clawmanager-serverlb
```

No other recovery, rebuild, tag, push, pull, registry mutation, K8S write, runtime mutation, database mutation, browser E2E, fresh instance mutation, `passes:true`, Close, longterm write-back, or Mem0 write was performed.

## Exact Commands Run

Before-state commands:

```sh
k3d cluster list
k3d cluster get clawmanager
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPortBindings={{json .HostConfig.PortBindings}}\nNetworkMode={{.HostConfig.NetworkMode}}\nPorts={{json .NetworkSettings.Ports}}'
docker inspect k3d-clawmanager-serverlb --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPortBindings={{json .HostConfig.PortBindings}}\nNetworkMode={{.HostConfig.NetworkMode}}\nPorts={{json .NetworkSettings.Ports}}'
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
```

Approved mutation commands:

```sh
docker start k3d-clawmanager-server-0
docker start k3d-clawmanager-serverlb
```

After-state commands:

```sh
k3d cluster list
k3d cluster get clawmanager
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPortBindings={{json .HostConfig.PortBindings}}\nNetworkMode={{.HostConfig.NetworkMode}}\nPorts={{json .NetworkSettings.Ports}}'
docker inspect k3d-clawmanager-serverlb --format 'Name={{.Name}}\nImage={{.Config.Image}}\nStateStatus={{.State.Status}}\nStateRunning={{.State.Running}}\nStateExitCode={{.State.ExitCode}}\nStateStartedAt={{.State.StartedAt}}\nStateFinishedAt={{.State.FinishedAt}}\nRestartPolicy={{.HostConfig.RestartPolicy.Name}}\nPortBindings={{json .HostConfig.PortBindings}}\nNetworkMode={{.HostConfig.NetworkMode}}\nPorts={{json .NetworkSettings.Ports}}'
kubectl cluster-info --request-timeout=5s
kubectl get nodes --request-timeout=5s
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}\nRepoDigests={{json .RepoDigests}}\nId={{.Id}}\nOs={{.Os}}\nArchitecture={{.Architecture}}'
```

Verification commands:

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md
rg -n "CLUSTER_READINESS_RECOVERY_DONE|CLUSTER_READINESS_RECOVERY_BLOCKED|docker start k3d-clawmanager-server-0|docker start k3d-clawmanager-serverlb|k3d cluster|get clawmanager|1/1|kubectl cluster-info|kubectl get nodes|localhost:5001|sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9|no fresh instance|no browser E2E|no K8S write|no build/tag/push/pull|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md
rg -n -i '(token|secret|password|passwd|credential|authorization|cookie|access[_-]?token|id[_-]?token|refresh[_-]?token|bearer)[[:space:]"`]*[:=][[:space:]"`]*[A-Za-z0-9_./+=-]{8,}|[?&](token|auth|key|code|access_token|id_token|refresh_token)=[^&[:space:]"`]+|k3d\.cluster\.token=[^[:space:]]+|https?://[^[:space:]"`]*[?&][^[:space:]"`]*(token|auth|key|code)=' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery-approval-packet.md
```

## Before State

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
RestartPolicy=unless-stopped
PortBindings={}
NetworkMode=bridge
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
RestartPolicy=unless-stopped
PortBindings={"30443/tcp":[{"HostIp":"","HostPort":"30443"}],"6443/tcp":[{"HostIp":"0.0.0.0","HostPort":"58334"}]}
NetworkMode=bridge
Ports={}
```

### Registry Health Before

```text
curl_exit=0 target=localhost:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

### Host Tag Image Inspect Before

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

### In-cluster Tag Image Inspect Before

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

## Approved Mutation Results

### `docker start k3d-clawmanager-server-0`

```text
k3d-clawmanager-server-0
docker_start_target=k3d-clawmanager-server-0 exit=0
```

Exit code: `0`

### `docker start k3d-clawmanager-serverlb`

```text
k3d-clawmanager-serverlb
docker_start_target=k3d-clawmanager-serverlb exit=0
```

Exit code: `0`

## After State

### `k3d cluster list`

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   1/1       0/0      true
```

### `k3d cluster get clawmanager`

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   1/1       0/0      true
```

### `docker inspect k3d-clawmanager-server-0`

```text
Name=/k3d-clawmanager-server-0
Image=docker.io/rancher/k3s:v1.33.6-k3s1
StateStatus=running
StateRunning=true
StateExitCode=0
StateStartedAt=2026-05-05T09:47:25.078380302Z
StateFinishedAt=2026-05-05T06:40:36.756563584Z
RestartPolicy=unless-stopped
PortBindings={}
NetworkMode=bridge
Ports={}
```

### `docker inspect k3d-clawmanager-serverlb`

```text
Name=/k3d-clawmanager-serverlb
Image=ghcr.io/k3d-io/k3d-proxy:5.8.3
StateStatus=running
StateRunning=true
StateExitCode=0
StateStartedAt=2026-05-05T09:47:29.752123513Z
StateFinishedAt=2026-05-05T06:40:36.664089376Z
RestartPolicy=unless-stopped
PortBindings={"30443/tcp":[{"HostIp":"","HostPort":"30443"}],"6443/tcp":[{"HostIp":"0.0.0.0","HostPort":"58334"}]}
NetworkMode=bridge
Ports={"30443/tcp":[{"HostIp":"0.0.0.0","HostPort":"30443"},{"HostIp":"::","HostPort":"30443"}],"6443/tcp":[{"HostIp":"0.0.0.0","HostPort":"58334"}]}
```

### `kubectl cluster-info --request-timeout=5s`

```text
Kubernetes control plane is running at https://0.0.0.0:58334
CoreDNS is running at https://0.0.0.0:58334/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy
Metrics-server is running at https://0.0.0.0:58334/api/v1/namespaces/kube-system/services/https:metrics-server:https/proxy

To further debug and diagnose cluster problems, use 'kubectl cluster-info dump'.
```

Exit code: `0`

### `kubectl get nodes --request-timeout=5s`

```text
NAME                       STATUS   ROLES                  AGE   VERSION
k3d-clawmanager-server-0   Ready    control-plane,master   19d   v1.33.6+k3s1
```

Exit code: `0`

### Registry Health After

```text
curl_exit=0 target=localhost:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

### Host Tag Image Inspect After

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

### In-cluster Tag Image Inspect After

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

## Success Criteria Check

| Criterion | Result |
| --- | --- |
| k3d `clawmanager` server readiness is `1/1` | PASS |
| `k3d-clawmanager-server-0` running | PASS |
| `k3d-clawmanager-serverlb` running | PASS |
| `kubectl cluster-info` read-only succeeds | PASS |
| `kubectl get nodes` read-only succeeds | PASS |
| `localhost:5001 /v2/` health remains `200` | PASS |
| host tag digest remains approved digest | PASS |
| in-cluster tag digest remains approved digest | PASS |

Blocker classification: none; recovery succeeded.

No blocked classifier applies:

- not `docker start failed`
- not `server container exits again`
- not `serverlb exits again`
- not `k3d readiness remains 0/1`
- not `kubectl API still unreachable`
- not `registry health regressed`
- not `approved image digest drift`

## Recommendation

Recommended next gate: rerun `Isolated 2Gi+ Fresh Instance Gate`.

The next gate should still start with read-only cluster, registry, and image digest preflight before performing any approved fresh instance mutation.

## Explicit Negatives / No Forbidden Action Proof

- no fresh instance
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no K8S write
- no runtime/database mutation
- no database write
- no runtime mutation
- no build/tag/push/pull
- no docker restart
- no docker stop
- no docker rm
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no registry config mutation
- no registry network mutation
- no k3d cluster create/delete/start/stop
- no manual pod patch
- no manual Service patch
- no kubectl cp write
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
- no `/tmp/gtclaw-startup-source-inspect-*/**` change
- no secrets/token/cookie/access URL plaintext
- no Mem0 write
- no passes:true
- no Close
