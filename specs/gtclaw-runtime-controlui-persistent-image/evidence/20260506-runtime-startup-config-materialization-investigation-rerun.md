# Runtime Startup Config Materialization Investigation Rerun - 2026-05-06

Worker: RuntimeStartupConfigMaterializationInvestigationRerunWorker
Verdict: `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_DONE`

Not `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_BLOCKED`: the recovered `openclaw-agent` binary and recovered default config are sufficient to prove the agent config-read path, the `openclaw_config_path` materialization behavior, and the absence of a safe YAML field for `gateway.controlUi.allowedOrigins`.

This was a read-only rerun investigation plus this single new evidence file. There was no implementation and no recovered-file, runtime-startup-artifact, backend, frontend, deployment, docs, longterm, AgentTeam, spec, plan, tasks, or existing-evidence modification.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Runtime Config Source Artifact Recovery | `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_DONE` |
| Prior Materialization Investigation | `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED` |
| Prior Origin Allowlist Implementation | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED` |

Recovered root used:

`/tmp/gtclaw-config-materialization-inspect-xLFBgllN`

No image export/extraction, docker create/cp/rm/build/tag/push/pull, runtime service execution, Kubernetes mutation, database mutation, registry mutation, fresh instance, browser E2E, Chrome DevTools MCP, or Playwright was performed by this rerun.

## Exact Recovered Files Analyzed

| Recovered path | Image source path | Finding |
| --- | --- | --- |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent` | `/usr/local/bin/openclaw-agent` | ELF 64-bit ARM aarch64 statically linked Go executable, with debug info, not stripped; sha256 `9f53969a3dcf4a1c4b596f54fcc724364d6f632dca7605253d0877f691e95b28`; size `20266626`; mode `0755`. |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | Agent default YAML; sha256 `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3`; size `745`; mode `0644`. |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/etc/openclaw-agent/config.yaml` | `/etc/openclaw-agent/config.yaml` | Missing in image. |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/config/.openclaw/openclaw.json` | `/config/.openclaw/openclaw.json` | Missing in image. |

The recovered default config contains:

```yaml
openclaw_command:
  - openclaw
  - gateway
  - run
openclaw_config_path: /config/.openclaw/openclaw.json
```

The reviewed startup artifact later adds `--bind lan --auth token` to `openclaw_command`; the recovered image default above predates that artifact change.

## Binary Identity

| Command | Key result |
| --- | --- |
| `file /tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent` | ELF 64-bit LSB executable, ARM aarch64, statically linked, Go BuildID present, with debug_info, not stripped. |
| `go version -m /tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent` | Go `go1.26.2`; module path `github.com/iamlovingit/clawmanager-openclaw-image/cmd/openclaw-agent`; module `github.com/iamlovingit/clawmanager-openclaw-image`; `CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=arm64`. |
| `go tool nm` targeted scan | Exposes `internal/config.Load`, `internal/configmanager.(*Manager).ApplyRevision`, `internal/configmanager.validateJSON`, `internal/configmanager.verifyChecksum`, `internal/control.(*Client).FetchConfigRevision`, and `internal/process.(*Manager).Start`. |

## Key String Findings

Targeted `strings` / `rg -a` scans of the recovered binary found these relevant literals:

- `/etc/openclaw-agent/config.yaml`
- `OPENCLAW_AGENT_CONFIG_PATH`
- `openclaw_config_path`
- `OpenClawConfigPath`
- `yaml:"openclaw_config_path"`
- `OPENCLAW_AGENT_OPENCLAW_CONFIG_PATH`
- `/config/.openclaw/openclaw.json`
- `openclaw_command`
- `OpenClawCommand`
- `yaml:"openclaw_command"`
- `ConfigPath`
- `ApplyRevision`
- `ConfigRevision`
- `/api/v1/agent/config/revisions`
- `config-staging`
- `config-backup`
- `.tmp`
- `.bak`
- `active_path`
- `previous_path`
- `disabled_path`

Targeted negative scan:

- No `allowedOrigins`, `gateway.controlUi`, `controlUi`, `control_ui`, `allowed_origins`, or `CONTROL_UI_ORIGIN` marker was found in the recovered binary strings.
- No `allowedOrigins`, `gateway.controlUi`, `controlUi`, `control_ui`, or `allowed_origins` marker exists in the recovered default config.

## Key Symbol And Objdump Findings

### Agent config read path

`go tool objdump -S -s 'github.com/iamlovingit/clawmanager-openclaw-image/internal/config.Load'` shows:

- source path `/src/internal/config/config.go`
- call to `os.Getenv`
- call to `os.ReadFile`
- call to `gopkg.in/yaml.v3.unmarshal`
- repeated calls to `internal/config.overrideStringAny`

Together with strings for `OPENCLAW_AGENT_CONFIG_PATH` and `/etc/openclaw-agent/config.yaml`, this proves `openclaw-agent` reads its YAML config from the env-selected path or the default `/etc/openclaw-agent/config.yaml`.

### Config struct field evidence

Strings include `*config.Config`, field-like names `OpenClawCommand`, `OpenClawConfigPath`, `OpenClawWorkspacePath`, `OpenClawSkillsPath`, and YAML tags including `yaml:"openclaw_command"` and `yaml:"openclaw_config_path"`.

The `config.Load` objdump unmarshals YAML into the config object and then applies env overrides. Therefore `openclaw_config_path` is a typed `Config` field, not an arbitrary pass-through YAML subtree.

### Config revision fetch

`go tool objdump -S -s 'internal/control.*FetchConfigRevision'` shows:

- source path `/src/internal/control/client.go`
- `FetchConfigRevision`
- call to `path.Join`
- call to `internal/control.(*Client).doJSON`

Strings include `/api/v1/agent/config/revisions`. This proves runtime config content is fetched as a config revision through the agent control-plane API, not read from a default `/config/.openclaw/openclaw.json` image file.

### ApplyRevision materialization

`go tool objdump -S -s 'ApplyRevision'` shows:

- source path `/src/internal/configmanager/manager.go`
- `internal/configmanager.(*Manager).ApplyRevision`
- call to `verifyChecksum`
- call to `validateJSON`
- call to `path/filepath.join`
- call to `os.MkdirAll`
- call to `os.WriteFile`
- call to `os.ReadFile`
- call to `os.rename` / `os.Rename`
- call to `internal/store.(*Store).Update`

The relevant call sequence is:

1. Verify the revision checksum.
2. Validate the revision content as JSON.
3. Create staging/backup directories.
4. Write revision content to a staging `.tmp` path with `os.WriteFile`.
5. Ensure the active config path directory exists.
6. Read the previous active config with `os.ReadFile` when present.
7. Write the previous active config to a backup path with `os.WriteFile`.
8. Atomically replace the active config by renaming the staged file to the configured active path with `os.Rename` / `os.rename`.
9. Update local store state with fields including revision/current path state.

This proves `openclaw_config_path` is the final active OpenClaw JSON config path used by configmanager. `os.ReadFile` and `os.Rename` target the active path. `os.WriteFile` writes revision content to a staging path and writes prior content to backup; the active path is updated through rename, not by direct final-path WriteFile.

### OpenClaw process start

`go tool objdump -S -s 'internal/process.*Start'` shows:

- source path `/src/internal/process/manager.go`
- call to `os/exec.CommandContext`
- call to `os/exec.(*Cmd).Start`

`go tool objdump -S -s 'Supervisor.*Run'` shows `internal/process.(*Manager).Start` is called, and `internal/configmanager.(*Manager).ApplyRevision` is also part of the supervisor run path.

No objdump or string evidence shows `openclaw_config_path` being appended as an explicit command-line argument to `openclaw gateway run`. The recovered default `openclaw_command` is only `openclaw gateway run`, and the reviewed artifact command is `openclaw gateway run --bind lan --auth token`.

## Required Answers

| Question | Answer |
| --- | --- |
| Does `openclaw-agent` read `/etc/openclaw-agent/config.yaml`? | Yes. Binary strings contain `/etc/openclaw-agent/config.yaml` and `OPENCLAW_AGENT_CONFIG_PATH`; `internal/config.Load` objdump shows `os.Getenv`, `os.ReadFile`, and YAML unmarshal. The reviewed run wrapper copies defaults to `/etc/openclaw-agent/config.yaml` when absent and then executes `/usr/local/bin/openclaw-agent`. |
| Is `openclaw_config_path` a Config struct field? | Yes. Strings contain `*config.Config`, `OpenClawConfigPath`, and `yaml:"openclaw_config_path"`; `config.Load` unmarshals YAML into that config object and applies env overrides. |
| Is `openclaw_config_path` used by configmanager? | Yes. The config manager has `ApplyRevision`; objdump shows it writes staged JSON, reads the active config path, backs up previous content, and renames staged content into the active path. The binary also carries `ConfigPath`, `active_path`, `previous_path`, and config-revision markers. |
| Is it used as an `os.WriteFile`, `os.ReadFile`, or `os.Rename` target? | It is the final active target for `os.ReadFile` and `os.Rename` / `os.rename`. Revision JSON is first written to staging with `os.WriteFile`; old active content is written to backup with `os.WriteFile`; the active config is updated by rename into `openclaw_config_path`. |
| Is it passed to `openclaw gateway run`? | Not as an explicit recovered command argument. The agent starts the configured `openclaw_command` with `os/exec.CommandContext`; recovered and reviewed commands do not include a config-path flag. |
| How is `/config/.openclaw/openclaw.json` created or updated? | It is created/updated by `configmanager.ApplyRevision` from control-plane config revision content fetched through `/api/v1/agent/config/revisions`. The image does not contain an existing `/config/.openclaw/openclaw.json` file. |
| Is creation from config revisions, default/template, manager env payload, or uncertain? | Proven runtime-side mechanism: config revisions. No default/template JSON file was recovered from the image. Manager env payloads are upstream control-plane inputs from prior evidence, but this binary proof does not show direct env-payload-to-file materialization; it shows revision-to-file materialization. |
| Does a safe YAML field exist to materialize `gateway.controlUi.allowedOrigins`? | No. The recovered binary and recovered default config do not contain `allowedOrigins`, `gateway.controlUi`, `controlUi`, `control_ui`, `allowed_origins`, or `CONTROL_UI_ORIGIN`. Adding `gateway.controlUi.allowedOrigins` directly to the agent YAML is not a safe YAML field path and may be ignored by the typed agent config. |
| What is the minimum compliant implementation path if no safe YAML field exists? | Do not add `gateway.controlUi.allowedOrigins` to `config.yaml` as an agent YAML field. The minimum startup-artifact path is JSON materialization at `/config/.openclaw/openclaw.json` before the OpenClaw gateway process starts, preferably by a narrowly scoped wrapper/command-wrapper that seeds or merges the `gateway.controlUi.allowedOrigins` JSON value without hardcoding secrets and without overwriting existing unknown config. If the implementation must survive all later config-revision overwrites and process restarts, the safer path is to recover/modify the source that creates config revisions or the agent configmanager merge behavior. |
| Is external expert escalation required now? | Not required to answer materialization behavior. It is reasonable only if Commander rejects a startup-artifact JSON seed/merge because of lifecycle risk around later config revisions and process restarts. |
| Can the Runtime Startup Artifact Origin Allowlist Implementation Gate be rerun safely? | Yes, conditionally: it is safe to rerun only if the gate is constrained to JSON materialization and explicitly forbids the unsafe YAML-field implementation. It is not safe to rerun an unconstrained gate that might add `gateway.controlUi.allowedOrigins` directly to the agent YAML. |

## Safe And Unsafe Implementation Conclusion

Unsafe:

- modifying `defaults/openclaw-agent/config.yaml` by adding a nested `gateway.controlUi.allowedOrigins` YAML field to the agent config
- assuming unknown YAML keys are consumed by OpenClaw gateway config
- assuming `/config/.openclaw/openclaw.json` exists in the image
- assuming manager env payloads directly write the runtime config file
- backend Origin rewrite

Conditionally safe for the next implementation gate:

- implement explicit JSON materialization for `/config/.openclaw/openclaw.json` with `gateway.controlUi.allowedOrigins` containing `https://localhost:30443`
- keep backend Origin behavior unchanged
- keep `openclaw gateway run --bind lan --auth token`
- avoid secret values and token-bearing URLs
- record the config-revision overwrite/restart residual risk unless the implementation also proves the revision content or agent merge path preserves the allowlist

## Recommended Next Gate

Recommended next gate:

`Runtime Startup Artifact Origin Allowlist Implementation Gate`

Required constraints for that gate:

- no unsafe YAML field implementation
- implement only startup-artifact JSON materialization or stop
- if the gate cannot materialize JSON without lifecycle risk, stop and write a `Runtime Config Source Expert Escalation` packet
- if a source-level revision merge is required, stop and request an `Additional Source Recovery Approval Packet`

`Runtime Config Source Expert Escalation` is not the immediate recommendation because the recovered binary is sufficient to prove the materialization path. It remains the fallback if Commander wants an external design decision on wrapper seeding versus source-level revision merge.

## Explicit Negatives

- no implementation
- no runtime startup artifact modification
- no recovered file modification
- no backend Origin rewrite
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no build/deploy
- no build/tag/push/pull
- no image export/extraction
- no docker create/cp/rm/build/tag/push/pull
- no OpenClaw runtime service process run
- no `/usr/local/bin/openclaw-agent` execution
- no K8S/runtime/database/registry mutation
- no fresh instance
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-rerun.md
rg -n "RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_DONE|RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_BLOCKED|openclaw-agent|/etc/openclaw-agent/config.yaml|openclaw_config_path|/config/.openclaw/openclaw.json|ApplyRevision|os.WriteFile|os.Rename|gateway.controlUi.allowedOrigins|safe YAML field|no implementation|no build/deploy|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-rerun.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-rerun.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-rerun.md` | `0` | No whitespace diagnostics. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_DONE`, `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_BLOCKED`, `openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `openclaw_config_path`, `/config/.openclaw/openclaw.json`, `ApplyRevision`, `os.WriteFile`, `os.Rename`, `gateway.controlUi.allowedOrigins`, `safe YAML field`, `no implementation`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `1` | No matches. Matched values were suppressed by using count-only matching; the first attempted scan had a shell quoting error and was rerun successfully. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-config-materialization-investigation-rerun.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
