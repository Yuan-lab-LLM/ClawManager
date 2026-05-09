# Browser/Manual E2E Origin Allowlist Approval Packet - 2026-05-06

Worker: BrowserManualE2EOriginAllowlistApprovalPacketWorker

Verdict: `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE`

Not `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_APPROVAL_PACKET_BLOCKED`: the prerequisite fresh instance runtime deployment gate is complete for instance `11` / `oc2gi-oa-131301`, and this packet can ask for a narrow future Browser/Manual E2E Origin Allowlist Gate.

This packet is an approval request only. It did not run browser E2E, open Chrome, use Chrome DevTools MCP, use Playwright, open the in-app browser, build, tag, push, pull, deploy, restart backend, create/delete/modify a fresh instance, mutate Kubernetes/runtime/database/registry state, manually patch a pod or Service, use `kubectl cp`, modify instance `9`, modify instance `10`, modify instance `11`, write Mem0, write longterm, set `passes:true`, run Close, stage, commit, or push.

## Approval Request

Please approve or reject whether a future worker may execute:

`Browser/Manual E2E Origin Allowlist Gate`

Recommended response options:

- `APPROVE_BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_GATE`: authorize the future browser/manual E2E gate with only the scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not run browser/manual E2E; provide the blocking concern or revised scope.

No approval is implied by this packet. Browser E2E is not authorized unless the user explicitly approves the future gate.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Fresh Instance Runtime Deployment Origin Allowlist Gate | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE` |
| Runtime image build/tag/push origin allowlist gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |
| Runtime startup artifact origin allowlist implementation rerun | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE` |
| Control UI origin allowlist root cause | `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_DONE` |
| WS challenge backend build/deploy | `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE` |

## Fresh Instance To Validate

| Field | Value |
| --- | --- |
| instance id/name | `11` / `oc2gi-oa-131301` |
| namespace | `clawmanager-user-1` |
| pod | `clawreef-11-oc2gi-oa-131301` |
| service | `clawreef-11-oc2gi-oa-131301-svc` |
| runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| imageID digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` |

Runtime validation already passed for instance `11`:

- pod Ready `true`
- restart count `0`
- OOMKilled `false`
- imageID matches approved image index digest
- Service exposes `3001` and `18789`
- PodIP `18789` reachable with HTTP `200`
- ServiceIP `18789` reachable with HTTP `200`
- `/config/.openclaw/openclaw.json` redacted summary proves `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- helper scripts and `/defaults/openclaw-agent/config.yaml` hashes match build evidence

## Future Gate User Path

If approved, the future Browser/Manual E2E Origin Allowlist Gate must verify this user path:

1. Open `https://localhost:30443`.
2. Enter GTManager through the existing approved login/session path.
3. Navigate to instance `11` / `oc2gi-oa-131301`.
4. Click `打开 GTClaw 控制台`.
5. Confirm the mediated route shape is used:

```text
wss://localhost:30443/api/v1/instances/11/control-ui
```

Evidence must record only redacted route shape, such as `/api/v1/instances/11/control-ui`, and must not record token-bearing URLs, cookie values, credential values, or access URL plaintext.

## Future Gate Acceptance Criteria

The future Browser/Manual E2E Origin Allowlist Gate must validate:

| Requirement | Acceptance result required |
| --- | --- |
| manual connect form | not present / no longer stuck at manual connect form |
| origin rejection | no `来源不被允许` |
| connect params regression | no `invalid connect params` |
| nonce regression | no `device.nonce` error |
| GTClaw control-ui state | reaches authenticated and usable console/chat state |
| WebSocket | WebSocket established and frame traffic observed; record counts/types only, not payload secrets |
| OpenClaw gateway token hygiene | OpenClaw gateway token not exposed in browser URL/query/localStorage/sessionStorage/console/network summaries/evidence |
| desktop /proxy regression | desktop /proxy still works and remains desktop, not control-ui |

The future gate must include screenshots or manual observations sufficient to show the control-ui state and desktop regression while redacting secrets and avoiding token/cookie/access URL plaintext.

## Future Evidence Requirements

The future E2E evidence must record:

- backend health, including `/healthz`
- instance `11` pod Ready state and restart count
- instance `11` OOM/no-OOM state
- service `3001` and `18789` still present
- control-ui route redacted as `/api/v1/instances/11/control-ui` or equivalent non-secret route shape
- GTManager navigation observation for instance `11` / `oc2gi-oa-131301`
- `打开 GTClaw 控制台` action observation
- screenshots or manual observations with secret redaction
- WebSocket established status and redacted frame traffic summary
- localStorage/sessionStorage/console/network summary proving no OpenClaw gateway token exposure
- desktop /proxy regression result proving desktop remains desktop and is not repointed to control-ui
- no token value, cookie value, credential, secret, bearer value, auth header value, or access URL plaintext

## Future Gate Allowed Scope

If approved, the future Browser/Manual E2E Origin Allowlist Gate may only:

- use an existing approved browser/manual path to open `https://localhost:30443`
- authenticate to GTManager without recording credential values, token values, cookies, auth headers, or access URLs
- navigate GTManager to existing instance `11`
- click `打开 GTClaw 控制台`
- inspect browser-visible UI state, screenshots, console summaries, storage key presence, network route shapes, WebSocket established status, and redacted frame traffic summaries
- run read-only backend health and current instance `11` pod/service metadata checks needed for E2E evidence
- write one future E2E evidence file if separately approved by that gate

## Future Gate Explicitly Forbidden

Approval of this packet must not authorize:

- build/tag/push/pull
- backend deploy/restart
- fresh instance creation/deletion/modification
- K8S/runtime/database/registry mutation
- manual pod patch
- manual Service patch
- `kubectl cp`
- modifying instance `9`
- modifying instance `10`
- modifying instance `11`
- source modification
- evidence modification except the future gate's one new E2E evidence file
- modifying backend/frontend/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push
- token/cookie/credential/secret/access URL plaintext output

## Follow-up Gate Order

1. If user approves: `Browser/Manual E2E Origin Allowlist Gate`.
2. Then Commander review of the E2E evidence and remaining feature evidence.
3. Then commit approval and commit only if E2E passes and the user explicitly approves commit scope.
4. Only after explicit user approval may any later `passes:true`, Close, or longterm write-back be considered.

## Current Packet Explicit Negatives

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no browser opened
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance
- no fresh instance creation/deletion/modification
- no K8S mutation
- no runtime mutation
- no database mutation
- no database migration
- no registry mutation
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no modification to instance `9`
- no modification to instance `10`
- no modification to instance `11`
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no runtime startup artifact modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist-approval-packet.md
rg -n "BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE|BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_APPROVAL_PACKET_BLOCKED|oc2gi-oa-131301|instances/11/control-ui|manual connect form|来源不被允许|invalid connect params|device.nonce|gateway.controlUi.allowedOrigins|WebSocket|desktop /proxy|no build/tag/push/pull|no fresh instance|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist-approval-packet.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `oc2gi-oa-131301`, `instances/11/control-ui`, `manual connect form`, `来源不被允许`, `invalid connect params`, `device.nonce`, `gateway.controlUi.allowedOrigins`, `WebSocket`, `desktop /proxy`, `no build/tag/push/pull`, `no fresh instance`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
