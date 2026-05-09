# WS Challenge Bridge Review - 2026-05-06

## Verdict

Verdict: `WS_CHALLENGE_BRIDGE_REVIEW_DONE`

Not `WS_CHALLENGE_BRIDGE_REVIEW_BLOCKED`: the implementation and safety
hardening satisfy the reviewed backend-only WebSocket bridge requirements, and
no blocking finding was identified in the allowed source/evidence scope.

## Findings First

No blocking findings.

No required Review Fix Worker Gate item was found. The remaining work is gate
sequencing, not a source fix: backend build/deploy and any browser E2E remain
separate approval-gated work.

## Code Behavior Assessment

Reviewed source scope:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `backend/internal/handlers/instance_handler.go`
- `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` read-only protocol check

Assessment:

- control-ui-only: the bridge behavior is inside
  `scope.AccessMode == AccessModeControlUI`; desktop `/proxy` remains on the
  existing pass-through path.
- desktop pass-through: the desktop branch still starts the two ordinary proxy
  pipes after upgrade and does not call the control-ui first-connect bridge.
- `connect.challenge`: for control-ui, the upstream-to-browser pipe starts
  before the browser first connect is read and rewritten, so upstream
  pre-connect frames including `connect.challenge` can reach the browser first.
- `device.nonce`: the rewrite preserves non-auth params as provided by the
  browser first connect, including non-empty `device.nonce` produced after the
  browser receives the challenge.
- server-owned token: `params.auth` is replaced as a whole with
  server-owned token auth.
- Browser auth token/password/deviceToken are not merged: browser auth fields
  are skipped with the original `auth` object and are absent from the rewritten
  first connect.
- no competing readers: upstream has one reader, the upstream-to-browser pipe;
  browser first connect is read before starting the browser-to-upstream pipe.
- fail closed: malformed first connect, upstream close before first connect,
  browser disconnect before first connect, and context cancellation before first
  connect all exit through closed/failed WebSocket paths rather than continuing.
- goroutine lifecycle: the first-frame goroutine uses a bounded read deadline,
  `ctx.Done()` is selected before first connect, close paths close both
  WebSocket connections, and the error channels are sized for their producers.
  I do not see a residual hang/leak blocker in the reviewed paths.

The runtime bundle check supports the dependency model: the browser client
handles `connect.challenge`, stores a connect nonce, sends connect after the
challenge, and places the nonce in `device.nonce`; without the challenge it has
a fallback timer path that can send an empty nonce.

## Test Coverage Assessment

Reviewed test coverage is sufficient for this gate.

Covered behaviors:

- `connect.challenge` is forwarded to the browser before rewritten first
  connect.
- Rewritten first connect keeps non-empty `device.nonce`.
- `params.auth` is replaced with server-owned token auth only.
- Browser auth token/password/deviceToken are not merged.
- Non-auth params are preserved.
- desktop pass-through preserves first frame and browser headers.
- Malformed first connect is not forwarded upstream and fails closed.
- Upstream close before first connect fails closed.
- Browser disconnect before first connect does not hang.
- Context cancel before first connect does not hang.
- Upstream dial failure remains sanitized.

No additional must-add unit case is required before the next gate. Optional
future coverage could add a runtime/browser E2E assertion and/or explicit
goroutine leak counting, but those are outside this read-only review and not
blockers for Backend Build/Deploy Approval Packet preparation.

## Secret/Security Assessment

Sanitized failure behavior is acceptable in the reviewed source paths:

- control-ui upstream auth strips browser `Authorization`, `Cookie`, and
  `X-OpenClaw-Token` before setting the server-owned token.
- control-ui WebSocket query forwarding strips route token and password.
- first-connect failure uses the generic sanitized close reason
  `control-ui websocket connect failed`.
- upstream WebSocket dial failure returns a sanitized generic error.
- failure tests assert observed errors do not contain route token, upstream
  token, browser auth header, or browser OpenClaw token shapes.

Secret-shape scan classification:

- Matches in reviewed backend source are fake test fixtures or source-code
  identifiers/assertions used to verify stripping and sanitization.
- I found no real secret, cookie value, or access URL value in the reviewed
  backend files or this review evidence.
- Matched values are intentionally not printed here.

## Recommended Next Gate

Recommended next gate: Backend Build/Deploy Approval Packet.

Review Fix Worker Gate is not recommended. External Expert Escalation is not
recommended. BLOCKED is not recommended.

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-review.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `WS_CHALLENGE_BRIDGE_REVIEW_DONE`, `WS_CHALLENGE_BRIDGE_REVIEW_BLOCKED`, `connect.challenge`, `device.nonce`, `server-owned token`, `desktop pass-through`, `competing readers`, `goroutine`, `fail closed`, `sanitized`, `fake test fixtures`, `Backend Build/Deploy Approval Packet`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan over reviewed backend files plus this evidence | `0` | Matches were emitted with values suppressed; classified as fake test fixtures, source-code identifiers/assertions, or this review's non-secret wording. No real secret/cookie/access URL value found. |
| path-limited `git status --short` | `0` | Shows the two reviewed backend service files modified and this review evidence untracked; no staged files. |

## Explicit Negatives

- no build/deploy
- no backend deploy/restart
- no browser E2E
- no fresh instance
- no K8S/runtime/database/registry mutation
- no frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks modification
- no existing evidence modification
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
