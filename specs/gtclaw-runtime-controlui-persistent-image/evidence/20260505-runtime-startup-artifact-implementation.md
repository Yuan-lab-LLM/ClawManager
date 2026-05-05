# Runtime Startup Artifact Implementation Gate - 2026-05-05

## Verdict

`RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`

This gate created a reviewed startup source/build-context artifact for the OpenClaw gateway pod-facing listen/auth change. It did not build an image and did not mutate runtime, Kubernetes, database, registry, browser state, or fresh instances.

## Dependency Gates

| Gate | State used |
| --- | --- |
| WS Auth Bridge Implementation Gate | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; Commander review recorded `go test -count=1 ./internal/services ./internal/handlers` passed. |
| Runtime Source Artifact Recovery Gate | `RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE`; recovered only `/defaults/openclaw-agent/config.yaml` and `/etc/services.d/openclaw-agent/run`. |
| Runtime Startup Artifact Implementation Approval Packet | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_APPROVAL_PACKET_DONE`. |
| A1 Source Gate | `A1_SOURCE_IMPLEMENTATION_BLOCKED`; this gate addresses the runtime startup artifact implementation blocker only. |

## Parent Image

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

## Artifact Root

Reviewed startup source/build-context artifact:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

The artifact preserves the original image path layout:

- `defaults/openclaw-agent/config.yaml`
- `etc/services.d/openclaw-agent/run`

It also includes:

- `MANIFEST.md`
- `Dockerfile`

The Dockerfile is only a future build gate input. It was not executed.

## Recovered Input Files

| Original path | Recovered input path | mode | size | sha256 |
| --- | --- | --- | ---: | --- |
| `/defaults/openclaw-agent/config.yaml` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml` | `0644` | `745` | `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3` |
| `/etc/services.d/openclaw-agent/run` | `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

The recovered input files under `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` were read only and were not modified.

## Output Artifact Files

| Artifact path | Mode | Size | sha256 |
| --- | --- | ---: | --- |
| `Dockerfile` | `0644` | `323` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` |
| `MANIFEST.md` | `0644` | `2917` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` |
| `defaults/openclaw-agent/config.yaml` | `0644` | `785` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` |
| `etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

## Before / After Diff Summary

`defaults/openclaw-agent/config.yaml` was the only startup file changed. The minimal diff adds pod-facing gateway bind/auth flags to `openclaw_command`:

```diff
 openclaw_command:
   - openclaw
   - gateway
   - run
+  - --bind
+  - lan
+  - --auth
+  - token
 openclaw_config_path: /config/.openclaw/openclaw.json
```

Expected resulting YAML shape:

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

`etc/services.d/openclaw-agent/run` is unchanged byte-for-byte from the recovered input. Its sha256 remains `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda`.

## Dockerfile Summary

`Dockerfile` contains only:

- a digest-pinned parent `FROM` for the approved parent image;
- `COPY --chmod=0644` for `/defaults/openclaw-agent/config.yaml`;
- `COPY --chmod=0755` for `/etc/services.d/openclaw-agent/run`.

No secret, token value, registry credential, access URL, or runtime setting appears in the Dockerfile.

## Exact Commands Used

Artifact directory creation and source copy:

```bash
set -euo pipefail
root='specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth'
evidence='specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md'
if [ -e "$root" ] || [ -e "$evidence" ]; then
  printf 'BLOCKED existing path root=%s evidence=%s\n' "$root" "$evidence" >&2
  exit 1
fi
mkdir -p "$root/defaults/openclaw-agent" "$root/etc/services.d/openclaw-agent"
cp -p /tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml "$root/defaults/openclaw-agent/config.yaml"
cp -p /tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run "$root/etc/services.d/openclaw-agent/run"
printf 'created=%s\n' "$root"
```

Artifact config change:

```diff
 openclaw_command:
   - openclaw
   - gateway
   - run
+  - --bind
+  - lan
+  - --auth
+  - token
 openclaw_config_path: /config/.openclaw/openclaw.json
```

Metadata commands:

```bash
stat -f 'path=%N mode=%OLp bytes=%z' specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run
diff -u /tmp/gtclaw-startup-source-inspect-y5sCboQ2/defaults/openclaw-agent/config.yaml specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml
cmp -s /tmp/gtclaw-startup-source-inspect-y5sCboQ2/etc/services.d/openclaw-agent/run specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run
```

No Docker command was run in this implementation gate.

## Secret Hygiene

No hardcode: no token value, secret value, cookie value, credential, registry credential, access URL, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was written to the artifact or this evidence.

`OPENCLAW_GATEWAY_TOKEN` appears only as an environment variable name / runtime token source in explanatory text, not as a value.

The artifact uses `--auth token` only as an OpenClaw CLI auth mode flag.

## Explicit Negatives

- no build/tag/push/pull
- no docker build/tag/push/pull/export/save/cp/container create
- no runtime/K8S/database/registry mutation
- no runtime image/resource setting mutation
- no fresh instance mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no modification to `backend/**`
- no modification to `frontend/**`
- no modification to `deployments/**`
- no modification to `docs/**`
- no modification to `longterm/**`
- no modification to `AgentTeam/**`
- no modification to `spec.md`, `plan.md`, or `tasks.md`
- no modification to existing evidence
- no modification to `/tmp/gtclaw-runtime-patch/**`
- no modification to recovered `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**`
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Recommended Next Gate

Recommended next gate:

`Runtime Image Build/Tag/Push Approval Packet`

That packet should decide whether to authorize image build/tag/push/pull from this reviewed startup artifact. Until that approval exists, this artifact is source/build-context only.

## Verification Results

Required verification commands:

```bash
find specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth -type f -print | sort
stat / shasum checks for artifact output files
rg -n "openclaw_command|--bind|lan|--auth|token|OPENCLAW_GATEWAY_TOKEN" specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth
rg -n "RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE|RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_BLOCKED|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|20260505-bind-lan-auth|--bind|lan|--auth|token|OPENCLAW_GATEWAY_TOKEN|sha256|no hardcode|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md
secret-shape scan against new evidence and new artifact files
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md
```

Verification results:

| Command | Result |
| --- | --- |
| `find specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth -type f -print \| sort` | Exit `0`; artifact contains `Dockerfile`, `MANIFEST.md`, `defaults/openclaw-agent/config.yaml`, and `etc/services.d/openclaw-agent/run`. |
| `stat` / `shasum -a 256` for artifact output files | Exit `0`; output file modes, sizes, and sha256 values match the table above. |
| `rg -n "openclaw_command\|--bind\|lan\|--auth\|token\|OPENCLAW_GATEWAY_TOKEN" specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth` | Exit `0`; expected startup command markers found. |
| `rg -n "RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE\|RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_BLOCKED\|/defaults/openclaw-agent/config.yaml\|/etc/services.d/openclaw-agent/run\|20260505-bind-lan-auth\|--bind\|lan\|--auth\|token\|OPENCLAW_GATEWAY_TOKEN\|sha256\|no hardcode\|no build/tag/push/pull\|no browser E2E\|no passes:true\|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | Exit `0`; required evidence markers found. |
| no-index whitespace check over all new artifact files and this new evidence file | Exit `0`; no whitespace diagnostics. |
| secret-shape scan over the new evidence and new artifact files | Exit `1`; no token value, cookie value, credential, access URL, password value, or assignment-form value for `OPENCLAW_GATEWAY_TOKEN` found. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | Exit `0`; shows the artifact directory and implementation evidence as untracked. |
