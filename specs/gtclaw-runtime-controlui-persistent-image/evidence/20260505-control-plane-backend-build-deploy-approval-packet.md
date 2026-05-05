# Control Plane Backend Build/Deploy Approval Packet

Date: 2026-05-05

Worker: ControlPlaneBackendBuildDeployApprovalPacketWorker

Topology: serial

## Verdict

CONTROL_PLANE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_DONE

This packet is an approval request only. It does not build, tag, push, pull, deploy, restart backend, create a fresh instance, run browser E2E, mutate Kubernetes/runtime/database/registry state, write longterm/Mem0, set `passes:true`, or Close.

## Approval Request

Commander/user approval is requested for the next gate:

`Control Plane Backend Build/Deploy Gate`

Approve or reject this gate explicitly before any backend build, backend deploy, backend restart, image tag/publish, or control-plane workload update occurs.

## Dependency Gate Status

| Gate | Evidence | Status |
| --- | --- | --- |
| Backend Runtime Listener Follow-up Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE`; Commander reviewed as passed |
| Fresh rerun after cluster recovery | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED`; running backend Service exposed `3001` but not `18789`; runtime label crash fixed in source but not deployed |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| Control UI 18789 A1 Source Implementation | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | backend source changes present for Service `18789`, control-ui proxy auth, and token handling |

## Source Scope That Must Be Deployed

The future build/deploy gate must put the currently approved backend source changes into the running ClawManager control plane:

1. A1 Service `18789` source changes:
   - OpenClaw create/start service path includes `3001` plus additional `18789`.
   - non-OpenClaw desktop Service generation does not unexpectedly expose `18789`.
   - `/control-ui` proxy targets the control-ui Service path rather than desktop `3001`.
2. WS Auth Bridge source changes:
   - control-ui WebSocket first `connect` frame receives server-side auth injection.
   - browser-provided upstream auth material is stripped or ignored.
   - desktop `/proxy` behavior remains unaffected.
3. Backend Runtime Listener Follow-up hostname/label fix:
   - OpenClaw pod runtime-facing hostname is short and instance-id scoped.
   - `clawreef-9 (OpenClaw)` test proof is `21` bytes, satisfying `<= 63 bytes`.
   - user-facing instance name remains preserved in metadata/API model behavior.
   - `instance-id` and `app` labels/selectors remain the routing identity.

## Read-only Deployment Context

Manifest context observed read-only:

- K3S and K8S manifests define the ClawManager control-plane workload as `Deployment/clawmanager-app` in namespace `clawmanager-system`.
- The backend/control-plane container is named `clawmanager-app`.
- The workload exposes application port `8443` and readiness/liveness probes use `/healthz`.
- K3S exposes the control-plane Service through NodePort `30443`.

This packet does not edit deployments or query/mutate the cluster.

## Future Gate Minimal Allowed Mutation Proposal

If approved, the future `Control Plane Backend Build/Deploy Gate` should allow only the minimum mutations required to run the already-approved backend source:

- backend build: build the backend/control-plane image or binary using the repo-approved local workflow.
- tag/publish only if the deployment workflow requires an image reference for the control-plane workload.
- backend deploy: update/restart only the ClawManager backend/control-plane workload required to run the new backend source.
- post-deploy read-only smoke checks only.

The future gate should record exact commands, image/binary identity, workload identity, and rollback target with secrets redacted.

## Future Gate Must Preserve And Verify

The future build/deploy gate must preserve and verify:

- no frontend rebuild unless explicitly required and separately justified.
- no runtime image rebuild unless explicitly required and separately justified.
- no database schema migration unless separately approved.
- no fresh instance creation in the build/deploy gate.
- registry/image mutation only if needed for backend deployment and explicitly named before execution.
- published runtime image digest remains unchanged; the OpenClaw runtime image from the fresh rerun must not be rebuilt, retagged, pushed, pulled, or replaced by this gate.
- no manual pod patch, no manual Service patch, and no `kubectl cp` write.
- no browser E2E in the build/deploy gate.
- no token, cookie, credential, secret, or access URL plaintext in commands or evidence.

## Future Gate Success Conditions

The future `Control Plane Backend Build/Deploy Gate` should be considered successful only if evidence proves all of the following:

1. The running backend is demonstrably using the newly built/deployed backend source.
2. Backend package smoke/readiness passes after deploy.
3. Existing `/healthz` endpoint works, or existing `/api/v1/auth/me` works with auth material fully redacted.
4. The `clawmanager-app` backend/control-plane pod is not in a crash loop and has no unexpected restart loop after deploy.
5. The published runtime image digest remains unchanged.
6. No fresh instance is created during this build/deploy gate.
7. No browser E2E, `passes:true`, Close, longterm write-back, or Mem0 write occurs.

## Recommended Future Gate Order

1. `Control Plane Backend Build/Deploy Gate`
2. Rerun isolated 2Gi fresh instance proving Service exposes `3001` plus `18789` and `18789` is reachable on PodIP and ServiceIP
3. Listener/hash verification
4. Browser E2E
5. Only after fresh E2E plus explicit user approval: `passes:true`, Close, and longterm write-back

## Current Prohibitions Remain Active

This packet does not authorize any of the following now:

- backend build
- backend deploy
- backend restart
- build/tag/push/pull
- K8S write
- runtime mutation
- database mutation
- registry mutation
- fresh instance creation, deletion, or modification
- cleanup/delete instance 9
- manual pod patch
- manual Service patch
- `kubectl cp` write
- browser E2E
- Chrome DevTools MCP
- Playwright
- longterm write-back
- Mem0 write
- no passes:true
- no Close

## Scope Boundary For This Packet

This packet only writes:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy-approval-packet.md`

It does not modify backend, frontend, runtime, deployments, docs, longterm, AgentTeam, spec.md, plan.md, tasks.md, or existing evidence.

## Secret Hygiene

This packet records no token value, cookie value, credential, secret, access URL, `.env`, `.codex/auth.json`, or `.codex/config.toml` content.
