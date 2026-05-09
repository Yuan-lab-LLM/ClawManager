# Control UI Device Identity Required Runtime Image Delivery Approval Packet

## Verdict

CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_APPROVAL_PACKET_DONE

This packet requests approval for the next runtime image delivery gate only. It does not execute image build, tag, push, pull, deploy, browser E2E, manual E2E, instance mutation, cleanup, Close, write-back, passes:true, git stage/commit/push, or Mem0 write.

## Requested Approval Token

APPROVE_CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_GATE

## Dependency State

- Root-cause and patch gate: CONTROLUI_DEVICE_IDENTITY_REQUIRED_ROOT_CAUSE_AND_PATCH_GATE_DONE.
- Commander read-only review: evidence DONE, marker and sensitive scan passed, runtime patch/verifier source and assembly consistent, temp patched readback verifier exit 0.
- Backend auth-contract patch: delivered and `/healthz` passed.
- Current gate: approval packet only; no direct entry into build, deploy, browser, or E2E work.

## Current Blocker

- Instance: `17`.
- Instance path shape: `/api/v1/instances/{id}/control-ui/chat?session={session}`.
- Runtime image shape: local k3d registry OpenClaw image with immutable timestamp tag.
- Current blocker: instance 17 is still running an old runtime image that does not include the new `sharedAuthOk` mediated helper patch.
- Root-cause status: runtime received the backend-mediated Control UI evidence shape, including `X-Forwarded-Prefix` / `x-forwarded-prefix`, but the missing-device decision path did not consume the already-validated shared-auth result through the helper used by the runtime auth contract.

## Auth Method Terminology

- `authMethod=shared-secret` in prior evidence is a conceptual description for the backend-mediated shared-auth path.
- Runtime internal literals are `token|password`; runtime patch and verifier checks must use the runtime literal contract as the source of truth.
- The next gate must not reinterpret `authMethod=shared-secret` as a new runtime input literal.
- The next gate must keep invalid device-signature protection, route-prefix checks, and direct browser device-less rejection intact.

## Proposed Next Gate

Gate name:

CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_GATE

Scope:

1. Build a `linux/arm64` runtime image from `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/`.
2. Tag and push to the k3d registry with a new immutable tag.
3. Produce image readback verifier proof, including status/code and hash/digest evidence.
4. Create exactly one fresh runtime instance with the new image.
5. Capture readiness/readback evidence only for that fresh runtime instance.

Explicit non-scope:

- no browser E2E.
- no manual E2E.
- no Control UI internal localization acceptance.
- no GTClaw/OpenClaw internal zh-CN acceptance claim after connection success.
- no Close.
- no write-back.
- no passes:true.
- no git stage/commit/push.
- no Mem0 write.

## Sanitized Evidence Rules

Record only:

- boolean results.
- enum values.
- path shape.
- status/code.
- hash/digest.

Do not record plaintext values for:

- token.
- password.
- key.
- cookie.
- bearer.
- authorization or auth header.
- access URL.

Evidence may record that a required field family was present as a boolean, or that a runtime literal enum matched `token|password`; it must not record the field value itself.

## Rollback And Non-Cleanup Rule

- Do not clean up instance 17.
- Do not delete or retag old runtime images.
- Do not delete old evidence or artifact directories.
- Do not mutate database, instance, storage, cache, or browser state for cleanup.
- Any cleanup requires a separate explicit user approval gate.

## Readiness/Readback Acceptance Boundary

The proposed next gate may prove only that the rebuilt image contains the patched runtime contract and that exactly one fresh runtime instance reaches readiness/readback with the new immutable image.

Successful connection readiness does not prove GTClaw/OpenClaw internal interface localization. Control UI internal localization acceptance remains reserved for a later browser/manual E2E gate.

## Non-Actions In This Gate

- no build/tag/push/pull image.
- no backend deploy.
- no runtime deploy.
- no kubectl/k3d/Helm mutation.
- no browser E2E, DevTools, or Playwright.
- no instance/database/storage/cache/cookie cleanup or mutation.
- no passes:true.
- no Close.
- no longterm write-back.
- no git stage/commit/push.
- no Mem0 write.
