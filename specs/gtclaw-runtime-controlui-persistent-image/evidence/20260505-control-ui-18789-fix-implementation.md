# Control-UI 18789 Fix Implementation Evidence - 2026-05-05

## Verdict

`CONTROL_UI_18789_FIX_IMPLEMENTATION_BLOCKED`

Option A was implemented as far as the approved runtime image / startup-level scope allowed, but the gate remains `BLOCKED`.

Two non-`latest` local-registry runtime images were built and pushed. The first changed the OpenClaw gateway startup bind mode from loopback to LAN-facing. The runtime rejected that because OpenClaw refuses non-loopback gateway binding when auth mode is `none`. The second added the supported token auth mode at startup, reusing the runtime config's existing token material without recording the token value. That allowed gateway startup to pass the bind/auth refusal point, but the fresh disposable instance was OOMKilled at `1Gi` before listener/reachability evidence could be captured.

Independent blocker: the fresh disposable instances created by the current control plane generated Services with only port `3001`; no fresh Service exposed `18789`. Because Option B backend/proxy/topology changes were explicitly not authorized, ServiceIP `18789` could not be made to pass without leaving scope.

No Chrome DevTools MCP browser E2E or `/proxy/` desktop regression rerun was executed after the failed pod/service preconditions. No `passes:true`, no Close, no longterm write-back, and no Mem0 write were performed.

## Scope Statement

Allowed implementation artifacts touched:

- local runtime image builds under `localhost:5001/clawmanager-openclaw/openclaw:*`
- in-cluster test image setting for `openclaw`, later restored
- task-created disposable instances `7` and `8`, later deleted
- this evidence file only:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md`

Not modified:

- `backend/`
- `frontend/`
- `deployments/`
- `docs/`
- `longterm/`
- `AgentTeam/`
- spec/plan/tasks
- existing evidence files
- `.codex/auth.json`
- `.codex/config.toml`

Registry constraints honored:

- no `latest` tag was overwritten
- no mutable tag was reused
- no external registry was used

Secret hygiene:

- no token value, cookie value, authorization header, or token-bearing access URL is recorded in this evidence
- one `kubectl describe pod` command printed environment values in raw tool output; this evidence intentionally redacts those values and records only non-secret metadata

## Dependency Gate Review

| Dependency | Status used |
| --- | --- |
| `20260504-control-ui-18789-root-cause.md` | Root cause completed: `openclaw-gateway` listened only on `127.0.0.1:18789`; PodIP and ServiceIP `18789` were refused; Service/Endpoint metadata for the previous fresh instance was correct. |
| `20260504-control-ui-18789-fix-approval-packet.md` | Commander-reviewed fix packet recommended Option A: runtime image / startup-level listen-address fix. Option B backend/proxy topology fix was not authorized. |
| User approval | User explicitly approved Option A and explicitly disallowed Option B, Close, `passes:true`, longterm write-back, and Mem0 write. |

## Bind Source Located

Read-only runtime inspection found the startup source of truth:

| Runtime path / command | Finding |
| --- | --- |
| `/etc/services.d/openclaw-agent/run` | Copies `/defaults/openclaw-agent/config.yaml` into `/etc/openclaw-agent/config.yaml` if absent, then starts `openclaw-agent`. |
| `/defaults/openclaw-agent/config.yaml` | Starts `openclaw gateway run` through `openclaw_command`. |
| `openclaw gateway run --help` | Supports `--bind <mode>` with modes including `loopback`, `lan`, `tailnet`, `auto`, and `custom`; supports `--auth token`. |
| `/config/.openclaw/openclaw.json` in the test runtime | `gateway.bind` was `loopback`, `gateway.port` was `18789`, auth mode was `none`, and token material was present but not printed. |

## Runtime Images Built

### Attempt 1 - Bind LAN Only

Image:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-18789-bindlan-20260505113050`

Digest evidence:

| Kind | Digest |
| --- | --- |
| image index | `sha256:68005bdbe3b093cc4eafa9e3c5db344b3e3daac8a78379a745e03fa515450678` |
| linux/arm64 manifest | `sha256:99412ec1a5eb7afec723d778ba43ea0d113363c8ecf833836845668045b2ac56` |

Startup delta:

```yaml
openclaw_command:
  - openclaw
  - gateway
  - run
  - --bind
  - lan
openclaw_config_path: /config/.openclaw/openclaw.json
```

Result:

Fresh instance `7` used the new image digest but `openclaw-gateway` did not listen. Runtime log showed:

```text
auth mode=none explicitly configured; all gateway connections are unauthenticated.
Refusing to bind gateway to lan without auth.
Set gateway.auth.token/password (or OPENCLAW_GATEWAY_TOKEN/OPENCLAW_GATEWAY_PASSWORD) or pass --token/--password.
```

### Attempt 2 - Bind LAN With Supported Token Auth Mode

Image:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-18789-bindlan-token-20260505114149`

Digest evidence:

| Kind | Digest |
| --- | --- |
| image index | `sha256:623631db38100e366f51371cb1c4ccfa16bb1aa46979b52af804ad16ff8a6015` |
| linux/arm64 manifest | `sha256:4719d58df0734d058c684c7210fcd677043ab6dc9011d95d144de952705d1df1` |

Startup delta:

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

Result:

Fresh instance `8` pulled and started the new image:

```text
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-18789-bindlan-token-20260505114149
imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:623631db38100e366f51371cb1c4ccfa16bb1aa46979b52af804ad16ff8a6015
```

The runtime log passed the prior auth refusal point:

```text
[gateway] loading configuration...
[gateway] resolving authentication...
[gateway] starting...
[gateway] auto-enabled plugins:
- browser configured, enabled automatically.
```

The pod then terminated:

```text
phase=Failed
terminated.reason=OOMKilled
terminated.exitCode=137
```

## Control-UI Hash / Size Preservation

The built image content for the four verified control-ui files remained unchanged:

| File | SHA-256 | Size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

Instance `7` also matched the same four hashes and sizes before it was deleted. Instance `8` was OOMKilled before pod file hash verification could be repeated, so the image-level hash preservation evidence is the final available evidence for attempt 2.

## Fresh Instance / Service Verification

| Instance | Image / resource result | Service result | Listener / reachability result |
| --- | --- | --- | --- |
| `6` | First retry at `2Gi` could not schedule because of insufficient node memory; deleted. | Not used. | Not reached. |
| `7` | Running at `1Gi` with image index digest `sha256:68005bdbe3b093cc4eafa9e3c5db344b3e3daac8a78379a745e03fa515450678`; later deleted. | Service `clawreef-7-gtclaw-18789-fix-20260505113544-svc` exposed only `3001/TCP`. | No `:18789` listener; `127.0.0.1:18789`, PodIP `10.42.0.63:18789`, and ServiceIP `10.43.138.96:18789` could not pass because gateway refused LAN bind without auth. |
| `8` | Pulled image index digest `sha256:623631db38100e366f51371cb1c4ccfa16bb1aa46979b52af804ad16ff8a6015`; terminated `OOMKilled` at `1Gi`; later deleted. | Service `clawreef-8-gtclaw-18789-fix2-20260505114346-svc` exposed only `3001/TCP`. | Could not exec after termination to capture listener or `127.0.0.1` / PodIP / ServiceIP probes. |

Service evidence for instance `8`:

```text
NAME                                              TYPE        CLUSTER-IP      PORT(S)
clawreef-8-gtclaw-18789-fix2-20260505114346-svc   ClusterIP   10.43.210.191   3001/TCP
```

Node resource evidence:

```text
memory requests after task-created failed pods were not counted: 6284Mi (80%)
running existing OpenClaw instances: instance 3 = 4Gi, instance 5 = 2Gi
```

Interpretation:

- `1Gi` fresh disposable instance can schedule but attempt 2 OOMKills after gateway startup begins.
- `2Gi` fresh disposable instance was not schedulable without stopping non-task existing workloads.
- Stopping or modifying existing non-task instances was not authorized.
- The current fresh-instance Service shape does not expose `18789`, so required ServiceIP `18789` validation cannot pass under Option A alone in this environment.

## Required Gate Outcomes

| Required gate | Result |
| --- | --- |
| `127.0.0.1:18789` reachable | `BLOCKED`; attempt 1 had no listener, attempt 2 pod terminated before probe. |
| PodIP `18789` reachable | `BLOCKED`; attempt 1 no listener, attempt 2 pod terminated before probe. |
| ServiceIP `18789` reachable | `BLOCKED`; fresh Services for attempts 1 and 2 exposed only `3001/TCP`, not `18789/TCP`. |
| Listener shows `0.0.0.0:18789` or equivalent | `BLOCKED`; attempt 1 no listener, attempt 2 terminated before listener scan. |
| Fresh pod imageID is new digest | `PASS` for instance `8`: `sha256:623631db38100e366f51371cb1c4ccfa16bb1aa46979b52af804ad16ff8a6015`. |
| Four control-ui file hash/size unchanged | `PASS` at image level for attempt 2; `PASS` at pod level for attempt 1. |
| Chrome DevTools MCP `/control-ui/` E2E | Not run because pod/service preconditions failed. |
| `/proxy/` desktop regression | Not rerun because pod/service preconditions failed; previous root-cause evidence had it passing. |
| Restore previous setting | `PASS`; see restore section. |

## Restore Previous Setting Result

Previous `openclaw` runtime image setting recorded before mutation:

```json
{
  "instance_type": "openclaw",
  "display_name": "OpenClaw ARM Local Registry",
  "image": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434",
  "is_enabled": true
}
```

Cleanup / restore results:

| Action | Result |
| --- | --- |
| Delete task-created instance `7` | HTTP `200` |
| Delete task-created instance `8` | HTTP `200`; no pod/service/PVC resources remained for `instance-id=8` |
| Restore previous `openclaw` image setting | HTTP `200` |
| Read back restored setting | `openclaw` image restored to `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` |

## Stop Decision

Stop condition reached.

The implementation cannot be accepted under the current authorization because:

1. The minimal runtime bind fix requires OpenClaw gateway auth when binding to a pod/service-facing address.
2. Adding supported token auth at startup progressed beyond the first refusal but the fresh disposable instance OOMKilled at the only schedulable `1Gi` resource size.
3. Fresh Services generated during this implementation did not expose `18789`, so ServiceIP `18789` validation cannot pass without an unapproved topology/control-plane change or manual Service patch.

Continuing would require at least one new approval:

- authorize a runtime resource/test-environment adjustment that allows a `2Gi` or equivalent fresh instance without disturbing non-task workloads, and explain how fresh Service `18789` should be created; or
- authorize an Option B / topology-control-plane packet to address Service generation and upstream auth/proxy behavior; or
- authorize a separate manual Kubernetes test harness, explicitly noting that it cannot count as normal fresh-instance acceptance evidence.

No such approval was present in this gate.

## Post-Write Verification Commands

Post-write verification results:

| Command | Result |
| --- | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md` | Exit `0`; no whitespace diagnostics. |
| `rg -n "CONTROL_UI_18789_FIX_IMPLEMENTATION_BLOCKED|BLOCKED|gtclaw-controlui-18789-bindlan|gtclaw-controlui-18789-bindlan-token|623631db|4719d58|68005bdb|127\\.0\\.0\\.1:18789|PodIP|ServiceIP|openclaw-gateway|--bind|--auth|OOMKilled|restore previous setting|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md` | Exit `0`; required evidence markers found. |
| Secret-shape `rg` scan for authorization headers, token/cookie fields, and known token prefixes | Exit `1` with no output; no matching secret-shaped strings found in this evidence. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md` | Shows the new implementation evidence plus the two dependency evidence files as untracked under this path-limited view. |

Command forms used:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md
rg -n "CONTROL_UI_18789_FIX_IMPLEMENTATION_BLOCKED|BLOCKED|gtclaw-controlui-18789-bindlan|gtclaw-controlui-18789-bindlan-token|623631db|4719d58|68005bdb|127\\.0\\.0\\.1:18789|PodIP|ServiceIP|openclaw-gateway|--bind|--auth|OOMKilled|restore previous setting|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md
rg secret-scan-redacted-pattern specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md
```
