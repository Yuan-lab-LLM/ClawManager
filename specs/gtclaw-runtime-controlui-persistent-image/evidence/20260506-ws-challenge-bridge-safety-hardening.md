# WS Challenge Bridge Safety Hardening - 2026-05-06

Verdict: `WS_CHALLENGE_BRIDGE_SAFETY_HARDENING_DONE`

Not `WS_CHALLENGE_BRIDGE_SAFETY_HARDENING_BLOCKED`: the allowed backend
service/test scope was sufficient to harden the control-ui WebSocket handshake
failure paths and to prove the required behaviors with focused tests.

## Code Changed

Yes.

Additional hardening was limited to
`backend/internal/services/instance_proxy_service.go`:

- `bridgeControlUIFirstConnect` now receives `ctx`.
- The first-connect wait now fails closed on `ctx.Done()` as well as upstream
  pre-connect failure and browser first-frame failure.
- The same sanitized close path is used for context cancellation before first
  connect.

No handler, frontend, runtime, deployment, database, registry, docs, longterm,
AgentTeam, spec, plan, or tasks files were modified.

## Safety Concerns Checked

| Concern | Result |
| --- | --- |
| control-ui-only | The bridge hardening is still inside `AccessModeControlUI`; desktop `/proxy` remains on the existing pass-through path. |
| desktop pass-through | Existing desktop WebSocket first-frame/header test remains in the required focused run and passed. |
| no competing readers | Control-ui uses one upstream reader: the upstream-to-browser pipe starts before first connect and continues after first connect. Browser first connect is read before starting the browser-to-upstream pipe, so there is no competing readers condition on either connection. |
| goroutine lifecycle | Added deterministic close/read-deadline coverage for malformed first connect, upstream pre-connect close, browser pre-connect disconnect, and context cancellation before first connect. |
| timeout | Existing first-frame read deadline remains in place; context cancellation now exits the wait without waiting for the full first-frame timeout. |
| close behavior | Malformed first connect, upstream pre-connect close, browser pre-connect disconnect, and context cancellation all close or fail the WebSocket path without allowing normal continued communication. |
| fail closed | Malformed browser first connect is not forwarded upstream; upstream pre-connect close fails the browser side closed; context cancellation before first connect returns the handler and closes upstream/browser paths. |
| sanitized failure | Failure assertions check close/read errors do not include route token shape, upstream token shape, browser auth header shape, or browser OpenClaw token header shape. |
| challenge forwarding | `connect.challenge` still reaches the browser before rewritten connect, allowing browser connect with non-empty `device.nonce`. |
| auth rewrite | `params.auth` is still replaced as a whole with server-owned token auth. Browser auth fields are not merged. |
| non-auth params | Existing challenge/auth tests preserve `device.nonce`, `client`, `role`, `scopes`, `caps`, `locale`, `userAgent`, and future fields. |

## Tests Added Or Updated

- Updated `TestProxyWebSocketWithControlUIScopeRejectsMalformedFirstFrameWithoutForwarding`
  to prove malformed first connect closes/fails both browser and upstream sides
  and does not leak sensitive material into observed errors.
- Added `TestProxyWebSocketWithControlUIScopeUpstreamCloseBeforeFirstConnectFailsClosed`.
- Added `TestProxyWebSocketWithControlUIScopeBrowserDisconnectBeforeFirstConnectDoesNotHang`.
- Added `TestProxyWebSocketWithControlUIScopeContextCancelBeforeFirstConnectDoesNotHang`.
- Added shared `requireWebSocketReadFailureForTest` helper for deterministic
  close/read-deadline assertions.

Existing required coverage retained:

- `TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect`
  proves challenge forwarded before rewritten connect and non-empty
  `device.nonce`.
- `TestProxyWebSocketWithControlUIScopeInjectsConnectAuthAndPreservesParams`
  proves server-owned token auth overwrite, browser auth not merged, and
  non-auth params preserved.
- `TestProxyWebSocketWithDesktopScopePassesThroughFirstFrameAndHeaders` proves
  desktop pass-through unchanged.
- `TestProxyWebSocketWithControlUIScopeUpstreamDialFailureIsSanitized` proves
  pre-upgrade upstream dial failure remains sanitized.

## RED/GREEN Evidence

Focused RED before hardening:

```text
go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScope(RejectsMalformedFirstFrameWithoutForwarding|UpstreamCloseBeforeFirstConnectFailsClosed|BrowserDisconnectBeforeFirstConnectDoesNotHang|ContextCancelBeforeFirstConnectDoesNotHang)'
exit 1
--- FAIL: TestProxyWebSocketWithControlUIScopeContextCancelBeforeFirstConnectDoesNotHang (2.00s)
    instance_proxy_service_test.go:943: proxy handler did not return after context cancel before first connect
FAIL
FAIL    clawreef/internal/services    2.608s
```

Focused GREEN after hardening:

```text
go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScope(RejectsMalformedFirstFrameWithoutForwarding|UpstreamCloseBeforeFirstConnectFailsClosed|BrowserDisconnectBeforeFirstConnectDoesNotHang|ContextCancelBeforeFirstConnectDoesNotHang)'
exit 0
ok      clawreef/internal/services    0.612s
```

## Test Output Summary

| Command | Exit | Result |
| --- | ---: | --- |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScope(RejectsMalformedFirstFrameWithoutForwarding|UpstreamCloseBeforeFirstConnectFailsClosed|BrowserDisconnectBeforeFirstConnectDoesNotHang|ContextCancelBeforeFirstConnectDoesNotHang)'` before hardening | `1` | Expected RED: context cancellation before first connect did not return promptly. |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWithControlUIScope(RejectsMalformedFirstFrameWithoutForwarding|UpstreamCloseBeforeFirstConnectFailsClosed|BrowserDisconnectBeforeFirstConnectDoesNotHang|ContextCancelBeforeFirstConnectDoesNotHang)'` after hardening | `0` | Focused safety tests passed. |
| `go test -count=1 ./internal/services -run 'TestProxyWebSocketWith(ControlUIScope\|DesktopScope)'` | `0` | Required focused WebSocket tests passed. |
| `go test -count=1 ./internal/services ./internal/handlers` | `0` | Required backend package tests passed. |

## Remaining Risks

- No runtime goroutine leak counter was added. The safety evidence instead uses
  deterministic close/read-deadline tests to prove the relevant failure paths do
  not hang.
- The full 10s first-frame timeout was not exercised directly to keep the gate
  fast. The production timeout remains configured, and context cancellation now
  gives the bridge a faster fail closed path when request lifecycle ends first.
- No browser E2E or live runtime verification was performed in this gate.

## Recommended Next Gate

Recommended next gate: Commander review, then Reviewer Gate. Browser/manual E2E
and any backend build/deploy remain separate approval-gated work.

## Explicit Negatives

- no build/deploy
- no backend deploy/restart
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no K8S write
- no runtime mutation
- no database mutation
- no registry mutation
- no image build/tag/push/pull
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no secrets/token/cookie/access URL plaintext output
- no longterm/Mem0
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
