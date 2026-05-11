# OpenClaw Remote Image Chat Message Disappears Root Cause And Patch

Date: 2026-05-11
Branch: dev

## User Problem

After setting the OpenClaw runtime image card to a remote pullable image and creating a runtime instance, sending a short chat message such as `你好` briefly appears in the Control UI and then disappears from the visible chat surface.

The user also asked whether instance 26 failed because the image path was replaced and whether instance 27 is using an old image.

## Instance State Finding

- Instance 26 is a superseded GTClaw logo replacement instance.
- Instance 26 remained Pending because the Kubernetes scheduler reported insufficient memory.
- Instance 26 did not fail because instance 27 used an old image.
- Replacing the runtime image card affects later instance creation; it does not mutate existing instance image references.
- Instance 27 used the configured remote OpenClaw image and reached Running/Ready.
- A later replacement attempt also reached Pending when capacity was unavailable.

## Chat Message Finding

The message was not dropped before reaching the backend or runtime:

- Runtime session files contained the user message text.
- Runtime session files contained later assistant/error records for the same session.
- ClawManager AI Gateway logs showed requests from the instance Pod IP to the configured LLM gateway endpoint.
- Gateway requests reached the upstream provider path, with some long-running successful responses and at least one canceled provider request.
- Runtime logs showed webchat reconnects and LLM idle timeout behavior.

Current root-cause class:

- Not a total ClawManager proxy/create failure.
- Not a simple "message never reached backend" failure.
- Most likely an interaction between the remote official OpenClaw runtime UI/session reload behavior and slow or canceled LLM provider responses.
- ClawManager contributed a risk because the AI Gateway provider HTTP timeout was hard-coded to 90 seconds, while the runtime idle timeout path observed in logs is 120 seconds.

## Patch

Changed:

- `backend/internal/aigateway/service.go`
- `backend/internal/aigateway/service_test.go`

Patch behavior:

- Replaced the hard-coded 90 second AI Gateway provider HTTP timeout with a configurable timeout.
- Added `CLAWMANAGER_AI_GATEWAY_HTTP_TIMEOUT_SECONDS`.
- Default timeout is now 180 seconds, intentionally longer than the observed 120 second OpenClaw runtime idle timeout.
- Invalid, empty, zero, or negative values fall back to the default.
- Values above 600 seconds are clamped to 600 seconds.

This is a Manager-side hardening patch. It does not claim that the remote OpenClaw image UI behavior is fully fixed without another deployed runtime/manual E2E check.

## Product Requirements For Remote Image Distribution

Before a remote OpenClaw image is considered product-ready for distribution:

- The image reference should be pinned or recorded by digest after pull verification.
- The created instance must reach Running/Ready.
- The runtime Control UI must load through ClawManager.
- The WebSocket connection must reach connected state.
- A short chat message must remain visible through history reload/reconnect.
- The runtime session file must persist the user message and assistant/error result.
- ClawManager AI Gateway must show a corresponding sanitized request record.
- Provider timeout, runtime timeout, and UI error handling must not fight each other.

## Next Debug Steps

1. Deploy the Manager-side timeout hardening only after approval.
2. Recreate or reuse a remote-image instance with enough cluster capacity.
3. Send one short message and record sanitized evidence:
   - instance id
   - image reference shape
   - pod ready status
   - session id shape
   - message persisted: true/false
   - assistant response persisted: true/false
   - prompt error code or timeout class
   - webchat reconnect count shape
4. If the message is persisted but hidden, inspect and patch the runtime Control UI history/reconnect rendering behavior in the image packaging path.
5. If the gateway still times out first, inspect configured provider latency and model route selection.

## Guardrails

- No token, password, cookie, bearer, authorization header, or access URL values are recorded here.
- No Kubernetes mutation was performed by this patch.
- No runtime image was built or deployed by this patch.
- No instance was created, stopped, deleted, or cleaned up by this patch.
- No git stage, commit, or push was performed.
