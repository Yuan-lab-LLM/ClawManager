# Control UI device signature auth contract implementation

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, implementation
Gate: CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_GATE

## Verdict

CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE

Implementation status: backend-only complete; runtime trusted-proxy configuration or runtime image gate required before claiming end-to-end browser/runtime repair.

This gate implemented the approved backend bridge sanitizer and tests. It did not build, tag, push, pull, or run an image. It did not run browser E2E, DevTools, Playwright, kubectl, k3d, Helm, instance mutation, database mutation, browser storage/cache/cookie cleanup, Mem0 write, passes:true, Close, longterm write-back, or git stage/commit/push.

## Dependencies

- CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE
- GPT_PRO_DECISION_REQUIRE_RUNTIME_CONTRACT_CHANGE

## Root-cause source-intake summary

Source intake evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-openclaw-runtime-auth-contract-source-intake.md`

Official OpenClaw gateway source confirms:

- `device.signature` validation is tied to first-connect auth input and nonce;
- current ClawManager bridge behavior that rewrites `params.auth` while retaining browser-generated `params.device` can create signature input mismatch and produce `device signature invalid`;
- secure trusted-proxy acceptance is runtime-side, based on trusted proxy source and identity headers, not on a browser-spoofable JSON marker;
- a device-less Control UI operator first connect is accepted only under the runtime trusted-proxy auth contract.

## Contract chosen

Backend bridge sanitizer:

- Control UI first-connect payloads still require a valid `connect` method before forwarding.
- The backend still injects server-owned upstream auth for the current backend-to-runtime token contract.
- When `params.auth` is rewritten, stale browser `params.device` and stale `device.signature` are removed from the forwarded first-connect payload.
- Safe non-auth, non-device params such as protocol range, client metadata, role, scopes, caps, locale, and future extension fields remain preserved.
- Non first-connect messages are not rewritten into upstream traffic.
- Direct desktop WebSocket traffic and direct OpenClaw client behavior are outside this backend bridge rewrite and remain unchanged.
- No browser-supplied `trustedProxy` JSON marker was introduced or trusted by this backend-only patch.

This avoids the known bad mix of rewritten `params.auth` with stale `params.device` and stale `device.signature`.

## Exact files changed

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-openclaw-runtime-auth-contract-source-intake.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md`

## Backend implementation details

`rewriteControlUIWebSocketConnectPayload` now removes both `params.auth` and `params.device` from the browser first-connect params before writing the backend-owned upstream auth. This keeps preserved metadata intact while preventing stale device signature material from reaching the runtime after auth replacement.

The tests now cover:

- rewritten `params.auth` is not mixed with stale `params.device` or stale `device.signature`;
- first-connect sanitizer/rewriter preserves safe metadata and removes browser auth/device material;
- upstream connect.challenge frames can still reach the browser before first connect;
- malformed or non-connect first frames are not rewritten/forwarded as upstream connect traffic;
- desktop WebSocket first frames are not rewritten by the Control UI bridge path;
- sensitive route/upstream auth material is not included in sanitized error text.

## Test results

Red/green regression check:

```text
command=cd backend && go test ./internal/services -run 'TestProxyWebSocketWithControlUIScopeInjectsConnectAuthAndDropsStaleDevice|TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect' -count=1
red_exit=1
red_result=failed before production patch because rewritten params retained stale browser device identity
green_exit=0
green_result=ok clawreef/internal/services
```

Required tests:

```text
command=cd backend && go test ./internal/services -run 'Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth' -count=1
exit=0
result=ok clawreef/internal/services
```

```text
command=cd backend && go test ./internal/services -count=1
exit=0
result=ok clawreef/internal/services
```

Static verification:

```text
command=git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-openclaw-runtime-auth-contract-source-intake.md
exit=0
result=no whitespace errors
```

```text
command=rg -n "device signature invalid|params.auth|params.device|device.signature|trustedProxy|gateway token|no browser E2E|no kubectl|no image build|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md
exit=0
result=required implementation evidence markers present
```

```text
command=rg sensitive-shape scan against the two new evidence files | wc -l
exit=0
result=0
```

```text
command=rg sensitive-shape scan against approved backend files plus the two new evidence files
exit=0
result=21 matches; matched only existing Go identifier names, route-query fixture shapes, and non-sensitive test placeholders in backend tests; evidence-only scan remained 0 and no real credential value was recorded in evidence
```

```text
command=git status --short -- approved changed paths
exit=0
result=backend/internal/services/instance_proxy_service.go and backend/internal/services/instance_proxy_service_test.go modified; two new evidence files untracked
```

## Runtime follow-up required

This is not an end-to-end runtime/browser fix claim. Official source shows complete Control UI acceptance without device identity depends on runtime trusted-proxy contract configuration or image/runtime packaging. A later approved runtime gate must validate:

- actual runtime auth mode and trusted proxy headers for the deployed image;
- trusted proxy source boundary cannot be spoofed by direct clients;
- direct token/password/device-signature clients remain compatible;
- browser rerun no longer shows `device signature invalid` after backend and runtime pieces are both deployed.

## Secret hygiene

No real token, password, key, cookie, bearer value, auth header value, access URL, or token-bearing full URL was recorded in this evidence. Test fixtures use non-sensitive placeholder strings inside backend unit tests only. Evidence records command shapes, exit codes, source file names, and behavior summaries, not credential values.

## Forbidden actions confirmation

- no browser E2E
- no DevTools
- no Playwright
- no kubectl
- no k3d
- no Helm
- no image build, tag, push, pull, or container run
- no frontend edits
- no deployments edits
- no docs edits
- no longterm write-back
- no AgentTeam edits
- no UnifiedFramework edits
- no existing evidence edits
- no control-ui artifact edits
- no localization artifact edits
- no runtime image assembly artifact edits
- no image tag or registry content mutation
- no database row, instance record, session, or asset mutation
- no Kubernetes resource mutation
- no storage/cache/cookie cleanup
- no gateway token exposure to browser-visible JSON, URL, log, screenshot, or evidence
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
