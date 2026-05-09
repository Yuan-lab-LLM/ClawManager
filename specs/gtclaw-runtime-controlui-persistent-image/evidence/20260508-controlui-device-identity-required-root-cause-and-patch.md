# Control UI device identity required root-cause and patch

Date/timezone: 2026-05-08, Asia/Shanghai

Task type: CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_GATE

Verdict:

CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_DONE

## Scope

- instance: 17 / oc2gi-tp-134542
- pod: clawmanager-user-1/clawreef-17-oc2gi-tp-134542
- route shape: /api/v1/instances/17/control-ui/chat?session=main
- runtime image: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130

## Root Cause

Observed user-visible error maps to runtime code CONTROL_UI_DEVICE_IDENTITY_REQUIRED, not DEVICE_IDENTITY_REQUIRED.

Confirmed backend WebSocket code path sets X-Forwarded-Prefix to the route prefix. A backend services regression test now records the upstream WebSocket header and verifies the shape `/api/v1/instances/<id>/control-ui`.

Runtime readback confirmed:

| Field | Value |
| --- | --- |
| isControlUi | true |
| role | operator |
| authOk | not sufficient for this mediated path |
| authMethod | shared-secret |
| forwardedPrefix | /api/v1/instances/<id>/control-ui |
| sharedAuthOk | required mediated shared-auth proof |

The failing condition was the runtime mediated Control UI helper requiring authOk as the shared-auth proof. Runtime already computes and passes sharedAuthOk for the same missing-device decision, but the helper did not consume it. A controlled non-browser runtime probe with the backend prefix shape still returned CONTROL_UI_DEVICE_IDENTITY_REQUIRED before the patch, proving the route prefix shape alone was not enough and isolating the failed decision input to the shared-auth proof path.

## Patch

- Runtime source patch helper now accepts mediated shared-auth proof from `sharedAuthOk === true`, while preserving compatible `authOk` behavior.
- The helper still requires `isControlUi === true`, `role === "operator"`, `authMethod` to be shared-secret, and `forwardedPrefix` to match `/api/v1/instances/<id>/control-ui`.
- Runtime verifier now fails old helper output and verifies the patched helper, including direct-spoof rejection cases.
- Backend test coverage now asserts WebSocket upstream X-Forwarded-Prefix.

Security boundaries retained:

- no insecure auth
- no global bypass
- no direct browser device-less allow
- invalid device-signature protection remains required by verifier
- no broad origin workaround
- trustedProxy JSON marker is not trusted input

## Verification

| Check | Result |
| --- | --- |
| RED verifier against old readback helper | exit=1, missing sharedAuthOk contract string |
| source runtime verifier against readback plus patch script | exit=0 |
| assembly runtime verifier against readback plus patch script | exit=0 |
| focused backend services test | exit=0 |
| backend services full package test | exit=0 |

Commands run:

```text
cd backend && go test ./internal/services -run 'Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth' -count=1
cd backend && go test ./internal/services -count=1
node specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs <runtime-readback-fd> specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
node specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs <runtime-readback-fd> specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs
```

## Non-actions

- no build/tag/push
- no pull image
- no backend deploy
- no runtime deploy
- no browser E2E
- no DevTools
- no Playwright
- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no instance mutation
- no database mutation
- no storage cleanup
- no cache cleanup
- no frontend changes
- no deployments changes
- no docs changes
- no longterm changes
- no AgentTeam changes
- no UnifiedFramework changes
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
