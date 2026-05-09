# Control UI Origin Allowlist Root Cause - 2026-05-06

Verdict: `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_DONE`

Not `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_BLOCKED`: the origin rejection
source and the current startup artifact gap are confirmed from read-only source,
artifact, prior evidence, and sanitized runtime logs.

## Scope

This was a read-only root-cause investigation plus this single evidence write.
There was no implementation, no build/deploy, no backend deploy/restart, no
browser E2E, no Chrome DevTools MCP, no Playwright, no fresh instance, no
runtime mutation, no K8S write, no database write, no registry mutation, no
manual pod patch, no manual Service patch, no `kubectl cp`, no Mem0 write, no
longterm write-back, no passes:true, and no Close.

## Manual Observation Input

User manual Browser/Manual E2E after
`WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE` observed:

- existing instance `10` / `oc2gi-185707`
- GTManager entry "打开 GTClaw 控制台"
- page still at the GTClaw manual connect form
- mediated route shape:
  `wss://localhost:30443/api/v1/instances/10/control-ui`
- visible error:
  `来源不被允许（请从网关主机打开 Control UI，或在 gateway.controlUi.allowedOrigins 中允许该来源）`

No token, cookie, query secret, credential, or access URL value was used or
recorded by this investigation.

## Root Cause Summary

The blocker is an OpenClaw runtime control-ui Origin allowlist rejection.

The browser-visible Chinese text is rendered by the OpenClaw control-ui browser
bundle, but it is a presentation of the runtime gateway's origin policy failure.
GTManager backend does not define that message and does not originate this
policy decision.

For the mediated route, the browser page origin is:

```text
https://localhost:30443
```

The WebSocket URL is `wss://localhost:30443/api/v1/instances/10/control-ui`,
but the WebSocket `Origin` header is the page origin, not the `wss://` URL and
not the path. Sanitized runtime logs from the existing instance show repeated
OpenClaw WebSocket closes before connect with:

```text
[ws] closed before connect ... origin=https://localhost:30443 host=[RUNTIME_HOST] code=1008 reason=n/a
```

The reviewed startup artifact
`runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml`
does not contain `gateway.controlUi.allowedOrigins`. It only adds
`--bind lan` and `--auth token` to `openclaw gateway run`.

## Required Answers

| Question | Answer |
| --- | --- |
| Where does the "来源不被允许 / gateway.controlUi.allowedOrigins" error come from? | The visible Chinese string is in the OpenClaw control-ui browser bundle. The underlying rejection is from the OpenClaw runtime gateway origin policy, evidenced by runtime WebSocket logs closing before connect with `origin=https://localhost:30443`. It is not a GTManager backend bridge error. |
| What browser Origin should be expected? | `https://localhost:30443`. The mediated WebSocket route is `wss://localhost:30443/api/v1/instances/10/control-ui`, but `Origin` is scheme/host/port of the page. |
| Does current reviewed startup artifact config include `gateway.controlUi.allowedOrigins`? | No. The reviewed `config.yaml` contains `openclaw_command` with `openclaw gateway run --bind lan --auth token`, and `openclaw_config_path: /config/.openclaw/openclaw.json`; no `gateway`, `controlUi`, or `allowedOrigins` entry is present. |
| Minimum correct design? | Add an explicit OpenClaw runtime config value for `gateway.controlUi.allowedOrigins` that includes `https://localhost:30443` for the local GTManager mediated origin. Do not rewrite the backend WebSocket `Origin` header as the first fix. |
| Does backend bridge need to modify `Origin`? | Not for the confirmed current root cause. Backend currently forwards the browser request headers to upstream, strips hop-by-hop WebSocket headers, injects server-owned auth, and does not replace `Origin`. That preserves the browser-visible trust boundary. |
| Are both runtime config and backend Origin rewrite needed? | No evidence indicates both are needed. Runtime allowlist config is the minimum correct fix; backend Origin rewrite should be avoided unless a later design gate proves runtime config cannot safely express the mediated origin. |
| Can this be fixed only through runtime startup artifact, without backend source? | Likely yes, provided the startup artifact can materialize the OpenClaw config at `/config/.openclaw/openclaw.json` or otherwise set `gateway.controlUi.allowedOrigins`. Existing backend source already forwards the real Origin and bridges server-owned auth. |
| Need new approval packet? | Yes. A new `Runtime Startup Artifact Origin Allowlist Implementation Approval Packet` is recommended before modifying the startup artifact or building a runtime image. |

## Evidence Table

| Evidence | Source | Finding |
| --- | --- | --- |
| Browser bundle contains origin error code and Chinese message | `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` snippets around lines `2` and `27` | Bundle contains `CONTROL_UI_ORIGIN_NOT_ALLOWED` and maps it, plus messages starting with `origin not allowed`, to `来源不被允许（请从网关主机打开 Control UI，或在 gateway.controlUi.allowedOrigins 中允许该来源）`. |
| Runtime saw the mediated browser Origin | sanitized `kubectl logs -n clawmanager-user-1 clawreef-10-oc2gi-185707 -c desktop --tail=800` | Repeated `[ws] closed before connect` entries show `origin=https://localhost:30443`, runtime host redacted, and WebSocket close code `1008` or `1006`. |
| Backend bridge does not rewrite Origin | `backend/internal/services/instance_proxy_service.go:343-357` | WebSocket proxy copies request headers into `upstreamHeader`, deletes hop-by-hop WebSocket headers, sets `X-Forwarded-Proto` and `X-Forwarded-Prefix`, and does not delete or replace `Origin`. |
| Backend injects server-owned token auth only | `backend/internal/services/instance_proxy_service.go:549-563` | Control-ui upstream auth strips browser `Authorization`, `Cookie`, and `X-OpenClaw-Token`, then sets server-owned `Authorization`. It does not implement the observed origin allowlist policy. |
| Startup artifact lacks allowlist | `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml:9-17` | The artifact has only `openclaw gateway run --bind lan --auth token` plus `openclaw_config_path`; no `gateway.controlUi.allowedOrigins`. |
| Backend fix already deployed | `20260506-ws-challenge-backend-build-deploy.md` | `Deployment/clawmanager-app` was updated to `clawmanager:control-plane-backend-ws-challenge-20260506091557`; runtime image digest did not drift, and no fresh instance was created. |

## Interpretation

The prior WebSocket challenge fix addressed a backend sequencing bug: the
browser needed upstream `connect.challenge` before sending first connect with a
non-empty `device.nonce`.

The new blocker happens earlier or at a different trust boundary. The runtime
gateway closes the mediated WebSocket before the authenticated connect can
complete because the browser-origin value is not allowed for control-ui access.
The user still sees the manual connect form because the control-ui client never
reaches authenticated/usable state after the origin rejection.

Rewriting `Origin` in GTManager backend would make the upstream runtime see a
different origin than the browser actually used. That would blur the runtime's
explicit browser-origin trust boundary and could hide future cross-origin
problems. The error message itself names the intended configuration knob:
`gateway.controlUi.allowedOrigins`.

## Recommended Minimal Design

Recommended next implementation design, pending explicit approval:

1. Extend the runtime startup artifact so the OpenClaw gateway config includes:

   ```yaml
   gateway:
     controlUi:
       allowedOrigins:
         - https://localhost:30443
   ```

   The final file format must match the actual OpenClaw config materialized at
   `/config/.openclaw/openclaw.json`; the YAML above is the semantic target,
   not necessarily the literal on-disk format.

2. Keep backend WebSocket `Origin` behavior unchanged.

3. Build/deploy a new runtime startup artifact only after a new approval packet.

4. After runtime deployment approval and later Browser/Manual E2E approval,
   verify instance `10` via the same mediated route and confirm:
   - no manual connect form remains
   - no `来源不被允许`
   - no `invalid connect params`
   - no `device.nonce` error
   - no token/cookie/access URL plaintext appears in evidence

## Recommended Next Gate

Recommended next gate:

`Runtime Startup Artifact Origin Allowlist Implementation Approval Packet`

Backend Origin Header Design Gate is not recommended as the immediate next
gate because backend rewriting is not needed for the confirmed root cause and
would weaken clarity around browser-visible origin trust.

External Expert Escalation is not currently required. The error text, runtime
logs, startup artifact contents, and backend header flow all point to the same
minimum fix direction. Escalate only if a later implementation gate cannot
safely materialize `gateway.controlUi.allowedOrigins` through the runtime
startup artifact.

## Explicit Negatives

- no implementation
- no build/deploy
- no backend deploy/restart
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance
- no fresh instance creation/deletion/modification
- no runtime mutation
- no K8S write
- no database write
- no database migration
- no registry mutation
- no runtime image build/tag/push/pull
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks modification
- no existing evidence modification
- no startup artifact modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no recovered `/tmp/gtclaw-startup-source-inspect-*` modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-origin-allowlist-root-cause.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_DONE`, `CONTROL_UI_ORIGIN_ALLOWLIST_ROOT_CAUSE_BLOCKED`, `来源不被允许`, `gateway.controlUi.allowedOrigins`, `Origin`, `https://localhost:30443`, `wss://localhost:30443/api/v1/instances/10/control-ui`, `manual connect form`, `no build/deploy`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-origin-allowlist-root-cause.md` | `0` | Shows this new evidence file as untracked. |
