# Runtime Startup Artifact - Trusted Proxy Auth Contract

Date/timezone: 2026-05-08, Asia/Shanghai

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

Approval token used:

- APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE

## Startup Scope

This artifact preserves the existing runtime startup behavior and switches the OpenClaw command wrapper from:

- `/usr/local/bin/openclaw-gateway-with-origin-allowlist`

to:

- `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`

The new wrapper still materializes the Control UI origin allowlist before launching:

- `openclaw gateway run --bind lan --auth token`

Token auth is intentionally retained because the deployed ClawManager backend bridge injects the server-side OpenClaw runtime credential. The device-less mediated Control UI exception is provided by the runtime source patch, not by switching the official gateway into trusted-proxy mode.

## File Manifest

| Output path | Target image path | Mode | Purpose |
|---|---|---:|---|
| `defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | `0644` | Uses the auth-contract wrapper and keeps token auth. |
| `usr/local/bin/openclaw-ensure-controlui-origin` | `/usr/local/bin/openclaw-ensure-controlui-origin` | `0755` | Preserves existing origin allowlist materialization. |
| `usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract` | `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract` | `0755` | Runs the origin materializer, then execs OpenClaw. |
| `etc/services.d/openclaw-agent/run` | `/etc/services.d/openclaw-agent/run` | `0755` | Preserves the existing service entrypoint. |

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
