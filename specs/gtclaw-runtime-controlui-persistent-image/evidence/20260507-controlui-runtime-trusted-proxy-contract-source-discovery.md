# Control UI runtime trusted proxy contract source discovery

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Runtime Contract Source Discovery

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_SOURCE_DISCOVERY

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_SOURCE_DISCOVERY_BLOCKED: runtime gateway first-connect validator source is not present in the repo-owned source or approved artifacts reviewed in this gate. The repo contains GTManager backend proxy code, the minified control-ui client bundle, and startup/image assembly artifacts, but not the OpenClaw gateway server source for connect auth, device signature validation, pairing retry, or trusted-proxy acceptance.

GPT Pro decision = REQUIRE_RUNTIME_CONTRACT_CHANGE

This was source discovery and minimum boundary design only. No implementation patch was applied.

## Dependency chain used

- `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_GPT_PRO_DECISION = REQUIRE_RUNTIME_CONTRACT_CHANGE`
- `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_DESIGN_DONE`
- `CONTROLUI_DEVICE_SIGNATURE_INVALID_ROOT_CAUSE_DONE`
- `BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_BLOCKED`

Prior evidence establishes the current blocker: correct instance route reached GTClaw control-ui, stale route/origin/1006 are excluded, and the remaining browser-visible failure is manual form plus `device signature invalid`.

## Source discovery summary

Runtime source found: no, not for the gateway/server validator.

Repo-local source/artifact surfaces found:

- Backend trusted route and bridge source:
  - `backend/internal/handlers/instance_handler.go:629-727`
  - `backend/internal/handlers/instance_handler.go:842-931`
  - `backend/internal/services/instance_access_service.go:29-35`
  - `backend/internal/services/instance_access_service.go:72-109`
  - `backend/internal/services/instance_proxy_service.go:32-54`
  - `backend/internal/services/instance_proxy_service.go:320-415`
  - `backend/internal/services/instance_proxy_service.go:438-563`
  - `backend/internal/services/instance_proxy_service_test.go:392-512`
  - `backend/internal/services/instance_proxy_service_test.go:514-650`
- Runtime startup/config artifacts:
  - `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml:9-18`
  - `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-ensure-controlui-origin:45-56`
  - `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md:51-80`
  - `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md:18-29`
- Control-ui client bundle, at the proven runtime destination root `/usr/local/lib/node_modules/openclaw/dist/control-ui`:
  - `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js:2`
  - `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js:27`
  - `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js:938`
  - `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js:4023`
  - `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js:4194`

Runtime gateway/server source not found:

- connect request schema
- connect auth token validation
- device signature validation
- pairing/device-token retry logic
- runtime-side `controlUi.allowedOrigins` usage
- runtime-side trustedProxy marker parsing or validation

Search notes:

- Repo dependency manifests found only `backend/go.mod`, `frontend/package.json`, `frontend/package-lock.json`, `frontend/vite.config.ts`, and `frontend/tsconfig.json`; no OpenClaw server package source or vendored OpenClaw module was found.
- `find`/`rg` over repo source and approved artifacts found protocol strings in backend tests, the control-ui bundle, startup scripts/manifests, and evidence. After excluding evidence and minified bundle files, no runtime gateway validator source remained.
- The existing startup and image assembly artifacts are Docker build contexts over a parent image. They copy startup scripts and control-ui static files but do not contain the OpenClaw gateway server implementation.

Blocked reason:

The true runtime source location for first connect validation cannot be named with file and line number from this repo because that source has not been ingested into the repository or approved artifacts. The next gate must intake the OpenClaw runtime/gateway source that built the parent image or recover the exact server source from a separately approved source artifact before patch planning can be precise.

## What the located code proves

### Control-ui client bundle

- `assets/index-M4TNVXB3.js:2` defines auth error constants including `DEVICE_AUTH_SIGNATURE_INVALID`, pairing-related states, device auth token storage, device identity storage, canonical device signing input `dt(...)`, device signature generation `er(...)`, `buildConnectPlan`, `buildConnectParams`, `handleMessage(...)`, and `connect.challenge` handling.
- The canonical signing input includes the auth token input and nonce, so a later rewrite of connect auth can invalidate `device.signature`.
- `assets/index-M4TNVXB3.js:27`, `938`, and `3906-3908` propagate runtime close/error messages such as `device signature invalid` into the manual connection UI.
- `assets/index-M4TNVXB3.js:4023` checks a runtime hello snapshot auth mode equal to `trusted-proxy`.
- `assets/index-M4TNVXB3.js:4194` only displays translated trusted-proxy status text when the runtime reports that mode. This is UI display, not proof that first connect can carry or validate a trustedProxy marker.

### Backend bridge

- `instance_access_service.go:96-109` defines the GTManager `control-ui` route-scoped boundary under `/api/v1/instances/<id>/control-ui`.
- `instance_handler.go:879-912` validates a route-scoped token or route-scoped cookie before proxying.
- `instance_handler.go:914-931` resolves upstream auth and dispatches HTTP/WebSocket proxying.
- `instance_proxy_service.go:46-54` obtains server-owned OpenClaw gateway token material from the instance.
- `instance_proxy_service.go:320-357` forwards WebSocket requests to the runtime and applies backend-owned upstream auth at the backend-to-runtime boundary.
- `instance_proxy_service.go:406-415` forwards runtime pre-connect frames such as `connect.challenge` before reading and rewriting the browser first connect.
- `instance_proxy_service.go:478-526` currently removes browser `params.auth`, injects server-owned auth into JSON, and preserves non-auth params, including `params.device`.
- `instance_proxy_service.go:549-563` strips browser auth headers/cookies and applies upstream auth headers to the runtime request.
- `instance_proxy_service_test.go:392-512` and `514-650` assert the current behavior, including preserved `params.device` and challenge forwarding.

### Runtime startup artifacts

- `config.yaml:9-18` runs `openclaw gateway run --bind lan --auth token` through the wrapper.
- `openclaw-ensure-controlui-origin:45-56` materializes `gateway.controlUi.allowedOrigins`.
- `MANIFEST.md:78-80` records that no static token value is stored; the OpenClaw gateway token is runtime environment material.
- No startup artifact contains device identity, device token, device signature, trustedProxy config, or first connect validator source.

## Minimum runtime trustedProxy contract shape

Accepted fields for mediated trusted proxy first connect:

- JSON request envelope fields needed by the existing protocol, such as request type/id and method `connect`.
- Non-auth connect params that describe client capabilities and context, such as protocol range, client identity, role, scopes, capabilities, user agent, locale, and session context.
- A backend-owned non-secret `trustedProxy` marker, or equivalent runtime-approved marker name, only if the runtime source confirms that name.

Removed fields for mediated trusted proxy first connect:

- browser `params.auth`
- browser `params.device`
- browser `device.signature`
- browser password material
- browser device-token material
- any browser-supplied OpenClaw gateway token in the JSON connect payload

trustedProxy marker validation rule:

- The marker is not proof by itself.
- The runtime must accept the marker only when the backend-to-runtime boundary has already authenticated with the server-owned OpenClaw gateway token and the request is on the trusted control-ui path/network boundary.
- A direct browser client that sends the marker without the authenticated backend-to-runtime boundary must fail.
- A client with only route-scoped GTManager access but no backend upstream auth must fail at runtime.

Failure behavior:

- Missing backend-to-runtime auth plus trustedProxy marker should fail as unauthorized or forbidden.
- Browser `params.auth`, `params.device`, or `device.signature` present in trustedProxy mode should be rejected or ignored by a documented rule; rejecting is easier to test.
- Direct-client invalid device signature must still fail as `device signature invalid`.
- Pairing/device-token retry behavior should not run for trustedProxy mode unless runtime maintainers explicitly require it.

Direct client regression boundaries:

- Standalone control-ui with token/password auth must continue to work.
- Direct device signature validation must continue to require the runtime challenge nonce.
- Pairing-required and device-token retry behavior must continue for direct clients.
- Origin checks using `gateway.controlUi.allowedOrigins` must continue for browser-originated direct control-ui traffic.

## Backend/runtime pairing boundary

Backend side:

- Delete browser `params.auth` from the first connect forwarded upstream.
- Delete browser `params.device` from the first connect forwarded upstream.
- Delete browser `device.signature` by removing the whole browser device object.
- Set only backend-owned non-secret trustedProxy marker fields approved by runtime source.
- Continue using server-owned OpenClaw gateway token only at the backend-to-runtime boundary.
- Do not place the OpenClaw gateway token into the JSON connect payload.
- Preserve route-scoped browser authentication through GTManager token/cookie validation.

Runtime side:

- Add or modify first connect schema and connect auth handling to accept the trustedProxy mode.
- Bind trustedProxy acceptance to authenticated backend-to-runtime auth, not to the marker alone.
- Bypass direct device signature validation only for authenticated trustedProxy mode.
- Keep direct client connect auth and device signature validation unchanged.

## Candidate file write set

backend mandatory:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`

backend optional:

- `backend/internal/handlers/instance_handler.go`, only if an explicit trusted-proxy mode flag must be carried into the proxy service.
- `backend/internal/services/instance_access_service.go`, only if route-scoped control-ui access metadata needs a new internal mode label.

runtime mandatory:

- Blocked pending source intake. Expected logical areas are runtime gateway WebSocket connect schema, connect auth validator, device signature validation, pairing/device-token retry logic, trustedProxy marker validation, and runtime protocol tests.
- Exact runtime source files cannot be named until the OpenClaw gateway source for the parent image is available in a repo-owned or approved source artifact.

startup artifact conditional:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml`, only if runtime requires a config knob enabling trusted proxy.
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-ensure-controlui-origin`, only if config materialization must add a trusted proxy config subtree beside `gateway.controlUi.allowedOrigins`.
- Startup artifact `Dockerfile` and `MANIFEST.md`, only as part of a later approved runtime image artifact gate.

control-ui bundle conditional:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`, only if the accepted runtime contract requires browser-visible first-connect behavior changes or UI state changes.
- Not mandatory for the preferred boundary if backend rewrites the first connect and runtime reports trusted proxy mode after successful connect.

## Test plan

backend unit tests:

- Verify mediated control-ui rewrite removes browser `params.auth`, browser `params.device`, and browser `device.signature`.
- Verify the JSON connect payload does not contain the OpenClaw gateway token.
- Verify a backend-owned non-secret trustedProxy marker is present only in control-ui trusted proxy mode.
- Verify backend-to-runtime upstream auth remains applied outside the JSON connect payload.
- Verify route query token and password material are stripped before upstream forwarding.
- Verify `connect.challenge` is still forwarded before first connect.
- Verify malformed JSON, non-text frame, missing connect method, and missing upstream auth fail closed.

runtime protocol tests:

- Trusted proxy first connect succeeds only with authenticated backend-to-runtime auth plus trustedProxy marker.
- trustedProxy marker alone fails.
- Trusted proxy first connect with browser `params.auth`, `params.device`, or `device.signature` fails or is ignored according to the accepted schema.
- Direct client token/password connect behavior remains unchanged.
- Direct device signature validation still checks nonce and rejects invalid signatures.
- Pairing-required and device-token retry behavior remains unchanged for direct clients.
- Origin/controlUi config usage remains enforced for direct browser-originated control-ui traffic.

integration tests:

- Backend bridge plus runtime gateway protocol test using server-owned upstream auth and stripped JSON auth/device fields.
- Negative integration test that a direct client cannot spoof trustedProxy.
- Regression integration for stale route, origin allowlist, and challenge forwarding.

fresh instance and browser/manual E2E gates:

- Runtime image build/tag/push gate only after runtime source patch approval.
- Fresh OpenClaw instance using the patched runtime image.
- Browser/manual E2E rerun proving `/api/v1/instances/<id>/control-ui` connects without manual gateway token/password entry, without `device signature invalid`, without stale route, without origin allowlist failure, and without disconnected 1006.

## Next gate recommendation

Recommended next gate: `CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE`

Purpose:

- Intake the exact OpenClaw runtime/gateway source corresponding to the parent image digest used by the current runtime artifact.
- Identify real runtime source files and line numbers for connect request schema, connect auth validation, device signature validation, pairing/device-token retry, origin/controlUi config usage, and any existing trusted-proxy mode.
- Only after that, open `CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_APPROVAL_PACKET`.

Do not start backend-only implementation unless runtime source intake proves the current runtime already supports the required trustedProxy contract. Current dependency decision says REQUIRE_RUNTIME_CONTRACT_CHANGE.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no implementation patch, no backend/frontend/runtime artifact/deployment/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no image build/tag/push/pull, no registry cleanup, no old session cleanup, no old asset cleanup, no old tag cleanup, no Mem0 write, no passes true, no Close, no git stage/commit/push, and no token/password/key/cookie/bearer/auth header/access URL plaintext recording.
