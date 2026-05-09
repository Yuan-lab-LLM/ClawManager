FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54

COPY --chmod=0644 defaults/openclaw-agent/config.yaml /defaults/openclaw-agent/config.yaml
COPY --chmod=0755 usr/local/bin/openclaw-ensure-controlui-origin /usr/local/bin/openclaw-ensure-controlui-origin
COPY --chmod=0755 usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract /usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract
COPY --chmod=0755 etc/services.d/openclaw-agent/run /etc/services.d/openclaw-agent/run

RUN mkdir -p /usr/local/share/gtclaw/trusted-proxy-auth-contract
COPY --chmod=0644 openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
COPY --chmod=0644 openclaw-runtime/verify-trusted-proxy-contract.mjs /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs
RUN chmod 0755 /usr/local/share/gtclaw /usr/local/share/gtclaw/trusted-proxy-auth-contract \
  && node /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js \
  && node /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs

COPY --chmod=0644 control-ui/index.html /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html
COPY --chmod=0644 control-ui/assets/index-M4TNVXB3.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js
COPY --chmod=0644 control-ui/assets/agents-_34Q844e.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/agents-_34Q844e.js
COPY --chmod=0644 control-ui/assets/config-form-x_UhxUYO.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/config-form-x_UhxUYO.js
COPY --chmod=0644 control-ui/assets/i18n-B06L7jQN.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js
COPY --chmod=0644 control-ui/assets/zh-CN-B26mMdbY.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
COPY --chmod=0644 control-ui/assets/nodes-BBk4VzkK.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/nodes-BBk4VzkK.js
COPY --chmod=0644 control-ui/assets/skills-BRWdbtpV.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-BRWdbtpV.js
COPY --chmod=0644 control-ui/assets/skills-shared-D6eRDyeb.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/skills-shared-D6eRDyeb.js
