# WS Challenge Bridge Implementation Gate - 2026-05-06

Verdict: `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_DONE`

Not `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_BLOCKED`: the approved backend-only
source/test scope was sufficient, focused RED/GREEN evidence was captured, and
the required relevant backend tests passed.

This backend-only gate implemented the approved control-ui `WebSocket bridge`
handshake sequencing fix for upstream `connect.challenge` forwarding.

## Modified Files

| File | Change |
| --- | --- |
| `backend/internal/services/instance_proxy_service.go` | Starts the control-ui upstream-to-browser WebSocket pipe before first connect rewrite, so upstream pre-connect frames such as `connect.challenge` reach the browser before the rewritten first connect is written upstream. Desktop `/proxy` WebSocket flow remains on the existing pass-through path. |
| `backend/internal/services/instance_proxy_service_test.go` | Added a focused RED/GREEN regression test where upstream sends `connect.challenge` before browser first connect, and the browser then sends first connect with non-empty `device.nonce`. Existing control-ui auth rewrite, malformed first connect, upstream failure, and desktop pass-through tests remain in the required focused group. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation.md` | Added this evidence packet. |

No handler changes were made. `backend/internal/handlers/instance_handler.go`
remained read-only.

## Root-Cause Dependencies

| Dependency | Evidence | Result used |
| --- | --- | --- |
| Root Cause Investigation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md` | Commander-reviewed verdict `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`. Root cause: upstream sends `connect.challenge`, runtime client sets `connectNonce`, `buildConnectParams` sends `params.device.nonce`, and the previous backend bridge waited on browser first connect before forwarding upstream pre-connect frames. |
| Implementation Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation-approval-packet.md` | Commander-reviewed and user-approved verdict `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE`. |

Manual reproduction dependency:

- Entry: GTManager instance `10` / `oc2gi-185707`, via "打开 GTClaw 控制台".
- Route shape: GTManager mediated route
  `wss://localhost:30443/api/v1/instances/10/control-ui`.
- Page error:
  `invalid connect params: at /device/nonce: must NOT have fewer than 1 characters`.

## Implementation Summary

- The change applies only when `scope.AccessMode == AccessModeControlUI`.
- For control-ui WebSockets, the upstream-to-browser pipe now starts before
  `bridgeControlUIFirstConnect` reads and rewrites browser first connect.
- That single upstream reader forwards upstream pre-connect data frames,
  including `connect.challenge`, to the browser before first connect is
  committed upstream.
- The bridge still validates that browser first connect is a text JSON object
  with `method == "connect"` and object `params`.
- The bridge still replaces the entire `params.auth` object with
  server-owned token auth.
- Browser-provided auth fields remain untrusted and are not merged into the
  upstream first connect.
- Non-auth params are preserved, including `device.nonce`, `client`, `role`,
  `scopes`, `caps`, `locale`, `userAgent`, and future fields.
- After rewritten first connect is written upstream, only the browser-to-upstream
  pipe is added; the upstream-to-browser pipe continues as the one upstream
  reader. This avoids competing readers on the same WebSocket connection.
- Desktop `/proxy` WebSocket pass-through is unchanged and still uses the
  existing two-pipe behavior.
- Malformed first connect and upstream dial/connect failure remain sanitized.

## RED/GREEN Test Summary

RED before implementation:

```text
go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect'
exit 1
--- FAIL: TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect (0.50s)
    instance_proxy_service_test.go:548: client did not receive upstream connect.challenge before first connect: read tcp ...: i/o timeout
FAIL
FAIL    clawreef/internal/services
```

GREEN after implementation:

```text
go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect'
exit 0
ok      clawreef/internal/services
```

Focused RED/GREEN coverage:

| Requirement | Coverage |
| --- | --- |
| Upstream sends `connect.challenge` before browser first connect | New focused test upstream writes `connect.challenge` immediately after WebSocket upgrade. |
| Browser receives challenge before sending connect | New focused test reads the challenge from the browser WebSocket before sending first connect. |
| Rewritten connect keeps non-empty `device.nonce` | New focused test asserts upstream receives `params.device.nonce == "nonce-1"`. |
| `params.auth` uses server-owned token only | New focused test and existing auth rewrite test assert browser auth is replaced by server-owned token auth. |
| Browser auth fields are not merged | New focused test and existing auth rewrite test assert browser auth token/password/deviceToken material is absent from rewritten first connect. |
| Non-auth params preserved | New focused test and existing auth rewrite test assert `client`, `role`, `scopes`, `device`, `caps`, `locale`, `userAgent`, and future field preservation. |
| Desktop pass-through unaffected | Existing desktop WebSocket test remains in the required focused group and passed after the implementation. |
| Malformed first connect sanitized | Existing malformed first connect test remains in the required focused group and passed after the implementation. |
| Upstream dial/connect failure sanitized | Existing upstream failure test remains in the required focused group and passed after the implementation. |

## Full Test Summary

| Command | Exit | Result |
| --- | ---: | --- |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect'` before implementation | `1` | Expected RED: browser did not receive upstream `connect.challenge` before first connect. |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect'` after implementation | `0` | New focused challenge forwarding regression test passed. |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWith(ControlUIScope|DesktopScope)'` | `0` | Required focused WebSocket bridge tests passed. |
| `go test -count=1 ./internal/services ./internal/handlers` | `0` | Required relevant backend package tests passed. |

## Security Notes

- No browser upstream token exposure was introduced by this gate.
- Browser route auth, browser WebSocket headers, and browser
  `connect.params.auth` remain untrusted for upstream OpenClaw auth.
- The rewritten control-ui first connect uses server-owned token auth only.
- Browser-provided auth token/password/deviceToken fields are not merged.
- Desktop pass-through remains unchanged and continues to preserve the existing
  desktop WebSocket first frame/header behavior.
- Sanitized failure behavior remains in place for malformed first connect and
  upstream dial/connect failure.
- Evidence records no token values, cookie values, credential values, or access
  URLs.

## Blockers

None for this backend-only implementation gate.

Remaining gates are still separate and not authorized by this evidence:

- Commander review.
- Reviewer Gate.
- Verifier Gate with browser/manual E2E approval packet.
- Backend build/deploy approval if live verification is required.
- Browser/manual E2E rerun.
- Commit/push after approved verification evidence.

## Explicit Negatives

- no build/deploy
- no build/tag/push/pull
- no backend deploy/restart
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance creation/deletion/modification
- no K8S write
- no runtime mutation
- no database mutation
- no registry mutation
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no secrets/token/cookie/access URL plaintext output
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_DONE`, `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_BLOCKED`, `connect.challenge`, `device.nonce`, `first connect`, `WebSocket bridge`, `server-owned token`, `desktop pass-through`, `RED`, `GREEN`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan over modified backend files plus this evidence | `0` | No matches. Matched secret values would have been suppressed. |
| path-limited `git status --short` | `0` | Shows `backend/internal/services/instance_proxy_service.go` and `backend/internal/services/instance_proxy_service_test.go` modified, and the root-cause evidence, approval packet, and this implementation evidence as untracked in the requested path scope. |
