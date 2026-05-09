# Control UI WebSocket auth/signature alignment design

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Designer / Design Gate

Gate: `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_DESIGN`

Dependencies:

- `CONTROLUI_DEVICE_SIGNATURE_INVALID_ROOT_CAUSE_DONE`
- `BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_BLOCKED`

## Verdict

CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_DESIGN_DONE

Recommended design: make GTManager the trusted proxy boundary for mediated control-ui WebSocket connections. Keep the server-owned OpenClaw gateway token entirely backend-side, keep GTManager route-scoped access as the browser-facing boundary, and stop forwarding a browser-generated `device.signature` that was signed against different `params.auth` input. The implementation should rewrite the first `connect` as one internally consistent trusted-proxy request by removing browser credential material and browser-signed device material, or by using a runtime-confirmed non-secret trusted-proxy marker.

This is a design-only gate. No implementation patch was applied.

## Confirmed root cause

The first control-ui WebSocket `connect` crosses two auth boundaries that are currently inconsistent:

1. The browser control-ui computes `params.device` and `device.signature` before sending the first `connect`.
2. The signature canonical string includes the auth token input and the runtime challenge nonce.
3. The GTManager backend bridge then replaces `params.auth` with server-owned OpenClaw gateway token material.
4. The bridge preserves `params.device` and its signature unchanged.
5. The runtime validates rewritten auth together with the preserved browser signature and returns `device signature invalid`.

The current backend code supports this chain:

- `backend/internal/services/instance_proxy_service.go:438-475` reads the first browser frame and writes the rewritten frame upstream.
- `backend/internal/services/instance_proxy_service.go:478-525` removes browser `params.auth`, injects server-owned auth, and preserves other fields including `params.device`.
- `backend/internal/services/instance_proxy_service.go:549-563` strips browser auth headers and applies server-owned upstream auth for control-ui.
- `backend/internal/services/instance_proxy_service_test.go:392-512` asserts the first connect auth rewrite while preserving non-auth params.
- `backend/internal/services/instance_proxy_service_test.go:514-650` asserts the runtime `connect.challenge` reaches the browser before the rewritten first connect and that browser `device.nonce` is preserved.
- `backend/internal/handlers/instance_handler.go:879-923` validates the route token or route-scoped cookie before proxying and resolves server-side upstream auth.
- `backend/internal/services/instance_access_service.go:96-109` defines the `/api/v1/instances/<id>/control-ui` access scope.

## Exclusions preserved from prior gates

- stale instance route excluded: the manual evidence used `/api/v1/instances/15/control-ui/chat?session=main` for instance 15 / `oc2gi-cp-150002`; no stale instance 10 or 11 route was observed.
- origin allowlist excluded: the runtime startup artifact materializes `gateway.controlUi.allowedOrigins`, and the browser evidence did not show the origin-denied message.
- disconnected 1006 excluded: the browser evidence did not show disconnected 1006, and backend tests show the challenge is forwarded before first connect.

## Design options

### Option A - Backend no longer rewrites the signed auth/device boundary

Design: stop changing `params.auth` after the browser signs `params.device`. The browser-signed `params.auth` and `params.device` would pass through as one signed unit.

Security impact: cryptographically consistent, but it moves the OpenClaw gateway credential problem back to the browser. Automatic mediated control-ui would only work if the browser already has the gateway token or password-equivalent material.

OpenClaw gateway token exposure: unacceptable for the desired product path if automatic connection requires GTManager to provide the browser with the token. If GTManager does not expose it, this option does not solve the blocked E2E.

GTManager route-scoped boundary: weakened if the browser receives reusable OpenClaw gateway token material, because possession of that token can outlive or bypass the GTManager route-scoped access token and cookie boundary.

Likely files:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`

Tests:

- Unit test that first connect payload is not modified after browser signing.
- Negative test that browser query token and password material are not forwarded as upstream query material.
- Regression test for route-scoped cookie validation and target path stripping.

Runtime image / fresh instance / browser E2E gates:

- No runtime image needed if the existing runtime accepts browser-supplied OpenClaw auth.
- Browser E2E still required.
- Fresh instance is recommended only to avoid old runtime/config evidence ambiguity.

Decision: reject for the mediated GTManager flow. It either fails without browser credentials or exposes credentials that should stay server-owned.

### Option B - Backend acts as a trusted proxy and aligns first connect

Design: keep the backend as the only holder of the OpenClaw gateway token. The upstream WebSocket handshake remains server-authenticated. For the first control-ui `connect`, the backend must stop mixing rewritten `params.auth` with stale browser-signed `params.device`. The preferred trusted proxy shape is:

- remove browser `params.auth` fields;
- remove browser `params.device` fields, including `device.signature`, because they were signed over browser-selected auth input;
- do not put the OpenClaw gateway token into the JSON `connect` payload;
- if the runtime requires an explicit indicator, add only a non-secret trusted-proxy marker that the runtime contract confirms;
- preserve non-auth, non-device client capability fields that are not signed credentials.

Security impact: strongest option if the runtime supports this trusted proxy contract. The browser proves only GTManager route access. The backend proves upstream OpenClaw access. No browser-controlled signature is reinterpreted after auth rewrite.

OpenClaw gateway token exposure: no exposure to browser JavaScript, route URLs, logs, or evidence. The token remains server-owned and is used only across the backend-to-runtime boundary.

GTManager route-scoped boundary: preserved. Browser access still depends on the GTManager route token or route-scoped cookie under `/api/v1/instances/<id>/control-ui`.

Likely files:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- maybe `backend/internal/handlers/instance_handler.go` only if a clearer trusted-proxy mode flag must be passed into the proxy service

Tests:

- Unit test that control-ui first connect removes browser `params.auth`, `params.device`, and `device.signature`.
- Unit test that no browser token, password, device token, cookie, or browser auth header material reaches the upstream JSON payload.
- Unit test that server-owned upstream auth is still applied at the backend-to-runtime WebSocket boundary.
- Unit test that challenge forwarding still happens before first connect.
- Unit test that unrelated client fields such as protocol range, client identity, role, scopes, caps, locale, user agent, and session query remain intact.
- Negative test for non-text first frame, malformed JSON, missing connect method, and missing upstream token.

Runtime image / fresh instance / browser E2E gates:

- If current OpenClaw runtime already accepts server-authenticated trusted proxy first connect without browser device signature, no runtime image is needed.
- If not, this option requires the runtime/control-ui contract change described in Option D.
- A fresh instance and browser manual E2E rerun are required after implementation because the prior gate was blocked in a live mediated browser flow.

Decision: recommend, subject to confirming the exact runtime trusted proxy contract before implementation.

### Option C - Browser obtains OpenClaw gateway auth

Design: provide the browser with OpenClaw gateway auth so the browser can compute `device.signature` over the same token that the runtime validates.

Possible mechanisms include HTML injection, a GTManager API endpoint, URL/query/fragment material, or prefilled local control-ui settings.

Security impact: poor for this product boundary. It turns a backend-owned runtime credential into browser-visible material, expands credential lifetime into browser storage or page memory, and increases accidental recording risk.

OpenClaw gateway token exposure: yes if automatic connection is expected. This conflicts with the current design goal that users should not type or see gateway token/password/key material.

GTManager route-scoped boundary: not preserved as the primary access boundary. A copied or persisted OpenClaw gateway token could authorize access outside the intended route-scoped GTManager path if the runtime is otherwise reachable.

Likely files:

- `backend/internal/handlers/instance_handler.go`
- `backend/internal/services/instance_proxy_service.go`
- maybe frontend or control-ui artifact code if settings injection is implemented

Tests:

- Backend tests proving no token is emitted in logs or route URLs would be mandatory but insufficient.
- Browser tests proving no visible token and no persistent token leak would be difficult and fragile.
- Security review required for every browser-visible transport and storage path.

Runtime image / fresh instance / browser E2E gates:

- Runtime image likely not required.
- Browser E2E required.
- Additional secret-leak scanning required.

Decision: reject. It solves signature alignment by exposing the wrong credential boundary.

### Option D - Runtime/control-ui auth contract adjustment

Design: change the OpenClaw runtime/control-ui protocol so a GTManager-mediated trusted proxy can authenticate with server-owned upstream auth and does not require browser-generated device auth for that mediated first connect. The browser would use route-scoped GTManager access, while the runtime would trust only a backend-to-runtime request path that is already authenticated by the server-held gateway credential and constrained by network reachability.

Security impact: can be strong if the runtime explicitly differentiates trusted proxy auth from direct browser auth, refuses untrusted direct clients, and does not accept spoofable browser headers as proof. Risk depends on exact runtime implementation.

OpenClaw gateway token exposure: no exposure if implemented correctly.

GTManager route-scoped boundary: preserved if the runtime trusted proxy mode is only reachable through GTManager's validated route and backend network path.

Likely files:

- runtime control-ui gateway auth code, exact source path outside this design gate
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/` only as a later artifact source if rebuilding control-ui is necessary
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/` if runtime config needs a trusted proxy setting
- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`

Tests:

- Runtime protocol tests for trusted proxy first connect without browser device signature.
- Runtime negative tests that direct browser clients cannot spoof trusted proxy state.
- Backend tests for the first-connect trusted proxy payload shape.
- Integration test between backend proxy and runtime gateway.

Runtime image / fresh instance / browser E2E gates:

- Runtime image build/tag/push gate required.
- Fresh OpenClaw instance required.
- Browser manual E2E rerun required.
- Existing persistence and origin allowlist evidence must be preserved or rerun as needed.

Blast radius: high. This changes the OpenClaw auth contract and may affect direct control-ui, webchat, TUI, CLI, device-token retry, pairing, and future upstream upgrades unless scoped narrowly to GTManager-mediated control-ui.

Decision: fallback if Option B is not supported by the current runtime contract.

## Recommendation

Choose Option B as the implementation direction, but do not implement until a short decision gate confirms the OpenClaw runtime behavior for a server-authenticated trusted proxy first connect with browser `params.device` removed.

Preferred final contract:

- Browser authenticates only to GTManager through the route-scoped access token or cookie.
- Backend authenticates to OpenClaw with the server-owned OpenClaw gateway token.
- Backend does not expose OpenClaw gateway token, password, key, cookie, bearer material, or access URL values.
- Backend does not preserve browser `device.signature` after changing or removing the auth boundary it was signed against.
- Runtime accepts the mediated trusted proxy request only when it is authenticated at the backend-to-runtime boundary and scoped to control-ui.

Rejected designs:

- Reject Option A because pass-through only cannot satisfy automatic GTManager-mediated control-ui without moving OpenClaw credentials to the browser.
- Reject Option C because browser-visible OpenClaw gateway auth breaks the intended server-owned credential boundary.
- Reject backend recomputation using the browser device identity because the backend does not and should not have the browser private key.
- Treat backend-generated proxy device identity as a fallback sub-design only if GPT Pro or runtime maintainers confirm it is the intended protocol shape; it introduces server key lifecycle, device-token storage semantics, and pairing implications.

## File write-set suggestion for a future implementation gate

Primary backend-only write set if current runtime supports trusted proxy mode:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`

Possible backend handler write if a clearer mode flag is needed:

- `backend/internal/handlers/instance_handler.go`

Conditional runtime write set only if the current runtime cannot accept the trusted-proxy first-connect shape:

- runtime gateway auth source, exact upstream path to be identified in that gate
- control-ui runtime source artifact only if browser behavior or UI copy must change
- runtime startup artifact only if a trusted proxy config knob must be materialized

No deployment/docs/longterm/AgentTeam/UnifiedFramework write is part of this design gate.

## Test plan

Backend unit tests:

- `go test ./internal/services -run 'ControlUI|ProxyWebSocket'` after adding focused tests.
- `go test ./internal/handlers -run 'ControlUI|Proxy'` if handler behavior changes.
- `go test ./...` before any implementation completion claim.

Static checks:

- `git diff --check` for all changed files.
- Sensitive material scan over the evidence and future patches.

Protocol checks:

- Verify first connect no longer contains browser `params.auth`, browser `params.device`, or `device.signature` when the trusted proxy mode is active.
- Verify upstream query still strips route token and password material.
- Verify route-scoped access still rejects mismatched route tokens and unsupported instance types.
- Verify challenge forwarding remains before first connect.

Runtime/browser checks after implementation:

- If backend-only, redeploy backend and rerun browser manual E2E against a known OpenClaw instance.
- If runtime contract changes, build the runtime image, start a fresh instance using that image, then rerun browser manual E2E.
- E2E pass criteria: opening `/api/v1/instances/<id>/control-ui` reaches usable GTClaw control-ui without manual gateway token or password entry, without `device signature invalid`, without stale route, without origin allowlist failure, and without disconnected 1006.

## Follow-up gate order

1. `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_GPT_PRO_DECISION`, recommended before implementation because the runtime trusted proxy first-connect contract is uncertain.
2. `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_BACKEND_PATCH`, backend implementation only if the decision confirms runtime-compatible trusted proxy shape.
3. `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_UNIT_VERIFICATION`, Go tests and sensitive scan.
4. `CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH`, conditional only if the runtime contract must change.
5. `CONTROLUI_RUNTIME_IMAGE_FRESH_INSTANCE_GATE`, conditional for runtime changes.
6. `BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_RERUN`, required before any passes true or close action.

## GPT Pro escalation packet draft

Use this packet only as a ready-to-forward prompt. Do not include credential values.

Question: confirm the intended OpenClaw control-ui runtime auth contract for a GTManager-mediated trusted proxy.

Context:

- GTManager serves `/api/v1/instances/<id>/control-ui` after validating a route-scoped token or route-scoped cookie.
- GTManager holds the server-owned OpenClaw gateway token and uses it only on the backend-to-runtime boundary.
- The browser control-ui currently builds first `connect` with `params.auth`, `params.device`, and `device.signature`.
- The signature canonical string includes the token input and the runtime challenge nonce.
- Existing GTManager code rewrites `params.auth` to server-owned auth but preserves `params.device`, causing `device signature invalid`.

Relevant files:

- `backend/internal/services/instance_proxy_service.go`, especially first-connect bridge and rewrite functions.
- `backend/internal/services/instance_proxy_service_test.go`, especially tests covering auth rewrite and challenge forwarding.
- `backend/internal/handlers/instance_handler.go`, especially route-scoped validation and upstream auth resolution.
- `backend/internal/services/instance_access_service.go`, especially control-ui scope.
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`, especially device signature canonicalization and connect builder.
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/`, especially token auth and origin allowlist startup configuration.

Decision needed:

- For a trusted proxy that already authenticates to the runtime with server-owned upstream auth, should the first `connect` omit browser `params.device` and browser `device.signature`, include a non-secret trusted-proxy marker, or include a backend-generated proxy device signature?
- If omission is accepted, which exact fields must remain in `connect` and which fields must be removed?
- If backend-generated proxy device signature is required, what stable identity and key lifecycle should GTManager use, and how should device-token or pairing behavior be handled?
- If the runtime currently lacks this contract, what is the smallest runtime-only change that keeps direct control-ui auth behavior unchanged?

Constraints:

- Do not expose OpenClaw gateway token, password, key, cookie, bearer material, auth header values, or access URL values to the browser or evidence.
- Preserve GTManager route-scoped access boundary.
- Do not rely on browser storage cleanup or manual credential entry.
- Keep stale route, origin allowlist, and 1006 fixes intact.

Preferred answer format:

- State the accepted first-connect payload shape.
- State whether runtime image changes are required.
- State backend files to modify.
- State minimum tests and E2E gates.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no implementation patch, no backend/frontend/runtime/deployment/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no database action, no instance mutation, no image build/tag/push/pull, no registry cleanup, no Mem0 write, no passes true, no Close, no stage/commit/push, and no token/password/key/cookie/bearer/auth header/access URL plaintext recording.
