# Control UI Stale Route Root Cause Investigation Evidence - 2026-05-06

Verdict: `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE`

Not `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_BLOCKED`: source-only GTManager frontend and backend access/proxy review did not find a path that generates an instance `10` control-ui URL for instance `11`; the reviewed runtime `control-ui bundle` does contain a persisted URL restore path that can make the page opened on instance `11` use the stale route from instance `10`.

This investigation was read-only. It performed no implementation, no build/deploy, no browser E2E, no browser storage cleanup, no URL editing, no token/password entry, no K8S/runtime/database/registry mutation, no fresh instance mutation, no Mem0/longterm write, no `passes:true`, no Close, and no git stage/commit/push.

## Inputs

| Input | Finding used |
| --- | --- |
| Browser/Manual E2E evidence | `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED`; browser address was on `instances/11/control-ui`, but the manual connect form showed `instances/10/control-ui` and `disconnected (1006): no reason`. |
| Fresh runtime evidence | instance `11` / `oc2gi-oa-131301` exists, pod is Ready, restart count is `0`, OOMKilled is false, Service exposes `3001` and `18789`, and `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`. |
| WS challenge backend deploy evidence | `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE`; deployed backend health was recorded as HTTP `200`. |
| Runtime bundle | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` and `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js`, read-only. |

## User-Observed Failure

| Field | Value |
| --- | --- |
| User path | GTManager instance `11` / `oc2gi-oa-131301` -> open GTClaw console |
| Expected route | `/api/v1/instances/11/control-ui` |
| Browser address/link | `https://localhost:30443/api/v1/instances/11/control-ui/chat?session=main` |
| Observed form WebSocket URL | `wss://localhost:30443/api/v1/instances/10/control-ui` |
| Observed state | still on the GTClaw manual connect form |
| Visible error | `disconnected (1006): no reason` |

The failure is not a user-input issue. It is a stale route mismatch: the instance `11` page opened, but the runtime UI attempted to connect through an instance `10` WebSocket route.

## GTManager Frontend Route Generation

Conclusion: GTManager generated the correct instance `11` control-ui top-level URL.

Evidence:

- `frontend/src/services/instanceService.ts:135`-`142`: `generateAccessToken(id, mode)` posts to `/instances/${id}/access` with `mode` as a query parameter.
- `frontend/src/services/instanceService.ts:155`-`165`: `getControlUiChatUrl(accessUrl)` strips query/hash from the backend URL and appends `chat?session=main`.
- `frontend/src/components/InstanceAccess.tsx:187`-`207`: `InstanceAccess` calls `generateAccessToken(instanceId, "control-ui")`, derives the chat URL from `data.proxy_url || data.access_url`, and opens that URL.
- `frontend/src/pages/instances/InstanceDetailPage.tsx:774`-`778`: `InstanceDetailPage` passes the current `instance.id` into `InstanceAccess`.
- `frontend/src/pages/instances/InstancePortalPage.tsx:256`-`281`: `InstancePortalPage` calls `generateAccessToken(selectedInstance.id, "control-ui")` and opens the derived chat URL.

The manual observation also confirms this: the browser address was the instance `11` route. This rules out a primary GTManager route generation bug for the observed mismatch.

## GTManager Session Reuse Review

Conclusion: no source path was found in GTManager that reuses an old instance `10` control-ui access URL for the instance `11` popup.

| Surface | Source-only finding |
| --- | --- |
| `generateAccessToken(instanceId, "control-ui")` | Uses the caller-provided id in the API path; no cached URL source was found. |
| `InstanceAccess` | Uses the prop `instanceId`; `InstanceDetailPage` supplies current `instance.id`. |
| `InstancePortalPage` | Uses `selectedInstance.id`; state reset on selection changes was previously observed in source. |
| `InstanceDetailPage` | Renders `InstanceAccess` with current `instance.id`. |
| `useInstanceDesktopAccess` | Has an in-memory `desktopSessionStore`, but the key is `${accessMode}:${instanceId}` and the hook path is for embedded desktop/proxy access. It does not open the separate control-ui popup. |

`useInstanceDesktopAccess` can preserve desktop iframe state, but its keying and use site do not explain a control-ui manual connect form on instance `11` showing `instances/10/control-ui`.

## Backend Access/Proxy Review

Conclusion: backend source is not a likely source of an instance `10` URL for an instance `11` access request. This is a source-only conclusion; this worker did not directly call the access API.

Evidence:

- `backend/internal/handlers/instance_handler.go:630`-`675`: `GenerateAccessToken` parses `:id`, loads that instance, and calls `ResolveInstanceAccessScope(instance.ID, instance.Type, requestedMode, ...)`.
- `backend/internal/services/instance_access_service.go:96`-`107`: control-ui scope uses `fmt.Sprintf("/api/v1/instances/%d/control-ui", instanceID)`, target port `18789`, and route-specific cookie name/path for that same instance id.
- `backend/internal/services/instance_access_service.go:150`-`183`: generated token claims carry the same instance id, access mode, route prefix, and access URL from the resolved scope.
- `backend/internal/handlers/instance_handler.go:717`-`727`: the response returns `access_url` and `proxy_url` from the same resolved scope.
- Tests assert the same contract for instance `42`: control-ui route prefix `/api/v1/instances/42/control-ui`, scoped token route prefix, access mode `control-ui`, and control-ui cookie path.

If instance `11` calls `/api/v1/instances/11/access?mode=control-ui`, reviewed source constructs `/api/v1/instances/11/control-ui/`, not instance `10`.

## Control-UI Bundle Persistence Review

Conclusion: the `control-ui bundle` has a persisted URL restore bug that explains the observed stale route.

Static bundle evidence:

- The bundle derives a page default from `location.protocol`, `location.host`, and the current control-ui base path. For an instance `11` page this default should resolve to the instance `11` route shape.
- The bundle initializes settings from persisted browser storage before connecting. It reads settings through the localStorage helper exported from `i18n-B06L7jQN.js`.
- The settings keys include `openclaw.control.settings.v1:<normalized-gateway>` and global fallback `openclaw.control.settings.v1`.
- When stored settings contain a `gatewayUrl`, the bundle uses the stored `gatewayUrl` unless it exactly equals the current page-derived URL. A stale stored `gatewayUrl` shaped like `wss://localhost:30443/api/v1/instances/10/control-ui` is not equal to the instance `11` page URL, so it survives and becomes active.
- The manual connect form's WebSocket URL input is bound to `settings.gatewayUrl`, and the WebSocket client is created with `url: settings.gatewayUrl`.

Read-only static storage key summary:

| Storage surface | Key names / route shape only |
| --- | --- |
| `localStorage` | `openclaw.control.settings.v1`, `openclaw.control.settings.v1:<normalized-gateway>`, `openclaw.i18n.locale`, route shape may include `wss://localhost:30443/api/v1/instances/<id>/control-ui` inside settings. |
| `sessionStorage` | `openclaw.control.token.v1`, `openclaw.control.token.v1:<normalized-gateway>`; key names only, no values read or recorded. |
| `IndexedDB` | no static `IndexedDB` / `indexedDB` usage found in the reviewed bundle. |
| device auth storage | `openclaw.device.auth.v1`, `openclaw-device-identity-v1`; key names only. |

No live browser storage inspection was performed. No localStorage, sessionStorage, IndexedDB, cache, or cookie values were read, cleared, or modified. No token, cookie, password, or secret value is recorded here.

## Root Cause Classification

Selected classification: `control-ui bundle persisted URL bug`.

Rejected classifications:

| Classification | Reason rejected |
| --- | --- |
| GTManager route generation bug | Browser address and source review show instance `11` route generation. |
| backend access response bug | Backend source constructs route prefix from the requested instance id; no API call was made, so this remains source-only. |
| frontend in-memory session reuse bug | Reviewed GTManager cache is desktop/proxy scoped by mode and instance id, not the control-ui popup route. |
| old browser tab/state only | Old state may be the source of the stale value, but the actionable root cause is bundle startup precedence: persisted `gatewayUrl` can override the current page-derived instance route. |
| insufficient evidence / BLOCKED | Manual observation plus static bundle review is sufficient to explain why an instance `11` page can use an instance `10` WebSocket URL. |

## Required Answers

1. GTManager frontend instance `11` entry generated the correct `/api/v1/instances/11/control-ui` URL: yes, supported by the observed browser address and source review.
2. `generateAccessToken`, `useInstanceDesktopAccess`, `InstanceAccess`, `InstancePortalPage`, and `InstanceDetailPage` do not show a source path that reuses an old instance `10` control-ui access URL for instance `11`.
3. Backend access/proxy source is unlikely to return an instance `10` control-ui URL for instance `11`; this is source-only because this gate did not call the API.
4. GTClaw/OpenClaw control-ui bundle reads persisted browser settings and prioritizes a stored `gatewayUrl` over the current page-derived route unless the stored value exactly equals the current page URL. That can make `/api/v1/instances/11/control-ui` load with `instances/10/control-ui`.
5. Read-only browser storage inspection was not performed. Static review recorded only storage key names and redacted route shapes.
6. Root cause classification is `control-ui bundle persisted URL bug`.
7. Recommended next gate is `Control UI Runtime Persistence Fix Approval Packet`.

## Recommended Next Gate

Recommended: `Control UI Runtime Persistence Fix Approval Packet`.

The next approval packet should authorize a narrow runtime control-ui bundle fix that treats GTManager-mediated `/api/v1/instances/<id>/control-ui` routes as authoritative for the active WebSocket URL, or isolates persisted `gatewayUrl` by mediated route so instance `10` cannot override instance `11`.

Optional diagnostic only if the user wants live confirmation before fixing: `Browser State Reset Diagnostic Approval Packet`. That packet must still forbid cleanup unless explicitly approved as a diagnostic step. This root-cause evidence does not require that step to proceed to a fix approval packet.

## Explicit Negatives

- no implementation
- no build/deploy
- no browser E2E
- no browser storage cleanup
- no manual WebSocket URL edit
- no gateway token/password input
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no backend deploy/restart
- no manual pod/service patch
- no kubectl cp
- no source modification outside this evidence
- no existing evidence modification
- no secret/token/cookie/access URL plaintext
- no Mem0 write
- no longterm write
- no passes:true
- no Close
- no git stage/commit/push

## Verification

Planned commands:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-stale-route-root-cause.md
rg -n "CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE|CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_BLOCKED|instances/11/control-ui|instances/10/control-ui|stale route|persisted URL|localStorage|sessionStorage|IndexedDB|useInstanceDesktopAccess|generateAccessToken|InstanceAccess|InstancePortalPage|control-ui bundle|manual connect form|disconnected \\(1006\\)|no implementation|no build/deploy|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-stale-route-root-cause.md
secret-shape scan with matched values suppressed
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-stale-route-root-cause.md
```

Verification results:

| Check | Exit | Result |
| --- | --- | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-stale-route-root-cause.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `instances/11/control-ui`, `instances/10/control-ui`, `stale route`, `persisted URL`, `localStorage`, `sessionStorage`, `IndexedDB`, `useInstanceDesktopAccess`, `generateAccessToken`, `InstanceAccess`, `InstancePortalPage`, `control-ui bundle`, `manual connect form`, `disconnected (1006)`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-stale-route-root-cause.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
