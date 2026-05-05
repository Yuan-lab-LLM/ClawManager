# Registry Connectivity Diagnostic Gate - 2026-05-05

## Verdict

REGISTRY_CONNECTIVITY_DIAGNOSTIC_DONE

This gate completed read-only Docker/k3d registry connectivity diagnostics. It did not recover or restart the registry, did not retry push, did not mutate Docker images/containers/networks, Kubernetes, runtime, database, browser state, fresh instances, `longterm/**`, Mem0, `passes:true`, or Close state.

## Scope and Dependency Gates

Approved diagnostic dependency:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md`
- dependency verdict: `REGISTRY_CONNECTIVITY_DIAGNOSTIC_APPROVAL_PACKET_DONE`

Prior blocked publication evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery.md`
- dependency verdict: `RUNTIME_IMAGE_PUSH_RECOVERY_BLOCKED`
- local image digest remained `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`
- prior `localhost:5001` and `127.0.0.1:5001` `/v2/` checks were unreachable

Target image references:

| Role | Reference |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| local image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |

## Preflight Read-only Review

Commands:

```sh
sed -n '1,260p' AGENTS.md
sed -n '1,260p' .specify/memory/constitution.md
sed -n '1,260p' backend/AGENTS.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/spec.md
sed -n '1,300p' specs/gtclaw-runtime-controlui-persistent-image/plan.md
sed -n '1,340p' specs/gtclaw-runtime-controlui-persistent-image/tasks.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md
sed -n '1,280p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery-approval-packet.md
sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md
```

Sanitized summary:

- Active gate authorizes only read-only registry connectivity diagnostics plus this evidence file.
- The existing approval packet path is untracked in the local worktree and was not modified:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md
```

## Docker Context and Local Visibility

Command:

```sh
docker context show
```

Sanitized output:

```text
desktop-linux
```

Command:

```sh
docker context inspect desktop-linux
```

Sanitized output:

```text
Name=desktop-linux
Description=Docker Desktop
DockerHost=unix Docker Desktop socket
SkipTLSVerify=false
TLSMaterial={}
```

Command:

```sh
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Command:

```sh
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Conclusion:

- Current Docker context is `desktop-linux`.
- Docker context mismatch is not plausible as the primary blocker in this gate because this context can inspect the target local image tags and Docker container metadata.

## Container Inventory

Command:

```sh
docker ps --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Command:

```sh
docker ps -a --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
ID=8899d4dd0abe Name=k3d-clawmanager-registry Image=registry:2 Status=Exited (143) 2 hours ago Ports=
ID=080b12049c8c Name=k3d-clawmanager-serverlb Image=ghcr.io/k3d-io/k3d-proxy:5.8.3 Status=Exited (137) 2 hours ago Ports=
ID=46c9605819a2 Name=k3d-clawmanager-server-0 Image=rancher/k3s:v1.33.6-k3s1 Status=Exited (137) 2 hours ago Ports=
ID=0d359126da13 Name=redis Image=redis:7 Status=Exited (0) 3 months ago Ports=
```

Command:

```sh
docker ps --filter name=registry --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Command:

```sh
docker ps -a --filter name=registry --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
ID=8899d4dd0abe Name=k3d-clawmanager-registry Image=registry:2 Status=Exited (143) 2 hours ago Ports=
```

Command:

```sh
docker ps --filter publish=5001 --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Command:

```sh
docker ps -a --filter publish=5001 --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Conclusion:

- A k3d registry container exists: `k3d-clawmanager-registry`, image `registry:2`.
- It is stopped: `Exited (143) 2 hours ago`.
- No running container is currently publishing port 5001.

## Candidate Registry Inspect and Logs

Command:

```sh
docker inspect k3d-clawmanager-registry --format 'Name={{.Name}}
ID={{.Id}}
Image={{.Config.Image}}
StateStatus={{.State.Status}}
StateRunning={{.State.Running}}
StateExitCode={{.State.ExitCode}}
StateStartedAt={{.State.StartedAt}}
StateFinishedAt={{.State.FinishedAt}}
RestartPolicy={{.HostConfig.RestartPolicy.Name}}
PortBindings={{json .HostConfig.PortBindings}}
NetworkMode={{.HostConfig.NetworkMode}}
Networks={{json .NetworkSettings.Networks}}
Ports={{json .NetworkSettings.Ports}}'
```

Sanitized output:

```text
Name=/k3d-clawmanager-registry
ID=8899d4dd0abe...
Image=docker.io/library/registry:2
StateStatus=exited
StateRunning=false
StateExitCode=143
StateStartedAt=2026-05-02T07:37:23Z
StateFinishedAt=2026-05-05T06:40:35Z
RestartPolicy=unless-stopped
PortBindings={"5000/tcp":[{"HostIp":"0.0.0.0","HostPort":"5001"}]}
NetworkMode=bridge
Networks include: bridge, k3d-clawmanager
Networks active endpoint IDs/IPs: empty because container is exited
Ports={}
```

Command:

```sh
docker inspect k3d-clawmanager-registry --format 'Labels={{json .Config.Labels}}'
```

Sanitized output:

```text
Labels={"app":"k3d","k3d.cluster":"","k3d.registry.host":"","k3d.registry.hostIP":"0.0.0.0","k3d.role":"registry","k3d.version":"v5.8.3","k3s.registry.port.external":"5001","k3s.registry.port.internal":"5000"}
```

Command:

```sh
docker logs --tail 80 k3d-clawmanager-registry 2>&1 | sed -E 's/([Tt]oken|[Pp]assword|[Pp]asswd|[Ss]ecret|[Cc]redential|[Aa]uthorization|[Cc]ookie)([=: ][^[:space:]]*)/\1=[REDACTED]/g; s/(access_token=|id_token=|refresh_token=|auth=|code=)[^&[:space:]]+/\1[REDACTED]/g'
```

Sanitized output summary:

```text
Recent registry log tail showed successful HEAD/GET/PUT registry activity around 2026-05-05T03:41:50Z to 2026-05-05T03:43:50Z.
Recent requests used hosts localhost:5001 and k3d-clawmanager-registry:5000.
Recent statuses included 200, 201, and expected 404 manifest-unknown checks during push flow.
No shutdown reason appeared in the last 80 log lines.
No token/cookie/credential/secret values were written to this evidence.
```

Interpretation:

- The registry container was configured to publish container port 5000 on host port 5001.
- Because the container is currently `exited`, Docker reports no active `Ports`, no active network endpoint, and no listener on port 5001.
- The logs support that this registry was usable earlier in the day, but they do not explain the later stop at `2026-05-05T06:40:35Z`.

## k3d Registry and Cluster Metadata

Command:

```sh
k3d registry list
```

Sanitized output:

```text
NAME                       ROLE       CLUSTER   STATUS
k3d-clawmanager-registry   registry             exited
```

Command:

```sh
k3d cluster list
```

Sanitized output:

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   0/1       0/0      true
```

Command:

```sh
k3d cluster get clawmanager
```

Sanitized output:

```text
NAME          SERVERS   AGENTS   LOADBALANCER
clawmanager   0/1       0/0      true
```

Command:

```sh
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}
Image={{.Config.Image}}
StateStatus={{.State.Status}}
StateRunning={{.State.Running}}
Labels={{json .Config.Labels}}
ExtraHosts={{json .HostConfig.ExtraHosts}}
Networks={{json .NetworkSettings.Networks}}'
```

Sanitized output:

```text
Name=/k3d-clawmanager-server-0
Image=docker.io/rancher/k3s:v1.33.6-k3s1
StateStatus=exited
StateRunning=false
Labels include k3d.cluster=clawmanager, k3d.cluster.network=k3d-clawmanager, k3d.server.loadbalancer=k3d-clawmanager-serverlb.
Redacted labels: k3d.cluster.token and k3d.cluster.url values were not written.
ExtraHosts=null
Networks include k3d-clawmanager, with no active endpoint IP because container is exited.
```

Command:

```sh
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}
Image={{.Config.Image}}
StateStatus={{.State.Status}}
StateRunning={{.State.Running}}
k3d.cluster={{index .Config.Labels "k3d.cluster"}}
k3d.cluster.network={{index .Config.Labels "k3d.cluster.network"}}
k3d.cluster.network.id={{index .Config.Labels "k3d.cluster.network.id"}}
k3d.server.loadbalancer={{index .Config.Labels "k3d.server.loadbalancer"}}
ExtraHosts={{json .HostConfig.ExtraHosts}}
Networks={{json .NetworkSettings.Networks}}'
```

Sanitized output:

```text
Name=/k3d-clawmanager-server-0
Image=docker.io/rancher/k3s:v1.33.6-k3s1
StateStatus=exited
StateRunning=false
k3d.cluster=clawmanager
k3d.cluster.network=k3d-clawmanager
k3d.cluster.network.id=1238a66ac980f86c2d6646ae20ea2ff244e2ed6c5e978b3f4ddd1c7fd676703d
k3d.server.loadbalancer=k3d-clawmanager-serverlb
ExtraHosts=null
Networks include k3d-clawmanager, with no active endpoint IP because container is exited.
```

Interpretation:

- `k3d registry list` shows the clawmanager registry container exists and is `exited`.
- `k3d cluster list` / `k3d cluster get clawmanager` show the `clawmanager` cluster exists but the server is `0/1`.
- The k3d registry metadata does not show an explicit `CLUSTER=clawmanager` association; the registry label `k3d.cluster` is empty.
- Docker metadata does show the registry and cluster server are intended to use the same `k3d-clawmanager` network when running.

## Docker Network Diagnostics

Command:

```sh
docker network ls --format 'ID={{.ID}} Name={{.Name}} Driver={{.Driver}} Scope={{.Scope}}'
```

Sanitized output:

```text
ID=b4c5972efa4d Name=bridge Driver=bridge Scope=local
ID=1c545cfecc7a Name=host Driver=host Scope=local
ID=1238a66ac980 Name=k3d-clawmanager Driver=bridge Scope=local
ID=6ea67e6ecc12 Name=none Driver=null Scope=local
```

Command:

```sh
docker network inspect k3d-clawmanager --format 'Name={{.Name}}
Id={{.Id}}
Driver={{.Driver}}
Scope={{.Scope}}
IPAM={{json .IPAM.Config}}
Labels={{json .Labels}}
Containers={{json .Containers}}'
```

Sanitized output:

```text
Name=k3d-clawmanager
Id=1238a66ac980f86c2d6646ae20ea2ff244e2ed6c5e978b3f4ddd1c7fd676703d
Driver=bridge
Scope=local
IPAM=[{"Gateway":"172.18.0.1","Subnet":"172.18.0.0/16"}]
Labels={"app":"k3d"}
Containers={}
```

Conclusion:

- Docker network `k3d-clawmanager` exists.
- `docker network inspect` shows no active containers connected to that network.
- `docker inspect k3d-clawmanager-registry` shows a stale/intended `k3d-clawmanager` network entry, but no active endpoint ID or IP because the registry is stopped.
- Therefore the registry is not currently reachable on the k3d network.

## Sanitized `/v2/` Endpoint Checks

Command:

```sh
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=localhost:5001 scheme=http method=HEAD status=000 remote_ip= err=Failed to connect to localhost port 5001 after 0 ms: Could not connect to server
```

Command:

```sh
host='localhost:5001'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=localhost:5001 scheme=http method=GET status=000 remote_ip= err=Failed to connect to localhost port 5001 after 0 ms: Could not connect to server
```

Command:

```sh
host='127.0.0.1:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=127.0.0.1:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=127.0.0.1:5001 scheme=http method=HEAD status=000 remote_ip= err=Failed to connect to 127.0.0.1 port 5001 after 0 ms: Could not connect to server
```

Command:

```sh
host='127.0.0.1:5001'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=127.0.0.1:5001 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=127.0.0.1:5001 scheme=http method=GET status=000 remote_ip= err=Failed to connect to 127.0.0.1 port 5001 after 0 ms: Could not connect to server
```

Candidate endpoint discovered from Docker/k3d metadata: `k3d-clawmanager-registry:5000`.

Command:

```sh
host='k3d-clawmanager-registry:5000'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=k3d-clawmanager-registry:5000 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=6
target=k3d-clawmanager-registry:5000 scheme=http method=HEAD status=000 remote_ip= err=Could not resolve host: k3d-clawmanager-registry
```

Command:

```sh
host='k3d-clawmanager-registry:5000'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=k3d-clawmanager-registry:5000 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=6
target=k3d-clawmanager-registry:5000 scheme=http method=GET status=000 remote_ip= err=Could not resolve host: k3d-clawmanager-registry
```

Candidate endpoint discovered from Docker port binding metadata: `0.0.0.0:5001`.

Command:

```sh
host='0.0.0.0:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=0.0.0.0:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=0.0.0.0:5001 scheme=http method=HEAD status=000 remote_ip= err=Failed to connect to 0.0.0.0 port 5001 after 0 ms: Could not connect to server
```

Command:

```sh
host='0.0.0.0:5001'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=0.0.0.0:5001 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7
target=0.0.0.0:5001 scheme=http method=GET status=000 remote_ip= err=Failed to connect to 0.0.0.0 port 5001 after 0 ms: Could not connect to server
```

Endpoint conclusion:

- `localhost:5001` `/v2/` HEAD/GET: unreachable.
- `127.0.0.1:5001` `/v2/` HEAD/GET: unreachable.
- `k3d-clawmanager-registry:5000` from the host: not resolvable.
- `0.0.0.0:5001` from the host: unreachable.
- No safe reachable registry endpoint was discovered for a push retry.

## Required Answers

1. Current Docker context is `desktop-linux`; it can inspect the target local Docker images and list local containers.
2. A k3d registry container exists: `k3d-clawmanager-registry`, image `registry:2`, status `Exited (143) 2 hours ago`, current `Ports=` empty. Inspect shows configured `5000/tcp` to host `0.0.0.0:5001`, but not active while exited.
3. No running container currently exposes/publishes port 5001. The stopped registry container has historical/configured `HostPort=5001`, but `docker ps --filter publish=5001` and active `Ports` show no current publication.
4. `k3d registry list` shows `k3d-clawmanager-registry` with status `exited`; its `CLUSTER` column is blank.
5. `k3d cluster list` and `k3d cluster get clawmanager` show cluster `clawmanager` exists but server readiness is `0/1`. Registry-to-cluster association is not explicit in `k3d registry list`; Docker labels/network metadata show the cluster server uses `k3d-clawmanager` network and the registry is intended to use the same network when running.
6. `docker network inspect k3d-clawmanager` shows the network exists but has `Containers={}`. The stopped registry inspect contains an intended/stale `k3d-clawmanager` network entry with no active endpoint ID/IP, so the registry is not currently connected as an active endpoint.
7. `/v2/` HEAD/GET results are all unsuccessful for `localhost:5001`, `127.0.0.1:5001`, `k3d-clawmanager-registry:5000`, and `0.0.0.0:5001`.
8. Blocker classification: `registry stopped`. Secondary observation: no current port 5001 publication because the registry is stopped. Not classified as Docker context mismatch. Not classified as registry running but port not published. Not classified as endpoint mismatch because no discovered endpoint is reachable while the registry and cluster containers are stopped.
9. Recommended next gate: `Registry Recovery Implementation Approval Packet`.

## Blocker Classification

| Candidate classification | Result | Evidence |
| --- | --- | --- |
| registry missing/stopped | `registry stopped` | `k3d-clawmanager-registry` exists but `StateStatus=exited`, `StateRunning=false`, `StateExitCode=143`; `k3d registry list` reports `exited` |
| registry running but port not published | no | registry is not running; current `docker ps --filter publish=5001` has no rows |
| registry endpoint mismatch | no primary evidence | internal name `k3d-clawmanager-registry:5000` is not host-resolvable and there is no active registry endpoint to test from k3d network |
| Docker context mismatch | not plausible | `desktop-linux` can inspect target local image tags and all relevant k3d containers |
| inconclusive | no | read-only diagnostics identify the immediate blocker as stopped registry/k3d stack |

## Final Recommendation

Recommend next gate: `Registry Recovery Implementation Approval Packet`.

Reason:

- The local image exists with unchanged digest.
- The k3d registry container exists but is stopped.
- No current container publishes port 5001.
- No safe reachable `/v2/` endpoint was discovered, so a `Push Retry Approval Packet tied to a discovered safe endpoint` is not justified yet.
- External expert escalation is not the immediate recommendation because the current read-only evidence identifies a concrete recovery target: restore the approved local k3d registry/cluster registry path under a separately approved mutation gate.

## No Mutation Proof

- no registry mutation/restart
- no registry start, restart, create, delete, or config mutation
- no push retry
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker network mutation
- no k3d registry mutation
- no k3d cluster mutation
- no K8S/runtime/database/browser/fresh instance mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec.md / plan.md / tasks.md modification
- no existing evidence modification
- no reviewed startup artifact modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` modification
- no secrets/token/cookie/credential/access URL values in this evidence
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Verification

Command:

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md
```

Sanitized output:

```text
exit=0
<no output>
```

Command:

```sh
rg -n "REGISTRY_CONNECTIVITY_DIAGNOSTIC_DONE|REGISTRY_CONNECTIVITY_DIAGNOSTIC_BLOCKED|localhost:5001|127.0.0.1:5001|k3d|registry|port 5001|Docker context|registry missing|registry stopped|port not published|endpoint mismatch|inconclusive|no registry mutation|no push retry|no docker pull|no docker build|no docker run|no docker create|no docker cp|no docker export|no docker save|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md
```

Sanitized output:

```text
exit=0
Required diagnostic keywords were present in the evidence.
```

Command:

```sh
rg -n -i '(token|secret|password|passwd|credential|authorization|cookie|access[_-]?token|id[_-]?token|refresh[_-]?token|bearer)[[:space:]"`]*[:=][[:space:]"`]*[A-Za-z0-9_./+=-]{8,}|[?&](token|auth|key|code|access_token|id_token|refresh_token)=[^&[:space:]"`]+|k3d\.cluster\.token=[^[:space:]]+|https?://[^[:space:]"`]*[?&][^[:space:]"`]*(token|auth|key|code)=' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md
```

Sanitized output:

```text
exit=1
<no matches>
```

Command:

```sh
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md
```

Sanitized output:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic-approval-packet.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md
```
