# Runtime Startup Artifact Origin Allowlist Implementation Approval Packet

Date: 2026-05-06
Worker: RuntimeStartupArtifactOriginAllowlistApprovalPacketWorker
Verdict: `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_DONE`

Not `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED`: the dependency gate and user/manual observation are sufficient to request the next narrow implementation gate. This packet does not authorize that implementation by itself.

## Approval Request

Please approve or reject whether a future worker may execute the Runtime Startup Artifact Origin Allowlist Implementation Gate.

Recommended approval options:

- `APPROVE_RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_GATE`: allow the future implementation gate with the exact scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not modify the runtime startup artifact; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gate:

- `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_DONE`

User Browser/Manual E2E observation after `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE`:

- mediated route shape: `wss://localhost:30443/api/v1/instances/10/control-ui`
- visible error: `来源不被允许（请从网关主机打开 Control UI，或在 gateway.controlUi.allowedOrigins 中允许该来源）`

Root cause confirmed:

- browser `Origin` is `https://localhost:30443`
- OpenClaw runtime gateway origin policy rejects that origin
- current reviewed startup artifact lacks `gateway.controlUi.allowedOrigins`
- backend Origin rewrite is not recommended as the first fix

The reviewed startup artifact currently contains `openclaw gateway run --bind lan --auth token` and `openclaw_config_path: /config/.openclaw/openclaw.json`.

## Future Allowed Scope

If approved, the future implementation gate may modify only:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/**`
- one new implementation evidence file for that gate

The future implementation gate must not modify backend source, frontend source, runtime source outside the reviewed startup artifact, deployments, docs, longterm files, existing evidence, spec, plan, tasks, or unrelated generated artifacts.

## Future Implementation Semantic Target

The future implementation must:

- preserve the existing OpenClaw gateway startup command: `openclaw gateway run --bind lan --auth token`
- add the OpenClaw gateway config semantic value: `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- match the current startup artifact's actual file format and materialization path for `/config/.openclaw/openclaw.json`
- block and write evidence instead of guessing if the reviewed artifact cannot safely materialize that value
- keep backend WebSocket Origin behavior unchanged: no backend Origin rewrite

The semantic target is the OpenClaw runtime config value, not a mandate to use a particular YAML/JSON literal shape if the startup artifact materializes `/config/.openclaw/openclaw.json` differently.

## Required Future Implementation Evidence

The future implementation evidence must record:

- changed file path
- before sha256 and after sha256 for every changed file
- before byte size and after byte size for every changed file
- before mode and after mode for every changed file
- exact diff summary
- confirmation of no secret values
- confirmation that `--bind lan` and `--auth token` remain present
- confirmation that `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- confirmation that no backend Origin behavior was changed

Evidence must not include token values, cookie values, credentials, secrets, registry credentials, access URL plaintext, `.env`, `.codex/auth.json`, or `.codex/config.toml` content.

## Future Forbidden Actions

The future implementation gate is forbidden from:

- backend Origin rewrite
- frontend/runtime source unrelated changes
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

Explicit required shorthand for downstream gate checks:

- no backend Origin rewrite
- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Gate Sequence After Approval

If approved:

1. Runtime Startup Artifact Origin Allowlist Implementation Gate.
2. Runtime Image Build/Tag/Push Approval Packet.
3. Deploy, fresh-instance, listener, and browser E2E approvals as separate gates.

The implementation gate must not skip directly to image build, deploy, fresh instance, listener checks, browser E2E, `passes:true`, Close, longterm write-back, or git operations.

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-approval-packet.md
rg -n "RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_DONE|RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED|gateway.controlUi.allowedOrigins|https://localhost:30443|/config/.openclaw/openclaw.json|--bind lan|--auth token|no backend Origin rewrite|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-approval-packet.md`

## Explicit Negatives

This approval packet performed:

- no implementation
- no artifact modification outside this new evidence file
- no build/tag/push/pull
- no deploy
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
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_DONE`, `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_APPROVAL_PACKET_BLOCKED`, `gateway.controlUi.allowedOrigins`, `https://localhost:30443`, `/config/.openclaw/openclaw.json`, `--bind lan`, `--auth token`, `no backend Origin rewrite`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-approval-packet.md` | `0` | Shows this new evidence file as untracked. |
