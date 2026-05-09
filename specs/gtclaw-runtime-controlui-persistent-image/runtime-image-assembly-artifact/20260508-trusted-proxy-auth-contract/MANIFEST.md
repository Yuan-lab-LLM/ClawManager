# Runtime Image Assembly Artifact - Trusted Proxy Auth Contract

Date/timezone: 2026-05-08, Asia/Shanghai

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

Approval token used:

- APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

## Parent Image

- Parent host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`
- Parent image index digest: `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`
- Parent contains official OpenClaw runtime package version `2026.4.14`.

## Assembly Scope

This build context:

- applies the trusted-proxy/device-less mediated Control UI shared-auth source patch;
- runs the source verifier during image build;
- installs the startup wrapper/config from `runtime-startup-artifact/20260508-trusted-proxy-auth-contract`;
- installs the runtime proof scripts under a `0755` proof directory;
- re-copies the reviewed localized Control UI files to preserve zh-CN output and patch hardcoded internal UI copy.
- adds the recovered `agents-_34Q844e.js` lazy chunk and copies it into the proven runtime Control UI assets path.

## Runtime Target Proofs Expected After Build

- `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js` contains `isGtManagerMediatedControlUiAuth`.
- `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js` reads `x-forwarded-prefix` from the upgrade request.
- `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js` accepts mediated token/password proof from `sharedAuthOk` while preserving the backend route-prefix requirement.
- `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js` normalizes the accepted device-less mediated Control UI session scopes to `operator.read` and `operator.pairing` only.
- `/usr/local/share/gtclaw/trusted-proxy-auth-contract` is readable and executable as a directory.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` hash becomes `6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js` hash becomes `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/nodes-BBk4VzkK.js` hash becomes `25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-BRWdbtpV.js` hash becomes `36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-shared-D6eRDyeb.js` hash becomes `f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/config-form-x_UhxUYO.js` hash becomes `8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` hash remains `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`.
- `/defaults/openclaw-agent/config.yaml` uses `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`.

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
- no kubectl mutation
- no k3d
- no Helm
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
