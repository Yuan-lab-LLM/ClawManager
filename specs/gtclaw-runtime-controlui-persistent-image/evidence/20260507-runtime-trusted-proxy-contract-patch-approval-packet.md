# Runtime trusted-proxy contract patch approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_APPROVAL_PACKET

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_APPROVAL_PACKET_DONE

This is an approval packet only. It authorizes neither implementation nor runtime operations.

GPT Pro decision = REQUIRE_RUNTIME_CONTRACT_CHANGE

## Source provenance

OpenClaw openclaw@2026.4.14, repo https://github.com/openclaw/openclaw.git, tag v2026.4.14, peeled commit 323493fa1b6adc1e10b9954a68d5eaa5a6ef1170.

Source discovery evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-openclaw-runtime-source-intake.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-ws-auth-signature-alignment-design.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-device-signature-invalid-root-cause.md`

## Root finding

Current runtime has config-level trusted-proxy mode, but not the GPT Pro hybrid contract.

Runtime source findings from source intake:

- `src/gateway/protocol/schema/frames.ts:20-69` defines `ConnectParamsSchema`; it accepts `auth` and `device`, rejects unknown fields, and has no `trustedProxy` marker.
- `src/gateway/server/ws-connection/message-handler.ts:348-390` requires the first pre-auth request to be `connect` and schema-valid.
- `src/gateway/server/ws-connection/auth-context.ts:70-130` uses JSON `connectParams.auth` for first connect auth in the current token/password flow.
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts:218-281` includes auth token input and nonce in the device signature payload.
- `src/gateway/server/ws-connection/connect-policy.ts:37-76` and `src/gateway/server/ws-connection/connect-policy.ts:104-145` allow missing device identity only for already accepted trusted-proxy auth or other explicit exceptions.
- `src/gateway/server/ws-connection/message-handler.ts:650-712` performs device signature validation and can return `device signature invalid`.
- `src/gateway/auth.ts:352-368` shows existing trusted-proxy mode is configured at runtime auth mode level and is mutually exclusive with shared token config.
- `src/gateway/auth.ts:375-417` and `src/gateway/auth.ts:527-554` validate current config-level trusted-proxy source/header rules.
- `src/gateway/auth.ts:449-470` and `src/gateway/auth.ts:593-617` validate current token/password connect auth from JSON input.

Backend source findings:

- `backend/internal/services/instance_proxy_service.go:320-415` applies upstream WS auth at the backend-to-runtime boundary and starts challenge forwarding before client-to-runtime first connect.
- `backend/internal/services/instance_proxy_service.go:438-475` reads the first browser frame and sends a rewritten `connect` upstream.
- `backend/internal/services/instance_proxy_service.go:478-526` removes browser `params.auth`, injects server-owned JSON auth, and preserves other params including `params.device`.
- `backend/internal/services/instance_proxy_service.go:535-563` strips browser route query material and applies server-owned upstream auth at the WS header boundary.
- `backend/internal/services/instance_proxy_service_test.go:392-650` currently asserts JSON auth injection, challenge forwarding, and preserved device params.

Root cause carried into this packet: backend/server-side auth injection and browser-side device signing currently cross an inconsistent boundary. The browser signs `params.device` against browser-selected auth input and runtime nonce, then backend changes the JSON auth input and preserves the old signature. The runtime validates the rewritten auth plus preserved browser signature and returns `device signature invalid`.

## Proposed runtime contract

The patch gate should approve a hybrid mediated control-ui contract:

- Browser authenticates only to GTManager using route-scoped token or cookie.
- Backend authenticates to OpenClaw runtime with the server-owned OpenClaw gateway token at the backend-to-runtime WS boundary.
- Backend does not put OpenClaw gateway token material in the JSON `connect` payload.
- Backend removes browser `params.auth`, `params.device`, and `device.signature` from mediated first connect.
- Runtime accepts a mediated first connect only when a backend-owned non-secret `trustedProxy` marker is present and the WS boundary has already passed runtime auth.
- The `trustedProxy` marker is never accepted as a standalone proof.
- Direct control-ui clients keep existing token/password, device signature validation, pairing, and device-token retry behavior.

Final trustedProxy marker proposal:

- JSON field: `params.trustedProxy`
- JSON value shape: object with `version`, `mediator`, and `scope`.
- Required values: `version` is `1`, `mediator` is `gtmanager`, and `scope` is `control-ui`.
- It carries no token, password, key, cookie, bearer material, user credential, or access URL.
- Runtime validation rule: accept the marker only after successful backend-to-runtime WS-boundary auth and, if the patch uses source allowlisting, only when the connection source passes the trusted proxy source rule. If either proof is absent, fail closed.
- Failure behavior: reject schema-invalid marker shapes, marker-only requests, direct clients attempting to use marker without the required upstream proof, and any mediated request that still includes browser `params.auth`, `params.device`, or `device.signature`.

## Exact proposed runtime write set

Mandatory runtime files:

- `src/gateway/protocol/schema/frames.ts`
  - Add strict schema support for `params.trustedProxy`.
  - Keep `additionalProperties: false`.
  - Keep direct-client schema behavior strict.
- `src/gateway/auth.ts`
  - Add or expose WS-boundary auth validation usable by mediated Control UI without requiring OpenClaw gateway token material in JSON connect payload.
  - Preserve existing token/password direct auth and existing config-level trusted-proxy behavior.
- `src/gateway/server/ws-connection/auth-context.ts`
  - Separate mediated WS-boundary auth state from JSON `connectParams.auth`.
  - Record whether upstream auth has passed for this WS connection.
  - Ensure marker-only requests fail.
- `src/gateway/server/ws-connection/connect-policy.ts`
  - Permit missing device identity only for authenticated mediated trustedProxy connect.
  - Preserve direct client device, pairing, and device-token retry boundaries.
- `src/gateway/server/ws-connection/message-handler.ts`
  - Recognize mediated trustedProxy connect after schema validation.
  - Reject or strip browser `params.auth`, `params.device`, and `device.signature` according to the final contract.
  - Bypass browser device signature validation only for authenticated mediated trustedProxy connect.
  - Preserve direct `device signature invalid` behavior for direct invalid signatures.
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts`
  - Keep direct signature canonicalization unchanged.
  - Add tests or helper branching only if mediated mode needs an explicit no-device path.

Mandatory runtime tests:

- Runtime protocol/schema/auth tests under the OpenClaw repo test layout covering the six files above.

Conditional runtime/config files:

- Runtime config source is conditional only if the final patch requires a dedicated hybrid trusted-GTManager config knob or reuse of source allowlisting beside token auth.
- Startup artifact updates are conditional on that runtime config decision and are not authorized by this packet.

## Exact proposed backend write set

Mandatory backend files:

- `backend/internal/services/instance_proxy_service.go`
  - Stop injecting server-owned OpenClaw gateway token into JSON first-connect payload.
  - Remove browser `params.auth`.
  - Remove browser `params.device`, which also removes `device.signature`.
  - Add only the approved backend-owned non-secret `params.trustedProxy` marker.
  - Continue applying server-owned upstream auth only at the backend-to-runtime boundary.
  - Continue stripping browser route query material from the upstream URL.
  - Continue forwarding `connect.challenge` before rewritten first connect.
- `backend/internal/services/instance_proxy_service_test.go`
  - Replace current expectations that preserve `params.device` and inject JSON auth.
  - Assert mediated JSON has no `params.auth`, no `params.device`, and no `device.signature`.
  - Assert mediated JSON includes the approved `trustedProxy` marker.
  - Assert OpenClaw gateway token material is absent from rewritten JSON.
  - Assert upstream auth remains applied at backend-to-runtime boundary.
  - Keep challenge forwarding, malformed first frame, and query stripping coverage.

Conditional backend file:

- `backend/internal/handlers/instance_handler.go`, only if patch review decides a clearer trusted-proxy mode flag must be passed from handler to proxy service. No handler write is otherwise needed.

## Startup artifact and control-ui bundle impact

Startup artifact:

- Not required for the approval packet itself.
- Conditional for the implementation gate.
- Required only if the runtime contract needs a new hybrid trusted-GTManager config knob or source allowlist materialization while still using token auth.
- Existing startup evidence already materializes `gateway.controlUi.allowedOrigins`; this approval packet does not authorize startup artifact edits.

Control-ui bundle:

- Not required for the preferred runtime/backend contract.
- Conditional only if product behavior should hide manual credential fields on GTManager routes or if runtime maintainers require a browser-visible protocol field change.
- The preferred implementation keeps bundle behavior unchanged and lets backend rewrite the first connect after `connect.challenge`.

## Test plan

Runtime protocol tests:

- `ConnectParamsSchema` accepts the exact `trustedProxy` marker and still rejects unknown fields.
- Marker-only requests fail.
- Mediated trustedProxy connect succeeds with backend-to-runtime auth and without browser `params.auth`, `params.device`, or `device.signature`.
- Mediated trustedProxy connect fails if browser auth/device fields are present and the final contract requires rejection.
- Direct token/password clients remain unchanged.
- Direct invalid signatures still fail with `device signature invalid`.
- Direct nonce mismatch, stale signature, pairing flow, and device-token retry remain unchanged.
- Existing config-level trusted-proxy mode remains covered.
- Origin/controlUi allowedOrigins enforcement remains covered.

Backend unit tests:

- Rewritten mediated first connect removes browser `params.auth`.
- Rewritten mediated first connect removes browser `params.device` and `device.signature`.
- Rewritten mediated first connect contains only the approved non-secret `trustedProxy` marker for mediation.
- Rewritten mediated first connect contains no OpenClaw gateway token material.
- Upstream auth remains at backend-to-runtime boundary.
- Browser route query token and password material are stripped from upstream URL.
- `connect.challenge` is forwarded before the first rewritten connect.
- Non-text first frame, malformed JSON, missing connect method, missing upstream auth, and extra JSON trailing data fail closed.

Integration tests:

- GTManager bridge plus patched runtime accepts mediated trustedProxy first connect.
- Direct client cannot spoof `trustedProxy` without the required upstream proof.
- Stale instance route, origin allowlist, and disconnected 1006 regressions remain excluded.

Runtime image/fresh instance/browser gates:

- Runtime image build/tag/push gate is required only after a separate approved patch gate.
- Fresh instance gate is required only after a runtime image exists.
- Browser/manual E2E gate must verify the correct instance route reaches usable control-ui without manual credential form and without `device signature invalid`.

## Approval options

APPROVE_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_GATE: approve the mandatory runtime and backend write sets above, with conditional startup/config work only if implementation proves the hybrid source allowlist/config knob is required.

REJECT_OR_BLOCK: <reason>

This approval packet does not authorize build/tag/push, image pull, container run, fresh instance creation, browser E2E, passes:true, Close, deployment changes, or source changes outside the later explicitly approved patch gate.

## Next gate recommendation

Recommended next gate: `CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH`.

Scope for next gate:

- Implement the runtime hybrid trustedProxy contract in the proposed runtime write set.
- Implement the backend first-connect rewrite alignment in the proposed backend write set.
- Add focused runtime and backend tests.
- Stop before runtime image build/tag/push, fresh instance, browser E2E, passes:true, or Close unless separate gates explicitly authorize them.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no implementation, no patch to backend/frontend/runtime artifacts/deployments/docs/longterm/AgentTeam/UnifiedFramework, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no image build/tag/push/pull, no container run, no registry cleanup, no old session cleanup, no old asset cleanup, no old tag cleanup, no Mem0 write, no passes:true, no Close, no git stage/commit/push, and no token/password/key/cookie/bearer/auth header/access URL plaintext recording.
