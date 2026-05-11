# GTClaw Runtime Control UI Logo Replacement Root Cause and Patch

Date/timezone: 2026-05-11, Asia/Shanghai

Gate: `GTCLAW_RUNTIME_CONTROLUI_LOGO_REPLACEMENT_ROOT_CAUSE_AND_PATCH_GATE`

Verdict: `GTCLAW_RUNTIME_CONTROLUI_LOGO_REPLACEMENT_ROOT_CAUSE_AND_PATCH_DONE`

## Scope

This gate replaces the GTClaw/OpenClaw runtime Control UI root favicon assets used by the internal Control UI logo helper. It does not change the GTManager admin frontend logo, backend auth, route scope, runtime security predicates, database state, Kubernetes resources, image tags, registry contents, or running instances.

## Root Cause

The runtime Control UI visible brand logo is resolved through the existing compiled helper in `assets/agents-utils-2iiM6XOJ.js`:

```text
function P(e){let t=i(e)?.replace(/\/$/,``)??``;return t?`${t}/favicon.svg`:`favicon.svg`}
```

The main bundle imports that helper:

```text
import{C as De,S as Oe,b as ke,d as Ae,f as je,g as Me,n as Ne,p as Pe,u as Fe}from"./agents-utils-2iiM6XOJ.js";
```

The running instance still inherited the parent OpenClaw runtime favicon because the repo-owned runtime overlay did not provide replacement root favicon files:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.svg: No such file or directory
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.ico: No such file or directory
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.svg: No such file or directory
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.ico: No such file or directory
```

The assembly Dockerfile also did not copy favicon files into the proven runtime Control UI root before this patch.

## Running Container Evidence

Read-only target:

```text
namespace: clawmanager-user-1
pod: clawreef-25-oc2gi-anloc-121909
runtime path: /usr/local/lib/node_modules/openclaw/dist/control-ui
```

Running favicon hash:

```text
fa7e2ec07ebfa696bcc8c27d7e36425cbb7b1772f6f7f04ce390cf5f1c35cf0e  favicon.svg
```

Sanitized lobster evidence:

```text
<linearGradient id="lobster-gradient" ...>
fill="url(#lobster-gradient)"
```

This proves the observed red lobster logo comes from the runtime Control UI favicon asset, not from the GTManager frontend logo.

## Patch

Official GT logo sources:

```text
frontend/public/gtmanager-logo.svg
frontend/public/gtmanager-logo.png
```

Changed files:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.svg
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.ico
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.svg
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.ico
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md
specs/gtclaw-runtime-controlui-persistent-image/evidence/20260511-gtclaw-runtime-controlui-logo-replacement-root-cause-and-patch.md
```

Favicon hashes:

```text
af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  favicon.svg
9b668b79294e64c912e09bd4f68f207b33d3957c0b1fc2ba230a04eb38651696  favicon.ico
```

The SVG favicon is byte-identical to `frontend/public/gtmanager-logo.svg`. The ICO favicon is generated from `frontend/public/gtmanager-logo.png` as a PNG-backed ICO.

Dockerfile coverage:

```text
COPY --chmod=0644 control-ui/favicon.svg /usr/local/lib/node_modules/openclaw/dist/control-ui/favicon.svg
COPY --chmod=0644 control-ui/favicon.ico /usr/local/lib/node_modules/openclaw/dist/control-ui/favicon.ico
```

No JavaScript asset patch was needed because the existing helper already resolves the visible logo to `favicon.svg`.

## Guardrails

Unchanged runtime asset hashes checked during this gate:

```text
cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f  control-ui/assets/zh-CN-B26mMdbY.js
781c551271b60118aa6395a4da9818400902aa30c6987bd2b9fbe68f3bce3125  control-ui/assets/index-M4TNVXB3.js
1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7  control-ui/assets/agents-_34Q844e.js
8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245  control-ui/assets/config-form-x_UhxUYO.js
```

No `lobster-gradient` remains in the patched repo-owned Control UI overlay paths checked by this gate.

## Delivery Note

This patch updates repo-owned runtime artifacts only. A later runtime image delivery gate is required to build/tag/push a new OpenClaw runtime image and then deliver it to affected runtime instances. The currently running instance 25 page will keep showing the inherited runtime image contents until that later image delivery and instance/runtime refresh happens.

## Explicit Non-actions

- no build/tag/push image
- no registry push
- no deploy or rollout
- no Kubernetes mutation
- no instance create/stop/restart/delete
- no database mutation
- no backend/auth/scope/runtime security predicate modification
- no OpenClaw to GTClaw global replacement
- no old evidence/session/PVC/image/browser cache cleanup
- no Mem0 or longterm write
- no passes:true
- no Close
- no git stage/commit/push
