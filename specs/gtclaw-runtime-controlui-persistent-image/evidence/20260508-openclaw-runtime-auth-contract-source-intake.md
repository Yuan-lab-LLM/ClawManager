# OpenClaw runtime auth contract source intake

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, runtime source intake
Parent gate: CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_GATE

## Verdict

OPENCLAW_RUNTIME_AUTH_CONTRACT_SOURCE_INTAKE_DONE

Official OpenClaw runtime gateway source was located and reviewed at the public `openclaw/openclaw` repository. This source intake performed no browser E2E, no DevTools, no Playwright, no kubectl/k3d/Helm, no image build, no image pull, no runtime mutation, and no source edits outside the approved backend files.

## Sources reviewed

- `https://github.com/openclaw/openclaw/blob/main/docs/gateway/protocol.md`
- `https://github.com/openclaw/openclaw/blob/main/docs/gateway/trusted-proxy-auth.md`
- `https://github.com/openclaw/openclaw/blob/main/src/gateway/server/ws-connection/message-handler.ts`
- `https://github.com/openclaw/openclaw/blob/main/src/gateway/server/ws-connection/handshake-auth-helpers.ts`
- `https://github.com/openclaw/openclaw/blob/main/src/gateway/server/ws-connection/connect-policy.ts`
- `https://github.com/openclaw/openclaw/blob/main/src/gateway/server/ws-connection/auth-context.ts`
- `https://github.com/openclaw/openclaw/blob/main/src/gateway/auth.ts`

## Source-intake findings

The official gateway first-connect path validates browser-provided device identity when `params.device` is present. The device signature helper derives the signed payload from the first-connect auth material and nonce. Therefore, forwarding a browser-generated `params.device` and `device.signature` after the backend rewrites `params.auth` creates inconsistent signature inputs and can produce `device signature invalid`.

The official trusted-proxy path is not a browser-spoofable JSON marker. The runtime source ties trusted-proxy acceptance to runtime auth mode, trusted proxy source address, and trusted identity headers resolved by gateway auth context. Direct clients cannot become trusted-proxy clients by adding a field to `params`.

The official connect policy allows a device-less Control UI operator connection only when it is recognized as trusted-proxy Control UI operator auth. That confirms the secure contract boundary: backend-proxied Control UI traffic must not mix rewritten `params.auth` with stale browser `params.device`, and full browser-ready acceptance requires the runtime to be configured or packaged for the trusted-proxy contract.

## Contract decision

Backend-side sanitizer is safe and required:

- when the backend rewrites first-connect `params.auth`, it must not forward stale `params.device` or stale `device.signature`;
- non-auth and non-device connect params remain preserved;
- non first-connect messages remain rejected by the first-connect gate and are not rewritten into upstream traffic;
- direct OpenClaw client behavior is not changed by this backend bridge sanitizer because it only applies to ClawManager Control UI proxy traffic.

Runtime-side source or configuration remains required for complete end-to-end acceptance:

- a follow-up runtime gate must confirm or enable `trusted-proxy` auth mode and trusted proxy identity headers for the actual image/runtime deployment;
- runtime tests must prove trusted-proxy acceptance is limited to the authenticated backend-to-runtime boundary;
- direct clients with spoofed trustedProxy-like params must still fail.

## Forbidden actions confirmation

- no browser E2E
- no DevTools
- no Playwright
- no kubectl
- no k3d
- no Helm
- no image build, tag, push, pull, or container run
- no runtime image or artifact mutation
- no frontend edits
- no deployments edits
- no database or instance mutation
- no storage/cache/cookie cleanup
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
