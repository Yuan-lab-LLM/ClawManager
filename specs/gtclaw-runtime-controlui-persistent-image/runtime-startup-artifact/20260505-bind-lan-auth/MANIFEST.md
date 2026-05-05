# Runtime Startup Artifact Manifest - 2026-05-05

## Artifact

Reviewed startup source/build-context artifact:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

This artifact prepares a minimal OpenClaw gateway startup change for a later image build gate. No image was built from this artifact in this gate.

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
| `defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | `0644` | `785` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` |
| `etc/services.d/openclaw-agent/run` | `/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

## Minimal Diff Summary

`defaults/openclaw-agent/config.yaml` changes only `openclaw_command` by adding pod-facing gateway listen/auth flags:

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

`etc/services.d/openclaw-agent/run` is unchanged byte-for-byte from the recovered input.

## Build Context Notes

`Dockerfile` is included only as a later build gate input. It contains a digest-pinned parent `FROM` and exactly two `COPY` instructions for the startup files, preserving modes through `COPY --chmod`.

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
