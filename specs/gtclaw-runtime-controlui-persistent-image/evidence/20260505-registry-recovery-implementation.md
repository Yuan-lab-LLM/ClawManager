# Registry Recovery Implementation Gate - 2026-05-05

## Verdict

REGISTRY_RECOVERY_IMPLEMENTATION_DONE

The approved local k3d registry recovery gate started the existing stopped registry container, verified `/v2/` health on `localhost:5001` and `127.0.0.1:5001`, retried the approved host tag push once, and confirmed final local image metadata still points to the approved digest.

## Scope and Approvals

Dependency approval packet:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation-approval-packet.md`
- verdict: `REGISTRY_RECOVERY_IMPLEMENTATION_APPROVAL_PACKET_DONE`

Dependency diagnostics:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md`
- verdict: `REGISTRY_CONNECTIVITY_DIAGNOSTIC_DONE`
- blocker classification: `registry stopped`

Prior blocked publication:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery.md`
- verdict: `RUNTIME_IMAGE_PUSH_RECOVERY_BLOCKED`

This gate used only the approved commands and did not perform cluster/serverlb recovery, rebuild, retag, pull, runtime mutation, Kubernetes mutation, database mutation, browser E2E, fresh instance mutation, `passes:true`, Close, longterm write-back, or Mem0 write.

## Target Image Identity

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| approved local image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| approved linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

## Pre-read / Scope Confirmation

Commands:

```sh
sed -n '1,180p' AGENTS.md
sed -n '1,200p' .specify/memory/constitution.md
sed -n '1,160p' backend/AGENTS.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation-approval-packet.md
```

Sanitized summary:

- User-approved mutation scope was limited to `docker start k3d-clawmanager-registry`, sanitized `/v2/` health checks, one approved host tag `docker push` if health was restored, and final metadata inspect.
- Cluster/serverlb recovery was not approved.
- Rebuild, pull, retag, run/create/cp/export/save/rm/restart, K8S/runtime/database/browser/fresh instance mutation, `passes:true`, Close, longterm write-back, and Mem0 write were not approved.

## Before-start Registry Inspect

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
Networks include bridge and k3d-clawmanager, with no active endpoint IDs/IPs while exited.
Ports={}
```

Before-start conclusion:

- The registry container existed and was stopped.
- The expected host port binding was configured but inactive while stopped.

## Before-start Image Inspect

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

Before-start image conclusion:

- Both local tags pointed to the approved local image digest before registry recovery.
- No image digest drift was observed.

## Registry Start

Command:

```sh
docker start k3d-clawmanager-registry
```

Output:

```text
k3d-clawmanager-registry
```

Exit: `0`

## After-start Registry Inspect

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
StateStatus=running
StateRunning=true
StateExitCode=0
StateStartedAt=2026-05-05T09:15:15Z
StateFinishedAt=2026-05-05T06:40:35Z
RestartPolicy=unless-stopped
PortBindings={"5000/tcp":[{"HostIp":"0.0.0.0","HostPort":"5001"}]}
NetworkMode=bridge
Networks include active bridge endpoint 172.17.0.2 and active k3d-clawmanager endpoint 172.18.0.2.
Ports={"5000/tcp":[{"HostIp":"0.0.0.0","HostPort":"5001"}]}
```

After-start conclusion:

- Registry container started successfully.
- Host port 5001 was actively published.
- k3d-clawmanager network endpoint was active.

## Sanitized `/v2/` Health Checks

Command:

```sh
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=0 target=localhost:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

Command:

```sh
host='localhost:5001'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=localhost:5001 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=0 target=localhost:5001 scheme=http method=GET status=200 remote_ip=127.0.0.1 err=
```

Command:

```sh
host='127.0.0.1:5001'; out=$(curl --head --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=127.0.0.1:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=0 target=127.0.0.1:5001 scheme=http method=HEAD status=200 remote_ip=127.0.0.1 err=
```

Command:

```sh
host='127.0.0.1:5001'; out=$(curl --request GET --silent --show-error --max-time 5 --noproxy '*' --output /dev/null --write-out 'target=127.0.0.1:5001 scheme=http method=GET status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=0 target=127.0.0.1:5001 scheme=http method=GET status=200 remote_ip=127.0.0.1 err=
```

Health conclusion:

- Registry health was restored on both `localhost:5001` and `127.0.0.1:5001`.

## Approved Push Retry

Command:

```sh
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Sanitized output summary:

```text
The push refers to repository [localhost:5001/clawmanager-openclaw/openclaw]
Several layers already existed.
New/missing layers were pushed.
gtclaw-controlui-persistent-bind-lan-auth-20260505162033: digest: sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9 size: 856
```

Exit: `0`

Push conclusion:

- The single approved push retry succeeded.
- Published tag digest matched the approved image index digest.

## Final Image Metadata

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

Final image conclusion:

- Final local image inspect still points to the approved digest.
- No image digest drift occurred.

## Final Registry Metadata

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
StateStatus=running
StateRunning=true
StateExitCode=0
StateStartedAt=2026-05-05T09:15:15Z
StateFinishedAt=2026-05-05T06:40:35Z
RestartPolicy=unless-stopped
PortBindings={"5000/tcp":[{"HostIp":"0.0.0.0","HostPort":"5001"}]}
NetworkMode=bridge
Networks include active bridge endpoint 172.17.0.2 and active k3d-clawmanager endpoint 172.18.0.2.
Ports={"5000/tcp":[{"HostIp":"0.0.0.0","HostPort":"5001"}]}
```

## DONE Conditions

| Condition | Result |
| --- | --- |
| registry container started/running | yes, `StateStatus=running`, `StateRunning=true` |
| `localhost:5001` or `127.0.0.1:5001` `/v2/` reachable | yes, both endpoints returned `status=200` for HEAD and GET |
| approved host tag push succeeds | yes, `docker push` exit `0` |
| final local image inspect still points to approved digest | yes, `Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| no forbidden action occurred | yes |

## Final Recommendation

Recommended next gate:

`Isolated 2Gi+ Fresh Instance Approval Packet`

Reason: registry endpoint and host-side publication are restored. Fresh instance work remains separately gated because cluster/serverlb recovery, Kubernetes mutation, runtime/database/browser work, and fresh instance creation were not approved in this gate.

## Explicit Negatives

- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker rm
- no docker restart
- no docker tag
- no docker retag
- no registry config mutation
- no docker network mutation
- no cluster/serverlb recovery
- no k3d cluster/server/serverlb start
- no k3d cluster/server/serverlb restart
- no K8S write
- no runtime image/resource setting mutation
- no database write
- no fresh instance creation, deletion, or modification
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no kubectl cp write
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
- no secrets/token/cookie/credential/access URL plaintext output
- no Mem0 write
- no longterm write
- no passes:true
- no Close
