# Registry Recovery Implementation Approval Packet - 2026-05-05

## Verdict

REGISTRY_RECOVERY_IMPLEMENTATION_APPROVAL_PACKET_DONE

This approval packet requests user approval for a later `Registry Recovery Implementation Gate`. It does not execute recovery, does not start or restart any container, does not retry push, and does not mutate Docker, k3d, Kubernetes, runtime, database, browser, fresh instance, source, deployment, documentation, longterm, Mem0, `passes:true`, or Close state.

## Approval Request

Please approve or reject whether the next gate may execute:

`Registry Recovery Implementation Gate`

Requested future gate scope:

1. Start the existing stopped k3d registry container only:

```sh
docker start k3d-clawmanager-registry
```

2. Verify registry health with sanitized `/v2/` checks against:

```text
localhost:5001
127.0.0.1:5001
```

3. If and only if registry health becomes reachable, retry push of the existing approved host tag once:

```sh
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

4. Inspect the final local tag metadata after the push attempt:

```sh
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

This packet recommends approving the same future gate to retry that existing local host tag once after `/v2/` health is restored. If the user does not approve that push retry sub-scope, the future gate must stop after health verification and produce a separate `Push Retry Approval Packet`.

## Current Blocker

Source evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md`
- verdict: `REGISTRY_CONNECTIVITY_DIAGNOSTIC_DONE`
- blocker classification: `registry stopped`

Confirmed blocker facts:

| Fact | Current status |
| --- | --- |
| registry container | `k3d-clawmanager-registry` exists |
| registry image | `registry:2` / `docker.io/library/registry:2` |
| registry state | `Exited (143)` / `StateStatus=exited` |
| active port 5001 publication | none |
| configured registry port binding | `0.0.0.0:5001 -> 5000/tcp`, inactive while container is stopped |
| `localhost:5001` `/v2/` | unreachable in diagnostic evidence |
| `127.0.0.1:5001` `/v2/` | unreachable in diagnostic evidence |
| `k3d-clawmanager-registry:5000` from host | not resolvable in diagnostic evidence |
| `clawmanager` k3d cluster | exists, server readiness `0/1` |
| local built image | exists locally |
| registry publication | incomplete / not accepted |

Runtime Image Push Recovery remains blocked:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery.md`
- verdict: `RUNTIME_IMAGE_PUSH_RECOVERY_BLOCKED`
- prior approved push retry failed because the local registry endpoint was not reachable.

## Target Image Identity

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| local image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

The future gate must not rebuild, pull, retag, or otherwise change this image identity unless a separate approval packet explicitly authorizes that mutation.

## Proposed Minimal Future Recovery Scope

The preferred recovery path is the smallest mutation that addresses the observed root cause:

1. Start the existing stopped registry container `k3d-clawmanager-registry`.
2. Run sanitized `curl` HEAD/GET `/v2/` checks against `localhost:5001` and `127.0.0.1:5001`.
3. If `/v2/` health is reachable, retry `docker push` for the existing approved host tag once.
4. Run `docker image inspect` for the final local tag and record digest/tag metadata.
5. Stop and report if `docker start` fails, `/v2/` remains unhealthy, or the single push retry fails.

The future implementation gate should not use `docker restart`. The approved first action should be `docker start k3d-clawmanager-registry` only, because the diagnostic evidence shows the container exists but is stopped.

## Push Retry Request

This packet explicitly requests that the user approve the future `Registry Recovery Implementation Gate` to include exactly one push retry after registry health is restored.

Approved push retry candidate, if user approves:

```sh
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Constraints for that retry:

- Use only the existing local host tag.
- Use only the existing local image digest `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.
- Retry once only after sanitized `/v2/` health is reachable.
- Do not use `docker pull`.
- Do not use `docker build` or docker rebuild.
- Do not use `docker run`.
- Do not use `docker create`.
- Do not use `docker cp`.
- Do not use `docker export`.
- Do not use `docker save`.
- Do not retag unless separately approved.

## Cluster and Server Recovery Handling

The diagnostic evidence also shows the k3d `clawmanager` cluster exists but server readiness is `0/1`. That matters for later fresh instance work, but it is not required for host-side registry publication if the registry container itself can be started and its host port is reachable.

This approval packet does not request permission to start, restart, repair, recreate, or otherwise mutate:

- `k3d-clawmanager-server-0`
- `k3d-clawmanager-serverlb`
- any Kubernetes resource
- any runtime instance
- any database state

Recommended handling:

- Keep cluster/serverlb recovery for a later `Isolated Fresh Instance / Cluster Readiness Approval Packet`.
- If the future registry recovery worker discovers that push publication actually requires cluster/server start, it must stop and produce a new approval packet instead of starting cluster/server containers.

## Future Implementation Gate Allowed Mutations If Approved

If and only if the user approves the future `Registry Recovery Implementation Gate`, that gate may perform only:

```sh
docker start k3d-clawmanager-registry
```

Sanitized health checks:

```sh
curl --head --max-time 5 http://localhost:5001/v2/
curl --request GET --max-time 5 http://localhost:5001/v2/
curl --head --max-time 5 http://127.0.0.1:5001/v2/
curl --request GET --max-time 5 http://127.0.0.1:5001/v2/
```

Optional same-gate push retry if user approves that sub-scope:

```sh
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Post-attempt read-only metadata:

```sh
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

All curl evidence must be sanitized to status, remote IP, and error summary only. No auth headers, token values, cookie values, credentials, secrets, or access URLs may be printed.

## Future Implementation Gate Prohibitions

The future gate must not perform:

- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker rm
- no docker restart unless a later approval packet explicitly requests it
- no docker tag or retag unless separately approved
- no registry config mutation
- no docker network mutation
- no k3d cluster start/restart unless separately approved
- no K8S write
- no runtime mutation
- no database mutation
- no browser mutation
- no fresh instance mutation
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
- no secrets/token/cookie/credential/access URL plaintext
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Follow-up Gate Order

1. If user approves: `Registry Recovery Implementation Gate`.
2. If registry recovery and publication succeeds: `Isolated 2Gi+ Fresh Instance Approval Packet`.
3. Then listener/hash verification.
4. Then browser E2E.
5. Then only after fresh E2E evidence plus explicit user approval: `passes:true` / `Close` / `longterm` write-back.

## This Approval Packet Did Not Perform

- no registry recovery
- no docker start
- no docker restart
- no docker push
- no push retry
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker rm
- no registry config mutation
- no docker network mutation
- no k3d cluster mutation
- no K8S/runtime/database/browser/fresh instance mutation
- no browser E2E
- no backend/frontend/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modifications
- no reviewed startup artifact modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write
- no passes:true
- no Close
