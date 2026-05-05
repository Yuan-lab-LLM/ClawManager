# Backend Runtime Listener Follow-up Implementation Approval Packet

Date: 2026-05-05

Assigned worker: BackendRuntimeListenerFollowupApprovalPacketWorker

Topology: serial

## Verdict

BACKEND_RUNTIME_LISTENER_FOLLOWUP_APPROVAL_PACKET_DONE

This packet is an approval request only. It does not authorize or perform implementation, build, deploy, fresh instance creation, browser E2E, Kubernetes/runtime/database/registry mutation, `passes:true`, Close, longterm write-back, or Mem0 write.

## Approval Request

Commander/user approval is requested for the next gate:

`Backend Runtime Listener Follow-up Implementation Gate`

Approve or reject this gate explicitly before any backend source follow-up begins.

## Dependency Gate Status

| Gate | Evidence | Status |
| --- | --- | --- |
| Isolated 2Gi Fresh Instance Rerun After Cluster Recovery | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED` |
| Cluster Readiness Recovery Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md` | `CLUSTER_READINESS_RECOVERY_DONE` |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| Runtime Startup Artifact Implementation Gate | referenced by the fresh-instance rerun dependency chain | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE` |
| Control UI 18789 A1 Source Implementation | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | `A1_SOURCE_IMPLEMENTATION_BLOCKED` because runtime startup/source artifact coverage was incomplete at that gate |

The dependency gate is satisfied enough to draft this packet, but it remains blocked for E2E progression because the fresh runtime listener is not yet proven.

## Current Blocker Summary

Fresh instance:

- id: `9`
- name: `gtclaw-fresh-bind-lan-auth-20260505-175724`
- pod: `clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724`

Confirmed non-blockers:

- Pod Ready passed.
- restart count was `0`.
- no OOM was observed.
- pod imageID matched the approved digest `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.
- `3001` desktop basic reachability passed on loopback, PodIP, and ServiceIP.

Blocking facts:

- Service exposes `3001` but not `18789`.
- `18789` is unreachable on loopback, PodIP, and ServiceIP.
- Logs show the OpenClaw gateway bound `0.0.0.0:18789`, reported ready, then exited because the generated label exceeded `63 bytes`.
- The failing label shape was `clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 (OpenClaw)`.

## Recommended Source Implementation Scope

If approved, the next gate should be backend source only and should stay focused on the two runtime listener blockers:

1. Ensure the OpenClaw pod hostname or runtime label input is short enough that `"<label> (OpenClaw)"` is no longer than `63 bytes`.
2. Preserve Kubernetes resource uniqueness by `instance-id` labels/selectors and stable generated resource names, not by requiring long user-facing instance names inside the runtime label path.
3. Preserve the user-facing instance name in the database and API responses.
4. Confirm the backend source path for the OpenClaw Service creates or repairs Service ports so `18789` is exposed alongside `3001`.
5. Add focused backend tests for label/hostname length behavior and Service `18789` exposure alongside `3001`.

Read-only source observations for the future implementation gate:

- `backend/internal/services/k8s/pod_service.go` currently uses `client.GetPodName(instanceID, instanceName)` for the pod name and does not set an explicit shorter pod hostname.
- `backend/internal/services/k8s/client.go` truncates Kubernetes object names to `63 bytes`, which is valid for K8S object names but not sufficient when OpenClaw appends ` (OpenClaw)`.
- `backend/internal/services/instance_service.go` currently passes user-facing `instance.Name` into Pod and Service config while also using `instance-id` for labels/selectors.
- `backend/internal/services/instance_service.go` has source logic that should request additional OpenClaw Service port `18789` when the primary desktop port is `3001`.
- `backend/internal/services/k8s/service_service.go` has source logic to build Service ports including `3001` and additional `18789`, with `18789` named `control-ui`.

## Allowed Future Implementation Gate Files

If, and only if, this packet is approved, the future `Backend Runtime Listener Follow-up Implementation Gate` may modify only:

- `backend/internal/services/k8s/pod_service.go`
- `backend/internal/services/k8s/pod_service_test.go` if needed
- `backend/internal/services/k8s/client.go`
- `backend/internal/services/k8s/client_test.go` if needed
- `backend/internal/services/k8s/service_service.go` only if a `18789` source gap is found
- `backend/internal/services/k8s/service_service_test.go`
- `backend/internal/services/instance_service.go` only if needed to pass safe runtime identity/service config
- `backend/internal/services/instance_service_test.go`
- a new implementation evidence packet for that gate

## Explicit Future Prohibitions

The future implementation gate must not do any of the following:

- no build/tag/push/pull
- no backend deploy
- no backend restart
- no fresh instance
- no browser E2E
- no patch existing pod/service
- no cleanup/delete instance 9
- no registry/image mutation
- no Kubernetes write outside approved backend source file edits
- no runtime mutation
- no database mutation
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close

## Required Future Implementation Evidence

If approved, the next gate evidence should include:

- Exact backend files changed within the allowed list.
- Short-hostname or short-runtime-label design, including a byte-length test proving `"<label> (OpenClaw)" <= 63 bytes`.
- Proof that user-facing instance name remains preserved in DB/API model behavior.
- Proof that selectors and identity still use `instance-id` so Service-to-Pod routing remains unique.
- Focused test coverage for OpenClaw `3001` plus `18789` Service exposure.
- Focused test coverage that non-OpenClaw desktop instances do not unexpectedly expose `18789`.
- Negative evidence confirming no build/tag/push/pull, no backend deploy, no fresh instance, no browser E2E, no runtime/K8S/database/registry mutation, no passes:true, and no Close.

## Proposed Follow-up Gate Order

1. `Backend Runtime Listener Follow-up Implementation Gate`
2. `Control Plane Backend Build/Deploy Approval Packet/Gate`
3. Rerun isolated 2Gi fresh instance with proof Service exposes `18789` and `3001`
4. Listener/hash verification
5. Browser E2E gate
6. Only after fresh E2E plus explicit user approval: `passes:true`, Close, and longterm write-back

## Scope Boundary For This Packet

This packet only writes:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation-approval-packet.md`

It does not modify backend, runtime, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence.

## Secret Hygiene

This packet records no token value, cookie value, credential, secret, access URL, `.env`, `.codex/auth.json`, or `.codex/config.toml` content.
