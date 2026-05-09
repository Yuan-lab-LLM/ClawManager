# Runtime Startup Artifact Origin Allowlist Implementation Rerun - 2026-05-06

Worker: RuntimeStartupArtifactOriginAllowlistImplementationWorker
Verdict: `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE`

Not `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_BLOCKED`: the materialization rerun proved there is no safe YAML field for `gateway.controlUi.allowedOrigins`, and this rerun implements startup-artifact JSON materialization at `/config/.openclaw/openclaw.json` before the OpenClaw gateway process is execed.

This gate modified only the reviewed runtime startup artifact and wrote only this new evidence file. It did not build, tag, push, pull, deploy, restart, create a fresh instance, mutate Kubernetes, mutate runtime/database/registry state, run browser E2E, run Chrome DevTools MCP, run Playwright, write Mem0, write longterm, set passes:true, run Close, stage, commit, or push.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Runtime Startup Config Materialization Investigation Rerun | `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_RERUN_DONE` |
| Runtime Config Source Artifact Recovery | `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_DONE` |
| Prior Runtime Startup Artifact Origin Allowlist Implementation | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_BLOCKED` |

Materialization proof used:

- `openclaw-agent` reads `/etc/openclaw-agent/config.yaml`.
- `openclaw_config_path` is a typed agent config field.
- `configmanager.ApplyRevision` writes staged JSON and renames it into `/config/.openclaw/openclaw.json`.
- No safe YAML field exists for `gateway.controlUi.allowedOrigins`.
- `/config/.openclaw/openclaw.json` is absent in the image by default.

## Changed Files

Artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

| Path | Change |
| --- | --- |
| `Dockerfile` | Added two `COPY --chmod=0755` entries for startup helper scripts. |
| `MANIFEST.md` | Updated artifact manifest and semantic summary for origin allowlist JSON materialization. |
| `defaults/openclaw-agent/config.yaml` | Prepended `/usr/local/bin/openclaw-gateway-with-origin-allowlist` to `openclaw_command`; kept `openclaw gateway run --bind lan --auth token`. |
| `usr/local/bin/openclaw-ensure-controlui-origin` | Added helper that merges `gateway.controlUi.allowedOrigins` into `/config/.openclaw/openclaw.json`. |
| `usr/local/bin/openclaw-gateway-with-origin-allowlist` | Added wrapper that runs the JSON materialization helper and then execs the original command. |

`etc/services.d/openclaw-agent/run` was inspected and remains unchanged byte-for-byte.

## Before / After Metadata

| Path | Before mode | After mode | Before bytes | After bytes | Before sha256 | After sha256 |
| --- | --- | --- | ---: | ---: | --- | --- |
| `Dockerfile` | `0644` | `0644` | `323` | `561` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` | `c467787d8805fb4305d217bc2efc529d633520e70e9dc5703ece08bed0093303` |
| `MANIFEST.md` | `0644` | `0644` | `2917` | `4010` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` | `02018d85f5aeec3d6c8f45e0f33a2eb3aa44114da6a437950d6bacdf8381ab9a` |
| `defaults/openclaw-agent/config.yaml` | `0644` | `0644` | `785` | `843` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` | `bdc8bf155539762c02f37ffbeb27e2dcec48bc5c3badaf4a17ec2edd6cd221c9` |
| `etc/services.d/openclaw-agent/run` | `0755` | `0755` | `289` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |
| `usr/local/bin/openclaw-ensure-controlui-origin` | absent | `0755` | absent | `2009` | absent | `c4151fa9a08ee04c41b212a9b30838f1f19d474fe50b6d2fdc848994d8fba071` |
| `usr/local/bin/openclaw-gateway-with-origin-allowlist` | absent | `0755` | absent | `201` | absent | `79910c9dc6a0dcd0d809af1fc21a45052afeae66732d7a0ff4185089e8c3995c` |

## Exact Diff Summary

`Dockerfile` now copies the two helper scripts into the later image build context with executable mode:

```Dockerfile
COPY --chmod=0755 usr/local/bin/openclaw-ensure-controlui-origin /usr/local/bin/openclaw-ensure-controlui-origin
COPY --chmod=0755 usr/local/bin/openclaw-gateway-with-origin-allowlist /usr/local/bin/openclaw-gateway-with-origin-allowlist
```

`defaults/openclaw-agent/config.yaml` changes only the `openclaw_command` argv list by adding the wrapper as argv[0]. It keeps the original gateway argv sequence:

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

The wrapper does:

```bash
/usr/local/bin/openclaw-ensure-controlui-origin
exec "$@"
```

Therefore `openclaw gateway run --bind lan --auth token` remains the executed command after JSON materialization succeeds.

## Semantic Change

The startup artifact now seeds or merges the active OpenClaw JSON config before the gateway process starts:

- target config path defaults to `/config/.openclaw/openclaw.json`
- origin defaults to `https://localhost:30443`
- existing valid object JSON is parsed and preserved semantically
- missing `gateway` and `controlUi` objects are created
- `gateway.controlUi.allowedOrigins` is created if absent
- `https://localhost:30443` is appended only if missing, making restart behavior idempotent
- unrelated JSON keys are preserved
- invalid JSON, a non-object root, a non-object `gateway`/`controlUi`, or non-array `gateway.controlUi.allowedOrigins` fails closed rather than overwriting incompatible config
- the helper writes a temporary file and renames it into `/config/.openclaw/openclaw.json`

No token value, cookie value, credential, access URL, registry credential, or hardcoded secret was added. `--auth token` is only the OpenClaw auth mode flag.

## Required Proofs

| Requirement | Proof |
| --- | --- |
| `--bind lan` remains | `defaults/openclaw-agent/config.yaml` keeps `- --bind` followed by `- lan`; this evidence also records the exact command phrase `openclaw gateway run --bind lan --auth token`. |
| `--auth token` remains | `defaults/openclaw-agent/config.yaml` keeps `- --auth` followed by `- token`; the wrapper execs the original argv after materialization. |
| No unsafe YAML field was used | `defaults/openclaw-agent/config.yaml` contains no `allowedOrigins`, `controlUi`, or `gateway.controlUi` YAML key. The only YAML change is prepending the wrapper command. |
| JSON materialization includes `https://localhost:30443` | `openclaw-ensure-controlui-origin` sets default origin `https://localhost:30443`, ensures `gateway.controlUi.allowedOrigins` is an array, and appends the origin only when absent. |
| Existing JSON is preserved | The helper parses existing valid object JSON, mutates only the required nested allowlist path, and writes the full object back. Unknown unrelated config keys are not deleted or replaced. |
| Idempotent restarts | The helper checks `allowedOrigins.includes(origin)` before pushing, so repeated container/process restarts do not duplicate `https://localhost:30443`. |
| no backend Origin rewrite | No file under `backend/**` was modified; this gate only changed the reviewed runtime startup artifact and this evidence. |

## Lifecycle Risk Assessment

Lifecycle residual risk remains and is explicitly accepted for this startup artifact gate:

- If `configmanager.ApplyRevision` later replaces `/config/.openclaw/openclaw.json` after the gateway has already started, this wrapper may not re-apply until the next gateway process start. In that lifecycle, the allowlist may need to be present in config revisions too.
- If the OpenClaw gateway watches active config and reacts to later revision writes without a process restart, a source-level config revision merge may still be required.
- This implementation is lifecycle-safe for startup and restart materialization because the wrapper runs before each gateway exec. It is not proof that every later config-revision overwrite preserves `gateway.controlUi.allowedOrigins`.
- The helper uses the target runtime's `node` command for JSON parsing/serialization. The recovered file subset did not include the full parent image filesystem, so this gate did not execute the target image or prove runtime dependency availability. If a later build/runtime gate shows `node` is absent, stop and request Runtime Config Source Expert Escalation or Additional Source Recovery Approval Packet instead of substituting an unsafe shell JSON parser.

## Safe / Unsafe Implementation Conclusion

Safe implementation used in this rerun:

- startup command wrapper before gateway process start
- explicit JSON materialization at `/config/.openclaw/openclaw.json`
- semantic merge of `gateway.controlUi.allowedOrigins`
- no backend Origin rewrite
- no hardcoded secret
- no overwrite of unknown unrelated config
- idempotent addition of `https://localhost:30443`

Unsafe implementation not used:

- adding `gateway.controlUi.allowedOrigins` as an unknown YAML key in `defaults/openclaw-agent/config.yaml`
- changing GTManager backend Origin behavior
- overwriting `/config/.openclaw/openclaw.json` with a fixed template that discards unknown config
- hardcoding runtime token values

## Recommended Next Gate

Recommended next gate:

`Runtime Image Build/Tag/Push Approval Packet`

The next gate should build from this reviewed artifact only after explicit approval, then validate that the helper scripts are present with mode `0755` and that the runtime image has a working JSON-capable runtime for the helper. Browser E2E and fresh instance work remain out of scope until separately approved.

## Explicit Negatives

- no unsafe YAML field implementation
- no backend Origin rewrite
- no backend modification
- no frontend modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/**` modification
- no `openclaw-agent` execution
- no OpenClaw runtime service execution
- no build/deploy
- no build/tag/push/pull
- no docker build/tag/push/pull/export/create/cp/rm
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod/service patch
- no `kubectl cp`
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `rg -n "allowedOrigins|localhost:30443|openclaw_config_path|--bind|--auth|openclaw.json|gateway|controlUi" specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth` | `0` | Expected artifact markers found in `config.yaml`, helper scripts, Dockerfile, and MANIFEST. |
| `rg -n "allowedOrigins|controlUi|gateway\\.controlUi" specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml` | `1` | No unsafe YAML field exists in agent `config.yaml`. |
| `bash -n` on `openclaw-ensure-controlui-origin`, `openclaw-gateway-with-origin-allowlist`, and `etc/services.d/openclaw-agent/run` | `0` | Shell syntax passed without executing OpenClaw runtime service. |
| `sed -n '8,61p' openclaw-ensure-controlui-origin \| node --check` | `0` | Inline JavaScript syntax passed on the local workstation; this did not execute target runtime or write config. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-rerun.md` | `0` | No whitespace errors. |
| required evidence marker `rg` scan | `0` | Required markers found, including the rerun verdict, `gateway.controlUi.allowedOrigins`, `https://localhost:30443`, `/config/.openclaw/openclaw.json`, `--bind lan`, `--auth token`, `unsafe YAML field`, `JSON materialization`, `lifecycle residual risk`, `no backend Origin rewrite`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on changed artifact files plus this evidence | `1` | No secret-shaped matches. Matched values were suppressed by count-only scan. |
| final `git status --short -- ...` | `0` | Shows modified `Dockerfile`, `MANIFEST.md`, `defaults/openclaw-agent/config.yaml`; new evidence file; and new artifact `usr/` helper directory in the requested scope. |
