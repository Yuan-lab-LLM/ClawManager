# WS Auth Bridge Implementation Gate - 2026-05-05

Verdict: `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`

This backend-only gate implemented the approved control-ui `WebSocket bridge` for first connect auth injection.

Commander review fix addendum: Commander found that control-ui upstream query stripping removed browser route `token` but not browser `password`, while the approval packet marks browser query token/password as untrusted upstream auth. This addendum keeps the verdict as `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` after the backend-only patch and focused tests passed.

## Modified Files

| File | Change |
| --- | --- |
| `backend/internal/services/instance_proxy_service.go` | Added control-ui-only WebSocket bridge behavior and a shared scope-aware upstream query helper. Control-ui strips browser route `token` and query password; desktop keeps existing behavior. |
| `backend/internal/services/instance_proxy_service_test.go` | Added/updated focused tests for connect injection, browser auth stripping, malicious auth overwrite, desktop pass-through, malformed first frame rejection, sanitized upstream dial failure, non-auth param preservation, and query password stripped behavior. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | Added and updated this implementation evidence. |

`backend/internal/handlers/instance_handler.go` was not changed by this gate.

## Implementation Summary

- The bridge activates only when `scope.AccessMode == AccessModeControlUI`.
- Non-control-ui desktop `/proxy/` websocket/proxy paths keep the existing direct pass-through behavior.
- The existing upstream handshake `Authorization: Bearer <token>` remains defense-in-depth only.
- Before starting the normal bidirectional pipe, control-ui WebSocket proxy reads the first browser-to-upstream frame.
- The first frame must be a text JSON object with `method:"connect"` and object `params`.
- The bridge preserves the request envelope and non-auth params, including unknown future non-auth fields.
- The bridge replaces the entire `params.auth` object with server-owned auth containing `connectParams.auth.token`.
- Browser-provided auth material is not merged into upstream OpenClaw auth.
- The rewritten first connect frame is written upstream before the normal bidirectional pipe starts.
- Malformed first frame handling closes with a sanitized failure and does not forward the frame upstream.
- Upstream WebSocket dial failure now returns a sanitized error without including dial details or credential-bearing material.
- Shared upstream query handling now strips browser route `token` for all proxy scopes.
- For control-ui HTTP and WebSocket upstream requests, query password is stripped along with route token.
- Desktop `/proxy/` HTTP and WebSocket upstream query behavior remains unchanged except for existing route token stripping; desktop password query values are preserved as before.

## Security Summary

- OpenClaw gateway token remains a server-side token.
- Browser route token/cookie authenticates to GTManager only, not OpenClaw upstream.
- Browser `Authorization`, `Cookie`, `X-OpenClaw-Token`, query token, and browser `connect.params.auth` are not trusted as upstream OpenClaw auth.
- The implementation does not expose the OpenClaw gateway token to browser JS, URL/query/hash, localStorage, cookie, logs, returned errors, or evidence.
- The evidence records no browser token values, no cookie values, no credential values, and no access URLs.
- Explicit negatives: no build/tag/push/pull, no runtime/K8S/database/registry mutation, no browser E2E, no Mem0 write, no longterm write, no passes:true, no Close.

## Focused Test Coverage

| Requirement | Coverage |
| --- | --- |
| Connect injection success | Upstream receives rewritten first connect with server-side `connectParams.auth.token`. |
| Browser auth stripped/not forwarded | Upstream does not receive browser `Cookie` or `X-OpenClaw-Token`; control-ui upstream auth uses the server-side header path and first-frame injection. |
| Browser query token stripped | Upstream request URI preserves non-token query fields and strips token query fields. |
| Control-ui query password stripped | Control-ui HTTP and WebSocket upstream request URIs preserve `session` and remove browser query password. |
| Desktop query behavior unaffected | Desktop HTTP and WebSocket upstream request URIs preserve non-auth desktop query fields, including password, matching existing behavior. |
| Malicious browser auth overwritten | Browser `connect.params.auth` is replaced rather than merged. |
| Desktop unaffected | Desktop `/proxy/` WebSocket preserves pass-through first frame and browser headers. |
| Non-text first frame | Not forwarded upstream. |
| Invalid JSON first frame | Not forwarded upstream. |
| JSON not object | Not forwarded upstream. |
| Wrong method | Not forwarded upstream. |
| Missing params | Not forwarded upstream. |
| Non-object params | Not forwarded upstream. |
| Upstream dial failure sanitized | Returned error is the sanitized upstream websocket failure string and does not include credential material. |
| Envelope/non-auth params preserved | Request `type`, `id`, `method`, and approved/future non-auth params are preserved. |

## Tests Run

| Command | Exit | Notes |
| --- | --- | --- |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWith(ControlUIScope|DesktopScope)'` before implementation | `1` | Expected RED; current source forwarded browser auth first frame and malformed first frames. |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWith(ControlUIScope|DesktopScope)'` after implementation | `0` | Focused WebSocket bridge tests passed. |
| `go test -count=1 ./internal/services -run 'TestProxy(WebSocket|Request)With(ControlUIScope|DesktopScope)'` before Commander review fix | `1` | Expected RED; control-ui HTTP and WebSocket upstream queries preserved browser query password. |
| `go test -count=1 ./internal/services -run 'TestProxy(WebSocket|Request)With(ControlUIScope|DesktopScope)'` after Commander review fix | `0` | Focused HTTP/WS control-ui query password stripped tests passed; desktop query behavior remained unaffected. |
| `go test -count=1 ./internal/services ./internal/handlers` | `0` | Required backend package verification passed. |

## Blockers

None for this backend-only implementation gate.

Remaining gates are still separate and not authorized by this evidence:

- Runtime Source Artifact Recovery.
- Runtime Startup Artifact Implementation.
- Fresh instance creation or mutation.
- Browser E2E.
- build/tag/push/pull.

## Verification Results

Latest Commander review fix verification:

```bash
git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```

Result: exit `0`; no whitespace errors.

```bash
go test -count=1 ./internal/services -run 'TestProxy(WebSocket|Request)With(ControlUIScope|DesktopScope)'
```

Result: exit `0`.

```bash
go test -count=1 ./internal/services ./internal/handlers
```

Result: exit `0`.

```text
ok  	clawreef/internal/services
ok  	clawreef/internal/handlers
```

```bash
rg -n "WS_AUTH_BRIDGE_IMPLEMENTATION_DONE|WS_AUTH_BRIDGE_IMPLEMENTATION_BLOCKED|query password|password stripped|WebSocket bridge|first connect|connectParams.auth.token|server-side token|no browser token|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```

Result: exit `0`; required markers found.

Required secret-shape scan over this evidence file plus the authorized backend implementation paths.

Result: exit `1`; no matches.

```bash
git status --short -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```

Result: exit `0`; status output:

```text
 M backend/internal/services/instance_proxy_service.go
?? backend/internal/services/instance_proxy_service_test.go
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```
