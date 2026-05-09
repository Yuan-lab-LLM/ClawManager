# Control UI device signature invalid root cause

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Root Cause Investigation

## Verdict

CONTROLUI_DEVICE_SIGNATURE_INVALID_ROOT_CAUSE_DONE

Root cause: the GTManager backend WebSocket bridge rewrites the first control-ui `connect` payload by replacing browser-supplied auth with the server-owned OpenClaw gateway token, but preserves the browser-generated `device` payload unchanged; the control-ui device signature is computed before that rewrite over inputs that include the auth token and challenge nonce, so the runtime validates a signature whose token input no longer matches the rewritten auth token and returns `device signature invalid`.

## Minimal observed chain

Source evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md:3`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md:24-33`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md:50-59`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md:66-79`

The upstream gate is `BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_BLOCKED`. It confirms instance 15 / `oc2gi-cp-150002` opened, the browser route after clicking the control-ui action was `/api/v1/instances/15/control-ui/chat?session=main`, no stale instance 10/11 route was observed, no `来源不被允许` origin failure was observed, and no disconnected 1006 failure was observed. The current blocker is the visible GTClaw `manual connection` form with `WebSocket URL 地址`, `gateway token`, and `password` inputs, plus the displayed runtime error `device signature invalid`.

## Control-ui bundle findings

Runtime path note: the image assembly manifest states the proven control-ui destination is `/usr/local/lib/node_modules/openclaw/dist/control-ui`; this investigation does not use `/opt/opensparrow/runtime/openclaw/dist/control-ui` as a fact source.

Sources:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md:18`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md:26-29`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js:1`

Key code locations:

- `assets/index-M4TNVXB3.js:27` maps known auth/origin error codes through `Kf/Gf`; otherwise it preserves the server-provided message. The exact text `device signature invalid` is not a local UI literal, so this message is propagated from the runtime close/error payload.
- `assets/index-M4TNVXB3.js:938` stores the close/error message in `lastError` by calling `Kf(...)` when the WebSocket closes with a structured message.
- `assets/index-M4TNVXB3.js:3906-3908` renders `lastError` inside the login/manual connection gate.
- `assets/index-M4TNVXB3.js:4450` also renders `lastError` in the app chrome as a danger pill.
- `assets/index-M4TNVXB3.js:3837-3904` renders the manual connection form. The input at `3846-3852` is the WebSocket URL field, the input at `3854-3864` is the gateway token field, and the input at `3878-3888` is the password field.
- `assets/zh-CN-B26mMdbY.js:1` provides the visible labels `WebSocket URL 地址`, `网关 token`, and `密码 (不存储)`.
- `assets/index-M4TNVXB3.js:4471` initializes settings, password, tab, connection state, and the current session key.
- `assets/index-M4TNVXB3.js:938` runs automatic connection during `connectedCallback` through `iO(...)`, then `YD(...)`; failure leaves the UI on the manual connection gate with `lastError`.
- `assets/index-M4TNVXB3.js:29` derives the mediated route gateway URL from the current `/api/v1/instances/:id/control-ui` location, detects GTManager-mediated control-ui routes, and loads any browser-persisted settings/token by normalized gateway URL.
- `assets/index-M4TNVXB3.js:2` defines the device auth canonical string builder `dt(...)`. The signed string includes protocol version, device id, client id, mode, role, scopes, signed time, auth token input, and nonce.
- `assets/index-M4TNVXB3.js:2` reads or creates the browser-side device identity store and computes `device.signature` in `er(...)` using the current auth token input and `connectNonce`.
- `assets/index-M4TNVXB3.js:2` builds `connect` params with both `auth` and `device`; `handleMessage(...)` stores `connect.challenge` as `connectNonce` before sending the signed connect request.

Control-ui interpretation:

- The route persistence fix is active for `/api/v1/instances/15/control-ui`, because the mediated route logic forces the gateway URL to the current route instead of a stale stored URL.
- The browser-side control-ui can derive the current WebSocket route and a local device identity, but it has no source for the server-owned upstream OpenClaw gateway token except local browser settings. The route access token/cookie is for GTManager access control, not an OpenClaw gateway signing secret for the browser.
- The signature input explicitly includes the auth token and nonce. Any downstream auth-token rewrite after signing invalidates the signature unless the device payload is recomputed or the runtime validation contract deliberately ignores that token input.

## Backend proxy and websocket bridge findings

Sources:

- `backend/internal/handlers/instance_handler.go:629-727`
- `backend/internal/handlers/instance_handler.go:842-931`
- `backend/internal/services/instance_access_service.go:29-35`
- `backend/internal/services/instance_access_service.go:96-109`
- `backend/internal/services/instance_proxy_service.go:32-54`
- `backend/internal/services/instance_proxy_service.go:141-220`
- `backend/internal/services/instance_proxy_service.go:320-415`
- `backend/internal/services/instance_proxy_service.go:438-563`
- `backend/internal/services/instance_proxy_service.go:725-778`
- `backend/internal/services/instance_proxy_service_test.go:392-512`
- `backend/internal/services/instance_proxy_service_test.go:514-650`

Key code locations:

- `instance_access_service.go:96-109` defines the `control-ui` access scope for OpenClaw instances under `/api/v1/instances/<id>/control-ui`, including route-scoped cookie and route-prefix validation.
- `instance_handler.go:842-844` routes control-ui proxy requests through `proxyInstanceWithMode(...AccessModeControlUI)`.
- `instance_handler.go:847-912` validates the GTManager route token or route-scoped cookie.
- `instance_handler.go:914-923` resolves server-side upstream auth and sends WebSocket requests to `ProxyWebSocketWithScopeAndUpstreamAuth`.
- `instance_handler.go:930-931` sends non-WebSocket requests to `ProxyRequestWithScopeAndUpstreamAuth`.
- `instance_proxy_service.go:46-54` constructs upstream auth from `instance.AccessToken`; it contains only the OpenClaw gateway token material.
- `instance_proxy_service.go:141-220` proxies static HTTP assets, strips the instance route prefix, applies upstream auth, injects a base href into HTML, and rewrites favicon hrefs. This path does not inject gateway token, password, device identity, or device signature state into the control-ui bundle.
- `instance_proxy_service.go:320-357` proxies the WebSocket path, strips route query token/password material before forwarding, sets forwarded headers, and applies server-side upstream auth at the WebSocket handshake boundary.
- `instance_proxy_service.go:406-415` starts the upstream-to-browser pipe first and bridges the first browser `connect` after forwarding the upstream challenge.
- `instance_proxy_service.go:438-475` receives the first browser frame and rewrites the connect payload before sending it upstream.
- `instance_proxy_service.go:478-526` preserves all non-auth params from the browser, including `device`, but replaces `params.auth` with a map containing the server-owned upstream token.
- `instance_proxy_service.go:535-546` removes route token and password query material before upstream forwarding.
- `instance_proxy_service.go:549-563` applies the server-owned upstream token to upstream HTTP/WebSocket authentication headers.
- `instance_proxy_service_test.go:392-512` proves browser token/password/device-token material is stripped or replaced while non-auth params, including `device`, are preserved.
- `instance_proxy_service_test.go:514-650` proves `connect.challenge` is forwarded before the rewritten connect request and the browser `device.nonce` is preserved.

Backend interpretation:

- `/api/v1/instances/:id/control-ui` correctly proxies static assets and WebSocket traffic for the selected instance.
- The WebSocket challenge/nonce is forwarded; the bridge does not generate a nonce and does not synthesize a device identity.
- The backend injects server-owned upstream gateway auth into the handshake/header path and into the first `connect` payload auth field, but no code injects or recomputes gateway token/password/device identity/device signature state inside the control-ui JavaScript runtime.
- The critical mismatch is at the WebSocket first-connect boundary: `params.device.signature` was computed by the browser before the bridge replaces `params.auth`.

## Runtime startup config materialization findings

Sources:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml:9-18`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-ensure-controlui-origin:4-12`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-ensure-controlui-origin:45-56`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-gateway-with-origin-allowlist:1-10`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md:51-66`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md:78-80`
- `backend/internal/services/instance_service.go:520-570`
- `backend/internal/services/instance_runtime.go:62-72`

Key code locations:

- `config.yaml:9-18` starts the gateway through the origin-allowlist wrapper with `openclaw gateway run --bind lan --auth token`.
- `openclaw-ensure-controlui-origin:45-56` materializes `gateway.controlUi.allowedOrigins` in `/config/.openclaw/openclaw.json`.
- `openclaw-gateway-with-origin-allowlist:1-10` runs the origin materialization script before starting the gateway.
- `MANIFEST.md:51-66` documents that startup materializes the control-ui allowed origin.
- `MANIFEST.md:78-80` states no token/secret is hardcoded in the artifact and the gateway token is runtime environment material.
- `instance_service.go:520-570` ensures `instance.AccessToken` exists and exposes it to the runtime as an OpenClaw gateway environment variable.
- `instance_runtime.go:62-72` assembles OpenClaw runtime environment keys; it does not materialize a browser device identity or device signature.

Runtime interpretation:

- `gateway.controlUi.allowedOrigins` is materialized.
- Gateway token auth is enabled and supplied to the runtime as environment material.
- There is no static startup materialization for device identity, device token, password, signature, or a browser-side signing contract.

## Data flow table

| Step | Source boundary | Evidence | Result |
| --- | --- | --- | --- |
| Browser route | GTManager UI opens instance 15 / `oc2gi-cp-150002` control-ui route | Manual E2E evidence lines 24-33 and 50-59 | Current path is `/api/v1/instances/15/control-ui/chat?session=main`; stale instance route excluded |
| Control-ui initial state | Bundle settings loader detects mediated `/api/v1/instances/:id/control-ui` route | `assets/index-M4TNVXB3.js:29` | Gateway URL is forced to the current instance route; local browser settings may provide only browser-stored token state |
| Static proxy | Backend proxies `/api/v1/instances/:id/control-ui` assets | `instance_handler.go:842-931`; `instance_proxy_service.go:141-220` | HTML/base/icon rewriting occurs; no gateway token/password/device identity injection into JavaScript |
| WS challenge | Runtime sends `connect.challenge`; backend bridge forwards upstream-to-browser first | `instance_proxy_service.go:406-415`; tests `514-650` | Browser receives nonce and sets `connectNonce`; disconnected 1006 excluded |
| Browser signature | Control-ui builds `auth` and `device`; signature canonical string includes auth token input and nonce | `assets/index-M4TNVXB3.js:2`; `assets/index-M4TNVXB3.js:938` | Browser signs with its local auth token input and challenge nonce, not with server-owned rewritten auth unless that token is available locally |
| Backend first-connect rewrite | Bridge rewrites first `connect` payload | `instance_proxy_service.go:438-526`; tests `392-512` | `params.auth` is replaced with server-owned upstream token while `params.device` and `device.signature` are preserved unchanged |
| Runtime validation/error | OpenClaw gateway validates rewritten auth plus preserved device signature | UI error propagation at `assets/index-M4TNVXB3.js:27`, `938`, `3906-3908` | Runtime rejects the mismatch and the UI displays `device signature invalid` on the manual connection form |

## Exclusions

- stale instance route excluded: the evidence route is `/api/v1/instances/15/control-ui/chat?session=main`, the instance page identifies instance 15 / `oc2gi-cp-150002`, and no instance 10/11 route was observed.
- origin allowlist excluded: the manual E2E evidence did not show `来源不被允许`, and the runtime startup artifact materializes `gateway.controlUi.allowedOrigins`.
- disconnected 1006 excluded: the manual E2E evidence did not show disconnected 1006, and backend tests/evidence show `connect.challenge` is forwarded before the first rewritten connect request.

## Root cause verdict

Primary root cause, strongest evidence: backend/server-side auth injection and browser-side device signing are split across a boundary that changes signed input after signing. The control-ui browser signs a device payload using its local auth token input plus nonce; the backend bridge then replaces only `params.auth` with the server-owned gateway token and preserves `params.device.signature`. Because the canonical signature input includes the token, runtime validation sees inconsistent auth and device-signature inputs and returns `device signature invalid`.

Secondary candidate, weaker evidence: browser-persisted device token or local settings under the current gateway URL could make the mismatch deterministic for a reused browser profile. This is not required for the failure because an empty or different browser token would still mismatch after the backend injects a server-owned token.

Less likely candidate: missing runtime startup materialization for device identity. The reviewed startup artifacts only materialize origin allowlist and gateway token env material; no evidence shows a required static device identity file. The control-ui bundle is designed to create a browser-side device identity dynamically.

Excluded candidates: stale instance route, origin allowlist failure, disconnected 1006, and missing challenge forwarding.

## Candidate next gate

candidate next gate: `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_DESIGN`

Scope for that gate should be design-only first: decide the trusted boundary for OpenClaw gateway auth and device signatures. The decision needs to align the server-owned upstream token, browser-generated device signature, and runtime validation contract without exposing token/password/key material in browser-visible evidence.

## Referenced paths

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-review.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-safety-hardening.md`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/`
- `backend/internal/handlers/instance_handler.go`
- `backend/internal/services/instance_access_service.go`
- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `backend/internal/services/instance_service.go`
- `backend/internal/services/instance_runtime.go`

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no image build/tag/push/pull, no registry cleanup, no browser storage/cache/cookie cleanup, no token/password/key/cookie/bearer/auth header/access URL plaintext recording, no source mutation outside this newly added evidence file, no backend/frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework/spec-plan-tasks edits, no existing evidence edits, no Mem0 write, no stage, no commit, no push, no passes true, and no Close.
