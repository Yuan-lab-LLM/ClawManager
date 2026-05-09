# Runtime Startup Artifact Manifest - 2026-05-05

## Artifact

Reviewed startup source/build-context artifact:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

This artifact prepares minimal OpenClaw gateway startup changes for later image build gates. No image was built from this artifact in this gate.

## Parent Image

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

## Input Files

| Original path | Recovered input path | mode | size | sha256 |
| --- | --- | --- | ---: | --- |
| `/defaults/openclaw-agent/config.yaml` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml` | `0644` | `745` | `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3` |
| `/etc/services.d/openclaw-agent/run` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

## Output Files

| Output path | Target image path | mode | size | sha256 |
| --- | --- | --- | ---: | --- |
| `defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | `0644` | `843` | `bdc8bf155539762c02f37ffbeb27e2dcec48bc5c3badaf4a17ec2edd6cd221c9` |
| `usr/local/bin/openclaw-ensure-controlui-origin` | `/usr/local/bin/openclaw-ensure-controlui-origin` | `0755` | `2009` | `c4151fa9a08ee04c41b212a9b30838f1f19d474fe50b6d2fdc848994d8fba071` |
| `usr/local/bin/openclaw-gateway-with-origin-allowlist` | `/usr/local/bin/openclaw-gateway-with-origin-allowlist` | `0755` | `201` | `79910c9dc6a0dcd0d809af1fc21a45052afeae66732d7a0ff4185089e8c3995c` |
| `etc/services.d/openclaw-agent/run` | `/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

## Minimal Diff Summary

`defaults/openclaw-agent/config.yaml` first changed `openclaw_command` by adding pod-facing gateway listen/auth flags:

```yaml
openclaw_command:
  - openclaw
  - gateway
  - run
  - --bind
  - lan
  - --auth
  - token
```

The origin-allowlist rerun keeps `openclaw gateway run --bind lan --auth token` and prepends a command wrapper that materializes `/config/.openclaw/openclaw.json` before the gateway command runs:

```yaml
openclaw_command:
  - /usr/local/bin/openclaw-gateway-with-origin-allowlist
  - openclaw
  - gateway
  - run
  - --bind
  - lan
  - --auth
  - token
openclaw_config_path: /config/.openclaw/openclaw.json
```

The helper `openclaw-ensure-controlui-origin` preserves any existing valid object JSON config, merges `gateway.controlUi.allowedOrigins`, and adds `https://localhost:30443` idempotently. It refuses to overwrite invalid JSON or incompatible non-object/non-array shapes.

`etc/services.d/openclaw-agent/run` is unchanged byte-for-byte from the recovered input.

## Build Context Notes

`Dockerfile` is included only as a later build gate input. It contains a digest-pinned parent `FROM` and `COPY` instructions for the reviewed startup files and helper scripts, preserving modes through `COPY --chmod`.

No build/tag/push/pull was executed in this gate.

## Secret Hygiene

No hardcoded token or secret value exists in this artifact.

`OPENCLAW_GATEWAY_TOKEN` is treated only as a runtime environment variable name and token source from backend/runtime configuration; no value is stored here.

## Explicit Negatives

- no hardcoded token/secret value
- no build/tag/push/pull
- no runtime/K8S/database/registry mutation
- no fresh instance mutation
- no browser E2E
- no Mem0 write
- no longterm write
- no passes:true
- no Close
