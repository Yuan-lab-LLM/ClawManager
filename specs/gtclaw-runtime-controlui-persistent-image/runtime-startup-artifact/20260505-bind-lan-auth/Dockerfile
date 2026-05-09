FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
COPY --chmod=0644 defaults/openclaw-agent/config.yaml /defaults/openclaw-agent/config.yaml
COPY --chmod=0755 usr/local/bin/openclaw-ensure-controlui-origin /usr/local/bin/openclaw-ensure-controlui-origin
COPY --chmod=0755 usr/local/bin/openclaw-gateway-with-origin-allowlist /usr/local/bin/openclaw-gateway-with-origin-allowlist
COPY --chmod=0755 etc/services.d/openclaw-agent/run /etc/services.d/openclaw-agent/run
