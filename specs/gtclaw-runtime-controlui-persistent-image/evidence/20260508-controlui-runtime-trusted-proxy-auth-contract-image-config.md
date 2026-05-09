# CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG

Date/timezone: 2026-05-08, Asia/Shanghai

Verdict:

CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE

Approval token used:

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

## Scope

Implemented and packaged the OpenClaw runtime trusted-proxy / device-less mediated Control UI auth contract as a new runtime image. This gate did not create a fresh runtime instance and did not run browser E2E.

Exact files/artifacts changed:

- `specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs`
- `specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/defaults/openclaw-agent/config.yaml`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-ensure-controlui-origin`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/etc/services.d/openclaw-agent/run`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/defaults/openclaw-agent/config.yaml`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-ensure-controlui-origin`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/etc/services.d/openclaw-agent/run`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/index.html`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/index-M4TNVXB3.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/i18n-B06L7jQN.js`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/zh-CN-B26mMdbY.js`

## Config-Only vs Source-Patch Decision

Decision: source patch plus startup/image packaging.

Config-only was not sufficient. OpenClaw v2026.4.14 official trusted-proxy mode requires configured trusted proxies and a trusted-proxy user header, while also rejecting coexistence with shared token auth. The deployed ClawManager backend bridge sends server-side shared runtime auth and `X-Forwarded-Prefix`, and does not send a trusted-proxy user header. Switching only config would either fail the current backend contract or require trusting spoofable browser-provided proxy/user headers.

The source patch adds a narrow device-less Control UI missing-device allow path only when:

- the client is Control UI;
- the role is `operator`;
- shared token/password auth already succeeded;
- the WebSocket upgrade request contains a ClawManager route prefix matching `/api/v1/instances/<id>/control-ui`.

It does not trust any JSON marker from the direct client first-connect payload.

## Runtime Image

Final runtime image:

- host tag `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130`
- cluster tag `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130`
- image index digest `sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010`
- linux/arm64 manifest digest `sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb`

Parent runtime image:

- host tag `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`
- image index digest `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`
- OpenClaw package version observed in image readback `2026.4.14`
- official release commit used for source scope `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`

Intermediate candidate:

- `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508125928`
- digest `sha256:187153d19538655e56016c51f7da0f23874eac1600aae005b1d2411043958087`
- superseded because image layer readback found `/usr/local/share/gtclaw` directory mode was not executable after `COPY --chmod=0644`; no registry cleanup was performed and this tag is not recommended for deployment.

## Runtime Tests and Proofs

Runtime source/config tests:

- Pre-patch helper absence check against localized image source exited `1` as expected because `isGtManagerMediatedControlUiAuth` was not present before the patch.
- `node specs/.../verify-trusted-proxy-contract.mjs /tmp/clawmanager-openclaw-image-readback/.../server.impl-BbJvXoPb.js specs/.../patch-openclaw-trusted-proxy-contract.mjs` exited `0`.
- The assembly-context copy of the same verifier exited `0`.
- Image build executed the verifier inside the Dockerfile and exited `0`; Docker emitted only the existing `FROM --platform=linux/arm64` warning.
- `docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130` exited `0`.
- `docker buildx imagetools inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130` exited `0` and reported the digest above.
- Registry/image layer readback via `docker save` plus tar extraction exited `0`.
- Readback verifier at `/usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs` exited `0`.

Readback runtime patch proof:

- `server.impl-BbJvXoPb.js` contains `isGtManagerMediatedControlUiAuth`.
- `server.impl-BbJvXoPb.js` reads `x-forwarded-prefix` from the WebSocket upgrade request.
- `server.impl-BbJvXoPb.js` keeps `device signature invalid`, `verifyDeviceSignature`, `resolveConnectAuthDecision`, `bootstrapTokenCandidate`, `verifyDeviceToken`, and `shouldSkipControlUiPairing`.
- `/usr/local/share/gtclaw/trusted-proxy-auth-contract` is `drwxr-xr-x`.
- `/defaults/openclaw-agent/config.yaml` uses `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`.

## zh-CN Preservation

Registry/image layer readback proved localized Control UI files still exist under:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

Hashes:

- `index.html` `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec`
- `assets/index-M4TNVXB3.js` `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648`
- `assets/i18n-B06L7jQN.js` `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63`
- `assets/zh-CN-B26mMdbY.js` `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`

The zh-CN Control UI localization did not revert.

## Direct Client Spoof and Regression Protection

Direct client spoof rejection proof:

- The verifier models a direct client JSON marker case and rejects it.
- The verifier rejects shared auth without the backend route prefix.
- The verifier rejects shared auth with the wrong route prefix.
- The verifier rejects non-operator and non-Control UI clients even when the route prefix exists.
- The patch reads no `connectParams.trustedProxy`, `connectParams.auth.trustedProxy`, or similar JSON trusted-proxy marker.

token/password/pairing/device-signature regression protection:

- The OpenClaw gateway remains launched with `--auth token`.
- The existing token/password auth decision path remains in `resolveConnectAuthState` and `resolveConnectAuthDecision`.
- Pairing helpers and device token helpers remain present.
- If a first-connect payload provides a device identity, the original device-signature verification block still runs and still returns `device signature invalid` on invalid signatures.

## Next Gate

Next gate should be a fresh runtime instance deployment/readiness gate, for example:

CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE

Recommended scope:

- create or select a fresh runtime instance using the final runtime image tag above;
- verify runtime 18789 health on that fresh instance;
- verify image/config readback from the fresh instance;
- verify there is no stale instance route;
- only after this readiness gate should browser rerun be scheduled.

Browser rerun is now appropriate only after a fresh runtime instance is on the final image. This gate did not mutate existing instance 16 / `oc2gi-loc-221427`.

## Forbidden Actions Confirmation

- no browser E2E
- no DevTools
- no Playwright
- no instance mutation
- no pod/service/PVC mutation
- no kubectl
- no k3d mutation
- no Helm
- no database mutation
- no backend modification
- no frontend modification
- no deployment manifest modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no UnifiedFramework modification
- no old artifact modification
- no old evidence modification
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no registry cleanup
- no container run

## Sensitive Data

No token, password, key, cookie, bearer material, auth header value, or access URL with credential material is recorded in this evidence.

Sensitive scan result:

- evidence sensitive-material scan exited `1` with no matches.

Final evidence verification:

- `git diff --check` on the allowed new artifact/evidence paths exited `0`.
- required marker `rg` scan exited `0`.
- scoped `git status --short` listed only new untracked paths under this gate's allowed artifact/evidence paths.
