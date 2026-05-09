# Browser / Manual E2E Gate - Control UI Persistence

Verdict: `BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_BLOCKED: correct instance 15 control-ui route opened, but the visible GTClaw control-ui remained on a manual connection form and showed device signature invalid`

Timestamp: 2026-05-07 15:39:17 CST, Asia/Shanghai

## Scope

Gate role: Verifier

Topology: serial

Task type: Browser/manual E2E Gate

Target: instance 15 / `oc2gi-cp-150002`

Browser surface used: current authorized GTManager browser session in Comet.

Repo screenshots added: none.

## Browser Verification Steps

1. Used the current authorized GTManager browser session without requesting or entering credentials.
2. Opened the GTManager page for instance 15 by navigating the existing GTManager tab to `/instances/15`.
3. Confirmed the visible instance page showed:
   - `实例 ID: 15`
   - `oc2gi-cp-150002`
   - status `运行中`
   - type `OPENCLAW`
4. Clicked the visible `打开 GTClaw 控制台` button.
5. Observed a new GTClaw control-ui tab titled `GTClaw 控制台`.
6. Verified the new tab route path only, without recording a full access URL:
   - `/api/v1/instances/15/control-ui/chat?session=main`
7. Observed the visible GTClaw page state.

## Actual Route

Actual route path/query observed:

```text
/api/v1/instances/15/control-ui/chat?session=main
```

No full access URL plaintext, token value, password value, key value, cookie value, bearer value, auth header value, or credential value was recorded.

## Required Checks

| # | Required verification | Result |
| ---: | --- | --- |
| 1 | GTManager instance 15 page can open | PASS. `/instances/15` opened in the authorized GTManager session. |
| 2 | Page/context confirms current instance is `oc2gi-cp-150002` | PASS. The page visibly showed instance ID 15 and `oc2gi-cp-150002`. |
| 3 | Click enters `/api/v1/instances/15/control-ui` or child route | PASS. The opened child route was `/api/v1/instances/15/control-ui/chat?session=main`. |
| 4 | Does not use stale instance 10 or stale instance 11 WebSocket URL | PASS for observed route and visible WebSocket URL shape. The visible path referenced instance 15, not stale instance 10 or stale instance 11. |
| 5 | Does not display `来源不被允许` | PASS. `来源不被允许` was not visible. |
| 6 | Does not remain on `disconnected 1006` | PASS for this symptom. `disconnected 1006` was not visible. |
| 7 | User does not need to hand-fill WebSocket URL | FAIL. The visible page was a connection form with a `WebSocket URL 地址` input, although it was prefilled for instance 15. |
| 8 | User does not need to enter gateway token | FAIL/BLOCKED. The visible page showed a gateway token input placeholder and did not enter a usable connected state. No gateway token was entered. |
| 9 | User does not need to enter password | FAIL/BLOCKED. The visible page showed a password input placeholder and did not enter a usable connected state. No password was entered. |
| 10 | Visible page is usable GTClaw control-ui, not desktop/proxy/404/cache/error page | FAIL/BLOCKED. It was the GTClaw-branded control-ui route, not desktop `/proxy/` and not 404, but it remained on the manual connection form and displayed `device signature invalid`; it was not a usable connected GTClaw control-ui. |

## Blocked Details

Minimal reproduction:

1. In the authorized GTManager browser session, open `/instances/15`.
2. Confirm instance 15 / `oc2gi-cp-150002` is visible.
3. Click `打开 GTClaw 控制台`.
4. Observe `/api/v1/instances/15/control-ui/chat?session=main`.
5. Observe the GTClaw connection form with `WebSocket URL 地址`, gateway token input, password input, and `device signature invalid`.

Observed error: `device signature invalid`.

Stale route status: no stale instance 10 or stale instance 11 route was observed.

Origin status: `来源不被允许` was not observed.

1006 status: `disconnected 1006` was not observed.

Credential status: no WebSocket URL, gateway token, or password was typed, requested from the user, printed, or recorded as a sensitive value.

Storage status: no storage cleanup, no browser cache cleanup, and no browser cookie cleanup was performed.

## Explicit Forbidden Actions Not Executed

- no backend source modification
- no frontend source modification
- no deployments modification
- no docs modification
- no longterm write-back
- no AgentTeam modification
- no UnifiedFramework modification
- no spec.md / plan.md / tasks.md modification
- no existing evidence modification
- no runtime-startup artifact modification
- no control-ui source artifact modification
- no runtime image assembly artifact modification
- no kubectl command
- no k3d command
- no Helm command
- no instance create/delete/modify action
- no database operation
- no image build/tag/push/pull
- no registry cleanup
- no old session cleanup
- no old asset cleanup
- no old tag cleanup
- no Playwright automation
- no state-changing DevTools operation
- no browser storage/cache/cookie cleanup
- no token/password/key/cookie/bearer/auth header/access URL plaintext capture
- no passes:true
- no Close
- no Mem0 write
- no git stage/commit/push
