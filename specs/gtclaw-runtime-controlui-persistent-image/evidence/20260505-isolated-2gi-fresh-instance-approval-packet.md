# Isolated 2Gi+ Fresh Instance Approval Packet - 2026-05-05

## Verdict

ISOLATED_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE

This approval packet requests user approval for a later `Isolated 2Gi+ Fresh Instance Gate`. It does not create a fresh instance, does not mutate Kubernetes, runtime settings, database state, browser state, registry state, Docker images, source, deployment, documentation, longterm, Mem0, `passes:true`, or Close state.

## Approval Request

Please approve or reject whether a future worker may execute:

`Isolated 2Gi+ Fresh Instance Gate`

Requested future gate scope:

1. Perform read-only cluster readiness preflight.
2. If the `clawmanager` cluster/server/serverlb is not ready, stop immediately and request a separate `Cluster Readiness Recovery Approval Packet`.
3. If cluster readiness is present, use the already published in-cluster runtime image tag to create one isolated fresh instance with at least 2Gi capacity.
4. Wait for the created runtime pod to reach Ready, then collect pod/service metadata and listener/hash evidence.
5. Do not run browser E2E in the fresh instance gate; browser evidence remains a later approval/gate.

This packet does not request permission to start, restart, repair, or recover the k3d cluster server or serverlb.

## Dependency Gate Record

| Gate | Evidence | Status used |
| --- | --- | --- |
| Registry Recovery Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation.md` | `REGISTRY_RECOVERY_IMPLEMENTATION_DONE`; registry running; `localhost:5001` `/v2/` health `200`; approved host tag push succeeded |
| Registry Recovery Implementation Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation-approval-packet.md` | `REGISTRY_RECOVERY_IMPLEMENTATION_APPROVAL_PACKET_DONE` |
| Registry Connectivity Diagnostic Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-connectivity-diagnostic.md` | `REGISTRY_CONNECTIVITY_DIAGNOSTIC_DONE`; prior blocker was `registry stopped`; also recorded `clawmanager` server `0/1` |
| Runtime Image Push Recovery Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery.md` | `RUNTIME_IMAGE_PUSH_RECOVERY_BLOCKED`; superseded for publication by registry recovery success |
| Runtime Image Build/Tag/Push Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md` | Build/tag and local image digest evidence for the target image |
| Runtime Startup Artifact Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | Startup artifact adds pod-facing gateway flags `--bind lan --auth token` |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | Backend control-ui WebSocket first-connect auth bridge implemented and tested |
| Prior fresh instance evidence | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md` | Historical pattern only; not reused as proof for the new published image |
| Prior browser E2E evidence | `20260504-browser-e2e.md`, `20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Historical blocker was control-ui `18789` connection refused; not reused as current browser evidence |

## Published Image Identity

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

Publication status from dependency evidence:

- Registry endpoint restored and running.
- `localhost:5001` `/v2/` HEAD/GET returned `200`.
- One approved `docker push` for the host tag succeeded.
- Final local image inspect still pointed to `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.

## Read-only Cluster Metadata Snapshot

Commands run for approval-boundary accuracy only:

```sh
k3d cluster list
docker inspect k3d-clawmanager-server-0 --format 'Name={{.Name}}
Image={{.Config.Image}}
StateStatus={{.State.Status}}
StateRunning={{.State.Running}}
StateExitCode={{.State.ExitCode}}
Ports={{json .NetworkSettings.Ports}}'
docker inspect k3d-clawmanager-serverlb --format 'Name={{.Name}}
Image={{.Config.Image}}
StateStatus={{.State.Status}}
StateRunning={{.State.Running}}
StateExitCode={{.State.ExitCode}}
Ports={{json .NetworkSettings.Ports}}'
```

Sanitized output summary:

```text
k3d cluster list:
clawmanager SERVERS=0/1 AGENTS=0/0 LOADBALANCER=true

k3d-clawmanager-server-0:
Image=docker.io/rancher/k3s:v1.33.6-k3s1
StateStatus=exited
StateRunning=false
StateExitCode=137
Ports={}

k3d-clawmanager-serverlb:
Image=ghcr.io/k3d-io/k3d-proxy:5.8.3
StateStatus=exited
StateRunning=false
StateExitCode=137
Ports={}
```

Cluster readiness conclusion:

- The registry is recovered, but the `clawmanager` cluster/server/serverlb is not currently ready.
- Because cluster/server/serverlb recovery is not authorized by this packet, the future fresh instance gate must stop before mutation if this readiness state remains unchanged.

## Future Gate Goal

If approved and only after cluster readiness is confirmed, the future `Isolated 2Gi+ Fresh Instance Gate` should:

1. Use the published in-cluster image tag:

```text
k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

2. Create one isolated fresh instance with a unique name and at least `2Gi` capacity.
3. Verify the created pod reaches Ready.
4. Record pod image, imageID, namespace/name, restart count, pod phase, readiness, and OOM-related status.
5. Verify Service exposes expected runtime ports `18789` and `3001`.
6. Verify PodIP and ServiceIP listener reachability for `18789`.
7. Verify the startup artifact effect: the OpenClaw gateway is pod-facing, not loopback-only.
8. Verify the deployed image identity matches the published digest and linux/arm64 manifest expectation.
9. Restore any temporary image setting after instance creation if such a setting was changed.
10. Write fresh instance evidence only; do not run browser E2E.

## Cluster Readiness Prerequisite

The future gate must start with read-only cluster readiness checks. If current k3d cluster/serverlb state is still not ready, the worker must:

1. Stop before any fresh instance, K8S, runtime, or database mutation.
2. Write a blocked finding in the future gate evidence or a short approval packet, depending on Commander instruction.
3. Recommend `Cluster Readiness Recovery Approval Packet`.

This packet does not authorize:

- starting `k3d-clawmanager-server-0`
- starting `k3d-clawmanager-serverlb`
- restarting any k3d cluster/server/serverlb container
- repairing k3d cluster configuration
- mutating Kubernetes resources to recover cluster readiness

Default cluster/server/serverlb recovery authorization: not granted.

## Future Implementation Gate Minimum Allowed Mutation If Approved

If the user approves the future `Isolated 2Gi+ Fresh Instance Gate` and cluster readiness preflight passes, the gate may request or perform only the minimum mutations needed for one isolated test instance:

- set or override the system runtime image setting only if the existing backend flow requires it and the user approves that sub-scope;
- record the previous runtime image/resource setting before any temporary change;
- create one isolated fresh instance with a unique name;
- allocate at least `2Gi` capacity;
- wait for and read pod/service metadata;
- restore the previous image/resource setting after instance creation if the setting was changed;
- write a new evidence packet.

The future gate evidence should include:

- exact commands or API calls used, with token/cookie/credential values omitted;
- previous and temporary image setting if setting mutation was used;
- fresh instance id/name;
- namespace, pod name, pod phase, Ready condition, restart count, and OOMKilled or OOM-related status;
- pod image and imageID;
- Service name/type, ServiceIP, ports, and target ports;
- PodIP;
- `18789` listener reachability from PodIP and ServiceIP;
- Service exposure for `18789` and `3001`;
- startup artifact effect proof that the OpenClaw gateway is pod-facing, not loopback-only;
- rollback/restore confirmation for any temporary image setting.

## Future Implementation Gate Prohibitions

The future fresh instance gate must not perform:

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no kubectl cp write
- no database mutation outside the approved fresh instance creation flow
- no registry mutation
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no docker push
- no docker tag
- no cluster/serverlb recovery unless separately approved
- no K8S write outside the approved fresh instance creation flow
- no runtime image/resource setting mutation except the approved temporary image-setting flow
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
- no secrets/token/cookie/credential/access URL plaintext
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Follow-up Gate Order

1. If user approves and cluster is ready: `Isolated 2Gi+ Fresh Instance Gate`.
2. If cluster is not ready: `Cluster Readiness Recovery Approval Packet`.
3. After a successful fresh instance gate: `Listener/Hash Verification Gate`.
4. Then `Browser E2E Approval Packet` / `Browser E2E Gate`.
5. Only after fresh E2E evidence plus explicit user approval: `passes:true` / `Close` / `longterm` write-back.

## This Approval Packet Did Not Perform

- no fresh instance creation
- no K8S write
- no database write
- no runtime mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no registry mutation
- no cluster/serverlb recovery
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec.md / plan.md / tasks.md modification
- no existing evidence modification
- no reviewed startup artifact modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write
- no passes:true
- no Close
