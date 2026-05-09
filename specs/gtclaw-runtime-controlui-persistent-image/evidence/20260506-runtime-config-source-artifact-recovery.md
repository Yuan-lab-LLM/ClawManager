# Runtime Config Source Artifact Recovery Gate - 2026-05-06

Worker: RuntimeConfigSourceArtifactRecoveryWorker
Verdict: `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_DONE`

Not `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_BLOCKED`: the approved recovery gate recovered enough runtime-side source/artifact evidence for a follow-up materialization investigation rerun. This gate did not implement a fix and did not modify the reviewed runtime startup artifact.

## Dependency Gates

| Gate | State used |
| --- | --- |
| Runtime Config Source Artifact Recovery Approval Packet | `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE`; user approved `APPROVE_RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_GATE` |
| Runtime Startup Config Materialization Investigation | `RUNTIME_STARTUP_CONFIG_MATERIALIZATION_INVESTIGATION_BLOCKED` |

Blocked reason carried forward:

- repo and reviewed artifact did not include the `openclaw-agent` implementation
- repo and reviewed artifact did not prove how `/etc/openclaw-agent/config.yaml` is read
- repo and reviewed artifact did not prove how `openclaw_config_path` materializes `/config/.openclaw/openclaw.json`
- repo and reviewed artifact did not find a safe YAML field for `gateway.controlUi.allowedOrigins`

## Parent Image

| Field | Value |
| --- | --- |
| parent image tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| expected repo digest | `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| inspected repo digest | `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| inspected platform | `linux/arm64` |
| image id | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| entrypoint | `["/init"]` |
| cmd | `null` |

No pull was needed or performed. Required shorthand: no build/tag/push/pull.

## Recovery Workspace

New extraction root:

`/tmp/gtclaw-config-materialization-inspect-xLFBgllN`

No old `/tmp` workspace, old extraction directory, old asset, or existing project artifact was deleted or modified.

## Temporary Container

| Field | Value |
| --- | --- |
| temporary container name | `gtclaw-config-materialization-inspect-gtclaw-config-materialization-inspect-xLFBgllN` |
| temporary container id | `5f4509a1a1eeaa589242f4f3454da2d07b9afd7006c1b470b0b1b66149dbf851` |
| container state used | stopped container only; never started |
| removal confirmation | `docker container rm` returned success in the extraction script and the script printed `CONTAINER_REMOVED=yes` |

## Exact Docker Commands Used

Image inspect command:

```bash
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Platform={{.Os}}/{{.Architecture}}
Id={{.Id}}
Entrypoint={{json .Config.Entrypoint}}
Cmd={{json .Config.Cmd}}
WorkingDir={{.Config.WorkingDir}}
User={{.Config.User}}'
```

Exact read-only extraction command sequence:

```bash
set -euo pipefail
image='localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506'
out=$(mktemp -d /tmp/gtclaw-config-materialization-inspect-XXXXXXXX)
name="gtclaw-config-materialization-inspect-$(basename "$out")"
cid=''
cleanup() {
  if [ -n "${cid:-}" ]; then
    docker container rm "$cid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
cid=$(docker container create --name "$name" "$image")
mkdir -p "$out/usr/local/bin" "$out/defaults/openclaw-agent" "$out/etc/openclaw-agent" "$out/config/.openclaw" "$out/_metadata"
printf 'OUT=%s\n' "$out"
printf 'CONTAINER_NAME=%s\n' "$name"
printf 'CONTAINER_ID=%s\n' "$cid"
if docker cp "$cid:/usr/local/bin/openclaw-agent" "$out/usr/local/bin/openclaw-agent"; then
  printf '/usr/local/bin/openclaw-agent copied\n' > "$out/_metadata/extraction-status.txt"
else
  printf '/usr/local/bin/openclaw-agent missing\n' > "$out/_metadata/extraction-status.txt"
fi
if docker cp "$cid:/defaults/openclaw-agent/config.yaml" "$out/defaults/openclaw-agent/config.yaml"; then
  printf '/defaults/openclaw-agent/config.yaml copied\n' >> "$out/_metadata/extraction-status.txt"
else
  printf '/defaults/openclaw-agent/config.yaml missing\n' >> "$out/_metadata/extraction-status.txt"
fi
if docker cp "$cid:/etc/openclaw-agent/config.yaml" "$out/etc/openclaw-agent/config.yaml"; then
  printf '/etc/openclaw-agent/config.yaml copied\n' >> "$out/_metadata/extraction-status.txt"
else
  printf '/etc/openclaw-agent/config.yaml missing\n' >> "$out/_metadata/extraction-status.txt"
fi
if docker cp "$cid:/config/.openclaw/openclaw.json" "$out/config/.openclaw/openclaw.json"; then
  printf '/config/.openclaw/openclaw.json copied\n' >> "$out/_metadata/extraction-status.txt"
else
  printf '/config/.openclaw/openclaw.json missing\n' >> "$out/_metadata/extraction-status.txt"
fi
docker container rm "$cid" >/dev/null
cid=''
printf 'CONTAINER_REMOVED=yes\n'
printf 'EXTRACTION_STATUS=%s\n' "$out/_metadata/extraction-status.txt"
cat "$out/_metadata/extraction-status.txt"
```

Only `docker create`, `docker cp`, and `docker rm` were used after image inspect. No OpenClaw runtime service process was run.

## Extraction Results

Image paths copied:

| Original image path | Local extracted path | Result |
| --- | --- | --- |
| `/usr/local/bin/openclaw-agent` | `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent` | copied |
| `/defaults/openclaw-agent/config.yaml` | `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/defaults/openclaw-agent/config.yaml` | copied |
| `/etc/openclaw-agent/config.yaml` | `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/etc/openclaw-agent/config.yaml` | missing in image |
| `/config/.openclaw/openclaw.json` | `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/config/.openclaw/openclaw.json` | missing in image |

Local extraction root files:

```text
/tmp/gtclaw-config-materialization-inspect-xLFBgllN/_metadata/extraction-status.txt
/tmp/gtclaw-config-materialization-inspect-xLFBgllN/defaults/openclaw-agent/config.yaml
/tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent
```

The `_metadata/extraction-status.txt` file is a local command-status file created by the extraction script. It is not image source and is not treated as source artifact.

## Extracted File Metadata

| Local path | Image source path | mode | owner/group metadata if available | sha256 | byte size |
| --- | --- | --- | --- | --- | ---: |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/usr/local/bin/openclaw-agent` | `/usr/local/bin/openclaw-agent` | `0755` | local extracted owner `eduardogan`, group `wheel`, uid `501`, gid `0`; original image owner/group not available from stopped-container `docker cp` metadata | `9f53969a3dcf4a1c4b596f54fcc724364d6f632dca7605253d0877f691e95b28` | `20266626` |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | `0644` | local extracted owner `eduardogan`, group `wheel`, uid `501`, gid `0`; original image owner/group not available from stopped-container `docker cp` metadata | `cb92a237d37725b12a0a9778522609420803535c4a29cc0204bc1c8f820295f3` | `745` |

Auxiliary local metadata file:

| Local path | Purpose | mode | owner/group metadata if available | sha256 | byte size |
| --- | --- | --- | --- | --- | ---: |
| `/tmp/gtclaw-config-materialization-inspect-xLFBgllN/_metadata/extraction-status.txt` | local command-status record; not image source | `0644` | local owner `eduardogan`, group `wheel`, uid `501`, gid `0` | `3f2315cc97e8ad8fcafcdb95523726bde696c6f32476fe3a8c56fa7efe5bf7c4` | `161` |

## Recovered Runtime-Side Evidence

Binary identity:

- `/usr/local/bin/openclaw-agent` is an ELF 64-bit ARM aarch64 static Go executable.
- It is not stripped and includes debug info.
- `go version -m` reports module path `github.com/iamlovingit/clawmanager-openclaw-image/cmd/openclaw-agent`.
- `go tool nm` exposes runtime-side packages including:
  - `github.com/iamlovingit/clawmanager-openclaw-image/internal/config.Load`
  - `github.com/iamlovingit/clawmanager-openclaw-image/internal/configmanager.(*Manager).ApplyRevision`
  - `github.com/iamlovingit/clawmanager-openclaw-image/internal/configmanager.validateJSON`
  - `github.com/iamlovingit/clawmanager-openclaw-image/internal/configmanager.verifyChecksum`
  - `github.com/iamlovingit/clawmanager-openclaw-image/internal/process.(*Manager).Start`

Config path and YAML evidence:

- `strings` on the agent binary contains `/etc/openclaw-agent/config.yaml`.
- `strings` on the agent binary contains `OPENCLAW_AGENT_CONFIG_PATH`.
- `strings` on the agent binary contains `openclaw_config_path` and the YAML tag `yaml:"openclaw_config_path"`.
- `strings` on the agent binary contains `openclaw_command` and the YAML tag `yaml:"openclaw_command"`.
- `strings` on the agent binary contains `/config/.openclaw/openclaw.json`.
- `strings` on the agent binary contains `OPENCLAW_AGENT_OPENCLAW_CONFIG_PATH`.
- `go tool objdump` for `internal/config.Load` shows source path `/src/internal/config/config.go`, calls to `os.Getenv`, `os.ReadFile`, and `gopkg.in/yaml.v3.unmarshal`.

Config revision evidence:

- `go tool nm` exposes `internal/configmanager.(*Manager).ApplyRevision`, `validateJSON`, and `verifyChecksum`.
- `go tool objdump` for `ApplyRevision` shows calls to `verifyChecksum`, `validateJSON`, `os.MkdirAll`, `os.WriteFile`, `os.ReadFile`, and `os.rename`.
- This is sufficient for the next investigation rerun to analyze how config revisions write or replace OpenClaw JSON config content. It is not treated as final source without review.

OpenClaw process evidence:

- `go tool nm` exposes `internal/process.(*Manager).Start`.
- `go tool objdump` for `process.(*Manager).Start` shows a call to `os/exec.CommandContext` followed by `os/exec.(*Cmd).Start`.
- This supports a next-gate investigation of how the recovered `openclaw_command` is executed.

Default config evidence:

- The recovered `/defaults/openclaw-agent/config.yaml` contains `openclaw_command` with `openclaw`, `gateway`, `run`.
- The recovered default config contains `openclaw_config_path: /config/.openclaw/openclaw.json`.
- It does not contain `gateway.controlUi.allowedOrigins`.
- It predates the reviewed startup artifact's later `--bind lan --auth token` addition.

Absent target evidence:

- `/etc/openclaw-agent/config.yaml` is not present in the image filesystem as a copied file.
- `/config/.openclaw/openclaw.json` is not present in the image filesystem as a copied file.
- No `allowedOrigins`, `controlUi`, `gateway.controlUi`, `control_ui`, `allowed_origins`, or `CONTROL_UI_ORIGIN` marker was found in the extracted agent binary strings.

## Sufficiency Assessment

Recovered files are sufficient for the next Runtime Startup Config Materialization Investigation Rerun Gate.

Reason:

- The previously missing `openclaw-agent` runtime-side artifact is now available as an extracted, not-stripped Go binary with symbol and debug metadata.
- The binary evidence can support a focused rerun on whether `openclaw-agent` reads `/etc/openclaw-agent/config.yaml`.
- The binary evidence can support a focused rerun on how `openclaw_config_path` is used by the config manager and process manager.
- The image does not contain a pre-existing `/config/.openclaw/openclaw.json` at the exact target path, which narrows the template/default question.
- The recovered default config and binary string evidence do not show a safe YAML field that directly materializes `gateway.controlUi.allowedOrigins`, so implementation must still wait for the rerun gate's conclusion.

This recovery gate does not by itself authorize or perform any runtime startup artifact change.

## Recommended Next Gate

Recommended next gate:

`Runtime Startup Config Materialization Investigation Rerun Gate`

The rerun should use the extracted files above to answer:

- whether `openclaw-agent` reads `/etc/openclaw-agent/config.yaml`
- how `openclaw_config_path` is used
- whether `/config/.openclaw/openclaw.json` is only created from config revisions, from defaults, or from both
- whether a safe YAML field can materialize `gateway.controlUi.allowedOrigins`
- whether the minimum compliant implementation should modify `config.yaml`, modify the run wrapper, recover additional source, or escalate externally

External Expert Escalation is not the recommended immediate next gate because the binary was recovered with symbols and debug info. Escalate only if the rerun investigation cannot reach a safe materialization conclusion from this recovered evidence.

## Secret Hygiene

No token value, cookie value, credential, secret value, registry credential, access URL plaintext, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was written.

The string `--auth token` appears only as an OpenClaw CLI auth mode flag in prior reviewed artifacts and not as a secret value.

## Explicit Negatives

- no implementation
- no runtime startup artifact modification
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no backend Origin rewrite
- no OpenClaw runtime service process run
- no docker pull/build/push/tag
- no registry mutation
- no K8S/runtime/database/browser mutation
- no fresh instance creation/deletion/modification
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no extracted file modification
- no old `/tmp` deletion
- no old extraction directory deletion
- no old asset deletion
- no extracted image filesystem treated as final source without review
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

Required shorthand:

- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Verification Plan

Required checks for this recovery evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery.md
rg -n "RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_DONE|RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_BLOCKED|openclaw-agent|/usr/local/bin/openclaw-agent|/etc/openclaw-agent/config.yaml|/defaults/openclaw-agent/config.yaml|/config/.openclaw/openclaw.json|/tmp/gtclaw-config-materialization-inspect-|read-only extraction|parent image|sha256|docker create|docker cp|docker rm|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery.md
```

Also required:

- secret-shape scan on this new evidence and extracted text metadata with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_DONE`, `RUNTIME_CONFIG_SOURCE_ARTIFACT_RECOVERY_BLOCKED`, `openclaw-agent`, `/usr/local/bin/openclaw-agent`, `/etc/openclaw-agent/config.yaml`, `/defaults/openclaw-agent/config.yaml`, `/config/.openclaw/openclaw.json`, `/tmp/gtclaw-config-materialization-inspect-`, `read-only extraction`, `parent image`, `sha256`, `docker create`, `docker cp`, `docker rm`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence and extracted text metadata | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-config-source-artifact-recovery.md` | `0` | Shows this new evidence file as untracked. |
