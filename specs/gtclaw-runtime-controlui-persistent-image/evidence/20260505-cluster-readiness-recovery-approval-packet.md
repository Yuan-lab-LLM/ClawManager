# Cluster Readiness Recovery Approval Packet

Date: 2026-05-05

## Verdict

CLUSTER_READINESS_RECOVERY_APPROVAL_PACKET_DONE

This approval packet requests user approval for a later `Cluster Readiness Recovery Gate`. It does not execute cluster recovery, does not start or restart any container, does not create a fresh instance, and does not mutate Kubernetes, runtime settings, database state, browser state, registry state, Docker images, source, deployments, docs, longterm, AgentTeam, `passes:true`, or Close state.

## Approval Request

Please approve or reject whether a future worker may execute:

`Cluster Readiness Recovery Gate`

Requested future gate intent:

1. Start only the existing stopped k3d cluster containers needed to restore local cluster readiness.
2. Perform read-only readiness verification after start.
3. Stop without broadening scope if the cluster does not recover cleanly.

This packet does not authorize the recovery implementation itself. It only defines the proposed future gate boundary for user approval.

## Dependency Gate Record

| Gate | Evidence | Status used |
| --- | --- | --- |
| Isolated 2Gi+ Fresh Instance Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance.md` | `ISOLATED_2GI_FRESH_INSTANCE_BLOCKED`; blocker was failed cluster readiness preflight |
| Isolated 2Gi+ Fresh Instance Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-approval-packet.md` | `ISOLATED_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE` |
| Registry Recovery Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation.md` | `REGISTRY_RECOVERY_IMPLEMENTATION_DONE`; registry health restored and approved image push succeeded |
| Registry Connectivity Diagnostic Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md` | Previously recorded cluster server readiness `0/1` while diagnosing registry state |
| Runtime Image Build/Tag/Push Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md` | Build/tag identity evidence for the published runtime image |

## Current Blocker Summary

The current blocker is cluster readiness, not registry publication.

Observed in the blocked fresh instance gate:

- `clawmanager` k3d cluster server readiness is `0/1`.
- `k3d-clawmanager-server-0` is exited.
- `k3d-clawmanager-serverlb` is exited.
- `kubectl cluster-info --request-timeout=5s` failed read-only access with connection refused.
- No isolated 2Gi+ fresh instance was created.

Registry and image status from dependency evidence:

- `localhost:5001 /v2/` registry health was restored.
- Published image digest remains `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.

## Published Image Identity To Preserve

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| approved image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| approved linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

The future recovery gate must preserve this image identity and must not build, pull, tag, retag, or push images.

## Proposed Future Gate Minimum Mutation

If the user approves the future `Cluster Readiness Recovery Gate`, the recommended minimum mutation is limited to starting the two existing stopped k3d cluster containers:

```sh
docker start k3d-clawmanager-server-0
docker start k3d-clawmanager-serverlb
```

After those starts, the future gate should run only read-only verification such as:

```sh
k3d cluster list
k3d cluster get clawmanager
docker inspect k3d-clawmanager-server-0
docker inspect k3d-clawmanager-serverlb
kubectl cluster-info --request-timeout=5s
kubectl get nodes
curl --head --max-time 5 http://localhost:5001/v2/
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

All curl and command outputs in the future gate must be sanitized. No token, cookie, credential, secret, or access URL values may be written.

## Future Gate Success Criteria

The future `Cluster Readiness Recovery Gate` may be considered successful only if all of the following are true:

- k3d `clawmanager` server readiness becomes `1/1`.
- `k3d-clawmanager-server-0` is running.
- `k3d-clawmanager-serverlb` is running.
- `kubectl cluster-info` read-only cluster access succeeds.
- `kubectl get nodes` read-only access succeeds.
- `localhost:5001 /v2/` registry health remains healthy.
- Published image tag and digest remain unchanged:
  `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.
- No forbidden action occurred.

If any required readiness check fails, the future gate must write `CLUSTER_READINESS_RECOVERY_BLOCKED` or an equivalent blocked verdict for that gate and stop without expanding scope.

## Future Gate Must Not Allow

The future `Cluster Readiness Recovery Gate` must not perform:

- k3d cluster delete/create
- k3d cluster start/stop beyond the explicitly approved container starts
- docker restart
- docker stop
- docker rm
- docker recreate
- docker pull
- docker build
- docker rebuild
- docker run
- docker create
- docker cp
- docker export
- docker save
- docker tag
- docker push
- registry config mutation
- registry network mutation
- docker network mutation
- K8S write
- runtime mutation
- database mutation
- browser mutation
- fresh instance mutation
- browser E2E
- Chrome DevTools MCP
- Playwright
- manual pod patch
- manual Service patch
- kubectl cp write
- backend change
- frontend change
- deployments change
- docs change
- longterm write-back
- AgentTeam change
- spec.md / plan.md / tasks.md change
- existing evidence change
- reviewed startup artifact change
- secrets/token/cookie/access URL plaintext
- Mem0 write
- passes:true
- Close

## Future Gate Evidence Requirements

If approved, the future implementation evidence should include:

- exact commands run;
- before and after `docker inspect` state for `k3d-clawmanager-server-0`;
- before and after `docker inspect` state for `k3d-clawmanager-serverlb`;
- `k3d cluster list` and `k3d cluster get clawmanager` readiness results;
- `kubectl cluster-info` read-only result;
- `kubectl get nodes` read-only result;
- registry `localhost:5001 /v2/` health result;
- final image inspect for the host tag and in-cluster tag;
- whether the approved digest remained unchanged;
- explicit no mutation proof for all prohibited actions.

## Later Gate Sequence

Recommended sequence after this approval packet:

1. If the user approves: `Cluster Readiness Recovery Gate`.
2. If cluster readiness recovery succeeds: rerun `Isolated 2Gi+ Fresh Instance Gate`.
3. If fresh instance evidence succeeds: `Listener/Hash Verification Gate`.
4. Then `Browser E2E Approval/Gate`.
5. Only after fresh E2E evidence and explicit user approval: `passes:true`, Close, and longterm write-back may be considered.

Current state still does not authorize:

- passes:true
- Close
- longterm write-back
- Mem0 write
- no fresh instance
- no browser E2E

## No Recovery Executed In This Packet

This packet did not execute:

- no docker start
- no docker restart
- no docker stop
- no docker rm
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker pull
- no docker build
- no docker tag
- no docker push
- no k3d cluster create/delete/start/stop
- no Kubernetes write
- no runtime mutation
- no database write
- no registry mutation
- no fresh instance creation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no kubectl cp write
- no passes:true
- no Close
