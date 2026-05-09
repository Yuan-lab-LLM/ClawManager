# WS Challenge Bridge Implementation Approval Packet - 2026-05-06

Verdict: `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE`

Not `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED`: the packet
could be written from the approved root-cause dependency and allowed read-only
source/evidence scope.

This packet is an approval request only: no implementation, no build/deploy,
no browser E2E, no fresh instance, no runtime mutation, no K8S mutation, no
database mutation, no registry mutation, no passes:true, and no Close.

## Approval Request

User approval is requested for whether to execute the next gate:
`WS Challenge Bridge Implementation Gate`.

Approval would authorize only the minimum backend implementation and test work
described in this packet. It would not authorize build/deploy, browser E2E,
fresh instance creation, K8S/runtime/database/registry mutation, passes:true,
Close, longterm write-back, Mem0 write, git stage/commit/push, or any other
forbidden action listed below.

Requested decision:

- Approve `WS Challenge Bridge Implementation Gate`.
- Reject or defer `WS Challenge Bridge Implementation Gate`.

## Root-Cause Dependency Summary

Dependency gate completed and Commander-reviewed:

- Evidence:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md`
- Verdict: `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`

Confirmed manual reproduction:

- Entry: GTManager instance `10` / `oc2gi-185707`, via "打开 GTClaw 控制台".
- Route shape: GTManager mediated route
  `wss://localhost:30443/api/v1/instances/10/control-ui`.
- Page error:
  `invalid connect params: at /device/nonce: must NOT have fewer than 1 characters`.

Confirmed root cause:

- Upstream OpenClaw sends `connect.challenge` before authenticated `connect`.
- The runtime client must receive `connect.challenge`, set `connectNonce`,
  and then send first connect with `params.device.nonce`.
- `buildConnectParams` includes `device.nonce` from `connectNonce`.
- Current backend control-ui WebSocket bridge dials upstream and upgrades the
  browser socket, then blocks on the browser first connect before any upstream
  pre-connect frame forwarding starts.
- Because the browser never receives `connect.challenge`, fallback first
  connect can carry an empty `device.nonce`.

## Proposed Implementation Scope

Minimum implementation target:

- Fix only control-ui WebSocket bridge handshake sequencing.
- Before browser first connect is rewritten and sent upstream, support upstream
  pre-connect frame forwarding, especially `connect.challenge`.
- Ensure the browser can receive `connect.challenge` and then send first
  connect with non-empty `device.nonce`.
- Continue replacing `params.auth` with server-owned token auth only.
- Continue preserving non-auth params, including `device.nonce`, `client`,
  `role`, `scopes`, `caps`, `locale`, and `userAgent`.
- Keep desktop `/proxy` WebSocket pass-through unchanged; desktop pass-through
  remains unchanged.

Recommended future implementation write scope:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation.md`
  only in the future implementation gate

Recommended design constraints for the future Worker:

- Do not introduce competing readers on either WebSocket connection.
- Coordinate upstream pre-connect reads and browser first connect reads as one
  handshake stage before normal bidirectional pipes start.
- Forward upstream pre-connect frames to the browser until the browser first
  connect is received and rewritten, or fail closed with sanitized behavior.
- Preserve the existing sanitized control-ui first-connect failure behavior.

## Explicitly Forbidden Implementation Scope

The future implementation gate must not modify:

- `frontend/**`
- `runtime/**`
- `deployments/**`
- `docs/**`
- `longterm/**`
- `AgentTeam/**`
- spec/plan/tasks
- existing evidence
- `/tmp/gtclaw-runtime-patch/**`

Current packet scope also forbids modifying those paths.

## Required Tests

Future Worker must provide focused RED/GREEN evidence:

- RED: current bridge fails when upstream sends `connect.challenge` before
  browser first connect.
- GREEN: bridge forwards `connect.challenge` to browser before rewritten
  first connect.
- GREEN: rewritten connect keeps non-empty `device.nonce`.
- GREEN: `params.auth` is replaced with server-owned token only.
- GREEN: browser auth fields are not merged.
- GREEN: desktop WebSocket first frame/header behavior unaffected and desktop
  pass-through remains unchanged.
- GREEN: malformed first connect remains sanitized.
- GREEN: upstream connect failure remains sanitized.

Required full relevant backend tests:

```bash
go test -count=1 ./internal/services ./internal/handlers
```

## Future Verification After Implementation

Future implementation evidence must include:

- Focused RED/GREEN tests for the challenge sequencing bug and security
  behavior.
- Full relevant Go tests from `backend/`:
  `go test -count=1 ./internal/services ./internal/handlers`.
- `git diff --check` over only modified implementation/evidence paths.
- Secret-shape scan with no plaintext output.
- Path-limited `git status --short` for only the future implementation files.

## Forbidden Actions

Still forbidden now and not authorized by this approval packet:

- build/deploy
- backend deploy/restart
- browser E2E
- Chrome DevTools MCP
- Playwright
- fresh instance creation/deletion/modification
- K3S/K8S write
- runtime mutation
- database mutation
- registry mutation
- image build/tag/push/pull
- manual pod patch
- manual Service patch
- `kubectl cp` write
- frontend/backend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks
  modification outside the explicitly approved future backend/evidence write
  scope
- existing evidence modification
- `/tmp/gtclaw-runtime-patch/**` modification
- secrets/token/cookie/access URL plaintext output
- git stage/commit/push
- Mem0 write
- longterm write-back
- passes:true
- Close

## Recommended Next Gate

If the user approves this packet, the recommended gate order is:

1. `WS Challenge Bridge Implementation Gate`.
2. Commander review.
3. Reviewer Gate.
4. Verifier Gate with browser/manual E2E approval packet.
5. Backend build/deploy approval if source changes need live verification.
6. Browser/manual E2E rerun.
7. Commit/push only after approved verification evidence.

If the user rejects or defers this packet, remain blocked on implementation and
do not alter backend source, tests, runtime state, deployment state, evidence
history, longterm state, or git state.

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE`, `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED`, `connect.challenge`, `device.nonce`, `connectNonce`, `first connect`, `WebSocket bridge`, `server-owned token`, `desktop pass-through`, `RED`, `GREEN`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this new evidence | `0` | No matches. Matched secret values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md` | `0` | Shows the root-cause evidence and this approval packet as untracked in the requested path scope. |
