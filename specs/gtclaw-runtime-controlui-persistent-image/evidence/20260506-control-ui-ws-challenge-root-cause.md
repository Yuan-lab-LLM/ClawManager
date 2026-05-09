# Control-ui WS Challenge Root Cause - 2026-05-06

Verdict: `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`

Not `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_BLOCKED`: the root cause is
confirmed from the allowed source and evidence paths.

## Scope

This was a read-only root-cause investigation plus this single evidence write.
There was no implementation, no build/deploy, no browser E2E, no runtime
mutation, no K8S mutation, no database mutation, no registry mutation, no
Mem0 write, no longterm write-back, no passes:true, and no Close.

## Root Cause Summary

The user-observed page was reached through the GTManager mediated route, not
the bare standalone `ws://localhost:18789` route. The runtime control-ui client
expects an upstream `connect.challenge` event before authenticated `connect`,
uses that challenge to set `connectNonce`, and then sends `device.nonce` through
`buildConnectParams`.

The current backend control-ui WebSocket bridge dials upstream, upgrades the
browser WebSocket, and then blocks on the browser first connect frame before it
starts any upstream-to-browser read/forward path. During that wait, upstream
pre-connect frames such as `connect.challenge` are not read or forwarded. The
browser therefore never receives the challenge, falls back to sending connect
without a challenge nonce, and emits an empty `device.nonce`.

The screenshot error `invalid connect params: at /device/nonce: must NOT have
fewer than 1 characters` is consistent with that sequence.

## Source Evidence Table

| Question | Source | Evidence |
| --- | --- | --- |
| GTManager mediated route? | `frontend/src/components/InstanceAccess.tsx:195-207`; `frontend/src/services/instanceService.ts:135-164`; `backend/internal/handlers/instance_handler.go:842-844,914-923`; `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e.md:74-82` | The UI requests an access token with mode `control-ui`, turns the returned manager access URL into a chat URL, and opens it. The backend has a dedicated control-ui proxy handler and sends WebSocket upgrades through `ProxyWebSocketWithScopeAndUpstreamAuth`. Prior E2E recorded only `/api/v1/instances/10/control-ui/...` manager paths. |
| Client waits for challenge? | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js:2` | The minified client handles event `connect.challenge`; when payload `nonce` is a string it assigns `this.connectNonce = nonce` and calls `this.sendConnect()`. |
| Challenge sets nonce used by connect? | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js:2` | The client initializes `connectNonce` to null, then `er(...)` computes `r = e.connectNonce ?? ""` and returns a `device` object with `nonce:r`. |
| `buildConnectParams` includes `device.nonce`? | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js:2` | `buildConnectParams(...)` returns `device:e.device`; therefore the `device` object produced from `connectNonce` is sent in connect params. |
| Empty nonce if no challenge? | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js:2` | `queueConnect()` clears `connectNonce`, starts a 750ms timer, and calls `sendConnect()` if no challenge arrives first. With `connectNonce` still null, the generated `device.nonce` is the empty string. |
| Backend waits for browser first connect? | `backend/internal/services/instance_proxy_service.go:361-386,418-438` | The backend dials upstream, upgrades the browser socket, then for control-ui calls `bridgeControlUIFirstConnect`; that helper sets a browser read deadline, calls `clientConn.ReadMessage()`, rewrites the connect payload, and writes it upstream. |
| No upstream pre-connect forwarding during wait? | `backend/internal/services/instance_proxy_service.go:385-408` | The normal bidirectional pipes start only after `bridgeControlUIFirstConnect` returns. No upstream `ReadMessage()` path runs before the browser first connect has been read and rewritten. |
| Tests missed challenge sequence? | `backend/internal/services/instance_proxy_service_test.go:388-401,418-431`; `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md:45-64` | The focused test upstream waits to read a browser connect frame and then responds. The test supplies a non-empty nonce in the browser connect payload, so it does not simulate upstream pre-connect `connect.challenge` or missing challenge behavior. |

## Manual Observation Correlation

The user screenshot showed instance `10` / `oc2gi-185707` opened from the
GTManager "open GTClaw console" entry. The WebSocket URL shape was
`wss://localhost:30443/api/v1/instances/10/control-ui`, which is the manager
HTTPS port and manager API path. It is not the bare standalone runtime route
`ws://localhost:18789`.

The visible error was:

```text
invalid connect params: at /device/nonce: must NOT have fewer than 1 characters
```

That error matches the confirmed bridge behavior: the OpenClaw control-ui
client can only populate `device.nonce` after receiving `connect.challenge`;
the backend WebSocket bridge currently prevents the pre-connect challenge from
reaching the browser before the browser fallback sends first connect.

## Why Previous E2E Missed It

The prior Browser E2E evidence recorded localized DOM shell and route loading
for `/api/v1/instances/10/control-ui/...` with body markers such as
`GTClaw 控制台`, `控制台`, `网关`, and `连接`
(`20260505-browser-e2e.md:74-84`).

For WebSocket/session bootstrap, it recorded status `101`, created WebSocket
events, frame event count, and secret hygiene metadata
(`20260505-browser-e2e.md:86-99`). It labeled that as an
authenticated-ready equivalent, but it did not prove authenticated ready by
checking usable chat, absence of the manual connect form, absence of visible
connect-param errors, or a post-connect success state.

Therefore the old Browser E2E could pass while the page still displayed a
manual connection form and the upstream rejected the first connect frame for
empty `device.nonce`.

## Recommended Minimal Design

The next implementation design should keep the existing security boundary and
change only the control-ui WebSocket bridge handshake sequencing:

1. Add control-ui WebSocket bridge support for upstream pre-connect frame
   forwarding, especially `connect.challenge`.
2. Before committing the first browser connect upstream, concurrently coordinate
   upstream challenge reads and browser first connect reads without introducing
   competing readers on the same socket.
3. Forward the upstream `connect.challenge` to the browser so the runtime client
   sets `connectNonce`.
4. When the browser sends first connect, continue server-side token injection by
   replacing browser `params.auth` with the server-owned auth token while
   preserving non-auth params, including the now-populated `device.nonce`.
5. After the rewritten first connect is written upstream, start the normal
   bidirectional pipes.
6. Preserve browser auth stripping, server-side token injection, and desktop
   `/proxy` pass-through unchanged.

## Recommended Next Gate

Recommended next gate: `WS Challenge Bridge Implementation Approval Packet`.

External Expert Escalation is not required from this investigation because the
protocol failure is specific and source-backed. BLOCKED is not recommended
unless Commander wants an external review of the concurrent handshake design
before authorizing implementation.

## Explicit Negatives

- no implementation
- no build/deploy
- no browser E2E
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S write
- no runtime mutation
- no database mutation
- no registry mutation
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no secrets/token/cookie/access URL plaintext output
- no git stage/commit/push
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`, `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_BLOCKED`, `connect.challenge`, `device.nonce`, `connectNonce`, `buildConnectParams`, `first connect`, `WebSocket bridge`, `GTManager mediated route`, `invalid connect params`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this new evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md` | `0` | Shows this new evidence file as untracked. |
