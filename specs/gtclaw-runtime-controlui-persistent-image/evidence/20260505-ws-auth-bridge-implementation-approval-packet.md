# WS Auth Bridge Implementation Approval Packet - 2026-05-05

Verdict: `WS_AUTH_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE`

This packet requests explicit user approval for the next gate:

`Do you approve executing the WS Auth Bridge Implementation Gate under the scope below?`

No implementation was performed in this gate. This evidence records no implementation, no build/tag/push/pull, no browser E2E, no runtime mutation, no K8S mutation, no database mutation, no registry mutation, no fresh instance mutation, no longterm write, no Mem0 write, no passes:true, and no Close.

## Dependency Gates

| Gate | Required state |
| --- | --- |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED` |
| Runtime Startup / WebSocket Auth Contract Investigation | `RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED` |
| External Expert verdict | `WS_BRIDGE_REQUIRED` |
| WS Auth Protocol Design Gate | `WS_AUTH_PROTOCOL_DESIGN_DONE` |

The approval request is narrow: implement the backend WebSocket bridge for control-ui auth only. It does not approve runtime startup source recovery, runtime startup artifact implementation, image work, browser E2E, or fresh instance work.

## Files Allowed If User Approves

If the user explicitly approves the `WS Auth Bridge Implementation Gate`, the implementation may edit only:

| File | Allowed purpose |
| --- | --- |
| `backend/internal/services/instance_proxy_service.go` | Implement control-ui-only WebSocket bridge behavior, first connect frame validation/rewrite, server-side token injection, sanitized errors, and unchanged desktop path. |
| `backend/internal/services/instance_proxy_service_test.go` | Add focused tests for the WebSocket bridge behavior and regressions. |
| `backend/internal/handlers/instance_handler.go` | Only if needed for sanitized handler-level WebSocket errors or explicitly scoped bridge options. |

## Default Deny List

The implementation approval must not include these changes unless a later packet explicitly authorizes them:

| Path or area | Default decision |
| --- | --- |
| `backend/internal/services/instance_service.go` | Denied by default. Only allowed later if the user separately approves a dedicated gateway token/secret-ref design. |
| `frontend/**` | Denied. No HTML/JS rewrite and no embedding OpenClaw gateway token. |
| `runtime/**` | Denied. |
| `deployments/**` | Denied. |
| `docs/**` | Denied. |
| `longterm/**` | Denied. |
| `AgentTeam/**` | Denied. |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Denied. |
| `specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Denied. |
| `specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | Denied. |
| Existing evidence files | Denied. |
| `/tmp/gtclaw-runtime-patch/**` | Denied. Read-only reference only; no edits. |

## Implementation Scope Draft

The approved implementation should add a backend control-ui-only WebSocket bridge.

Required behavior:

1. Apply only when the resolved route scope is control-ui.
2. Preserve the existing upstream handshake `Authorization: Bearer <token>` only as defense-in-depth.
3. Do not treat handshake `Authorization: Bearer <token>` as sufficient completion for WebSocket auth unless later runtime/source proof proves equivalence to `connectParams.auth.token`.
4. Intercept the first browser-to-upstream text JSON WebSocket frame before starting the normal bidirectional pipe.
5. Validate that the first connect frame is a JSON request with `method:"connect"` and object `params`.
6. Preserve the connect request envelope and approved non-auth params, including request `type`, `id`, `method`, `minProtocol`, `maxProtocol`, `client`, `role`, `scopes`, `device`, `caps`, `userAgent`, `locale`, and any later-approved challenge or nonce fields.
7. Replace the entire `params.auth` object with server-owned auth containing the server-side token as `connectParams.auth.token`.
8. Never merge browser-provided auth fields into upstream OpenClaw auth.
9. Send the rewritten first connect frame upstream.
10. Start the normal bidirectional pipe only after the rewritten first connect frame is sent.
11. Keep non-control-ui desktop `/proxy/` websocket/proxy behavior unaffected.

Recommended auth object policy:

- Preserve selected non-auth `params` fields.
- Replace the entire `params.auth` object.
- Do not merge browser-provided `params.auth.token`, `params.auth.password`, `params.auth.deviceToken`, query token, cookie token, or header token into upstream auth.

This design is intended to provide no browser token exposure while still satisfying the visible OpenClaw control-ui contract of `connectParams.auth.token`.

## Failure Handling Requirements

The approved implementation must handle failures as follows:

| Condition | Required handling |
| --- | --- |
| Non-text first frame | Sanitized close/failure; do not forward upstream. |
| Invalid JSON first frame | Sanitized close/failure; do not forward upstream. |
| Wrong first method | Sanitized close/failure; do not forward upstream. |
| Missing `params` | Sanitized close/failure; do not forward upstream. |
| Non-object `params` | Sanitized close/failure; do not forward upstream. |
| Upstream WebSocket dial failure | Sanitized failure; no token/cookie/secret/full payload in returned errors or log-like strings. |
| Upstream connect failure after rewritten first connect | Sanitized handling; no server-side token, route token, cookie value, secret, or full payload in returned errors or log-like strings. |

The implementation must not write the server-side token, browser route token, cookie value, access URL, or first-frame full payload to returned errors, logs, test failure messages, or evidence.

## Required Focused Tests

The implementation approval should require tests for:

| Test | Required assertion |
| --- | --- |
| Connect injection success | A browser first connect frame without trusted auth reaches upstream with server-side `connectParams.auth.token`. |
| Browser auth stripped/not forwarded | Browser `Authorization`, `Cookie`, `X-OpenClaw-Token`, and query token are not forwarded as upstream OpenClaw auth. |
| Malicious browser connect auth overwritten | Browser-provided `connect.params.auth` is overwritten, not trusted, and not merged into upstream auth. |
| No token in errors/log-like returned strings | Dial failure, malformed first frame, and upstream connect failure paths do not include token/cookie/secret values. |
| Desktop `/proxy/` websocket/proxy unaffected | Non-control-ui desktop websocket/proxy routes do not use the control-ui bridge or auth rewrite. |
| Malformed first frame not forwarded | Non-text, invalid JSON, wrong method, missing params, or non-object params never reach upstream. |
| Upstream connect failure sanitized | Rewritten connect rejection does not leak the server-side token or browser route credentials. |
| Envelope and non-auth params preserved | The bridge preserves request envelope fields and approved non-auth params while replacing `params.auth`. |

## Security Constraints

The implementation approval is valid only if these security boundaries remain true:

- OpenClaw gateway token is a server-side token only.
- There must be no browser token exposure: no browser response JSON, no URL/query/hash, no localStorage, no cookie, no console output, no logs, and no evidence entry may contain the upstream token value.
- Browser route token/cookie authenticates the browser to GTManager only, not to OpenClaw upstream.
- Browser-provided `Authorization`, `Cookie`, `X-OpenClaw-Token`, query token/password, and `connect.params.auth` are untrusted for upstream OpenClaw auth.
- No HTML/JS rewrite may embed or expose the OpenClaw gateway token.

## Still Not Authorized

This packet does not authorize:

- Runtime Source Artifact Recovery.
- Runtime Startup Artifact Implementation.
- build/tag/push/pull.
- Fresh instance creation, deletion, restart, patch, or mutation.
- Browser E2E, Chrome DevTools MCP, or Playwright.
- Runtime image/resource setting mutation.
- K8S write, database write, or registry mutation.
- Manual pod patch, manual Service patch, or `kubectl cp` write.
- Mem0 write, longterm write, passes:true, or Close.

## Recommended Follow-Up Gate Order

If the user approves this packet:

1. `WS Auth Bridge Implementation Gate`.
2. `Runtime Source Artifact Recovery Approval Packet`.
3. `Runtime Startup Artifact Implementation Gate`.
4. Isolated fresh instance gate.
5. Listener/hash gate.
6. Browser E2E gate.

If the user does not approve the `WS Auth Bridge Implementation Gate`, the feature remains blocked for WebSocket control-ui auth and should not proceed to Runtime Startup Artifact Implementation.

## Approval Decision Needed

User decision requested:

- Approve: authorize `WS Auth Bridge Implementation Gate` under this packet's exact file scope, implementation scope, failure handling, tests, and security constraints.
- Reject or defer: do not implement the bridge; keep runtime startup artifact work blocked until WebSocket auth is resolved by another approved design.

## Verification Results

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation-approval-packet.md
```

Result: exit `0`; no whitespace errors.

```bash
rg -n "WS_AUTH_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE|WS_AUTH_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED|WS_AUTH_PROTOCOL_DESIGN_DONE|WS_BRIDGE_REQUIRED|connectParams.auth.token|WebSocket bridge|first connect|server-side token|no browser token|no implementation|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation-approval-packet.md
```

Result: exit `0`; required markers found.

```bash
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
```

Result: exit `0`; status output:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation-approval-packet.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-protocol-design.md
```
