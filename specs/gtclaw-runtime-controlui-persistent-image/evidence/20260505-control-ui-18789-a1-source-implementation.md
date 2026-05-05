# Control UI 18789 A1 Source Implementation Packet

Date: 2026-05-05

Verdict: `A1_SOURCE_IMPLEMENTATION_BLOCKED`

## Scope Result

Backend source-level A1 changes are implemented and focused backend tests pass. Full A1 remains blocked because the approved repository scope does not contain a modifiable OpenClaw gateway startup source/build-context artifact that can set the pod/service-facing bind mode (`gateway.bind=lan`, `--bind lan`, or equivalent). I did not guess a runtime startup fix by editing an unproven image tar or running runtime/K8S/browser mutation.

The source work completed in this gate covers `3001/TCP`, `18789/TCP`, `control-ui`, `token auth`, `server-side` token handling, `Service generation`, and `proxy upstream` behavior. It does not perform runtime verification.

## Implemented Source Changes

| Area | Result |
| --- | --- |
| Service generation | OpenClaw create/start lifecycle now adds `18789/TCP` as an additional Service port when the primary desktop port is `3001/TCP`; non-OpenClaw `3001/TCP` instances do not expose `18789/TCP`. Existing `3000/8082` behavior is preserved. |
| Runtime token source | OpenClaw pod env now includes `OPENCLAW_GATEWAY_TOKEN` from the existing server-side per-instance `AccessToken`; the token is not returned through JSON because the model field is `json:"-"`. |
| Proxy upstream | `/control-ui` proxy keeps browser-facing route token/cookie validation, then derives OpenClaw upstream auth server-side from the instance record. |
| Proxy auth header handling | `/control-ui` strips browser `Authorization`, `Cookie`, and `X-OpenClaw-Token` before upstream and injects `Authorization: Bearer <server-side token>`. |
| WebSocket source path | The WebSocket control-ui proxy path calls the same upstream auth helper as HTTP, so source-level header handling is consistent. |
| Desktop no-injection | Desktop `/proxy` does not receive the OpenClaw gateway token and keeps existing desktop header behavior. |
| No fallback | control-ui upstream failures still fail as control-ui errors and do not fall back to desktop `3001/TCP`. |

## Blocker

The full A1 runtime startup requirement is not complete in source:

- The repo search found no approved OpenClaw runtime startup source/build-context file such as `/defaults/openclaw-agent/config.yaml` or `/etc/services.d/openclaw-agent/run`.
- Local OpenClaw package evidence confirms HTTP token auth accepts `Authorization: Bearer <token>` and startup accepts `OPENCLAW_GATEWAY_TOKEN`, but bind mode still requires a real startup/config source change (`gateway.bind`, `--bind`, or equivalent).
- Local OpenClaw package evidence also shows the control-ui WebSocket handshake uses `connectParams.auth.token`; this source gate injects the same server-side bearer header on the WebSocket upstream handshake, but acceptance of that contract must be proven in the Runtime Verification Gate or resolved by an approved protocol/topology design.

Next approval needed: authorize a Runtime Startup Source Artifact Gate that identifies the exact modifiable startup source/build-context file for OpenClaw gateway bind/auth, or approve a narrow protocol design gate for control-ui WebSocket upstream auth if bearer handshake auth is not accepted by the runtime.

## Modified Files

- `backend/internal/services/instance_service.go`
- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/handlers/instance_handler.go`
- `backend/internal/services/instance_service_test.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md`

## Tests

| Command | Result |
| --- | --- |
| `go test ./internal/services ./internal/services/k8s ./internal/handlers` from `backend/` before implementation | Failed as expected: missing `ProxyRequestWithScopeAndUpstreamAuth`, `InstanceProxyUpstreamAuth`, and `additionalServicePortsForInstance`. |
| `go test ./internal/services ./internal/services/k8s ./internal/handlers` from `backend/` after implementation | Passed: `ok clawreef/internal/services`, `ok clawreef/internal/services/k8s`, `ok clawreef/internal/handlers`. |

Focused coverage added/updated:

- OpenClaw Service generation includes `3001/TCP` plus `18789/TCP`.
- non-OpenClaw desktop Service generation does not expose `18789/TCP`.
- `/control-ui` proxy upstream remains ServiceIP service-port based and control-ui scoped.
- upstream token auth is injected only for `/control-ui`.
- desktop `/proxy` does not inject the OpenClaw gateway token.
- browser Authorization/Cookie/token headers are not used as OpenClaw upstream credentials.
- WebSocket control-ui proxy source path uses the same upstream auth helper.
- control-ui failure does not fallback to desktop `3001/TCP`.

## Negative Evidence

- no runtime mutation
- no K8S write
- no database write
- no registry mutation
- no build/tag/push/pull image
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance create/delete/modify
- no runtime image/resource setting mutation
- no manual Service patch
- no secrets plaintext in this packet
- no Mem0
- no longterm
- no passes:true
- no Close

## Verification

| Command | Result |
| --- | --- |
| `git diff --check -- backend/internal/services/instance_service.go backend/internal/services/instance_proxy_service.go backend/internal/handlers/instance_handler.go backend/internal/services/instance_proxy_service_test.go backend/internal/services/instance_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Passed with no whitespace errors reported. |
| `go test ./internal/services ./internal/services/k8s ./internal/handlers` from `backend/` | Passed. |
| `rg -n "A1_SOURCE_IMPLEMENTATION_DONE|A1_SOURCE_IMPLEMENTATION_BLOCKED|3001/TCP|18789/TCP|control-ui|token auth|server-side|Service generation|proxy upstream|no browser E2E|no runtime mutation|no registry mutation|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Passed; all required markers found. |
| `git status --short -- backend/internal/services/instance_service.go backend/internal/services/instance_proxy_service.go backend/internal/handlers/instance_handler.go backend/internal/services/instance_proxy_service_test.go backend/internal/services/instance_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Shows the three touched backend source files modified and the two backend test files plus this evidence file untracked. |
