# Runtime Startup Config Materialization Investigation Approval Packet

Date: 2026-05-06
Worker: RuntimeStartupConfigMaterializationInvestigationApprovalPacketWorker
Verdict: `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_DONE`

Not `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_BLOCKED`: the prior implementation gate produced a clear blocked reason and a narrow read-only investigation is the next required gate. This packet does not authorize that investigation by itself.

## Approval Request

Please approve or reject whether a future worker may execute the Runtime Startup Config Materialization Investigation Gate.

Recommended approval options:

- `APPROVE_RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_GATE`: allow the future investigation gate with the exact read-only scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not run the investigation; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gate:

- `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED`

Blocked reason used by this packet:

- the reviewed startup artifact only proves `openclaw_config_path: /config/.openclaw/openclaw.json`
- it does not prove which `openclaw-agent` YAML field materializes OpenClaw JSON config
- directly adding `gateway.controlUi.allowedOrigins` to the agent YAML may be ignored
- directly changing the run wrapper to write JSON may conflict with agent lifecycle or config-revision behavior

Target value remains:

- OpenClaw gateway config semantic value `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- backend Origin behavior remains unchanged

## Future Investigation Allowed Scope

If approved, the future Runtime Startup Config Materialization Investigation Gate may perform read-only investigation only.

Allowed future scope:

- repo-internal source and artifact read-only search for `openclaw-agent` config schema and config materialization behavior
- read-only review of `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/**`
- read-only review of existing evidence needed to preserve the gate chain
- local already-existing image read-only inspect/export only if this packet is approved and the user later approves the investigation gate
- if image extraction is needed after that future approval, extract only into a new `/tmp/gtclaw-config-materialization-inspect-*` directory
- no deletion of old `/tmp` workspaces, old assets, previous extraction directories, or existing project artifacts

This packet itself authorizes no image inspect, no image export, no extraction, and no runtime startup artifact modification.

## Required Future Investigation Answers

The future investigation must answer:

| Question | Required answer shape |
| --- | --- |
| Does `openclaw-agent` read `/etc/openclaw-agent/config.yaml`? | Cite repo source or extracted image source/file evidence, with paths and non-secret snippets only. |
| How is `openclaw_config_path` used? | Explain whether it only points to `/config/.openclaw/openclaw.json`, creates it, updates it, merges defaults into it, or passes it to `openclaw gateway run`. |
| Does a config template/default `/config/.openclaw/openclaw.json` exist? | Identify template/default/source path if present, or record absence with search scope. |
| Is there a safe YAML field that can materialize `gateway.controlUi.allowedOrigins`? | Identify the exact field name and expected shape, or record that none was found. |
| If no safe YAML field exists, what is the minimum compliant implementation path? | Decide whether the next implementation should change `config.yaml`, change `run`, or request additional source artifact recovery. |
| Is external expert escalation needed? | Answer yes/no with reason. If yes, provide a focused ready-to-forward packet without secrets. |

The future investigation must preserve the existing `openclaw gateway run --bind lan --auth token` requirement and must not recommend backend Origin rewrite unless a later separate design gate proves runtime config cannot safely express the mediated origin.

## Future Forbidden Actions

This approval packet and the future investigation gate are forbidden from:

- implementation
- modifying the runtime startup artifact
- modifying backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence
- backend Origin rewrite
- build/tag/push/pull
- backend deploy/restart
- fresh instance creation/deletion/modification
- K8S/runtime/database/registry mutation
- browser E2E
- Chrome DevTools MCP
- Playwright
- manual pod patch
- manual Service patch
- `kubectl cp`
- secrets/token/cookie/access URL plaintext output
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push

Explicit required shorthand for downstream checks:

- no implementation
- no build/deploy
- no browser E2E
- no passes:true
- no Close

## Gate Sequence After Approval

If approved:

1. Runtime Startup Config Materialization Investigation Gate.
2. Rerun Runtime Startup Artifact Origin Allowlist Implementation Gate only if materialization proof is clear.
3. Runtime Image Build/Tag/Push Approval Packet.

Deploy, fresh-instance, listener, and browser E2E approvals remain separate later gates and are not authorized here.

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md
rg -n "RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_DONE|RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_BLOCKED|openclaw-agent|/etc/openclaw-agent/config.yaml|openclaw_config_path|/config/.openclaw/openclaw.json|gateway.controlUi.allowedOrigins|/tmp/gtclaw-config-materialization-inspect-|no implementation|no build/deploy|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md`

## Explicit Negatives

This approval packet performed:

- no investigation
- no extraction
- no image inspect/export
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
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_DONE`, `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_BLOCKED`, `openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `openclaw_config_path`, `/config/.openclaw/openclaw.json`, `gateway.controlUi.allowedOrigins`, `/tmp/gtclaw-config-materialization-inspect-`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md` | `0` | Shows this new evidence file as untracked. |
