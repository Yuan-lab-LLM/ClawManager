# OpenClaw runtime source intake

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Source Intake

Gate: CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE

## Verdict

CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE_DONE

Runtime gateway first-connect validator source was found for the relevant OpenClaw runtime version.

GPT Pro decision = REQUIRE_RUNTIME_CONTRACT_CHANGE

This gate performed source discovery only. It made no implementation patch.

## Source provenance used

- Local candidate path checked: `/Users/eduardogan/Desktop/GHJProject/opensparrow`
- Local candidate git root: `/Users/eduardogan/Desktop/GHJProject/opensparrow`
- Local candidate git commit: `828e2b75a2d7e9248f2fe193954f0859b64b0498`
- Local candidate remote: `ssh://git@ssh.github.com:443/gegej1/opensparrow.git`
- Runtime package evidence from prior ClawManager evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md:80` records `openclaw@2026.4.14`.
- Local pnpm package index used: `/Users/eduardogan/Library/pnpm/store/v10/index/83/eb8a9099da49a481acf3bfd55f12a7d09b6b924c8dc8292816009fb965f39a-openclaw@2026.4.14.json`
- Package repository from package metadata: `git+https://github.com/openclaw/openclaw.git`
- Remote tag lookup: `refs/tags/v2026.4.14` tag object `cf4eecdd3b9dc7940a70814d7521b317506a9482`; peeled commit `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`.
- Source intake path used for exact TS files: `https://raw.githubusercontent.com/openclaw/openclaw/v2026.4.14/...`
- Local raw-source cache used for inspection only: `/tmp/openclaw-*-v2026.4.14.ts`
- Local package bundle used to cross-check source-region mappings: `openclaw@2026.4.14` pnpm store files under `/Users/eduardogan/Library/pnpm/store/v10/files/`.

Notes:

- `git ls-remote` worked and identified the relevant tags.
- Full HTTPS clone and full codeload tarball attempts did not complete due network transport timeouts.
- Individual raw source files for the target validator did download successfully, so source intake is not blocked.
- No parent image pull or image extraction was used.

## Connect Source Locations

| Target | Runtime source | Key lines | What it proves |
|---|---|---:|---|
| connect request schema | `src/gateway/protocol/schema/frames.ts` | 20-69 | `ConnectParamsSchema` accepts `device` and `auth`, uses `additionalProperties: false`, and has no `trustedProxy` field. |
| WS challenge / nonce | `src/gateway/server/ws-connection.ts` | 244-248 | Runtime sends `connect.challenge` with a generated nonce before the first `connect` request is validated. |
| first connect validator entry | `src/gateway/server/ws-connection/message-handler.ts` | 348-390 | First pre-auth frame must be request method `connect` and must validate against `ConnectParamsSchema`. |
| origin/controlUi usage | `src/gateway/server/ws-connection/message-handler.ts` | 461-499 | Browser-origin WS requests are checked against `gateway.controlUi.allowedOrigins` or host-header fallback. |
| connect auth state | `src/gateway/server/ws-connection/auth-context.ts` | 70-130 | First connect auth calls `authorizeWsControlUiGatewayConnect` using `connectParams.auth`; shared-auth probe calls `authorizeHttpGatewayConnect` with the same JSON auth object. |
| device-token retry logic | `src/gateway/server/ws-connection/auth-context.ts` | 150-228 | Bootstrap and device-token candidates are verified only when device identity exists. |
| device signature input | `src/gateway/server/ws-connection/handshake-auth-helpers.ts` | 218-281 | Device signature payload includes the auth token input and nonce, then verifies v3/v2 signatures. |
| device-token retry hint | `src/gateway/server/ws-connection/handshake-auth-helpers.ts` | 297-325 | Token mismatch plus device identity can produce `retry_with_device_token`. |
| missing device policy | `src/gateway/server/ws-connection/connect-policy.ts` | 37-76, 104-145 | Control UI can skip device identity only for explicit trusted-proxy auth, break-glass disable, local insecure exception, or role policy. |
| device signature validation | `src/gateway/server/ws-connection/message-handler.ts` | 650-712 | Runtime rejects invalid identity, stale timestamp, nonce mismatch, and `device signature invalid`. |
| pairing/device-token flow | `src/gateway/server/ws-connection/message-handler.ts` | 714-799, 842-1003, 1097-1226 | Auth decision, pairing skip checks, pairing request/approval, device-token issuance, and hello response are all inside first connect. |
| trusted proxy auth config | `src/gateway/auth.ts` | 352-368 | Existing trusted-proxy mode requires `trustedProxy` config and is mutually exclusive with shared token config. |
| trusted proxy validator | `src/gateway/auth.ts` | 375-417, 527-554 | Existing trusted-proxy validation checks trusted proxy source, rejects loopback source, validates configured identity header and optional allowlist/required headers. |
| token/password connect auth | `src/gateway/auth.ts` | 449-470, 593-617 | Token/password mode validates JSON `connectParams.auth`, not only the WS upgrade header. |
| controlUi runtime config | `openclaw@2026.4.14 dist/server.impl-BbJvXoPb.js` | 18818-18865 | Runtime startup reads `gateway.controlUi.allowedOrigins`, requires it for non-loopback Control UI, and requires `gateway.trustedProxies` when auth mode is trusted-proxy. |
| default origin materialization helper | `openclaw@2026.4.14 dist/gateway-control-ui-origins-B-lkqr3D.js` | 2-58 | Runtime package has a helper to seed default `gateway.controlUi.allowedOrigins` for non-loopback bind modes. |

## Runtime Contract Findings

Current runtime has an existing trusted-proxy mode, but it is not the GPT Pro accepted contract.

Existing trusted-proxy mode:

- is configured through `gateway.auth.mode = "trusted-proxy"`;
- requires `gateway.auth.trustedProxy.userHeader`;
- requires the remote source to be in `gateway.trustedProxies`;
- rejects loopback trusted-proxy sources;
- optionally validates required headers and allowUsers;
- lets Control UI operator sessions skip device identity and pairing only when runtime auth mode is trusted-proxy and the trusted-proxy auth result succeeded.

Mismatch with GPT Pro accepted shape:

- `ConnectParamsSchema` has no `trustedProxy` marker field and forbids unknown fields.
- In token mode, runtime validates token/password from `connectParams.auth`; the WS upgrade header alone is not sufficient for first connect.
- Existing trusted-proxy mode is mutually exclusive with shared token config, but GPT Pro requires backend-to-runtime use of server-owned OpenClaw gateway token.
- Current backend injects server-owned token into JSON `params.auth`; runtime then includes that token input in the device signature payload. If the browser signed with a different token input, the runtime reaches `device signature invalid`.
- Therefore a backend-only change that removes JSON `params.auth` and `params.device` would fail unless runtime changes first-connect auth semantics.

Adaptation verdict:

- Existing runtime code proves trusted-proxy concepts already exist, but only as a config-level proxy-auth mode.
- It does not support the accepted hybrid contract: server-owned upstream auth plus backend-owned non-secret trustedProxy marker plus no browser `params.auth`, no browser `params.device`, and no `device.signature` in the mediated JSON connect payload.
- REQUIRE_RUNTIME_CONTRACT_CHANGE remains correct.

## Candidate Runtime Write Set

candidate runtime write set:

- `src/gateway/protocol/schema/frames.ts`
  - Add the accepted mediated trustedProxy marker field, or an equivalent runtime-approved field name, to `ConnectParamsSchema`.
  - Keep direct-client schema boundaries strict.
- `src/gateway/auth.ts`
  - Add or expose validation for backend-to-runtime server-owned auth on the WS boundary without requiring that token to appear in JSON connect payload.
  - Keep existing direct token/password and existing trusted-proxy mode behavior.
- `src/gateway/server/ws-connection/auth-context.ts`
  - Separate JSON connect auth from upstream/WS-boundary auth for mediated Control UI.
  - Ensure marker alone never authenticates.
- `src/gateway/server/ws-connection/connect-policy.ts`
  - Allow missing device identity only when the mediated trustedProxy marker is present and backend-to-runtime auth has already succeeded.
- `src/gateway/server/ws-connection/message-handler.ts`
  - Bypass browser device signature validation only for authenticated mediated trustedProxy connects.
  - Reject or ignore browser `params.auth`, `params.device`, and `device.signature` in mediated mode according to the final patch contract.
  - Preserve direct-client validation and pairing behavior.
- Runtime protocol tests under the OpenClaw repo test layout for the files above.

Conditional runtime/startup files:

- `src/gateway/server-runtime-config.ts`, only if the accepted contract needs a config knob for GTManager as trusted proxy while still using shared upstream auth.
- `src/config/gateway-control-ui-origins.ts`, only if config materialization changes beside existing allowedOrigins behavior.

## Candidate Backend Write Set

candidate backend write set:

- `backend/internal/services/instance_proxy_service.go`
  - Stop injecting the OpenClaw gateway token into JSON first-connect payload.
  - Remove browser `params.auth`.
  - Remove browser `params.device`, which also removes `device.signature`.
  - Add only the backend-owned non-secret trustedProxy marker accepted by runtime.
  - Keep server-owned OpenClaw gateway token use at the backend-to-runtime boundary only.
- `backend/internal/services/instance_proxy_service_test.go`
  - Replace current expectations that preserve `params.device` and inject JSON auth.
  - Add assertions that mediated JSON has no browser auth/device fields, contains the approved marker, and does not contain OpenClaw gateway token material.
  - Keep challenge-forwarding coverage.

Current backend locations confirming the old behavior:

- `backend/internal/services/instance_proxy_service.go:320-415`
- `backend/internal/services/instance_proxy_service.go:438-563`
- `backend/internal/services/instance_proxy_service_test.go:392-650`

## Startup And Control UI Artifact Impact

Startup artifact:

- Conditional, not automatically mandatory.
- Existing startup config runs `openclaw gateway run --bind lan --auth token`, and current artifact already materializes `gateway.controlUi.allowedOrigins`.
- If patch uses the existing runtime `gateway.auth.mode = "trusted-proxy"` path, startup must configure `gateway.trustedProxies` and `gateway.auth.trustedProxy`, but that would diverge from GPT Pro's server-owned gateway-token boundary.
- If patch implements the accepted hybrid trustedProxy marker contract, startup changes are needed only if runtime requires a new config knob for trusted GTManager mediation.

Control UI bundle:

- Conditional, likely not mandatory for the preferred boundary.
- Backend can rewrite the first connect after `connect.challenge`; Control UI can continue to sign its own direct-client payload, while mediated mode strips browser auth/device before runtime validation.
- Bundle changes are needed only if product behavior should hide manual credential fields for GTManager routes or if runtime requires a browser-visible contract change.

## Data Flow Table

| Step | Current behavior | Contract issue | Required direction |
|---|---|---|---|
| Browser route | Browser reaches GTManager `/api/v1/instances/<id>/control-ui/...` | Route correctness is not enough for runtime first-connect auth | Keep route-scoped GTManager auth. |
| Initial control-ui state | Browser builds direct-style `connect` with auth/device material when available | Browser-controlled auth/device reaches backend bridge | Backend must sanitize mediated first connect. |
| WS challenge | Runtime emits `connect.challenge` with nonce | Backend forwards challenge unchanged | Keep challenge forwarding. |
| Backend rewrite | Backend currently strips browser `params.auth` but injects server token into JSON and preserves `params.device` | Device signature payload includes JSON token input, so injected token can invalidate browser signature | Do not put OpenClaw gateway token in JSON; remove browser device/signature in mediated mode. |
| Runtime schema | `ConnectParamsSchema` allows `auth` and `device`; no `trustedProxy` marker | Marker would be rejected today | Add accepted marker or equivalent. |
| Runtime auth | Token/password auth reads `connectParams.auth`; existing trusted-proxy mode reads proxy headers and trusted source | WS header auth alone is not enough in token mode; existing trusted-proxy mode conflicts with server-owned token requirement | Add hybrid mediated auth path bound to backend-to-runtime auth. |
| Runtime device validation | Device identity and signature are validated before final auth decision | Preserved browser device signature can fail as `device signature invalid` | Bypass device validation only for authenticated mediated trustedProxy mode. |
| Pairing/device-token | Pairing and device-token retry require device identity unless trusted-proxy auth already succeeded | Mediated connect without runtime contract would hit missing-device/auth failures | Preserve direct behavior, skip only authenticated mediated mode. |

## Test Plan

test plan:

- Runtime protocol tests:
  - `ConnectParamsSchema` accepts the final mediated trustedProxy marker and still rejects unknown direct-client fields.
  - Marker alone fails without authenticated backend-to-runtime boundary.
  - Mediated trustedProxy connect succeeds with no browser `params.auth`, no browser `params.device`, and no `device.signature`.
  - Mediated connect containing browser auth/device fields is rejected or ignored by the final documented rule.
  - Token/password direct clients remain unchanged.
  - Direct invalid device signature still fails with `device signature invalid`.
  - Direct nonce mismatch and stale signature still fail.
  - Device-token retry remains unchanged for direct clients.
  - Pairing-required flow remains unchanged for direct clients.
  - Existing config-level trusted-proxy mode remains covered.
  - `gateway.controlUi.allowedOrigins` remains enforced for browser-originated Control UI traffic.
- Backend unit tests:
  - First-connect rewrite removes browser `params.auth`.
  - First-connect rewrite removes browser `params.device` and therefore `device.signature`.
  - Rewritten JSON does not contain OpenClaw gateway token material.
  - Rewritten JSON includes only the accepted non-secret marker.
  - Upstream auth remains applied at backend-to-runtime boundary.
  - Query token and password material remain stripped from upstream URL.
  - `connect.challenge` is still forwarded before first connect.
  - Malformed JSON, non-text frames, missing connect method, and missing upstream auth fail closed.
- Integration tests:
  - GTManager bridge plus runtime gateway accepts mediated trustedProxy connect.
  - Direct client cannot spoof the marker.
  - Stale route, origin allowlist, and 1006 regressions stay excluded.
- Fresh instance/browser gates:
  - Runtime image build/tag/push only after a separate patch approval gate.
  - Fresh OpenClaw instance gate after runtime image is available.
  - Browser/manual E2E gate verifies correct route connects without manual credential form and without `device signature invalid`.

## Next Gate Recommendation

Recommended next gate:

`CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_APPROVAL_PACKET`

Purpose:

- Approve the exact runtime and backend write sets above.
- Decide final mediated marker field name and runtime validation rule.
- Decide whether startup config needs a hybrid trusted-GTManager knob.
- Keep implementation blocked until that patch approval gate is done.

Do not start backend-only implementation. Current runtime source proves backend-only patch is insufficient under REQUIRE_RUNTIME_CONTRACT_CHANGE.

## Forbidden Actions Statement

Forbidden actions were not executed. Specifically: no implementation, no patch, no backend/frontend/runtime artifact/deployment/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no image build/tag/push, no image pull, no image extraction, no container run, no registry cleanup, no old session cleanup, no old asset cleanup, no old tag cleanup, no Mem0 write, no passes:true, no Close, no git stage/commit/push, and no token/password/key/cookie/bearer/auth header/access URL plaintext recording.

Approved source-intake actions that were executed: read-only local source/package inspection, `git ls-remote`, failed read-only clone attempts, failed read-only source tarball intake, and successful read-only raw source file fetches for OpenClaw runtime/gateway source.
