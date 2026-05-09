# Control UI device identity required root-cause and patch approval packet

Date: 2026-05-08

Role: Worker

Topology: serial

Task type: CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET

## Verdict

CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE

This packet is approval-only. It does not implement, patch, build, deploy, run browser E2E, use DevTools, use Playwright, or mutate Kubernetes, instances, database rows, storage, cache, cookies, images, source, artifacts, git index, remotes, Mem0, longterm, Close, or passes:true.

## Approval request

Proposed next gate:

```text
CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_GATE
```

Approval token requested:

```text
APPROVE_CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_GATE
```

## Current blocker

Target instance:

```text
17 / oc2gi-tp-134542
```

Target pod:

```text
clawmanager-user-1/clawreef-17-oc2gi-tp-134542
```

Observed path-only route:

```text
/api/v1/instances/17/control-ui/chat?session=main
```

User-provided manual browser observation:

- GTClaw 控制台 is visible.
- zh-CN copy is visible.
- The previous `device signature invalid` error no longer appears.
- The current visible blocker is `device identity required (use HTTPS/localhost or allow insecure auth explicitly)`.

Interpretation:

- This is no longer the old device-signature mismatch blocker.
- This is not a localization acceptance gate yet.
- The immediate target is to move instance 17 from the connection form/error state to a real connected/ready state.
- The next gate must prove which trusted-proxy allow condition is not being satisfied before any patch.
- The next gate must not blindly enable insecure auth.

## Dependency facts

| Dependency gate or observation | Status used by this packet |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_DONE | Satisfied. Exactly one fresh 2Gi instance was created: `17 / oc2gi-tp-134542`. Pod Running/Ready, desktop container ready, restart_count=0, oom_killed=false, Service ports `3001` and `18789`, EndpointSlice ready/serving, `18789` HTTP 200, trusted-proxy readback, startup wrapper/config proof, and zh-CN hash proof passed. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Satisfied. The trusted-proxy runtime image was configured and delivered for the fresh instance path. |
| CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED / backend patch delivered | Accepted as documented. Backend delivery was complete and healthy; the older block reason was tied to instance 16 runtime readiness/config, not the fresh instance 17 browser observation. |
| User manual browser observation | Instance 17 reaches GTClaw 控制台 with zh-CN text; old `device signature invalid` is gone; current blocker is `device identity required`. |

## Existing contract surface

Backend evidence/source-read facts:

- Backend has control-ui routes under `/api/v1/instances/:id/control-ui`.
- Backend sets `X-Forwarded-Prefix` for HTTP proxy requests.
- Backend sets `X-Forwarded-Prefix` for WebSocket upstream requests.
- Backend control-ui proxy should preserve route mediation through `/api/v1/instances/<id>/control-ui`.

Runtime patch evidence/source-read facts:

- The runtime helper is `isGtManagerMediatedControlUiAuth`.
- The allow condition requires `isControlUi === true`.
- The allow condition requires `role === "operator"`.
- The allow condition requires `authOk === true`.
- The allow condition requires `authMethod` to be shared-auth based: token or password.
- The allow condition requires `forwardedPrefix` to match `/api/v1/instances/<id>/control-ui`.
- The runtime verifier keeps direct-client protections such as `device signature invalid`, `verifyDeviceSignature`, `resolveConnectAuthDecision`, `bootstrapTokenCandidate`, `verifyDeviceToken`, and `shouldSkipControlUiPairing`.

Current hypothesis boundary:

- The observed `device identity required` means the runtime missing-device decision likely did not take the `isGtManagerMediatedControlUiAuth` allow path.
- The next gate must identify whether the miss is caused by `isControlUi`, `role`, `authOk`, `authMethod`, `forwardedPrefix`, or a code path returning `CONTROL_UI_DEVICE_IDENTITY_REQUIRED` / `DEVICE_IDENTITY_REQUIRED` before those parameters are evaluated.
- No final fix may be selected until the missing condition is proven.

## Required next-gate diagnosis

The approved root-cause gate must collect sanitized evidence for these questions:

1. Exact auth code:
   - Confirm whether the browser/runtime error code is `CONTROL_UI_DEVICE_IDENTITY_REQUIRED` or `DEVICE_IDENTITY_REQUIRED`.
   - Record only sanitized code/status/path evidence. Do not record token/password/key/cookie/bearer/authorization/access URL plaintext.

2. Backend-to-runtime forwarded prefix:
   - Confirm the actual upstream WebSocket request sent by backend includes `X-Forwarded-Prefix`.
   - Confirm whether the runtime receives that value as `forwardedPrefix`.
   - Confirm it matches `/api/v1/instances/17/control-ui` or explain the exact sanitized mismatch.

3. Missing-device decision parameters:
   - Confirm the runtime decision sees `isControlUi`.
   - Confirm the runtime decision sees `role`.
   - Confirm the runtime decision sees `authOk`.
   - Confirm the runtime decision sees `authMethod`.
   - Confirm the runtime decision sees `forwardedPrefix`.
   - Record only booleans, enum names, path shapes, and counts. Do not record credential values.

4. Control UI first-connect/auth payload boundary:
   - Confirm whether backend first-connect rewriting delivers shared auth in the runtime-expected shape.
   - Confirm whether runtime assigns `authMethod` for that shared auth.
   - Confirm whether direct browser/device-less connections without backend route mediation remain rejected.

5. Patch selection:
   - Patch only after the failing condition is proven.
   - Prefer backend header or first-connect parameter propagation if actual upstream data is missing.
   - Otherwise patch runtime allow-condition plumbing only for ClawManager backend-mediated Control UI connections.
   - Do not allow direct browser/device-less connections.
   - Do not enable insecure auth as the final fix.

## Patch boundary for the next gate

Allowed after approval, if root cause is proven:

- Minimal backend patch to `backend/internal/services/instance_proxy_service.go` and focused tests if `X-Forwarded-Prefix`, first-connect auth shape, or WebSocket mediation data is not actually delivered to runtime.
- Minimal runtime patch to the trusted-proxy patch/verifier artifacts if runtime receives safe backend-mediated evidence but fails to pass it into the missing-device decision.
- Focused tests/proofs that direct client paths still fail without backend route mediation.
- Evidence packet for the patch decision and resulting build/deploy plan.

Not allowed in the next gate:

- no insecure auth as a final fix.
- no global bypass.
- no direct browser/device-less allow.
- no weakening of invalid device-signature protections.
- no broad accepted-origin workaround as the root fix.
- no browser E2E acceptance run.
- no image build/tag/push/pull unless a later image-delivery gate approves it.
- no deployment of a new backend/runtime image unless a later delivery gate approves it.
- no GTClaw/OpenClaw localization acceptance until instance 17 reaches connected/ready state.

## Root-cause decision matrix

| Proven failed condition | Preferred minimal action |
| --- | --- |
| `X-Forwarded-Prefix` missing on backend WebSocket upstream | Patch backend WebSocket header propagation and add/extend backend proxy tests. |
| `X-Forwarded-Prefix` present upstream but not visible as runtime `forwardedPrefix` | Patch runtime header plumbing into the missing-device decision and update runtime verifier. |
| `forwardedPrefix` present but path does not match `/api/v1/instances/<id>/control-ui` | Patch the incorrect route-prefix source or normalization path; keep exact backend-mediated route requirement. |
| `authOk` false or shared auth absent | Patch backend first-connect/shared-auth delivery or runtime shared-auth detection, whichever evidence proves is missing. |
| `authMethod` absent despite shared auth success | Patch runtime method propagation so the allow path can distinguish token/password shared auth without storing or exposing values. |
| `role` not `operator` | Patch only the trusted backend-mediated role assignment path if ClawManager is expected to open operator Control UI. |
| `isControlUi` false | Patch only the Control UI client/proxy classification path, not general webchat or CLI paths. |
| Error returns before trusted-proxy allow decision | Patch decision ordering only if the earlier return is proven and the new ordering remains restricted to backend-mediated Control UI. |

## Delivery plan after root-cause patch

The root-cause/patch gate may produce source/artifact changes and tests only after approval. It must not build, tag, push, pull, or deploy images as part of this approval packet.

Required downstream gates after a successful patch:

1. Backend/runtime image delivery approval packet, if a new image is needed.
2. Image build/delivery gate, if approved.
3. Fresh/current runtime readiness/readback gate, if runtime image changes.
4. Browser/manual E2E gate proving instance 17 or a newly approved fresh instance reaches connected/ready state without `device identity required` and without `device signature invalid`.
5. Only after connected/ready evidence exists: GTClaw/OpenClaw zh-CN localization acceptance inside the actual connected UI.

## Forbidden actions confirmation

Confirmed for this approval packet:

- no implementation.
- no backend/runtime/control-ui/source/artifact/image modification.
- no build/tag/push/pull image.
- no browser E2E.
- no DevTools.
- no Playwright.
- no kubectl/k3d/Helm mutation.
- no instance/database mutation.
- no storage/cache/cookie cleanup.
- no insecure auth enablement.
- no global auth bypass.
- no Mem0 write.
- no passes:true.
- no Close.
- no longterm write-back.
- no git stage/commit/push.
- no token/password/key/cookie/bearer/authorization/access URL plaintext recorded.

## Verification commands

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md
```

```bash
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md
```

```bash
rg -n "CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE|device identity required|CONTROL_UI_DEVICE_IDENTITY_REQUIRED|DEVICE_IDENTITY_REQUIRED|X-Forwarded-Prefix|forwardedPrefix|isControlUi|authOk|authMethod|no insecure auth|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md
```

```bash
rg -n "(token|password|cookie|bearer|authorization|access URL)" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md || true
```

```bash
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md` | 0 | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md` | 1 | Expected exit for new-file no-index diff; no whitespace errors reported. |
| Required marker `rg` scan | 0 | Found verdict, blocker text, auth-code names, `X-Forwarded-Prefix`, `forwardedPrefix`, `isControlUi`, `authOk`, `authMethod`, and forbidden-action markers. |
| `rg -n "(token|password|cookie|bearer|authorization|access URL)" ... || true` | 0 | Hits were expected policy/marker references only: approval-token wording, token/password authMethod names, cookie cleanup prohibition, and plaintext-recording prohibition. No credential values, cookies, bearer values, authorization header values, or access URLs were recorded. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-identity-required-root-cause-and-patch-approval-packet.md` | 0 | File is untracked, as expected for this newly written approval packet. |
