# Runtime Source Artifact Recovery Gate - 2026-05-05

## Verdict

`RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE`

This gate performed the approved local read-only extraction of exactly two OpenClaw startup files from the specified local parent image into a new `/tmp/gtclaw-startup-source-inspect-*` workspace.

No startup fix was implemented. No extracted file was modified. The extracted image filesystem is not treated as final source without review.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Runtime Source Artifact Recovery Approval Packet | `RUNTIME_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE` |
| WS Auth Bridge Implementation Gate | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED`; remaining blocker was runtime startup source/build-context recovery |

## Parent Image

| Field | Value |
| --- | --- |
| parent image host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| parent image in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| parent image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| parent image linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |
| local inspect platform | `linux/arm64` |
| local inspect repo digest | `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |

Docker daemon was available for this gate. The candidate parent image resolved locally to the expected repo digest and platform.

## Recovery Workspace

New local workspace:

`/tmp/gtclaw-startup-source-inspect-y5sCboQ2`

Only this new workspace was created for recovered startup source inspection. No old `/tmp` workspace, old asset, old extraction directory, or old project asset was deleted or modified.

## Exact Read-only Extraction Commands

The exact read-only extraction command sequence used was:

```bash
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Platform={{.Os}}/{{.Architecture}}
Id={{.Id}}'
```

```bash
set -euo pipefail
image='localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506'
out=$(mktemp -d /tmp/gtclaw-startup-source-inspect-XXXXXXXX)
mkdir -p "$out/defaults/openclaw-agent" "$out/etc/services.d/openclaw-agent"
cid=$(docker container create "$image")
cleanup() {
  if [ -n "${cid:-}" ]; then
    docker container rm "$cid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
docker cp "$cid:/defaults/openclaw-agent/config.yaml" "$out/defaults/openclaw-agent/config.yaml"
docker cp "$cid:/etc/services.d/openclaw-agent/run" "$out/etc/services.d/openclaw-agent/run"
docker container rm "$cid" >/dev/null
cid=''
printf 'OUT=%s\n' "$out"
printf 'CONTAINER_REMOVED=yes\n'
```

The temporary container was created only as a stopped container for `docker cp`, was not started, and was removed by this gate.

No `docker export`, `docker save`, image pull, image build, image tag, image push, runtime image/resource setting mutation, Kubernetes write, database write, registry mutation, browser E2E, Chrome DevTools MCP, Playwright, manual pod patch, manual Service patch, or `kubectl cp` write was performed.

## Extracted File Manifest

| Original path | Local extracted path | mode | owner/group metadata if available | sha256 | byte size |
| --- | --- | --- | --- | --- | ---: |
| `/defaults/openclaw-agent/config.yaml` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml` | `0644` | local extracted owner `eduardogan`, group `wheel`, uid `501`, gid `0`; original image owner/group not available from the approved stopped-container `docker cp` method | `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3` | `745` |
| `/etc/services.d/openclaw-agent/run` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run` | `0755` | local extracted owner `eduardogan`, group `wheel`, uid `501`, gid `0`; original image owner/group not available from the approved stopped-container `docker cp` method | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` | `289` |

No other image paths were extracted.

## Startup Source Classification

The two recovered files are sufficient to form a reviewed startup source artifact for the next approval packet, with review required before any implementation:

| File | Classification |
| --- | --- |
| `/defaults/openclaw-agent/config.yaml` | Primary startup source candidate. It contains `openclaw_command` with `openclaw`, `gateway`, `run`, plus `openclaw_config_path: /config/.openclaw/openclaw.json`. This is the most likely file to carry a minimal `--bind lan` and `--auth token` change. |
| `/etc/services.d/openclaw-agent/run` | Startup wrapper candidate. It creates the agent/config directories, copies `/defaults/openclaw-agent/config.yaml` to `/etc/openclaw-agent/config.yaml` when absent, then executes `/usr/local/bin/openclaw-agent`. No minimal change is currently indicated here. |

Keyword inspection found no existing `gateway.bind`, `--bind lan`, `--auth token`, or `OPENCLAW_GATEWAY_TOKEN` literal in either recovered file.

Expected minimal future implementation surface, if later approved:

- Add pod-facing gateway bind/auth flags to `openclaw_command` in `/defaults/openclaw-agent/config.yaml`, likely `--bind lan` and `--auth token`.
- Keep `OPENCLAW_GATEWAY_TOKEN` as the server-side token source already injected by backend source; do not hardcode token values into the recovered files.
- Leave `/etc/services.d/openclaw-agent/run` unchanged unless a later review proves the wrapper itself must change.

The recovered files are not final source by themselves. They are a local startup source artifact for review and for a later Runtime Startup Artifact Implementation Approval Packet.

## Additional Source / Build-context Need

No additional source recovery is required to review the minimal startup command change.

A later implementation gate still needs an approved, minimal build context plan that copies only the reviewed startup artifact changes into the parent image. That later gate is separate and remains unauthorized here.

If later review decides that runtime behavior requires editing files outside `/defaults/openclaw-agent/config.yaml` and `/etc/services.d/openclaw-agent/run`, the next gate must be an `Additional Runtime Source Recovery Approval Packet`, not implementation.

## Recommended Next Gate

Recommended next gate:

`Runtime Startup Artifact Implementation Approval Packet`

Reason: the approved source recovery recovered the two startup files, identified `/defaults/openclaw-agent/config.yaml` as the likely minimal change point for `--bind lan` and `--auth token`, and did not find evidence that another source file is currently required.

Do not proceed directly to implementation without a new approval packet.

## Secret Hygiene

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, or token-bearing access URL was printed or written into this evidence.

`OPENCLAW_GATEWAY_TOKEN` is referenced only as an environment variable name and not as a value.

## Explicit Negatives

- no build/tag/push/pull
- no runtime/K8S/database/registry mutation
- no runtime image/resource setting mutation
- no fresh instance create/delete/modify
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no extracted file modification
- no old `/tmp` cleanup or old asset deletion
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close

## Verification Results

Verification commands required by the gate were run after writing this evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md
rg -n "RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE|RUNTIME_SOURCE_ARTIFACT_RECOVERY_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|/tmp/gtclaw-startup-source-inspect-|parent image|sha256|byte size|mode|gateway.bind|--bind lan|--auth token|OPENCLAW_GATEWAY_TOKEN|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md
```

| Command | Result |
| --- | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md` | Exit `0`; no whitespace diagnostics. |
| `rg -n "RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE|RUNTIME_SOURCE_ARTIFACT_RECOVERY_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|/tmp/gtclaw-startup-source-inspect-|parent image|sha256|byte size|mode|gateway.bind|--bind lan|--auth token|OPENCLAW_GATEWAY_TOKEN|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md` | Exit `0`; required markers found. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md` | Shows both files as untracked: this recovery evidence and the prior approval packet. |
