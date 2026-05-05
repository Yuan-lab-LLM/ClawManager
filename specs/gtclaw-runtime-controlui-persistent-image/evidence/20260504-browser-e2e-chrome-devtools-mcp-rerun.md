# Browser E2E Chrome DevTools MCP Rerun - 2026-05-04

## Verdict

`BLOCKED`

`BROWSER_E2E_CHROME_DEVTOOLS_MCP_DONE` was not reached because all three authorized `/control-ui/` browser routes returned HTTP `502` through the GTManager proxy before the GTClaw control-ui DOM could render.

This is a strict rerun of the Browser E2E gate using the Chrome DevTools MCP server. It supplements, and does not overwrite, the prior evidence file:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md`

## Gate Statement

Strict Browser E2E rerun only. No pod, instance, runtime image/resource setting, Kubernetes resource, database, registry, image, or tag was created, deleted, restarted, patched, or modified. No pod files were read or written. No `kubectl cp` was used. No backend, frontend, deployment, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence file, `/tmp/gtclaw-runtime-patch/**`, Mem0, `passes:true`, or Close state was modified.

Only this new repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md`

Temporary sanitized run output was written to:

`/tmp/gtclaw-strict-e2e-mcp-result.json`

## Tooling

| Field | Value |
| --- | --- |
| MCP package | `chrome-devtools-mcp@0.23.0` |
| MCP server | `chrome_devtools` / `Chrome DevTools MCP server` / `0.23.0` |
| Transport | stdio JSON-RPC, line-delimited |
| Browser | Google Chrome |
| Executable | `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome` |
| Mode | headless, isolated temporary browser context |
| Viewport | `1280x720` |
| TLS | `--acceptInsecureCerts` for local self-signed `https://localhost:30443` |
| Network privacy | `--redactNetworkHeaders`, `--no-usage-statistics`, `--no-performance-crux` |

MCP tools used:

- `new_page`
- `resize_page`
- `evaluate_script`
- `navigate_page`
- `take_snapshot`
- `list_network_requests`

This rerun used Chrome DevTools MCP directly, not local Playwright, not Playwright MCP, and not manual human browser inspection.

## Dependency Gate

| Dependency | Evidence file | Result used |
| --- | --- | --- |
| Browser E2E approval packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md` | Browser E2E approved for instance `5` only |
| Fresh pod hash evidence | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md` | Fresh deployed pod hash matched all four allowlist files |
| Previous browser E2E evidence | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md` | Prior CDP-based result was `BLOCKED`; this rerun validates with Chrome DevTools MCP |

## Authorized Target

| Field | Value |
| --- | --- |
| Instance | `5` / `gtclaw-fresh-20260504095843` |
| Pod | `clawmanager-user-1/clawreef-5-gtclaw-fresh-20260504095843` / `desktop` |
| Image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| Image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

## API Preflight

Authentication and access generation were performed inside the isolated Chrome session through Chrome DevTools MCP `evaluate_script`. Credential, token, and cookie values were not returned or recorded.

| Check | Result |
| --- | --- |
| Admin authentication | HTTP `200`; authenticated |
| `/api/v1/auth/me` | HTTP `200` |
| Instance lookup for `5` | HTTP `200` |
| `/api/v1/instances/5/access?mode=control-ui` | HTTP `200`; access mode `control-ui`; safe path `/api/v1/instances/5/control-ui/`; target port `18789`; token field present but value not recorded |
| `/api/v1/instances/5/access?mode=desktop` | HTTP `200`; access mode `desktop`; safe path `/api/v1/instances/5/proxy/`; target port `3001`; token field present but value not recorded |
| Browser location token check | `false`; no requested browser location contained `token=` |

## Browser Control-UI DOM Checks

All control-ui checks were performed by navigating the isolated Chrome page to tokenless safe paths after route-cookie generation.

| Route label | Safe requested path | Browser document HTTP | Visible DOM body rendered `GTClaw 控制台` | `lang=zh-CN` | Chinese markers `控制台`, `网关`, `连接` | Desktop fallback | 404 marker | 502/proxy marker | Result |
| --- | --- | ---: | --- | --- | --- | --- | --- | --- | --- |
| `/control-ui/` root | `/api/v1/instances/5/control-ui/` | `502` | no | no | missing | no | no | yes | fail |
| `/control-ui/chat?session=main` | `/api/v1/instances/5/control-ui/chat?session=main` | `502` | no | no | missing | no | no | yes | fail |
| `/control-ui/` history fallback | `/api/v1/instances/5/control-ui/history-fallback-check` | `502` | no | no | missing | no | no | yes | fail |

Chrome DevTools MCP accessibility snapshots for the three failing routes showed browser-visible proxy errors. The redacted failure class was:

```text
Failed to proxy request: failed to execute proxy request: Get "http://10.43.47.127:18789/...": dial tcp 10.43.47.127:18789: connect: connection refused
```

Chrome DevTools MCP network summaries recorded:

| Route | Network document request |
| --- | --- |
| `/api/v1/instances/5/control-ui/` | `GET /api/v1/instances/5/control-ui/ [502]` |
| `/api/v1/instances/5/control-ui/chat?session=main` | `GET /api/v1/instances/5/control-ui/chat?session=main [502]` |
| `/api/v1/instances/5/control-ui/history-fallback-check` | `GET /api/v1/instances/5/control-ui/history-fallback-check [502]` |

## Desktop Regression

`access?mode=desktop` and `/proxy/` remained desktop behavior.

| Check | Result |
| --- | --- |
| `/api/v1/instances/5/proxy/` browser document HTTP | `200` |
| Browser title | `ClawManager Desktop` |
| Browser lang | `en` |
| Visible desktop body markers | `Waiting for stream...`, `Selkies`, `Video Settings`, `Clipboard`, `Files`, `Apps` |
| `GTClaw 控制台` in visible DOM body | no |
| Desktop replaced by control-ui | no |
| Network summary | `/api/v1/instances/5/proxy/ [200]`, JS/CSS/assets loaded under `/proxy/` |
| Desktop regression verdict | pass |

## Required Criteria Summary

| Criterion | Result |
| --- | --- |
| `/control-ui/` root visible DOM body renders `GTClaw 控制台` | fail: HTTP `502` |
| `/control-ui/chat?session=main` visible DOM body renders `GTClaw 控制台` | fail: HTTP `502` |
| history fallback visible DOM body renders `GTClaw 控制台` | fail: HTTP `502` |
| `lang=zh-CN` or equivalent DOM evidence | fail: no control-ui DOM rendered |
| Chinese markers `控制台`, `网关`, `连接` | fail: no control-ui DOM rendered |
| control-ui routes have no desktop fallback | pass |
| control-ui routes have no 404 marker | pass: `502`, not `404` |
| `access?mode=desktop` keeps desktop behavior | pass |
| `/proxy/` keeps desktop behavior | pass |
| no token value recorded | pass |
| no cookie value recorded | pass |
| no token-bearing URL recorded | pass |
| no passes:true | pass |
| no Close | pass |

## Secret Hygiene

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, `Bearer ...` value, `Set-Cookie`, `Cookie`, JWT-like value, or token-bearing URL was recorded in this evidence.

The sanitized run output reported:

| Secret pattern | Recorded |
| --- | --- |
| `token=` URL | no |
| `Bearer ...` pattern | no |
| `Cookie` / `Set-Cookie` header | no |
| default password literal | no |
| JWT-like token | no |

## Blocker

The strict Chrome DevTools MCP rerun confirms the prior blocker: the fresh instance and authorized access flow are reachable, but GTManager cannot connect to the runtime control-ui upstream on target port `18789` for instance `5`.

The current gate remains `BLOCKED`. This is not a browser tooling artifact: both the previous browser run and this Chrome DevTools MCP rerun show the same three-route `502` result, while `/proxy/` desktop regression succeeds.

No fix was attempted under this gate.

## Verification Commands

Commands to verify this rerun evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md
rg -n "BROWSER_E2E_CHROME_DEVTOOLS_MCP_DONE|BLOCKED|chrome-devtools-mcp@0.23.0|Chrome DevTools MCP|gtclaw-controlui-persistent-20260504005506|clawreef-5-gtclaw-fresh-20260504095843|/control-ui/|chat\\?session=main|history fallback|/proxy/|desktop regression|GTClaw 控制台|zh-CN|控制台|网关|连接|502|connection refused|no token value|no cookie value|no token-bearing URL|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md
```
