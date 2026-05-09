# Runtime Startup Artifact Origin Allowlist Implementation Gate - 2026-05-06

Worker: RuntimeStartupArtifactOriginAllowlistImplementationWorker
Verdict: `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED`

Not `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_DONE`: this gate did not modify the reviewed startup artifact because the approved evidence and artifact do not safely prove how to materialize `gateway.controlUi.allowedOrigins` into `/config/.openclaw/openclaw.json`.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Control UI Origin Allowlist Root Cause | `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_DONE` |
| Runtime Startup Artifact Origin Allowlist Implementation Approval Packet | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_DONE`; user approved `APPROVE_RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_GATE` |

Confirmed root-cause inputs:

- browser `Origin`: `https://localhost:30443`
- mediated route shape: `wss://localhost:30443/api/v1/instances/10/control-ui`
- current reviewed startup artifact lacks `gateway.controlUi.allowedOrigins`
- backend Origin rewrite is not recommended

## Reviewed Artifact

Artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

Reviewed files:

- `defaults/openclaw-agent/config.yaml`
- `etc/services.d/openclaw-agent/run`
- `Dockerfile`
- `MANIFEST.md`

The current artifact preserves the existing gateway command:

```yaml
openclaw_command:
  - openclaw
  - gateway
  - run
  - --bind
  - lan
  - --auth
  - token
openclaw_config_path: /config/.openclaw/openclaw.json
```

## Blocked Reason

The implementation target is clear: OpenClaw gateway config semantic value `gateway.controlUi.allowedOrigins` must include `https://localhost:30443`.

The safe materialization mechanism is not proven in the approved source set:

- `defaults/openclaw-agent/config.yaml` shows the agent config field `openclaw_config_path: /config/.openclaw/openclaw.json`, but it does not show a schema key that would be consumed as OpenClaw JSON config content.
- `etc/services.d/openclaw-agent/run` only copies `/defaults/openclaw-agent/config.yaml` to `/etc/openclaw-agent/config.yaml` when absent, then executes `/usr/local/bin/openclaw-agent`.
- The approved recovered startup source artifact contains only the two startup files above, not the `openclaw-agent` implementation or a default `/config/.openclaw/openclaw.json` template.
- Adding an unverified YAML key such as `gateway.controlUi.allowedOrigins` to the agent config could be ignored by `openclaw-agent`.
- Directly writing `/config/.openclaw/openclaw.json` from the wrapper would create a new materialization behavior without proof that it matches the agent lifecycle or will not be overwritten by the agent/config-revision flow.

Therefore this gate stopped before editing the artifact. This follows the approval packet requirement to block rather than guess if the reviewed artifact cannot safely materialize the value.

## Changed Files

No runtime startup artifact file was changed.

| Path | Before mode | After mode | Before bytes | After bytes | Before sha256 | After sha256 | Change |
| --- | --- | --- | ---: | ---: | --- | --- | --- |
| `runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml` | `0644` | `0644` | `785` | `785` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` | unchanged |
| `runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run` | `0755` | `0755` | `289` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` | unchanged |
| `runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md` | `0644` | `0644` | `2917` | `2917` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` | unchanged |
| `runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile` | `0644` | `0644` | `323` | `323` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` | unchanged |

Only this new evidence file was written.

## Diff Summary

No artifact diff was produced because this gate blocked before modifying `runtime-startup-artifact/20260505-bind-lan-auth/**`.

Semantic target remains:

- preserve `openclaw gateway run --bind lan --auth token`
- materialize `/config/.openclaw/openclaw.json` with `gateway.controlUi.allowedOrigins` containing `https://localhost:30443`
- no backend Origin rewrite

## Secret Hygiene

No token value, cookie value, credential, secret, registry credential, access URL plaintext, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was written.

The string `--auth token` appears only as the OpenClaw CLI auth mode flag.

## Recommended Next Gate

Recommended next gate:

`Runtime Startup Config Materialization Investigation Approval Packet`

That gate should request a narrow read-only approval to inspect the `openclaw-agent` config schema or implementation path that writes `/config/.openclaw/openclaw.json`. It should answer exactly which reviewed startup artifact field or file is safe to change so that `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`.

After that proof exists, rerun `Runtime Startup Artifact Origin Allowlist Implementation Gate`, then proceed to `Runtime Image Build/Tag/Push Approval Packet` only if implementation succeeds.

## Explicit Negatives

- no backend Origin rewrite
- no backend modification
- no frontend modification
- no unrelated runtime source modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no recovered `/tmp/gtclaw-startup-source-inspect-*` modification
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
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

Required checks for this implementation evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md
rg -n "RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_DONE|RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED|gateway.controlUi.allowedOrigins|https://localhost:30443|/config/.openclaw/openclaw.json|--bind lan|--auth token|no backend Origin rewrite|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md
rg -n "allowedOrigins|localhost:30443|--bind|--auth|openclaw_config_path|gateway|controlUi" specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth
```

Also required:

- secret-shape scan on changed artifact files plus this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md` | `0` | No whitespace errors. |
| required evidence marker `rg` scan | `0` | Required markers found, including `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_DONE`, `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED`, `gateway.controlUi.allowedOrigins`, `https://localhost:30443`, `/config/.openclaw/openclaw.json`, `--bind lan`, `--auth token`, `no backend Origin rewrite`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| startup artifact marker `rg` scan | `0` | Existing startup artifact markers found, including `gateway`, `--bind`, `--auth`, and `openclaw_config_path`. No `allowedOrigins` artifact marker is expected because this gate blocked before artifact modification. |
| secret-shape scan on startup artifact plus this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
