# Control-ui localization browser/manual E2E rerun evidence

Date/timezone: 2026-05-08, Asia/Shanghai

Role/task: Verifier, serial topology, Browser Manual E2E Rerun

Gate: CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_BROWSER_CAPABLE_RERUN_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_BLOCKED
- CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE

## Verdict

CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE

Route correctness and visible localization were observed for instance `16 / oc2gi-loc-221427`.

Known auth-contract blocker observed: `device signature invalid`. This rerun records the blocker only; it does not fix or patch trustedProxy, runtime auth, backend bridge behavior, device signature generation, token flow, password flow, cookies, cache, storage, Kubernetes, database, image, or runtime resources.

## Target

```text
instance_id=16
instance_name=oc2gi-loc-221427
expected_route=/api/v1/instances/16/control-ui
accepted_child_route=/api/v1/instances/16/control-ui/chat?session=main
```

Dependency runtime evidence read:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md`

Relevant dependency facts:

```text
CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE
instance_id=16
instance_name=oc2gi-loc-221427
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427
service=clawreef-16-oc2gi-loc-221427-svc
control_ui_18789=HTTP 200 on loopback, PodIP, and ServiceIP
runtime_path=/usr/local/lib/node_modules/openclaw/dist/control-ui
```

## Browser surface

Preferred Browser Use surface was checked first. The Node REPL `js` tool required by the in-app browser backend was not exposed in this session. Computer Use was also checked and returned an Apple Events permission failure.

Actual browser control surface used:

```text
browser=Google Chrome headless
control=Chrome DevTools Protocol
started_at_utc=2026-05-08T02:06:15.407Z
finished_at_utc=2026-05-08T02:06:23.851Z
browser_profile=temporary isolated Chrome profile
browser_storage_cache_cookie_cleanup=false
```

The browser used the visible ClawManager login form, then opened `/instances/16`. Credential values, tokens, cookies, auth headers, and full access URLs were not printed or recorded.

To keep the route observable in the CDP-controlled page, the browser session redirected the visible `window.open` action into the current tab after clicking the visible `打开 GTClaw 控制台` button. The access generation still came from the visible GTManager control-ui entry and the backend-set route cookie. No source, auth, proxy, storage, cookie, Kubernetes, database, image, or runtime mutation was made.

## Instance context

Observed GTManager instance page:

```text
route=/instances/16
instance_id_visible=true
instance_id_visible_text=实例 ID: 16
instance_name_visible=true
instance_name=oc2gi-loc-221427
control_ui_entry_button=打开 GTClaw 控制台
```

This confirms the browser context was instance `16 / oc2gi-loc-221427`, not a stale instance.

## Route observed

Observed control-ui route path:

```text
route_observed=/api/v1/instances/16/control-ui/chat?session=main
route_under_instance_16_control_ui=true
stale_instance_10_used=false
stale_instance_11_used=false
stale_instance_15_used=false
token_query_recorded=false
```

No token-bearing full URL was recorded.

## Localization observed

Browser-visible page state:

```text
document_title=GTClaw 控制台
html_lang=zh-CN
GTClaw 控制台 visible=true
zh_CN_visible_text=true
manual_connection_literal_visible=false
device_signature_invalid_visible=true
```

Visible GTClaw / zh-CN text observed:

```text
GTClaw
GTClaw 控制台
WebSocket URL 地址
网关令牌
密码 (不存储)
连接
device signature invalid
如何连接
在主机上启动网关：
获取带令牌的控制台 URL：
将 WebSocket URL 和令牌粘贴到上方，或直接打开带令牌的 URL。
```

The page rendered the expected GTClaw Chinese control-ui shell and zh-CN connection labels. The deeper runtime session did not reach an authenticated-ready chat state because the known `device signature invalid` auth-contract blocker was visible.

## Known blocker handling

Observed blocker:

```text
known_blocker=device signature invalid
known_blocker_class=auth-contract blocker
fix_attempted=false
trustedProxy_patch=false
runtime_auth_patch=false
```

This rerun treats the blocker as known auth-contract behavior. It does not attempt a trustedProxy patch, runtime auth patch, backend bridge fix, browser storage cleanup, cookie cleanup, cache cleanup, or secret entry.

## Secret hygiene

No token value, password value, key value, cookie value, bearer value, auth header value, JWT value, registry credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing full access URL is recorded in this evidence.

The recorded route is path-only and contains no token query.

## Forbidden actions confirmation

- no trustedProxy patch
- no runtime auth patch
- no backend source edits
- no frontend source edits
- no runtime source edits
- no control-ui artifact edits
- no storage cleanup
- no browser cache cleanup
- no browser cookie cleanup
- no Kubernetes mutation
- no database mutation
- no image mutation
- no runtime pod mutation
- no instance mutation
- no deployment mutation
- no longterm write-back
- no Mem0 write
- no `passes:true`
- no Close
- no git stage/commit/push

## Verification commands

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md
/Users/eduardogan/.npm-global/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/path/rg -n 'CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE|oc2gi-loc-221427|/api/v1/instances/16/control-ui/chat\\?session=main|GTClaw 控制台|zh-CN|device signature invalid|no trustedProxy patch|no runtime auth patch|no storage cleanup|passes:true|no Close|no git stage/commit/push' specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md
/Users/eduardogan/.npm-global/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/path/rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `sed -n '1,260p' ...` | `0` | Evidence content rendered for review. |
| `git diff --check -- ...` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found in the evidence file. |
| individual required marker loop | `0` | Output: `all required markers present`. |
| secret/access URL shape scan piped to `wc -l` | `0` | Output was `0`. |
| `git status --short -- ...` | `0` | Shows this allowed new rerun evidence file as untracked. |
