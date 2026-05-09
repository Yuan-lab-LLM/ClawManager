# Runtime Startup Config Materialization Investigation Gate - 2026-05-06

Worker: RuntimeStartupConfigMaterializationInvestigationWorker
Verdict: `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED`

Not `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_DONE`: repo and reviewed-artifact read-only evidence do not prove how `openclaw-agent` materializes `/config/.openclaw/openclaw.json`, and this gate is not authorized to perform image export/extraction. Do not rerun the Runtime Startup Artifact Origin Allowlist Implementation Gate yet.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Runtime Startup Config Materialization Investigation Approval Packet | `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_APPROVAL_PACKET_DONE`; user approved `APPROVE_RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_GATE` |
| Runtime Startup Artifact Origin Allowlist Implementation Gate | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED` |

Blocked reason carried forward:

- reviewed startup artifact proves only `openclaw_config_path: /config/.openclaw/openclaw.json`
- reviewed startup artifact does not prove which `openclaw-agent` YAML field materializes OpenClaw JSON config
- directly adding `gateway.controlUi.allowedOrigins` may be ignored
- directly writing `/config/.openclaw/openclaw.json` from the run wrapper may conflict with agent lifecycle or config-revision behavior

## Scope Performed

This was a read-only investigation plus this single evidence write.

No image export/extraction was performed. No K8S read/log command was needed or run.

## Source And Artifact Paths Inspected

Project rules and feature gates:

- `AGENTS.md`
- `.specify/memory/constitution.md`
- `backend/AGENTS.md`
- `specs/gtclaw-runtime-controlui-persistent-image/spec.md`
- `specs/gtclaw-runtime-controlui-persistent-image/plan.md`
- `specs/gtclaw-runtime-controlui-persistent-image/tasks.md`

Current feature evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-approval-packet.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-origin-allowlist-root-cause.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-source-artifact-recovery.md`

Reviewed startup artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md`

Repo source searched/read for related control-plane config flow:

- `backend/internal/services/openclaw_config_service.go`
- `backend/internal/services/instance_agent_service.go`
- `backend/internal/services/instance_config_revision_service.go`
- `backend/internal/services/instance_command_service.go`
- `backend/internal/services/instance_service.go`
- `backend/internal/services/k8s/pod_service.go`
- `backend/internal/services/k8s/client.go`
- `backend/internal/models/openclaw_config.go`
- `backend/internal/models/instance_desired_state.go`
- `backend/internal/models/instance_runtime_status.go`
- repo-wide `rg` searches for `openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `openclaw_config_path`, `/config/.openclaw/openclaw.json`, `gateway.controlUi.allowedOrigins`, `allowedOrigins`, and `controlUi`

Local already-existing image metadata inspected, without export/extraction:

- `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506`
- metadata result: repo digest `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`, platform `linux/arm64`, entrypoint `["/init"]`, cmd `null`, working dir `/`, user empty

## Evidence Summary

Reviewed artifact evidence:

- `etc/services.d/openclaw-agent/run` creates `/var/lib/openclaw-agent`, `/var/log/openclaw-agent`, and `/etc/openclaw-agent`.
- If `/etc/openclaw-agent/config.yaml` is absent, the run wrapper copies `/defaults/openclaw-agent/config.yaml` to `/etc/openclaw-agent/config.yaml`.
- The wrapper then executes `/usr/local/bin/openclaw-agent` with no command-line config argument.
- `defaults/openclaw-agent/config.yaml` contains `openclaw_config_path: /config/.openclaw/openclaw.json`, `openclaw_command`, and the existing `openclaw gateway run --bind lan --auth token` command.
- The reviewed artifact contains no `gateway.controlUi.allowedOrigins`.

Repo source evidence:

- ClawManager backend creates OpenClaw bootstrap payload environment variables such as `CLAWMANAGER_OPENCLAW_BOOTSTRAP_MANIFEST_JSON`, `CLAWMANAGER_OPENCLAW_CHANNELS_JSON`, `CLAWMANAGER_OPENCLAW_SKILLS_JSON`, and related resource payloads in `backend/internal/services/openclaw_config_service.go`.
- ClawManager can write those rendered env payloads into a Kubernetes Secret through `EnsureSnapshotSecret`, and pods can import Secret env values through `EnvFromSecretNames`.
- ClawManager config revision APIs publish a rendered manifest into `instance_config_revisions` and set `desired_config_revision_id` through `apply_config_revision`.
- ClawManager agent heartbeat responses return `desired_config_revision_id`.
- These backend paths define manager-to-agent control-plane data, but they do not contain the runtime `openclaw-agent` implementation that reads `/etc/openclaw-agent/config.yaml` or writes `/config/.openclaw/openclaw.json`.

Image metadata evidence:

- Metadata-only image inspect confirms the parent image exists locally and uses `/init`.
- Image metadata does not reveal the `openclaw-agent` schema, config file parsing behavior, or `/config/.openclaw/openclaw.json` materialization behavior.

## Required Answers

| Question | Answer |
| --- | --- |
| Does `openclaw-agent` read `/etc/openclaw-agent/config.yaml`? | Not proven by repo or reviewed-artifact source. The run wrapper copies `/defaults/openclaw-agent/config.yaml` to `/etc/openclaw-agent/config.yaml` and then executes `/usr/local/bin/openclaw-agent`, which strongly implies the path is the intended agent config path. However, the actual `openclaw-agent` implementation is not present in repo source or the reviewed artifact, so direct read behavior is unverified. |
| How is `openclaw_config_path` used? | Not proven. The reviewed YAML defines it as `/config/.openclaw/openclaw.json`, but repo search found no implementation reference that creates, merges, writes, or passes this path to `openclaw gateway run`. The reviewed `openclaw_command` itself does not include a config-path argument. |
| Is `openclaw_config_path` just a path? | It is only proven to be a YAML field containing a path. No source evidence proves the runtime semantics. |
| Does it create the file? | Not proven. No repo or reviewed artifact source creates `/config/.openclaw/openclaw.json`. |
| Does it merge defaults? | Not proven. No repo or reviewed artifact source shows merge logic for OpenClaw JSON config defaults. |
| Does it write OpenClaw JSON config? | Not proven. The manager backend can render bootstrap env payloads and config revision manifests, but the runtime agent code that would write OpenClaw JSON is absent. |
| Is it passed to `openclaw gateway run`? | Not by the reviewed `openclaw_command`. The command is `openclaw gateway run --bind lan --auth token`; there is no explicit config-path argument in the reviewed artifact. |
| Does repo/artifact contain a `/config/.openclaw/openclaw.json` template/default source? | No template/default source was found in the repo or reviewed artifact search. Only references to the path were found. |
| Is there a safe YAML field that can materialize `gateway.controlUi.allowedOrigins`? | No safe field was found. Adding `gateway.controlUi.allowedOrigins` directly to the agent YAML remains unproven and could be ignored. |
| If no safe YAML field exists, what is the minimum compliant implementation path? | Do not modify `config.yaml` or `run` yet. The minimum compliant path is to request additional runtime config source artifact recovery, because the missing source is the `openclaw-agent` schema/materialization implementation or a default config/template source from the image. |
| Is external expert escalation needed? | Not yet. The next local step is source artifact recovery. Escalate only if recovered source/binary evidence still cannot establish a safe materialization path. |
| Is user approval for image export/extraction needed? | Yes, if the team wants to answer materialization behavior from the runtime image. This gate did not authorize image export/extraction. A new approval packet should authorize extraction only into a new `/tmp/gtclaw-config-materialization-inspect-*` workspace and forbid deleting old `/tmp` assets. |

## Conclusion

It is not safe to rerun Runtime Startup Artifact Origin Allowlist Implementation Gate from the current evidence.

Reason: the investigation did not find a proven YAML field or source path that can materialize `gateway.controlUi.allowedOrigins` into `/config/.openclaw/openclaw.json`. The implementation would still be guessing.

## Recommended Next Gate

Recommended next gate:

`Runtime Config Source Artifact Recovery Approval Packet`

That packet should request explicit approval for image export/extraction into a new `/tmp/gtclaw-config-materialization-inspect-*` workspace, with a narrow target of recovering or inspecting only the runtime-side sources or artifacts needed to prove:

- whether `/usr/local/bin/openclaw-agent` reads `/etc/openclaw-agent/config.yaml`
- how `openclaw_config_path` is used
- whether a default/template source exists for `/config/.openclaw/openclaw.json`
- whether any supported agent config field can materialize `gateway.controlUi.allowedOrigins`

Do not proceed to Runtime Startup Artifact Origin Allowlist Implementation Gate until that proof is available.

## Explicit Negatives

- no implementation
- no runtime startup artifact modification
- no backend modification
- no frontend modification
- no runtime source modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no backend Origin rewrite
- no image export/extraction
- no build/tag/push/pull
- no build/deploy
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no K8S read/log command
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this investigation evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation.md
rg -n "RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_DONE|RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED|openclaw-agent|/etc/openclaw-agent/config.yaml|openclaw_config_path|/config/.openclaw/openclaw.json|gateway.controlUi.allowedOrigins|image export/extraction|/tmp/gtclaw-config-materialization-inspect-|no implementation|no build/deploy|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_DONE`, `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED`, `openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `openclaw_config_path`, `/config/.openclaw/openclaw.json`, `gateway.controlUi.allowedOrigins`, `image export/extraction`, `/tmp/gtclaw-config-materialization-inspect-`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation.md` | `0` | Shows this new evidence file as untracked. |
