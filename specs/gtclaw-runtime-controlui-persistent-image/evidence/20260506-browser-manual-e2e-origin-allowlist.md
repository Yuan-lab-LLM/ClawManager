# Browser/Manual E2E Origin Allowlist Gate - 2026-05-06

Worker: BrowserManualE2EOriginAllowlistWorker

Verdict: `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED`

Not `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_DONE`: the user manually tested the approved instance `11` / `oc2gi-oa-131301` control-ui path and reported that the page still remained at the GTClaw manual connect form. The browser address/link was on the expected instance `11` route, but the form WebSocket URL showed the stale instance `10` route. The gate is blocked on stale route / persisted URL mismatch, not on a user needing to type a URL, key, token, or password.

Key conclusion:

```text
现在不是让用户填 URL/key 的问题，而是 instance 11 打开后 UI 仍拿着 instance 10 的 WS 地址。
```

This gate wrote only this evidence file. It did not implement, build, tag, push, pull, deploy, restart backend, create/delete/modify a fresh instance, mutate Kubernetes/runtime/database/registry state, manually patch a pod or Service, use `kubectl cp`, clear localStorage/sessionStorage/cache, edit the WebSocket URL, enter a gateway token/password, modify browser state beyond recording the supplied observation, write Mem0, write longterm, set `passes:true`, run Close, stage, commit, or push.

## Dependency Gates

| Dependency | Status used |
| --- | --- |
| Browser/Manual E2E Origin Allowlist Approval Packet | `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE` |
| Fresh Instance Runtime Deployment Origin Allowlist Gate | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE` |
| Runtime image build/tag/push origin allowlist gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |
| Runtime startup artifact origin allowlist implementation rerun | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE` |

User explicitly reported manual failure and allowed read-only browser / Chrome DevTools inspection. Browser/DevTools tooling was not required to determine the gate verdict because the supplied manual observation already proves the expected instance `11` route and observed instance `10` WebSocket route mismatch. No additional browser interaction was performed by this worker.

## Target User Path

Expected user path:

1. Enter GTManager.
2. Open instance `11` / `oc2gi-oa-131301`.
3. Click `打开 GTClaw 控制台`.
4. Reach GTClaw control-ui through the mediated route.

Expected mediated route:

```text
/api/v1/instances/11/control-ui
```

Expected WebSocket route shape:

```text
wss://localhost:30443/api/v1/instances/11/control-ui
```

## Manual Observation

User-supplied manual observation:

| Field | Observed |
| --- | --- |
| browser address/link | `https://localhost:30443/api/v1/instances/11/control-ui/chat?session=main` |
| page state | still at GTClaw manual connect form |
| form WebSocket URL | `wss://localhost:30443/api/v1/instances/10/control-ui` |
| visible error | `disconnected (1006): no reason` |

The browser opened an instance `11` URL, but the runtime UI used an instance `10` WebSocket URL. This is a stale route mismatch:

```text
expected=/api/v1/instances/11/control-ui
observed_form_ws=/api/v1/instances/10/control-ui
result=blocked_stale_route
```

## Acceptance Criteria Result

| Requirement | Result |
| --- | --- |
| no manual connect form | fail; manual connect form still exists |
| no `来源不被允许` | no longer reported in this manual observation |
| no `invalid connect params` | no longer reported in this manual observation |
| no `device.nonce` error | no longer reported in this manual observation |
| authenticated/usable console/chat state | fail; not reached |
| WebSocket established and frame traffic observed | fail; observed `disconnected (1006): no reason` |
| OpenClaw gateway token not exposed | no token/key/password was entered or recorded; no exposure reported |
| desktop /proxy regression | not tested in this failed gate |

Verdict rationale:

- The prior origin allowlist runtime gate proved instance `11` has `gateway.controlUi.allowedOrigins` including `https://localhost:30443`.
- The browser route reported by the user is an instance `11` route.
- The form WebSocket URL reported by the user is an instance `10` route.
- Therefore the immediate blocker is stale route / persisted URL state, not a missing origin allowlist, not a need for manual URL entry, and not a need for the user to provide gateway token/password.

## Browser Storage / Network / Console Summary

This worker did not clear or modify localStorage, sessionStorage, cache, cookies, URL fields, form fields, or runtime state.

Redacted browser-visible summary from the supplied observation:

| Surface | Redacted summary |
| --- | --- |
| page URL | instance `11` control-ui chat route was open |
| page state | GTClaw manual connect form still visible |
| form state | WebSocket URL field displayed stale instance `10` route |
| WebSocket/network | expected instance `11`; observed form route instance `10`; visible disconnect was `disconnected (1006): no reason` |
| console | no console transcript supplied; no secret value recorded |
| localStorage | not cleared; direct key/value inspection was not performed by this worker; stale instance `10` URL in localStorage remains unconfirmed |
| sessionStorage | not cleared; direct key/value inspection was not performed by this worker; stale instance `10` URL in sessionStorage remains unconfirmed |
| page state stale route | confirmed by manual observation: instance `11` page used instance `10` WS URL |

Read-only source context, not a fix:

- `frontend/src/hooks/useInstanceDesktopAccess.ts` keeps an in-memory `desktopSessionStore` keyed by `accessMode:instanceId`.
- `frontend/src/services/instanceService.ts` builds the control-ui chat path from the access URL returned by `POST /api/v1/instances/:id/access?mode=control-ui`.
- `frontend/src/components/InstanceAccess.tsx` and `frontend/src/pages/instances/InstancePortalPage.tsx` call `generateAccessToken(instanceId, "control-ui")` before opening the control-ui chat URL.

This source context supports the next investigation direction but does not prove whether the stale instance `10` value came from browser localStorage, sessionStorage, in-memory page state, old tab state, control-ui runtime internal persistence, or another route construction path.

## Desktop Regression

Desktop `/proxy` regression was not tested in this failed Browser/Manual E2E gate. The control-ui path failed before authenticated/usable console/chat state was reached, and this worker did not run additional browser E2E.

Prior runtime deployment evidence for instance `11` showed port `3001` reachability from PodIP and ServiceIP, but that is not a browser `/proxy` regression result.

## Blocker Classification

Primary blocker:

`Control UI stale route / persisted URL mismatch`

Specific blocker:

```text
GTManager instance 11 / oc2gi-oa-131301 opened an instance 11 control-ui URL,
but the GTClaw manual connect form still used /api/v1/instances/10/control-ui
as the WebSocket URL.
```

This is not a request for the user to manually correct the form URL or enter a key. Manual URL/key/token/password entry is forbidden and would not be accepted as evidence.

## Recommended Next Gate

Recommended next gate:

`Control UI Stale Route / Persisted URL Root Cause Investigation Gate`

That gate should be read-only unless separately approved otherwise. It should determine whether the stale instance `10` route is coming from:

- GTClaw control-ui localStorage
- GTClaw control-ui sessionStorage
- GTClaw control-ui in-memory page state
- existing browser tab state
- GTManager route/access URL generation
- backend `access?mode=control-ui` response for instance `11`
- runtime control-ui default connection persistence

It must not clear storage, edit the WebSocket URL, enter gateway token/password, mutate instances, patch pods/services, or implement a fix without a separate approval gate.

## Explicit Negatives

- no localStorage cleanup
- no sessionStorage cleanup
- no cache cleanup
- no manual WebSocket URL edit from instance `10` to instance `11`
- no gateway token/password entry
- no implementation
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
- no backend modification
- no frontend modification
- no runtime artifact modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist.md
rg -n "BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_DONE|BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED|oc2gi-oa-131301|instances/11/control-ui|instances/10/control-ui|manual connect form|disconnected \\(1006\\)|来源不被允许|invalid connect params|device.nonce|stale route|localStorage|sessionStorage|no build/tag/push/pull|no fresh instance|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `oc2gi-oa-131301`, `instances/11/control-ui`, `instances/10/control-ui`, `manual connect form`, `disconnected (1006)`, `来源不被允许`, `invalid connect params`, `device.nonce`, `stale route`, `localStorage`, `sessionStorage`, `no build/tag/push/pull`, `no fresh instance`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-browser-manual-e2e-origin-allowlist.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
