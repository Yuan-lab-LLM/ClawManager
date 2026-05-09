# Control UI trusted-proxy auth contract browser/manual E2E approval packet

Date: 2026-05-08

Role: Worker

Topology: serial

Task type: CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_APPROVAL_PACKET

## Verdict

CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE

This packet is approval-only. It did not run browser E2E, DevTools, Playwright, or manual UI navigation.

## Dependency gates

| Gate | Status used by this packet |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_EXPANDED_CAPACITY_RECOVERY_AND_FRESH_INSTANCE_DONE | Satisfied. The prior gate created exactly one fresh standard 2Gi runtime instance: `17 / oc2gi-tp-134542`, pod `clawmanager-user-1/clawreef-17-oc2gi-tp-134542`, with runtime readiness/readback complete. |
| CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED | Accepted as a documented dependency state. Backend delivery was complete and healthy; the block reason was the old instance 16 runtime lacking trusted-proxy/device-less mediated Control UI auth. The fresh instance 17 runtime gate supersedes that old-runtime blocker for this browser/manual E2E approval. |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Satisfied. The target trusted-proxy runtime image was configured and published for runtime use. |

## Proposed next gate

proposed_next_gate=CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_GATE

requested_approval_token=APPROVE_CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_GATE

## Target instance and route

target_instance_id=17

target_instance_name=oc2gi-tp-134542

target_pod=clawmanager-user-1/clawreef-17-oc2gi-tp-134542

target_runtime_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130

target_image_index_digest=sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010

target_image_linux_arm64_digest=sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb

Primary path-only route:

```text
/api/v1/instances/17/control-ui
```

Allowed child path-only route:

```text
/api/v1/instances/17/control-ui/chat?session=main
```

The next gate must record only path-only routes or sanitized route labels. It must not record token/password/key/cookie/bearer/auth header/access URL plaintext.

## Fresh runtime evidence available before browser/manual E2E

The prior fresh-instance gate recorded:

- exactly_one_new_instance=true for instance `17 / oc2gi-tp-134542`.
- no second instance.
- pod `clawmanager-user-1/clawreef-17-oc2gi-tp-134542` reached Pod Running and Ready=True.
- desktop container was ready.
- restart_count=0.
- oom_killed=false.
- Service `clawreef-17-oc2gi-tp-134542-svc` exposed ports `3001` and `18789`.
- EndpointSlice ready/serving evidence existed for `3001` and `18789`.
- Runtime `18789` returned HTTP 200 from loopback, PodIP, and ServiceIP checks in the prior gate.
- running-container trusted-proxy proof directory and verifier existed.
- verifier exited `0` against the packaged server bundle.
- server bundle contained `isGtManagerMediatedControlUiAuth` and `x-forwarded-prefix`.
- direct-client protections remained present, including `device signature invalid`, `verifyDeviceSignature`, `resolveConnectAuthDecision`, `bootstrapTokenCandidate`, `verifyDeviceToken`, and `shouldSkipControlUiPairing`.
- startup config used `openclaw-gateway-with-gtmanager-auth-contract`.
- zh-CN Control UI hash proof matched the expected localized bundle hashes.

## Browser/manual E2E scope for next gate

The next gate may perform browser/manual validation only after the requested approval token is granted.

Allowed browser/manual scope after approval:

- Open the ClawManager UI through the normal user-visible route.
- Navigate to the target instance `17 / oc2gi-tp-134542`.
- Open the GTClaw Control UI for instance 17 using the normal ClawManager route.
- Record sanitized, path-only route evidence for `/api/v1/instances/17/control-ui` or `/api/v1/instances/17/control-ui/chat?session=main`.
- Record visible UI observations needed to decide whether trusted-proxy auth works.
- Record sanitized screenshots only if no token/password/key/cookie/bearer/auth header/access URL plaintext is visible.

The next gate must not create another instance, change Kubernetes resources, mutate database rows, build or pull images, clean storage/cache/cookies, or modify source/artifacts.

## Expected positive observations

The browser/manual E2E gate should pass only if all applicable observations are true:

- route under instance 17.
- path-only route starts with `/api/v1/instances/17/control-ui`.
- not stale 16.
- not stale 15.
- not stale 11.
- GTClaw 控制台 visible.
- zh-CN visible.
- no device signature invalid.
- no manual connection fallback.
- no manual token/password entry prompt as the accepted success state.
- connected/ready chat or equivalent authenticated-ready state if visible.
- the UI does not require direct-client device-pairing fallback to proceed.

## Failure observations for next gate

The browser/manual E2E gate should return blocked or failed evidence if any of these occur:

- The visible route points to instance 16, 15, 11, or any runtime other than instance 17.
- The UI still displays `device signature invalid`.
- The UI falls back to a manual connection fallback instead of the mediated route.
- GTClaw 控制台 is not visible.
- zh-CN localized UI is not visible.
- No connected/ready chat or equivalent authenticated-ready state is reachable when such a state is expected by the visible UI.
- The only available evidence would expose token/password/key/cookie/bearer/auth header/access URL plaintext.

## Why browser/manual E2E is now appropriate

Browser/manual E2E was intentionally deferred until a fresh runtime instance proved the trusted-proxy image and startup configuration inside a running container. That prerequisite is now met by instance `17 / oc2gi-tp-134542`, so the next gate can safely test the user-visible Control UI auth behavior without conflating browser state with runtime image readiness.

## Forbidden actions confirmation for this approval packet

Confirmed:

- no browser E2E.
- no DevTools.
- no Playwright.
- no K8S mutation.
- no instance/database mutation.
- no image build/tag/push/pull.
- no source/artifact modification.
- no storage/cache/cookie cleanup.
- no Mem0 write.
- no passes:true.
- no Close.
- no longterm write-back.
- no git stage/commit/push.
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded.

## Verification commands

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md
```

```bash
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md
```

```bash
rg -n "CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE|APPROVE_CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_GATE|CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_GATE|instance 17|oc2gi-tp-134542|/api/v1/instances/17/control-ui|/api/v1/instances/17/control-ui/chat\\?session=main|GTClaw 控制台|zh-CN|device signature invalid|manual connection fallback|authenticated-ready|not stale 16|not stale 15|not stale 11|no browser E2E|no DevTools|no Playwright|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md
```

```bash
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md
```

```bash
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md` | 0 | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md` | 1 | Expected exit for a new-file no-index diff; no whitespace errors reported. |
| Required marker `rg` scan | 0 | Found verdict, approval token, proposed next gate, instance 17 target, route paths, expected observations, and forbidden-action markers. |
| Sensitive plaintext scan | 1 | No token/password/key/cookie/bearer/auth header/access URL plaintext matches found. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-trusted-proxy-auth-contract-browser-manual-e2e-approval-packet.md` | 0 | File is untracked, as expected for this newly written approval packet. |
