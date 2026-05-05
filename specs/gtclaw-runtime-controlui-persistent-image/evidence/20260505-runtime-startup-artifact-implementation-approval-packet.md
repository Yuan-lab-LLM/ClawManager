# Runtime Startup Artifact Implementation Approval Packet - 2026-05-05

## Verdict

`RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_DONE`

This packet requests explicit user approval for the next gate:

`Do you approve executing the Runtime Startup Artifact Implementation Gate under the scope and prohibitions below?`

This approval packet performed no implementation, no source/build-context creation, no extracted file modification, no image build, no image tag, no image push, no image pull, no runtime image/resource setting mutation, no Kubernetes write, no database write, no registry mutation, no fresh instance mutation, no browser E2E, no Chrome DevTools MCP, no Playwright, no Mem0 write, no longterm write-back, no passes:true, and no Close.

## Dependency Gates

| Gate | Current state |
| --- | --- |
| WS Auth Bridge Implementation Gate | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; Commander re-verified focused tests and `go test -count=1 ./internal/services ./internal/handlers` passed. |
| Runtime Source Artifact Recovery Approval Packet | `RUNTIME_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`. |
| Runtime Source Artifact Recovery Gate | `RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE`; Commander read-only review accepted recovery of only `/defaults/openclaw-agent/config.yaml` and `/etc/services.d/openclaw-agent/run`. |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED`; remaining blocker is runtime startup artifact implementation not yet approved or executed. |

## Parent Image

| Field | Value |
| --- | --- |
| parent image host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| parent image in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| parent image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| parent image linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

The parent image is the already verified persistent runtime artifact from the image delivery loop and source recovery evidence. It remains the source identity for future reviewed startup artifact implementation.

## Recovered Startup Source Artifact

| Original path | Recovered local path | mode | size | sha256 |
| --- | --- | --- | ---: | --- |
| `/defaults/openclaw-agent/config.yaml` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml` | `0644` | `745` | `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3` |
| `/etc/services.d/openclaw-agent/run` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

The recovered files are read-only source inputs for review. They must not be edited in place.

## Minimal Implementation Plan If Approved

The Runtime Startup Artifact Implementation Gate should be limited to creating a reviewed startup source/build-context artifact from the recovered source, then writing implementation evidence. It must not build an image unless a separate approval explicitly authorizes image work.

Minimum implementation approach:

1. Create or modify a clearly named reviewed startup source/build-context artifact path under a new approved local workspace.
2. Copy only the recovered startup artifact files needed for the startup change into that workspace.
3. Do not modify the recovered `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` files directly.
4. Prefer the smallest change in `/defaults/openclaw-agent/config.yaml` under `openclaw_command`.
5. Target pod-facing gateway listen/auth behavior, for example adding `--bind lan` and `--auth token`, or an evidence-supported equivalent configuration.
6. No hardcode: do not hardcode token values or secret values in any file.
7. Treat `OPENCLAW_GATEWAY_TOKEN` only as an environment variable name and runtime token source already provided by backend source.
8. Keep `/etc/services.d/openclaw-agent/run` unchanged by default because current evidence shows it only copies `/defaults/openclaw-agent/config.yaml` to `/etc/openclaw-agent/config.yaml` if absent and starts `/usr/local/bin/openclaw-agent`.
9. Modify `/etc/services.d/openclaw-agent/run` only if the implementation evidence explicitly proves the wrapper must change.
10. Write a new implementation evidence packet with exact source artifact path, before/after diff summary, hashes, and secret hygiene notes.

The expected minimal startup command shape is:

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

This command shape is a proposed implementation target, not an executed change in this approval packet.

## Scope Allowed If User Approves This Packet

If the user approves the Runtime Startup Artifact Implementation Gate, the next gate may:

| Scope | Allowed purpose |
| --- | --- |
| New reviewed startup source/build-context artifact path | Copy recovered startup source inputs into a controlled artifact workspace and apply the reviewed minimal startup command change there. |
| New implementation evidence file | Record the implementation result, file hashes, byte sizes, exact changed lines, secret hygiene, and gate status. |
| Read-only recovered source inputs | Read `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` only as immutable source inputs. |
| Read-only `/tmp/gtclaw-runtime-patch/**` | Reference existing control-ui static artifact metadata only; no modifications. |

Build/tag/push/pull decision:

- `no build/tag/push/pull` remains the approved boundary for this packet.
- Image build, image tag, image push, image pull, local registry interaction, and artifact digest publication require a later dedicated runtime image build/tag/push approval packet.
- The implementation gate may prepare a build context artifact, but must stop before any Docker build/tag/push/pull command.

## Explicitly Forbidden In The Implementation Gate Unless Separately Approved

- Modifying `backend/**`.
- Modifying `frontend/**`.
- Modifying `deployments/**`.
- Modifying `docs/**`.
- Modifying `longterm/**`.
- Modifying `AgentTeam/**`.
- Modifying `specs/gtclaw-runtime-controlui-persistent-image/spec.md`.
- Modifying `specs/gtclaw-runtime-controlui-persistent-image/plan.md`.
- Modifying `specs/gtclaw-runtime-controlui-persistent-image/tasks.md`.
- Modifying existing evidence files.
- Modifying `/tmp/gtclaw-runtime-patch/**`.
- Modifying recovered `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` extracted files in place.
- Extracting any additional image files.
- Treating extracted image filesystem as final source without review.
- Hardcoding a secret or token value.
- Logging, printing, or writing token values, cookie values, credentials, access URLs, `.env`, `.codex/auth.json`, or `.codex/config.toml` content.
- build/tag/push/pull.
- Runtime image/resource setting mutation.
- Kubernetes write.
- Database write.
- Registry mutation.
- Fresh instance create/delete/modify.
- Browser E2E, Chrome DevTools MCP, or Playwright.
- Manual pod patch.
- Manual Service patch.
- `kubectl cp` write.
- Mem0 write.
- longterm write-back.
- passes:true.
- Close.

## Required Implementation Evidence If Approved

The implementation evidence must include:

| Evidence item | Requirement |
| --- | --- |
| Verdict | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE` or `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_BLOCKED`. |
| Source input identity | Recovered input paths, original image paths, modes, sizes, and sha256 values from this packet. |
| Parent image identity | parent image host tag, in-cluster tag, image index digest, and linux/arm64 manifest digest. |
| Build-context/source artifact path | Exact local path of the newly created reviewed startup source/build-context artifact. |
| File delta | Minimal diff summary, expected to be limited to `/defaults/openclaw-agent/config.yaml` unless evidence proves the wrapper must change. |
| Token hygiene | Confirm `OPENCLAW_GATEWAY_TOKEN` appears only as a variable name/runtime token source and no token value is hardcoded. |
| Command evidence | Exact commands used to create/copy/modify the source artifact, with no secret values. |
| Scope negatives | no build/tag/push/pull, no runtime/K8S/database/registry mutation, no browser E2E, no passes:true, no Close. |

## Follow-up Gate Order

If the user approves this packet:

1. `Runtime Startup Artifact Implementation Gate`.
2. `Runtime Image Build/Tag/Push Approval Packet`, then implementation gate if explicitly approved.
3. Isolated `2Gi+` fresh instance approval/implementation gate.
4. Listener/hash verification gate.
5. Browser E2E gate.
6. Only after fresh E2E evidence and explicit user approval: `passes:true`, Close, and longterm write-back may be considered.

If the user rejects or defers this packet, A1 remains `A1_SOURCE_IMPLEMENTATION_BLOCKED` at the runtime startup artifact implementation blocker.

## Approval Decision Needed

User decision requested:

- Approve: authorize the Runtime Startup Artifact Implementation Gate under this packet's exact file scope, artifact scope, minimal startup command plan, and prohibitions.
- Reject or defer: do not create or modify startup source/build-context artifacts; keep runtime startup artifact implementation blocked.

## Verification Results

Required verification commands:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md
rg -n "RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_DONE|RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|/tmp/gtclaw-startup-source-inspect-y5sCboQ2|--bind lan|--auth token|OPENCLAW_GATEWAY_TOKEN|parent image|sha256|no hardcode|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md
```

Verification results:

| Command | Result |
| --- | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md` | Exit `0`; no whitespace diagnostics. |
| `rg -n "RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_DONE|RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|/tmp/gtclaw-startup-source-inspect-y5sCboQ2|--bind lan|--auth token|OPENCLAW_GATEWAY_TOKEN|parent image|sha256|no hardcode|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md` | Exit `0`; required markers found. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md` | Shows this approval packet and the recovery evidence as untracked in the path-limited status. |
