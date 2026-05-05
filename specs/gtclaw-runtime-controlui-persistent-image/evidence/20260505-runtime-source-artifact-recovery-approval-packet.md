# Runtime Source Artifact Recovery Approval Packet - 2026-05-05

## Verdict

`RUNTIME_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`

This packet requests explicit user approval for the next gate:

`Do you approve a Runtime Source Artifact Recovery Gate that creates a new /tmp/gtclaw-startup-source-inspect-* workspace and performs read-only extraction of only the two OpenClaw startup files listed below from the specified local image?`

This approval packet performed no read-only extraction, no Docker export/save/cp, no container create/cp, no image extraction, no build/tag/push/pull, no runtime image/resource mutation, no Kubernetes write, no database write, no registry mutation, no browser E2E, no Chrome DevTools MCP, no Playwright, no fresh instance mutation, no Mem0 write, no longterm write-back, no passes:true, and no Close.

## Dependency Gates

| Gate | Current state |
| --- | --- |
| WS Auth Bridge Implementation Gate | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; Commander re-verified focused tests and `go test -count=1 ./internal/services ./internal/handlers` passed. |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED`; remaining blocker is missing approved runtime startup source/build-context. |
| Runtime Startup / WebSocket Auth Contract Investigation | `RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED`; startup files were identified only inside image/pod context, not as approved source. |

## Candidate Parent Image

The recommended recovery parent is the already verified persistent runtime image from existing evidence. This packet did not run Docker metadata inspect because the next operation requires user approval and the existing evidence already records the immutable identity.

| Item | Value |
| --- | --- |
| Host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| In-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| Image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |
| Evidence source | `20260504-image-delivery-loop-rerun-after-baseline-decision.md`, corroborated by later fresh-instance/browser approval evidence. |

Rationale:

- The image is the latest verified persistent artifact for this feature.
- It was built from the approved base plus the four control-ui static files only.
- Startup files should represent the runtime startup layout that must be reviewed before any `gateway.bind=lan`, `--bind lan`, `--auth token`, or equivalent pod-facing bind change.

## Files Requested For Recovery

If approved, the next recovery gate may extract only these files:

| File | Purpose |
| --- | --- |
| `/defaults/openclaw-agent/config.yaml` | Candidate OpenClaw gateway startup/config source. Prior evidence says it starts `openclaw gateway run` through `openclaw_command`. |
| `/etc/services.d/openclaw-agent/run` | Candidate service startup script. Prior evidence says it copies default config if absent and starts `openclaw-agent`. |

No other image files, directories, package trees, control-ui assets, secrets, env files, or runtime state may be extracted.

## Required Output For The Future Recovery Gate

The next recovery gate, if explicitly approved by the user, must write a separate evidence file and record all of the following for each recovered file:

| Required field | Requirement |
| --- | --- |
| Parent image identity | Record parent image tag, image index digest, linux/arm64 manifest digest, and platform. |
| Extracted file path | Record the new local path under `/tmp/gtclaw-startup-source-inspect-*`. |
| Original file path | Record the exact original path from the image. |
| File mode | Record mode such as `0644` or executable bits if present. |
| Owner metadata | Record owner/group metadata if available from the read-only extraction method. |
| `sha256` | Record SHA-256 for each extracted file. |
| Byte size | Record byte size for each extracted file. |
| Exact command | Record the exact read-only extraction command used, with no secret values. |
| Secret hygiene | Confirm no token value, cookie value, credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or access URL was printed or written. |

Future command shape to record if approved, not executed in this packet:

```bash
OUT="$(mktemp -d /tmp/gtclaw-startup-source-inspect-XXXXXXXX)"
# Create or inspect a local container/image only for read-only file copy from the approved parent image.
# Copy only:
#   /defaults/openclaw-agent/config.yaml
#   /etc/services.d/openclaw-agent/run
# Then record mode, owner if available, sha256, byte size, parent image identity, and command evidence.
```

The recovery gate must stop after extraction and classification. It must not modify extracted files and must not build an image.

## Explicitly Forbidden In The Future Recovery Gate Unless Separately Approved

- Extracting any path other than `/defaults/openclaw-agent/config.yaml` and `/etc/services.d/openclaw-agent/run`.
- Modifying extracted files.
- Deleting old `/tmp` workspaces or old project assets.
- Treating the extracted image filesystem as final source without review.
- build/tag/push/pull.
- Runtime image/resource setting mutation.
- Kubernetes write.
- Database write.
- Registry mutation.
- Fresh instance create/delete/modify.
- Browser E2E, Chrome DevTools MCP, or Playwright.
- Manual pod patch, manual Service patch, or `kubectl cp` write.
- Writing secrets, token values, cookie values, credentials, or access URLs into evidence.
- Mem0 write.
- longterm write-back.
- passes:true.
- Close.

## Required Classification After Recovery

The future recovery gate must answer:

1. Are `/defaults/openclaw-agent/config.yaml` and `/etc/services.d/openclaw-agent/run` sufficient to create a reviewed startup source artifact?
2. Which exact file contains the minimal startup change for pod-facing bind/auth?
3. Is the expected change limited to `gateway.bind=lan`, `--bind lan`, `--auth token`, `OPENCLAW_GATEWAY_TOKEN`, or an equivalent control-ui gateway setting?
4. Is any additional source/build-context needed before Runtime Startup Artifact Implementation?
5. Should the next gate be Runtime Startup Artifact Implementation Approval, another source recovery gate, or BLOCKED?

## Recommended Follow-Up Gate Order

If the user approves this packet:

1. `Runtime Source Artifact Recovery Gate`.
2. `Runtime Startup Artifact Implementation Approval Packet` or `Runtime Startup Artifact Implementation Gate`, depending on recovery findings and user approval.
3. Isolated fresh instance gate with sufficient capacity.
4. Listener/hash verification gate.
5. Browser E2E gate.

Without explicit user approval for this packet, runtime startup artifact work remains blocked.

## Approval Decision Needed

User decision requested:

- Approve: authorize the `Runtime Source Artifact Recovery Gate` to create a new `/tmp/gtclaw-startup-source-inspect-*` workspace and perform read-only extraction of only the two startup files from the candidate parent image above.
- Reject or defer: do not extract startup files; keep Runtime Startup Artifact Implementation blocked.

## Verification Results

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md
```

Result: exit `0`; no whitespace errors.

```bash
rg -n "RUNTIME_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE|RUNTIME_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|/tmp/gtclaw-startup-source-inspect-|read-only extraction|parent image|sha256|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md
```

Result: exit `0`; required markers found.

```bash
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```

Result: exit `0`; status output:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery-approval-packet.md
?? specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
```
