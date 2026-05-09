# Control UI auth contract backend/runtime delivery approval packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, approval packet
Gate: CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_APPROVAL_PACKET

## Verdict

CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_APPROVAL_PACKET_DONE

This is an approval packet only. It does not implement code, build, tag, push, pull, or run an image. It does not run browser E2E, DevTools, Playwright, kubectl, k3d, Helm, instance mutation, database mutation, Mem0 write, passes:true, Close, longterm write-back, or git stage/commit/push.

No token, password, key, cookie, bearer value, auth header value, or access URL plaintext is recorded in this packet.

## Dependency gates

- CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE
- OPENCLAW_RUNTIME_AUTH_CONTRACT_SOURCE_INTAKE_DONE

Dependency evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-openclaw-runtime-auth-contract-source-intake.md`

## Current delivery status

The backend-only patch is complete but not deployed.

Current completed backend patch behavior:

- the ClawManager Control UI WebSocket bridge removes stale browser `params.auth`;
- it removes stale browser `params.device`, including stale `device.signature`;
- it injects backend-owned upstream auth for the current backend-to-runtime token contract;
- it preserves safe non-auth, non-device metadata;
- it leaves direct desktop WebSocket traffic and direct OpenClaw client behavior outside this bridge rewrite.

Current non-delivery facts from dependency evidence:

- no ClawManager backend image was built, tagged, pushed, pulled, or deployed in the backend-only implementation gate;
- no running backend pod/process was restarted or replaced in that gate;
- no browser rerun was performed after a backend delivery;
- no runtime trusted-proxy config/image packaging was proven active in the running runtime.

Therefore the current repository patch is not yet evidence that the running ClawManager backend contains the fix.

## Why browser may still show device signature invalid

A browser may still see `device signature invalid` for either or both of these reasons:

1. The running ClawManager backend may still be serving the pre-patch bridge. In that case it can still rewrite `params.auth` while forwarding stale browser `params.device` and stale `device.signature`, preserving the known invalid-signature mismatch.
2. Even after the backend patch is delivered, official OpenClaw source intake says device-less Control UI operator acceptance depends on a runtime trusted-proxy auth contract. If the deployed runtime image/config does not recognize the backend as a trusted proxy, the runtime may still reject the first connect rather than accepting a mediated device-less Control UI connection.

This means the backend patch is necessary but not sufficient for an end-to-end browser/runtime repair claim.

## Requested approval

Proposed next implementation gate:

`CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_GATE`

Requested approval token:

`APPROVE_CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_GATE`

The next gate must not proceed without that exact approval token.

## Exact next implementation scope

The next implementation gate should cover only the delivery and runtime-contract work needed to move the completed auth-contract patch into the running path.

Backend delivery scope:

- confirm the current backend source contains the completed bridge sanitizer patch;
- build, tag, push, and deploy or otherwise roll out a ClawManager backend image/process that contains the patch, only under the separate approval token above;
- restart or roll the backend runtime only as required to make the patched backend serve traffic;
- verify the backend health endpoint after rollout;
- avoid backend source changes unless rollout verification proves a narrow delivery-only fix is required and Commander approves that write scope.

Runtime trusted-proxy scope:

- inspect the actual OpenClaw runtime source/config/image packaging used by the target runtime;
- confirm whether the deployed runtime already supports the required trusted-proxy Control UI auth contract;
- if not present, patch only the required OpenClaw runtime auth contract/config/image packaging for trusted-proxy mediated Control UI acceptance;
- prove trusted-proxy acceptance requires the backend-to-runtime trusted boundary and is not a browser-spoofable JSON marker;
- preserve direct OpenClaw clients using the existing token, password, pairing, and device-signature flows.

Out of scope for the next implementation gate unless separately approved:

- unrelated backend refactors;
- frontend UI or localization changes;
- desktop `/proxy` behavior changes;
- deployments manifest edits outside the minimal backend/runtime rollout need;
- passes:true, Close, longterm write-back, or git stage/commit/push.

## Backend image delivery decision

Backend delivery does require a build/deploy step for the running ClawManager backend image or an equivalent approved backend process rollout.

Reason: the completed sanitizer patch is in backend source and tests, but the running backend will not use it until the backend artifact/process serving traffic is replaced or restarted from that patched source. A browser rerun before that delivery can still hit the old bridge and repeat the known signature mismatch.

This approval packet does not authorize that build/deploy. It only requests approval for the next implementation gate to perform it.

## Runtime trusted-proxy config/image decision

Runtime trusted-proxy config/image work likely requires a separate runtime patch unless the next gate proves the currently deployed runtime image already contains and enables the required contract.

Required confirmation points:

- runtime auth mode or equivalent trusted-proxy configuration is present for mediated Control UI traffic;
- trusted proxy source and identity checks are enforced at the runtime boundary;
- runtime accepts device-less mediated Control UI connect only after the trusted backend-to-runtime proof succeeds;
- direct clients cannot spoof trusted-proxy acceptance through browser-controlled JSON;
- image packaging actually includes the runtime source/config changes that implement this contract.

If any point is missing, the next gate should patch runtime source/config/image packaging in the approved scope, then stop before browser E2E unless a separate browser rerun approval is present.

## Key risks

- Trusted proxy spoofing: a direct client must not be able to bypass device signature validation by sending a trusted-proxy-like field or marker.
- Secret exposure: server-owned gateway token, password, key, cookie, bearer material, auth header values, and token-bearing URLs must remain out of browser-visible JSON, logs, screenshots, and evidence.
- Direct client regression: existing OpenClaw direct token/password/pairing/device-signature flows must continue to work unless a separate breaking-change approval exists.
- Partial delivery: deploying only the backend or only the runtime contract can leave the browser on the manual connection gate or replace `device signature invalid` with another connection failure.
- Stale route evidence: browser verification must prove the tested route belongs to the fresh/current target instance, not a previous instance route.

## Verification plan for the next gate

Backend health:

- after backend rollout, verify the running ClawManager backend health endpoint succeeds;
- confirm the running backend artifact or process corresponds to the patched source;
- verify no credential values appear in logs or evidence.

Runtime `18789`:

- verify the selected OpenClaw runtime exposes Control UI on `18789`;
- verify static root and WebSocket readiness through the expected in-cluster/backend-mediated path;
- confirm the runtime image/config in use contains the trusted-proxy contract or record BLOCKED with the missing packaging/config reason.

Browser rerun:

- after backend delivery and runtime trusted-proxy confirmation, run a separately approved browser/manual E2E rerun;
- verify the Control UI opens without `device signature invalid`;
- verify it does not fall back to an unexplained manual credential requirement;
- verify no stale browser storage or stale access route is driving the result.

No stale instance route:

- record the tested instance id/name and route shape;
- prove the route is the current target instance route and not an older instance route;
- verify backend route access and runtime target both resolve to the same current instance.

## Approval options

Approve:

`APPROVE_CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_GATE`

Block:

`CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_APPROVAL_PACKET_BLOCKED: <reason>`

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically:

- no implementation code
- no backend source edit
- no frontend source edit
- no runtime source edit
- no runtime image assembly artifact edit
- no deployments edit
- no docs edit
- no existing evidence edit
- no build, tag, push, pull, or container run
- no browser E2E
- no DevTools
- no Playwright
- no kubectl
- no k3d
- no Helm
- no instance mutation
- no database access or mutation
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
