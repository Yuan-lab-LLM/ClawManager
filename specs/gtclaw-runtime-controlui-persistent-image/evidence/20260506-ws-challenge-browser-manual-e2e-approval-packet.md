# WS Challenge Browser/Manual E2E Approval Packet

Date: 2026-05-06
Worker: BrowserManualE2EApprovalPacketWorker
Verdict: WS_CHALLENGE_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE

## Approval Request

Please approve or reject the next Browser/Manual E2E Gate for the deployed WS challenge bridge fix.

Approval would authorize a future worker to perform browser/manual E2E verification only against the already deployed backend fix and existing instance 10. Approval would not authorize build/deploy, backend restart, fresh instance creation, runtime image rebuild, K8S/runtime/database/registry mutation, manual pod or Service patching, `kubectl cp`, source edits, existing evidence edits, git stage/commit/push, `passes:true`, Close, longterm write-back, or Mem0 write.

Recommended approval options:

- `APPROVE_WS_CHALLENGE_BROWSER_MANUAL_E2E_GATE`: run the future Browser/Manual E2E Gate with the minimum scope below.
- `REJECT_OR_BLOCK`: do not run browser/manual E2E; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gates are complete:

- `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`
- `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_DONE`
- `WS_CHALLENGE_BRIDGE_SAFETY_HARDENING_DONE`
- `WS_CHALLENGE_BRIDGE_REVIEW_DONE`
- `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE`

Relevant deployed-backend fact from `20260506-ws-challenge-backend-build-deploy.md`:

- the backend source fix was deployed to existing `Deployment/clawmanager-app`
- rollout completed
- `/healthz` returned HTTP 200
- `/api/v1/auth/me` without auth returned expected HTTP 401
- runtime image digest did not drift
- no fresh instance was created
- no browser E2E was executed in the backend deploy gate

Root-cause and implementation facts to verify live:

- the runtime control-ui expects upstream `connect.challenge`
- the browser then sends connect with non-empty `device.nonce`
- the bridge forwards upstream pre-connect frames to the browser before rewriting first connect
- the bridge keeps server-side token injection and strips browser auth material from upstream auth
- prior manual observation saw a manual connect form, `invalid connect params`, or `device.nonce` error before the deployed source fix could be verified live

## Future Allowed Scope

If the user approves `APPROVE_WS_CHALLENGE_BROWSER_MANUAL_E2E_GATE`, the future Browser/Manual E2E Gate may:

- open `https://localhost:30443`
- enter GTManager through the existing approved login/session path
- open instance `10` / `oc2gi-185707`
- click or otherwise use the user-facing "打开 GTClaw 控制台" entry
- verify the mediated route only:
  - `wss://localhost:30443/api/v1/instances/10/control-ui`
- inspect browser-visible state, network/WebSocket event summaries, console summaries, and screenshots with secret redaction
- perform read-only status checks needed to record deployed backend image/tag/digest, instance 10 pod Ready/restart count, and Service ports 3001/18789
- perform desktop `/proxy` regression check

The future gate must not create a new instance. It must use existing instance 10 only.

## Required User Path

The future Browser/Manual E2E Gate must verify this user path:

1. Open `https://localhost:30443`.
2. Enter GTManager.
3. Navigate to instance `10` / `oc2gi-185707`.
4. Open "打开 GTClaw 控制台".
5. Confirm the browser uses the mediated control-ui route under `wss://localhost:30443/api/v1/instances/10/control-ui`, not direct pod, PodIP, ServiceIP, or port-forward access.

## Required Acceptance Criteria

The future Browser/Manual E2E Gate must record pass/fail for all of these criteria:

- GTClaw control-ui no longer remains on the manual connect form.
- No `invalid connect params` error is visible.
- No `device.nonce` error is visible.
- Upstream `connect.challenge` reaches the browser.
- Subsequent WebSocket connect uses non-empty `device.nonce`.
- Backend bridge continues server-side token injection.
- OpenClaw gateway token is not exposed in browser-visible URL, query, localStorage, sessionStorage, console output, network summaries, screenshots, or evidence snippets.
- GTClaw control-ui reaches authenticated/usable state, at minimum an actual console/chat interface instead of the manual connect form.
- Desktop `/proxy` regression still returns HTTP 200 and remains desktop, not control-ui.

## Required Evidence Fields

The future Browser/Manual E2E Gate must record:

- deployed backend image/tag/digest from `Deployment/clawmanager-app`
- runtime pod for instance 10 Ready state and restart count
- Service ports 3001 and 18789 still exposed
- mediated control-ui route confirmation, redacted
- absence of token/cookie/access URL plaintext
- screenshot or observation summary with secret redaction
- whether `connect.challenge` reached the browser
- whether follow-up connect used non-empty `device.nonce`
- whether server-side token injection remained in effect without exposing OpenClaw gateway token
- desktop `/proxy` regression result

Evidence must redact or omit all token, cookie, credential, secret, session, and access URL values. Route shape may be recorded only without sensitive query/cookie/header values.

## Future Forbidden Actions

The future Browser/Manual E2E Gate is forbidden from:

- build/tag/push/pull
- backend deploy/restart
- fresh instance creation/deletion/modification
- K8S/runtime/database/registry mutation
- manual pod patch
- manual Service patch
- `kubectl cp`
- source modifications
- modifying existing evidence
- modifying backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks
- writing secrets, tokens, cookies, credentials, or access URL plaintext to output or evidence
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push

## Gate Sequence After Approval

If approved:

1. Browser/Manual E2E Gate.
2. Commander review.
3. Commit approval packet only if E2E passes and the user approves.
4. Commit gate only after explicit user approval.

No commit, `passes:true`, Close, or longterm write-back may occur from this approval packet.

## Verification Plan

Required checks for this approval packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-browser-manual-e2e-approval-packet.md`
- `rg -n "WS_CHALLENGE_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE|WS_CHALLENGE_BROWSER_MANUAL_E2E_APPROVAL_PACKET_BLOCKED|manual connect form|invalid connect params|device.nonce|connect.challenge|wss://localhost:30443/api/v1/instances/10/control-ui|server-side token|no build/tag/push/pull|no fresh instance|no K8S/runtime/database/registry mutation|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-browser-manual-e2e-approval-packet.md`
- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-browser-manual-e2e-approval-packet.md`

## Explicit Negatives

This approval packet performed:

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual browser test
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no secret, token, cookie, credential, or access URL plaintext output
- no Mem0
- no longterm write-back
- no `passes:true`
- no Close
- no git stage/commit/push
