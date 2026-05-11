# GTClaw runtime Control UI logo replacement runtime delivery and manual E2E evidence

Date: 2026-05-11

Verdict: GTCLAW_RUNTIME_CONTROLUI_LOGO_REPLACEMENT_RUNTIME_DELIVERY_DONE

## Scope

- Delivered the already-patched GTClaw runtime Control UI `favicon.svg` and `favicon.ico` overlay into a new runtime image.
- Pushed the new image to the local k3d registry.
- Created exactly one fresh replacement runtime instance for manual user validation.
- Did not perform browser/manual E2E final acceptance.

## Runtime image

- Host registry tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-logo-replacement-20260511093501`
- Cluster registry tag: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-logo-replacement-20260511093501`
- Digest: `sha256:d0a94ee4e3cfd49d7b73b98a37ed6e94bb8a595e3e986e0b9affdcfd3fb7a256`

Build readback from the image:

```text
af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  favicon.svg
9b668b79294e64c912e09bd4f68f207b33d3957c0b1fc2ba230a04eb38651696  favicon.ico
cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f  assets/zh-CN-B26mMdbY.js
favicon.svg lobster-gradient absent
trusted-proxy auth contract verifier passed for /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
```

## Replacement instance

- Instance ID: `26`
- Instance name: `gtclaw-logo-repl-093501`
- Namespace: `clawmanager-user-1`
- Pod: `clawreef-26-gtclaw-logo-repl-093501`
- Service: `clawreef-26-gtclaw-logo-repl-093501-svc`
- Pod IP: `10.42.0.131`
- Service IP: `10.43.39.132`
- Image in pod: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-logo-replacement-20260511093501`
- Image ID in pod: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:d0a94ee4e3cfd49d7b73b98a37ed6e94bb8a595e3e986e0b9affdcfd3fb7a256`

Pod status:

```text
ready=True
phase=Running
READY 1/1
STATUS Running
RESTARTS 0
```

Pod filesystem readback:

```text
af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  favicon.svg
9b668b79294e64c912e09bd4f68f207b33d3957c0b1fc2ba230a04eb38651696  favicon.ico
cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f  assets/zh-CN-B26mMdbY.js
favicon.svg lobster-gradient absent
favicon.svg: SVG Scalable Vector Graphics image, ASCII text
favicon.ico: MS Windows icon resource - 1 icon, 115x120 with PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced, 32 bits/pixel
trusted-proxy auth contract verifier passed for /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
```

## 18789 runtime HTTP verification

Container environment has `HTTP_PROXY`/`HTTPS_PROXY` configured and `NO_PROXY` does not include the Pod IP or Service IP. Direct 18789 checks were therefore run with `curl --noproxy '*'`.

```text
http://127.0.0.1:18789/ 200
http://10.42.0.131:18789/ 200
http://10.43.39.132:18789/ 200
```

## Proxied static asset readback

A short-lived `control-ui` instance access credential was generated for instance `26`. The credential value is intentionally redacted from this evidence file.

Manual E2E sanitized path shape:

```text
/api/v1/instances/26/control-ui/chat?session=main&token=<redacted-short-lived-instance-access-token>
```

Backend proxy readback with the redacted credential:

```text
/api/v1/instances/26/control-ui/?token=<redacted-short-lived-instance-access-token>        200
/api/v1/instances/26/control-ui/chat?session=main&token=<redacted-short-lived-instance-access-token>  200
/api/v1/instances/26/control-ui/favicon.svg?token=<redacted-short-lived-instance-access-token>        200
/api/v1/instances/26/control-ui/favicon.ico?token=<redacted-short-lived-instance-access-token>        200

af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  proxied favicon.svg
9b668b79294e64c912e09bd4f68f207b33d3957c0b1fc2ba230a04eb38651696  proxied favicon.ico
```

## Capacity handling

- No capacity cleanup was required.
- User quota readback was sufficient for one additional 1 CPU / 2 GB / 20 GB OpenClaw instance.
- Existing instance `25` / `oc2gi-anloc-121909` was not stopped or modified.
- Instances `17` and `18` were not touched.
- No old PVC, session, evidence, image, or browser cache cleanup was performed.

## Non-actions

- No source or artifact files were modified in this runtime delivery gate, except this evidence file.
- No backend/auth/scope/security predicate changes were made.
- No `operator.admin` grant was added.
- No `missing_scope` bypass was added.
- No OpenClaw/GTClaw/OpenSparrow technical-name global replacement was performed.
- No browser/manual E2E final acceptance was executed.
- No `passes:true` or Close action was executed.
- No git stage, commit, or push was executed.
