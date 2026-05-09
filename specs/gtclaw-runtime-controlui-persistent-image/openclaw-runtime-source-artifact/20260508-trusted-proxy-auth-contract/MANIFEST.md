# OpenClaw Runtime Source Artifact - Trusted Proxy Auth Contract

Date/timezone: 2026-05-08, Asia/Shanghai

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

Approval token used:

- APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

## Source Baseline

- Official OpenClaw release: `v2026.4.14`
- Official release commit: `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`
- Runtime package target: `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js`
- Runtime package version observed from current localized image: `2026.4.14`

## Config-Only Decision

Config-only is not sufficient for the deployed ClawManager backend contract.

The official OpenClaw trusted-proxy mode requires `gateway.trustedProxies` plus a configured trusted-proxy user header and rejects coexistence with shared token auth. The deployed backend-mediated Control UI bridge currently injects server-side token auth and a route prefix, and it does not inject a trusted-proxy user header. A pure config switch would either fail auth or require trusting browser-supplied proxy/user headers.

## Patch Scope

`patch-openclaw-trusted-proxy-contract.mjs` applies a minimal compiled-runtime patch:

- Add `isGtManagerMediatedControlUiAuth(params)`.
- Allow missing device identity only for Control UI operator auth when shared token/password auth has already succeeded (`sharedAuthOk` or compatible `authOk`) and the backend route prefix is present.
- Preserve the mediated session scopes only for that allow path by normalizing them to the minimum required Control UI operator set: `operator.read` and `operator.pairing`.
- Do not grant `operator.admin`, `operator.write`, `operator.approvals`, or `operator.talk.secrets` in the mediated scope normalizer.
- Use `X-Forwarded-Prefix` from the WebSocket upgrade request as the backend route boundary marker.
- Do not read any JSON trusted-proxy marker from the browser first-connect payload.
- Preserve the official device signature validation block for all connections that still provide a device identity.

`verify-trusted-proxy-contract.mjs` statically verifies the patch anchors, checks direct-client spoof cases, and asserts that token/password/pairing/device-signature paths remain present.

## Security Boundary

This artifact treats the ClawManager backend as the mediated boundary because the backend overwrites browser first-connect auth with a server-side runtime credential and sets the route prefix on the upstream WebSocket request. The browser does not receive the runtime credential, and JSON fields in the first-connect payload are not trusted as proxy proof.

## Explicit Non-actions

- no backend modification
- no frontend modification
- no deployment manifest modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no UnifiedFramework modification
- no old artifact or old evidence modification
- no browser E2E
- no instance mutation
- no kubectl
- no k3d
- no Helm
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
