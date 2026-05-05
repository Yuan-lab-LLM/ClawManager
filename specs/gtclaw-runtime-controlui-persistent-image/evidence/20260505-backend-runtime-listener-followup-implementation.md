# Backend Runtime Listener Follow-up Implementation Gate

Date: 2026-05-05

Worker: BackendRuntimeListenerFollowupImplementationWorker

Topology: serial

## Verdict

BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE

This backend source/test gate implemented the OpenClaw runtime-facing hostname fix and added focused tests for hostname/label safety and Service `18789` exposure behavior. It did not build, deploy, restart backend, create a fresh instance, patch Kubernetes resources, mutate runtime/database/registry state, run browser E2E, set `passes:true`, or Close.

## Dependency Evidence

| Gate | Evidence | Status used |
| --- | --- | --- |
| Backend Runtime Listener Follow-up Implementation Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation-approval-packet.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_APPROVAL_PACKET_DONE`; user explicitly approved this implementation gate |
| Fresh rerun after cluster recovery | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED`; Service exposed `3001` but not `18789`; OpenClaw bound `0.0.0.0:18789` then exited because generated label exceeded `63 bytes` |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| Runtime Startup Artifact Implementation Gate | referenced by the fresh rerun dependency chain | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE` |

## Modified Files

Files changed by this gate:

- `backend/internal/services/k8s/pod_service.go`
- `backend/internal/services/k8s/pod_service_test.go`
- `backend/internal/services/k8s/service_service_test.go`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md`

Pre-existing dirty allowed files were observed before this gate and were not reverted or normalized by this gate:

- `backend/internal/services/instance_service.go`
- `backend/internal/services/instance_service_test.go`
- `backend/internal/services/k8s/client.go`
- `backend/internal/services/k8s/service_service.go`

No frontend, runtime, deployments, docs, longterm, AgentTeam, spec.md, plan.md, tasks.md, existing evidence, reviewed startup artifact, `/tmp/gtclaw-runtime-patch/**`, or `/tmp/gtclaw-startup-source-inspect-*/**` files were modified by this gate.

## Exact Code Behavior Summary

### OpenClaw runtime label/hostname safety

- `backend/internal/services/k8s/pod_service.go` now computes an OpenClaw-only runtime-facing pod hostname with `runtimeHostnameForPod`.
- For OpenClaw pods, the hostname is `clawreef-<instance-id>`, sanitized through the existing K8S name sanitizer.
- For non-OpenClaw pods, hostname remains empty so Kubernetes keeps the previous default behavior.
- The pod resource name still uses `GetPodName(instanceID, instanceName)`.
- The user-facing instance name is still preserved in pod metadata as `instance-name`.
- Service/Pod identity and routing still use stable `instance-id` and `app` labels/selectors, not the long user-facing name.

Byte-length proof from focused test:

- fresh blocked instance id: `9`
- runtime-facing hostname under test: `clawreef-9`
- decorated OpenClaw label under test: `clawreef-9 (OpenClaw)`
- decorated label byte length: `21`
- requirement: `<= 63 bytes`

### Service `18789` exposure

No Service source gap was proven in the current backend source, so `backend/internal/services/k8s/service_service.go` was not changed by this gate.

Current source behavior already includes:

- `additionalServicePortsForInstance("openclaw", 3001)` returns `18789`.
- OpenClaw create/start service config passes additional Service port `18789` alongside primary desktop `3001`.
- `ServiceService` builds Service ports with `3001` named `http` and `18789` named `control-ui`.
- Service selectors continue to use `instance-id` and `app`.
- Non-OpenClaw desktop service creation does not expose `18789` unless the caller explicitly requests it.
- Existing desktop websocket/service port behavior for `3000` and `8082` remains covered.

The live fresh instance Service exposed only `3001` because that instance was created by the currently running backend, and this gate was not authorized to build or deploy backend source. A Control Plane Backend Build/Deploy gate is required before rerunning the fresh instance proof.

## Tests Run

| Command | Exit | Result |
| --- | ---: | --- |
| `go test -count=1 ./internal/services/k8s -run 'TestCreateOpenClawPodUsesShortRuntimeHostnameForLongUserFacingName'` before implementation | `1` | Expected RED: failed because `Hostname was empty`. |
| `go test -count=1 ./internal/services/k8s -run 'TestCreate(OpenClawPodUsesShortRuntimeHostnameForLongUserFacingName\|NonOpenClawPodDoesNotSetRuntimeHostname\|ServiceCreatesDesktopAndControlUIPortsWhenRequested\|ServiceWithoutAdditionalPortsDoesNotExposeControlUI)'` after implementation | `0` | Focused OpenClaw hostname and Service exposure tests passed. |
| `go test -count=1 ./internal/services/k8s` | `0` | K8S service package tests passed. |
| `go test -count=1 ./internal/services -run 'TestAdditionalServicePorts\|TestBuildGatewayEnv\|Test.*OpenClaw.*Label\|Test.*Hostname\|Test.*Service'` | `0` | Focused instance service tests passed. |
| `go test -count=1 ./internal/services ./internal/services/k8s ./internal/handlers` | `0` | Approved backend package verification passed. |

## Focused Coverage

- OpenClaw long user-facing name produces runtime hostname `clawreef-9`, and `clawreef-9 (OpenClaw)` is exactly `21` bytes, satisfying `<= 63 bytes`.
- User-facing instance name remains preserved in pod metadata.
- Pod identity labels still include `instance-id` and `app`.
- Non-OpenClaw pod creation does not set the runtime-facing hostname.
- OpenClaw desktop primary `3001` service path includes additional `18789` control-ui port in current source.
- Service selectors still use `instance-id` and `app`.
- Service creation without additional ports does not expose `18789`.
- Existing desktop websocket/service port behavior remains intact through `3000` and `8082` coverage.

## Blockers

No blocker remains for this backend source/test implementation gate.

Remaining work is intentionally outside this gate:

- Control plane backend build/deploy approval and execution.
- Fresh 2Gi instance rerun proving Service exposes both `3001` and `18789`.
- Listener/hash verification.
- Browser E2E.
- Later `passes:true`, Close, and longterm write-back only after fresh E2E plus explicit user approval.

## Recommendation

Recommended next packet:

`Control Plane Backend Build/Deploy Approval Packet`

Do not rerun the isolated 2Gi fresh instance until backend build/deploy is explicitly approved and completed.

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- backend/internal/services/k8s/pod_service.go backend/internal/services/k8s/pod_service_test.go backend/internal/services/k8s/client.go backend/internal/services/k8s/client_test.go backend/internal/services/k8s/service_service.go backend/internal/services/k8s/service_service_test.go backend/internal/services/instance_service.go backend/internal/services/instance_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `0` | Requested path-limited whitespace check reported no errors. |
| `rg -n "BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE\|BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_BLOCKED\|18789\|3001\|label\|hostname\|63 bytes\|OpenClaw\|instance-id\|no build/tag/push/pull\|no backend deploy\|no fresh instance\|no browser E2E\|no passes:true\|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `0` | Required markers found. |
| secret-shape scan on this evidence file | `1` | No matches. |
| `git status --short -- backend/internal/services/k8s/pod_service.go backend/internal/services/k8s/pod_service_test.go backend/internal/services/k8s/client.go backend/internal/services/k8s/client_test.go backend/internal/services/k8s/service_service.go backend/internal/services/k8s/service_service_test.go backend/internal/services/instance_service.go backend/internal/services/instance_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `0` | Shows allowed dirty paths only in this status scope. |
| additional no-index whitespace scan for untracked gate files | `0` | No output; no whitespace errors found in new/untracked gate files. |

## Explicit Negatives

- no build/tag/push/pull
- no backend deploy
- no backend restart
- no fresh instance
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no K8S write
- no runtime mutation
- no database mutation
- no registry mutation
- no existing pod patch
- no existing Service patch
- no cleanup/delete instance 9
- no frontend change
- no deployments change
- no docs change
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close
- no token value, cookie value, credential, secret, or access URL plaintext
