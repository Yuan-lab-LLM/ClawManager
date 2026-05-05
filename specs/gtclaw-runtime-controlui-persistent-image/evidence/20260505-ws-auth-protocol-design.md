# WS Auth Protocol Design Gate - 2026-05-05

Verdict: `WS_AUTH_PROTOCOL_DESIGN_DONE`

External Expert input accepted: `WS_BRIDGE_REQUIRED`.

Recommended next gate: `WS Auth Bridge Implementation Approval Packet`.

Secondary gate still required before runtime startup artifact work: `Runtime Source Artifact Recovery Approval Packet`.

This gate is design-only: no implementation, no build/tag/push/pull, no browser E2E, no runtime mutation, no K8S mutation, no database mutation, no registry mutation, no longterm write, no Mem0 write, no passes:true, no Close.

## Scope

Allowed write was limited to this evidence file.

Design question: can the feature move from the blocked A1 source/runtime-startup investigation state into a backend WebSocket bridge implementation approval packet without exposing the OpenClaw gateway token to browser code or route data?

Dependency state:

| Dependency | Accepted state |
| --- | --- |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED` |
| Runtime Startup / WebSocket Auth Contract Investigation | `RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED` |
| External Expert verdict | `WS_BRIDGE_REQUIRED` |
| External Expert recommended next gate | WS Auth Protocol Design Gate |

## Evidence Inputs

Read-only sources inspected:

| Source | Relevant finding |
| --- | --- |
| `backend/internal/services/instance_proxy_service.go` | Control-ui upstream auth helper strips browser `Authorization`, `Cookie`, and `X-OpenClaw-Token`, then sets upstream `Authorization: Bearer <token>`. The WebSocket path dials upstream with those headers, upgrades the browser socket, and then pipes frames without first connect inspection or rewrite. |
| `backend/internal/handlers/instance_handler.go` | GTManager route token/cookie is validated before proxying. The handler calls `ControlUIUpstreamAuthForInstance` and forwards WebSocket upgrades to `ProxyWebSocketWithScopeAndUpstreamAuth`. Handler changes are not expected unless a later implementation approval requires extra bridge options or context. |
| `backend/internal/services/instance_service.go` | Current A1 source uses the server-side instance token to populate `OPENCLAW_GATEWAY_TOKEN` for OpenClaw instances. A dedicated gateway token or secret-ref would be a later explicit design/approval decision. |
| `backend/internal/services/instance_proxy_service_test.go` | Existing focused tests cover HTTP header stripping/injection and the shared header helper, but there is no first connect WebSocket bridge coverage. |
| `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | Browser client constructs `new WebSocket(this.opts.url)`, so it cannot set arbitrary browser WebSocket handshake Authorization headers. After open, it calls `request("connect", buildConnectParams(...))`; the request frame is equivalent to `{type:"req", id, method:"connect", params}` and `buildConnectParams` includes `auth:e.auth`. Token auth maps to the visible contract `connectParams.auth.token`. |

No token value, no cookie value, no token-bearing URL, and no credential was recorded.

## Recommended Design

Implement a backend control-ui-only WebSocket bridge.

The bridge belongs in `backend/internal/services/instance_proxy_service.go` behind the existing control-ui route scope. It must apply only when `scope.AccessMode == AccessModeControlUI`. Non-control-ui desktop websocket/proxy behavior must keep the current direct bidirectional pipe behavior.

Keep upstream handshake `Authorization: Bearer <token>` as defense-in-depth. It is useful if the exact OpenClaw server version also honors handshake Authorization, but it must not be treated as the completion path unless runtime/source proof later proves equivalence to `connectParams.auth.token`.

The completion path is server-side injection of the OpenClaw gateway token into the first connect frame:

1. GTManager validates the browser route token/cookie before proxying, as it does today.
2. Backend dials the upstream control-ui WebSocket with browser auth headers stripped and the existing server-side upstream `Authorization: Bearer <token>` defense-in-depth header set.
3. Backend upgrades the browser socket.
4. For control-ui only, backend reads the first browser-to-upstream WebSocket message before starting the normal bidirectional pipe.
5. The first browser message must be a text JSON request with `method:"connect"` and an object `params`.
6. Backend preserves the request envelope fields needed by the client protocol, including `type`, `id`, `method`, and non-auth `params` fields such as `minProtocol`, `maxProtocol`, `client`, `role`, `scopes`, `device`, `caps`, `userAgent`, `locale`, and future challenge/nonce fields when present.
7. Backend replaces the entire `params.auth` object with a server-owned auth object containing the server-side token, equivalent to `connectParams.auth.token`.
8. Backend does not merge browser-provided `params.auth.token`, `params.auth.password`, `params.auth.deviceToken`, or equivalent auth material into upstream auth.
9. Backend writes the rewritten first connect request upstream.
10. After the rewritten first connect frame is sent, backend starts the existing bidirectional pipe.

Recommended `params.auth` policy: preserve selected non-auth `params` fields, but replace the entire `params.auth` object with server-owned auth. This is safer than setting only `params.auth.token` on a browser-supplied auth object, because it prevents malicious browser auth fields from riding alongside the server-side token. If runtime/source proof later shows required non-secret auth subfields, add an explicit allowlist and document it before implementation.

## Failure Handling

Malformed first frame behavior:

- If the first frame is not a text frame, is not valid JSON, is not an object request, has a missing or non-string `method`, has `method` other than `connect`, or has non-object `params`, the bridge must close the client and upstream sockets with a generic protocol error.
- The malformed first frame must not be forwarded upstream.
- Returned errors and logs must not include the OpenClaw gateway token, browser route token, cookie value, or full client payload.

Upstream connect failure behavior:

- If upstream WebSocket dial fails, keep the existing generic bad-gateway style failure but sanitize the error path so no server-side token is printed or returned.
- If the rewritten connect request reaches upstream and upstream responds with a normal protocol error, the bridge may pass protocol frames through, but implementation tests must verify that no shared upstream token is echoed by the bridge's own errors or logs.
- If runtime/source proof later shows upstream can echo the submitted token, implementation must redact that field before forwarding or logging.

## Security Boundary

OpenClaw gateway token handling:

- The OpenClaw gateway token is a server-side token only.
- There must be no browser token exposure: no browser response JSON, no URL/query/hash, no localStorage, no cookie, no console output, no client-visible generated script, no logs, and no evidence entry may contain the upstream token value.
- Browser-provided `Authorization`, `Cookie`, `X-OpenClaw-Token`, query token/password values, and `connect.params.auth` are not trusted as upstream OpenClaw auth.
- The route token/cookie authenticates the browser to GTManager only. It is not accepted as OpenClaw upstream auth.
- HTML/JS rewrite that embeds or exposes the OpenClaw gateway token is rejected for this gate.

## Minimal Implementation Scope Draft

Implementation is not authorized by this evidence.

When implementation is later approved, the minimal expected scope is:

| File | Expected scope |
| --- | --- |
| `backend/internal/services/instance_proxy_service.go` | Add control-ui-only WebSocket bridge helpers for first connect frame read, validation, auth replacement, sanitized failure handling, and post-connect piping. Preserve desktop websocket/proxy behavior. |
| `backend/internal/services/instance_proxy_service_test.go` | Add focused unit/integration tests around bridge behavior with a local test upstream WebSocket server. |
| `backend/internal/handlers/instance_handler.go` | Only if needed to pass explicit bridge options or sanitize handler-level WebSocket errors; no handler change is expected for the baseline design. |
| `backend/internal/services/instance_service.go` | Only if a later approval explicitly introduces a dedicated gateway token or secret-ref separate from the current instance AccessToken/`OPENCLAW_GATEWAY_TOKEN` source. |

No frontend, runtime, deployment, K8S, database, registry, or browser E2E work is included in this design gate.

## Required Test Design

Implementation approval should require focused tests for:

| Test | Required assertion |
| --- | --- |
| Connect injection success | Browser sends a first connect frame without trusted auth; upstream receives a connect frame whose `params.auth.token` was injected from the server-side token. |
| Browser auth stripped/not forwarded | Browser `Authorization`, `Cookie`, `X-OpenClaw-Token`, and query token values are not forwarded as upstream OpenClaw auth. |
| Malicious browser connect auth overwritten | Browser-provided `connect.params.auth.token`, `password`, `deviceToken`, or related fields are overwritten by replacing the entire `params.auth` object, or later safely merged only by an explicit allowlist. |
| No token in errors/log-like returned messages | Failed dial, malformed first frame, and upstream connect failure paths do not include any token/cookie/secret values in returned errors or log-like strings. |
| Non-control-ui desktop websocket/proxy unaffected | Desktop `/proxy/` WebSocket and regular proxy routes continue direct pass-through behavior and do not receive control-ui connect injection. |
| Malformed first frame behavior | Non-text, invalid JSON, wrong method, missing params, or non-object params close with sanitized protocol failure and are not forwarded upstream. |
| Upstream connect failure behavior | Upstream rejection after rewritten first connect is handled without leaking the server-side token and without changing desktop behavior. |
| Envelope preservation | Rewritten first connect preserves request `type`, `id`, `method`, and approved non-auth params needed by the runtime protocol. |

## Runtime/Source Proof Still Needed

These items remain proof requirements before runtime completion can be claimed:

1. Exact OpenClaw connect message schema for the current runtime version, including whether the envelope is always `{type:"req", id, method:"connect", params}`.
2. Whether `device`, `client`, `role`, `scopes`, `caps`, `userAgent`, `locale`, challenge nonce, or other fields must be preserved exactly.
3. Whether the OpenClaw server ever echoes the shared upstream token in connect responses, errors, events, logs, or debug frames.
4. Whether upstream handshake `Authorization: Bearer <token>` has any protocol effect. Until proven, it remains defense-in-depth only.

## Gate Decision

The WS Auth Protocol Design Gate can close as `WS_AUTH_PROTOCOL_DESIGN_DONE` because the next implementation direction is specific, bounded, and testable without exposing the upstream OpenClaw token to the browser.

Recommended next gate: `WS Auth Bridge Implementation Approval Packet`.

Do not enter Runtime Startup Artifact Implementation yet. A separate `Runtime Source Artifact Recovery Approval Packet` is still required to recover or create approved startup source/build-context for `gateway.bind=lan`, `--bind lan`, or equivalent pod-facing bind behavior.

## Verification Results

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md
```

Result: exit `0`; no whitespace errors.

```bash
rg -n "WS_AUTH_PROTOCOL_DESIGN_DONE|WS_AUTH_PROTOCOL_DESIGN_BLOCKED|WS_BRIDGE_REQUIRED|connectParams.auth.token|Authorization: Bearer|WebSocket bridge|first connect|server-side token|no browser token|no implementation|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md
```

Result: exit `0`; required markers found.

```bash
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
```

Result: exit `0`; status output:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md
```
