# Runtime Config Source Artifact Recovery Approval Packet

Date: 2026-05-06
Worker: RuntimeConfigSourceArtifactRecoveryApprovalPacketWorker
Verdict: `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`

Not `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED`: the prior materialization investigation reached a clear blocked state, and the next required gate is a narrow source/artifact recovery from an already-existing local runtime image. This packet does not authorize that recovery by itself.

## Approval Request

Please approve or reject whether a future worker may execute the Runtime Config Source Artifact Recovery Gate.

Recommended approval options:

- `APPROVE_RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_GATE`: allow the future recovery gate with exactly the image, read-only extraction scope, evidence requirements, and prohibitions below.
- `REJECT_OR_BLOCK`: do not run the recovery; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gate:

- `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED`

Blocked reason carried forward:

- repo and reviewed runtime startup artifact do not include the `openclaw-agent` implementation
- repo and reviewed artifact do not prove how `/etc/openclaw-agent/config.yaml` is read
- repo and reviewed artifact do not prove how `openclaw_config_path` materializes `/config/.openclaw/openclaw.json`
- no safe YAML field was found that can materialize `gateway.controlUi.allowedOrigins`
- additional runtime config source artifact recovery is required before implementation

The target semantic value remains:

- OpenClaw gateway config `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- backend Origin behavior remains unchanged

## Candidate Parent Image

The future recovery gate must prefer this already-existing and previously validated persistent runtime image:

| Field | Value |
| --- | --- |
| parent image tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| known repo digest | `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| platform | `linux/arm64` |

The future gate must not perform image pull/build/push and must not mutate a registry. Required shorthand for downstream checks: no build/tag/push/pull.

## Future Recovery Allowed Scope

If approved, the future Runtime Config Source Artifact Recovery Gate may perform only these actions:

- read-only inspect the existing local parent image metadata
- create a stopped temporary container only if needed for `docker cp` read-only extraction
- extract only to a new `/tmp/gtclaw-config-materialization-inspect-*` directory
- remove only the temporary stopped container created by that same recovery gate
- avoid deleting or modifying old `/tmp` workspaces, old extraction directories, old assets, or existing project artifacts

This packet itself authorizes no image inspect, no image export/extraction, no container creation, no implementation, no artifact modification, no build/deploy, and no browser E2E.

## Future Extraction And Inspection Targets

The future recovery gate must limit extraction and inspection to files needed to prove `openclaw-agent` config materialization behavior:

| Target | Purpose |
| --- | --- |
| `/usr/local/bin/openclaw-agent` behavior/schema, if extractable | Determine whether the agent reads `/etc/openclaw-agent/config.yaml` and how it uses `openclaw_config_path`. |
| `/etc/openclaw-agent/config.yaml`, only if present in the image | Check the image default/current agent config shape without exposing secret values. |
| `/defaults/openclaw-agent/config.yaml` | Compare default agent config with the reviewed startup artifact. |
| any source/template/default related to `/config/.openclaw/openclaw.json` | Prove whether a default OpenClaw JSON config exists and whether it includes or can receive `gateway.controlUi.allowedOrigins`. |
| any docs/schema/help output available without running runtime services | Prove supported config fields without starting OpenClaw runtime service processes. |

If the needed proof requires broader filesystem export/extraction or running a runtime service, the future recovery gate must stop and write blocked evidence instead of widening scope.

## Required Future Recovery Evidence

The future recovery evidence must record:

- parent image tag, known repo digest, and platform
- extracted file paths
- file mode
- owner metadata if available
- sha256 for every extracted file
- byte size for every extracted file
- exact read-only extraction command
- no secret values
- whether recovered binary, source, template, default config, or docs/schema/help evidence is sufficient to answer materialization
- whether `/usr/local/bin/openclaw-agent` appears to read `/etc/openclaw-agent/config.yaml`
- whether `openclaw_config_path` creates, merges, writes, or passes `/config/.openclaw/openclaw.json`
- whether a safe field exists to materialize `gateway.controlUi.allowedOrigins`

Matched secret-shaped values must be suppressed in evidence and verification output.

## Future Forbidden Actions

This approval packet and the future recovery gate are forbidden from:

- running OpenClaw runtime service processes
- build/tag/push/pull
- registry mutation
- K8S/runtime/database/browser mutation
- fresh instance creation/deletion/modification
- browser E2E
- Chrome DevTools MCP
- Playwright
- manual pod patch
- manual Service patch
- `kubectl cp`
- modifying extracted files
- treating the extracted image filesystem as final source without review
- backend Origin rewrite
- implementation
- modifying the runtime startup artifact
- modifying backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence
- secrets/token/cookie/access URL plaintext output
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push

Explicit required shorthand for downstream checks:

- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Gate Sequence After Approval

If approved:

1. Runtime Config Source Artifact Recovery Gate.
2. Runtime Startup Config Materialization Investigation Rerun Gate.
3. Runtime Startup Artifact Origin Allowlist Implementation Gate only if the materialization proof is clear.

Runtime image build/tag/push, deploy/fresh-instance, listener, and browser E2E approvals remain separate later gates and are not authorized here.

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery-approval-packet.md
rg -n "RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE|RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED|openclaw-agent|/usr/local/bin/openclaw-agent|/etc/openclaw-agent/config.yaml|/defaults/openclaw-agent/config.yaml|/config/.openclaw/openclaw.json|/tmp/gtclaw-config-materialization-inspect-|read-only extraction|parent image|sha256|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery-approval-packet.md`

## Explicit Negatives

This approval packet performed:

- no recovery
- no image inspect/export/extraction
- no container creation
- no implementation
- no runtime startup artifact modification
- no backend Origin rewrite
- no build/tag/push/pull
- no build/deploy
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no secret, token, cookie, credential, or access URL plaintext output
- no Mem0 write
- no longterm write-back
- no `passes:true`
- no Close
- no git stage/commit/push

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`, `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_BLOCKED`, `openclaw-agent`, `/usr/local/bin/openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `/defaults/openclaw-agent/config.yaml`, `/config/.openclaw/openclaw.json`, `/tmp/gtclaw-config-materialization-inspect-`, `read-only extraction`, `parent image`, `sha256`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery-approval-packet.md` | `0` | Shows this new evidence file as untracked. |
