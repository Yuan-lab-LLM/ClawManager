# Runtime Startup / WebSocket Auth Contract Investigation - 2026-05-05

## Verdict

`RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED`

`RUNTIME_STARTUP_WS_AUTH_CONTRACT_DONE` was not reached.

This was a read-only decision gate. It performed no implementation, no build/tag/push/pull, no runtime image/resource setting mutation, no Kubernetes write, no database write, no registry mutation, no browser E2E, no Chrome DevTools MCP, no Playwright, no Mem0 write, no longterm write-back, no passes:true, and no Close.

Recommended next gate: external expert escalation focused on the OpenClaw startup source recovery and WebSocket auth protocol, before any Runtime Startup Artifact Implementation gate.

## Scope

Allowed write:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md`

Read-only inputs used:

- `AGENTS.md`
- `.specify/memory/constitution.md`
- `backend/AGENTS.md`
- `specs/gtclaw-runtime-controlui-persistent-image/spec.md`
- `specs/gtclaw-runtime-controlui-persistent-image/plan.md`
- `specs/gtclaw-runtime-controlui-persistent-image/tasks.md`
- relevant feature evidence, especially:
  - `20260503-path-verification.md`
  - `20260503-runtime-pod-source-artifact-recovery.md`
  - `20260504-control-ui-18789-root-cause.md`
  - `20260504-control-ui-18789-fix-approval-packet.md`
  - `20260505-control-ui-18789-fix-implementation.md`
  - `20260505-control-ui-18789-topology-capacity-decision-packet.md`
  - `20260505-control-ui-18789-a1-source-implementation.md`
- backend source/tests for Service generation, control-ui proxy, WebSocket proxy, token env injection, and focused A1 tests
- repo-local runtime/build/image artifact source search
- `/tmp/gtclaw-runtime-patch/**` read-only
- local Docker image metadata read-only

Secret hygiene:

- no token value
- no cookie value
- no credential or secret value
- no `.env` content
- no `.codex/auth.json` content
- no `.codex/config.toml` content
- no token-bearing URL
- no Authorization header value beyond the redacted contract form `Authorization: Bearer <token>`

## Dependency Review

| Dependency | Finding used |
| --- | --- |
| `20260505-control-ui-18789-a1-source-implementation.md` | Focused backend A1 tests passed, but full A1 was `A1_SOURCE_IMPLEMENTATION_BLOCKED` because no approved OpenClaw runtime startup source/build-context was present in repo scope. |
| `20260505-control-ui-18789-fix-implementation.md` | Prior runtime startup attempt found `/etc/services.d/openclaw-agent/run` and `/defaults/openclaw-agent/config.yaml` inside the runtime image/pod context; `--bind lan` without auth was refused; `--bind lan --auth token` progressed beyond auth refusal but runtime verification remained blocked. |
| `20260505-control-ui-18789-topology-capacity-decision-packet.md` | A1 was selected as minimum topology path, but token auth and Service generation were explicit blockers. |
| `20260503-path-verification.md` | Runtime-served control-ui path was `/usr/local/lib/node_modules/openclaw/dist/control-ui`; `/opt/opensparrow/runtime/openclaw/dist/control-ui` was not proven. |
| `20260503-runtime-pod-source-artifact-recovery.md` | `/tmp/gtclaw-runtime-patch` contains only four repaired control-ui static files, not OpenClaw startup files. |

## Commands Run

| Command | Secret-safe result |
| --- | --- |
| `sed -n '1,240p' AGENTS.md` | Confirmed project gates, forbidden actions, and secret hygiene. |
| `sed -n '1,260p' .specify/memory/constitution.md` | Confirmed no passes:true without E2E and no secret hardcoding. |
| `sed -n '1,240p' backend/AGENTS.md` | Confirmed backend scope and protected runtime identifiers. |
| `sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Confirmed persistent delivery, no manual pod patch, no token/cookie/secret evidence. |
| `sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Confirmed path/source-of-truth and digest-pinned artifact requirements. |
| `sed -n '1,360p' specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | Confirmed read-only gate boundaries and future implementation gates. |
| `rg -n "A1_SOURCE\|gateway\\.bind\|--bind lan\|OPENCLAW_GATEWAY_TOKEN\|Authorization: Bearer\|connectParams\|WebSocket\|websocket\|startup\|/defaults/openclaw-agent/config.yaml\|/etc/services.d/openclaw-agent/run" specs/gtclaw-runtime-controlui-persistent-image/evidence specs/gtclaw-runtime-controlui-persistent-image/*.md` | Located relevant evidence. |
| `sed -n '1,340p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Confirmed A1 source implementation blocker and focused backend test result. |
| `sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-topology-capacity-decision-packet.md` | Confirmed A1 topology design and token auth concerns. |
| `sed -n '1,300p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md` | Confirmed runtime startup paths found in image/pod context and prior blocked runtime attempt. |
| `sed -n '1,240p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md` | Confirmed root cause was loopback-only `18789` listener. |
| `rg -n "OPENCLAW_GATEWAY_TOKEN\|GatewayToken\|InstanceProxyUpstreamAuth\|ProxyRequestWithScopeAndUpstreamAuth\|additionalServicePorts\|control-ui\|Authorization\|Bearer\|WebSocket\|websocket" backend/internal/services backend/internal/handlers backend/internal/services/k8s` | Confirmed current A1 source surfaces for Service generation, token env, HTTP proxy, and WebSocket proxy. |
| `sed -n '1,120p' backend/internal/services/instance_proxy_service.go` and selected follow-up ranges | Confirmed `ControlUIUpstreamAuthForInstance`, HTTP upstream auth injection, and WebSocket upstream header injection. |
| `sed -n '180,340p' backend/internal/services/instance_service.go` and selected follow-up ranges | Confirmed OpenClaw additional Service port and `OPENCLAW_GATEWAY_TOKEN` env injection are source-level backend changes. |
| `sed -n '210,260p' backend/internal/services/k8s/service_service.go` | Confirmed Service port naming for `control-ui` target `18789`. |
| `rg -n "(/defaults/openclaw-agent/config\\.yaml\|/etc/services\\.d/openclaw-agent/run\|openclaw_command\|openclaw gateway run\|gateway\\.bind\|--bind\|OPENCLAW_GATEWAY_TOKEN\|--auth token\|--auth)"` with repo path exclusions | Found startup references only in evidence, not in a modifiable repo source/build-context. |
| `find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f -print` | Confirmed only four static control-ui files exist in the approved patch workspace. |
| `rg -n "openclaw_command\|gateway\\.bind\|OPENCLAW_GATEWAY_TOKEN\|connectParams\|auth\\.token\|Authorization: Bearer\|WebSocket\|websocket"` over `/tmp/gtclaw-runtime-patch` | Found control-ui client auth code in static JS, but no startup source. |
| Perl read-only snippet scans around `new WebSocket`, `sendConnect`, `buildConnectParams`, `Authorization`, and `auth` in `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | Confirmed the client opens WebSocket without custom headers, then sends a `connect` request whose params include an `auth` object derived from token/password state. |
| `docker image ls --format ... \| rg 'clawmanager-openclaw\|openclaw\|gtclaw'` | Confirmed local runtime images exist, including persistent and prior startup-attempt images. |
| `docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506 --format ...` | Read-only inspect confirmed local image metadata for the persistent image. |
| `docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-18789-bindlan-token-20260505114149 --format ...` | Read-only inspect confirmed local image metadata for the prior startup-attempt image. |

No local image export or extraction was performed in this gate.

## Required Answers

### 1. Is there a modifiable OpenClaw startup source/build-context in repo or approved artifacts?

No confirmed modifiable startup source/build-context exists in the repo or approved source artifact set.

Evidence:

- Repo search found no source/build-context file for `/defaults/openclaw-agent/config.yaml`, `/etc/services.d/openclaw-agent/run`, `openclaw_command`, `gateway.bind`, `--bind lan`, or equivalent startup control outside existing evidence text.
- `/tmp/gtclaw-runtime-patch/**` contains only the four approved control-ui static files:
  - `index.html`
  - `assets/i18n-B06L7jQN.js`
  - `assets/zh-CN-B26mMdbY.js`
  - `assets/index-M4TNVXB3.js`
- `20260505-control-ui-18789-fix-implementation.md` proves those startup paths exist inside runtime image/pod context, but that is not the same as an approved, modifiable source/build-context.
- Local Docker images exist, but built images are not source/build-context. Directly editing an extracted image filesystem would be an unreviewed artifact recovery/creation path and should not be treated as source.

Decision: cannot enter Runtime Startup Artifact Implementation Gate from current repo artifacts.

### 2. Minimal compliant startup source artifact recovery/creation plan

Minimum compliant plan:

1. Get explicit user approval for a Runtime Startup Source Artifact Recovery Gate.
2. Create a new local-only workspace under `/tmp/gtclaw-startup-source-inspect-*`.
3. Perform read-only extraction from a specified local image digest/tag only, without build/tag/push/pull and without runtime/K8S/database/registry mutation.
4. Extract only:
   - `/defaults/openclaw-agent/config.yaml`
   - `/etc/services.d/openclaw-agent/run`
5. Record file mode, owner metadata if available, SHA-256, byte size, parent image digest, and exact extraction command with no secret values.
6. Classify whether the extracted files are sufficient to create a reviewed startup source artifact that changes only `gateway.bind` / `--bind lan` and `--auth token` or equivalent.
7. Stop for Commander/user review before any build or runtime mutation.

Yes, explicit user approval is needed before extracting `/defaults/openclaw-agent/config.yaml` or `/etc/services.d/openclaw-agent/run` from a local image, even read-only, because this gate did not authorize extraction and because extraction would create a new startup source artifact under `/tmp/gtclaw-startup-source-inspect-*`.

### 3. Is `OPENCLAW_GATEWAY_TOKEN` plus `--auth token` enough for HTTP control-ui to accept `Authorization: Bearer <token>`?

For HTTP control-ui only: likely yes, based on current evidence.

Evidence:

- Prior runtime evidence showed OpenClaw refused `--bind lan` with auth mode `none` and explicitly named `OPENCLAW_GATEWAY_TOKEN` or `--token`/password as required auth material.
- The token-auth startup attempt using `--bind lan --auth token` progressed beyond the prior auth refusal point.
- The A1 source evidence records that local OpenClaw package evidence confirmed HTTP token auth accepts `Authorization: Bearer <token>`.
- Backend source now injects `OPENCLAW_GATEWAY_TOKEN` into OpenClaw pods and injects upstream `Authorization: Bearer <token>` for HTTP control-ui proxy requests, without exposing the token through JSON.

Limit:

- This answer covers HTTP control-ui upstream auth only.
- It does not prove the WebSocket control-ui auth contract.

### 4. Real control-ui WebSocket auth contract

The visible control-ui client contract is WebSocket application-level connect auth, not a browser-supplied upstream Authorization header.

Evidence from `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` read-only snippet scan:

- The client creates `new WebSocket(this.opts.url)` without custom Authorization headers. Browsers cannot set arbitrary WebSocket handshake headers through the standard `WebSocket` constructor.
- After the socket opens, the client calls `request("connect", buildConnectParams(...))`.
- `buildConnectParams(...)` includes an `auth` object derived from configured token/password state.
- The minified helper maps an auth token to an object equivalent to `connectParams.auth.token`.

Decision:

- Current evidence does not prove that the OpenClaw WebSocket server accepts upstream `Authorization: Bearer <token>` on the WebSocket handshake as a substitute for `connectParams.auth.token`.
- If the server does not accept handshake Authorization as connection auth, GTManager must implement a protocol design change before runtime verification:
  - preferred: a WebSocket bridge that intercepts the initial `connect` request and injects server-side `connectParams.auth.token` without exposing the OpenClaw token to the browser;
  - alternative: GTManager HTML/JS rewrite that causes the browser client to provide `connectParams.auth.token`, but this risks exposing the OpenClaw upstream token and needs explicit security review.

### 5. Can current backend bearer-header WebSocket injection be directly runtime verified?

No for the full control-ui WebSocket contract.

The current backend A1 source path injects `Authorization: Bearer <token>` into the upstream WebSocket handshake header. That may be useful if OpenClaw treats handshake Authorization as trusted gateway auth, but this gate did not prove that server behavior. The visible client contract instead sends auth inside the WebSocket JSON `connect` request.

Therefore a runtime verification of the current bearer-header-only WebSocket injection would be incomplete and could produce a false negative or false positive unless the OpenClaw server source or a controlled protocol test confirms that handshake Authorization is accepted for the `connect` phase.

Decision: protocol design change or external expert review is needed before treating current WebSocket bearer header injection as runtime-verifiable completion.

### 6. Recommended next gate

Recommended next gate: external expert escalation.

Reason:

- No repo-local or approved artifact startup source/build-context was confirmed.
- Creating a startup source artifact requires a new approved image extraction/recovery gate.
- HTTP token auth is sufficiently indicated, but WebSocket auth is not closed because visible client behavior requires `connectParams.auth.token`, while current backend source only injects handshake `Authorization: Bearer <token>`.
- Continuing directly into Runtime Startup Artifact Implementation would risk building a startup image that still fails control-ui WebSocket auth.

Secondary option if Commander does not escalate immediately: WS Auth Protocol Design Gate, followed by Runtime Startup Source Artifact Recovery Gate. Runtime Startup Artifact Implementation should wait until both source artifact and WebSocket auth contract are resolved.

## Decision Matrix

| Question | Decision |
| --- | --- |
| Startup source present in repo? | No. |
| Startup source present in `/tmp/gtclaw-runtime-patch`? | No. |
| Local image contains startup files? | Prior evidence indicates yes, but this gate did not extract them. |
| Need approval to extract `/defaults/openclaw-agent/config.yaml`? | Yes. |
| Need approval to extract `/etc/services.d/openclaw-agent/run`? | Yes. |
| `OPENCLAW_GATEWAY_TOKEN` + `--auth token` enough for HTTP `Authorization: Bearer <token>`? | Likely yes for HTTP, not enough to close WebSocket. |
| WebSocket accepts upstream Authorization header? | Not proven. Visible client contract uses `connectParams.auth.token`. |
| Current backend WebSocket bearer-header injection directly runtime-verifiable? | No, not as a complete acceptance path. |
| Enter Runtime Startup Artifact Implementation now? | No. |
| Gate verdict | `RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED`. |

## Negative Evidence

- no implementation
- no build/tag/push/pull
- no runtime mutation
- no runtime image/resource setting mutation
- no K8S write
- no database write
- no registry mutation
- no fresh instance create/delete/modify
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no pod file read/write in this gate
- no token value
- no cookie value
- no token-bearing URL
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close

## Verification Commands

Required post-write verification:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md
rg -n "RUNTIME_STARTUP_WS_AUTH_CONTRACT_DONE|RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED|startup source|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|gateway.bind|--bind lan|OPENCLAW_GATEWAY_TOKEN|Authorization: Bearer|connectParams.auth.token|WebSocket|no implementation|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
```

## Post-write Verification Results

| Command | Result |
| --- | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md` | Exit `0`; no whitespace diagnostics. |
| `rg -n "RUNTIME_STARTUP_WS_AUTH_CONTRACT_DONE|RUNTIME_STARTUP_WS_AUTH_CONTRACT_BLOCKED|startup source|/defaults/openclaw-agent/config.yaml|/etc/services.d/openclaw-agent/run|gateway.bind|--bind lan|OPENCLAW_GATEWAY_TOKEN|Authorization: Bearer|connectParams.auth.token|WebSocket|no implementation|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md` | Exit `0`; required markers found. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-ws-auth-contract-investigation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Shows this new evidence and the A1 source evidence as untracked in the path-limited status. |
| Secret-shape scan for token/cookie/header values in this evidence | Exit `1`; no matching token-shaped URL, cookie header, or non-redacted `Authorization: Bearer` value was found. |
