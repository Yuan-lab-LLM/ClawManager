# GTManager M1 Final E2E Evidence - Assets Refreshed Candidate

Date: 2026-04-30T12:32:07+08:00

Target: `https://localhost:30443`

Deployment candidate: `clawmanager:m1-gtmanager-assets-local-20260430120731`

Evidence root: `specs/gtmanager-m1-branding-localization/evidence/assets-final-2026-04-30/`

Result JSON: `assets-final-2026-04-30/playwright-assets-final-result.json`

## Verdict

**E2E failed**

Close/write-back was not performed. `passes:true` was not written.

Failure reason: clean-profile `/login` did not expose a visible image logo using `/gtmanager-logo.png`. The page favicon uses `/gtmanager-logo.png`, and the PNG itself loads correctly in the browser, but the login DOM had `logoImages: []`.

## Commands Run

| Command | Exit | Evidence |
| --- | ---: | --- |
| `curl -skS -w ... https://localhost:30443/healthz` | 0 | `assets-final-2026-04-30/logs/healthz.log` |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath=...image...` | 0 | `assets-final-2026-04-30/logs/deployment-image.log` |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o wide` | 0 | `assets-final-2026-04-30/logs/deployment-wide.log` |
| `kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide` | 0 | `assets-final-2026-04-30/logs/pods-wide.log` |
| `kubectl get svc,endpoints -n clawmanager-system clawmanager-gateway` | 0 | `assets-final-2026-04-30/logs/gateway-service-endpoints.log` |
| `curl -skS https://localhost:30443/login -o .../login.html` | 0 | `assets-final-2026-04-30/logs/login.html` |
| Extract deployed JS path with `rg` | 0 | `assets-final-2026-04-30/logs/deployed-js-path.txt` |
| `curl -skS https://localhost:30443/assets/index-BcCrhHyr.js -o .../deployed-app.js` | 0 | `assets-final-2026-04-30/logs/deployed-app.js` |
| JS bundle string checks with `grep -q` | see log | `assets-final-2026-04-30/logs/check-js-*.log` |
| `curl -skSI https://localhost:30443/gtmanager-logo.png` | 0 | `assets-final-2026-04-30/logs/logo-head.log` |
| `curl -skS https://localhost:30443/gtmanager-logo.png -o .../gtmanager-logo-deployed.png` | 0 | `assets-final-2026-04-30/gtmanager-logo-deployed.png` |
| `file .../gtmanager-logo-deployed.png` | 0 | `assets-final-2026-04-30/logs/logo-file.log` |
| `shasum -a 256 .../gtmanager-logo-deployed.png` | 0 | `assets-final-2026-04-30/logs/logo-sha256.log` |
| `npm init -y` in `/tmp/clawmanager-e2e-assets-final-2026-04-30` | 0 | temporary npm workspace |
| `npm install playwright@1.59.1` in `/tmp/clawmanager-e2e-assets-final-2026-04-30` | 0 | `assets-final-2026-04-30/logs/npm-install-playwright.log` |
| `node assets-final-2026-04-30/e2e-assets-final.mjs` | 0 | `assets-final-2026-04-30/logs/playwright-assets-final.log` |

Note: one attempted `npm --prefix /tmp/... init -y` unexpectedly created a root `package.json`; it was created by this run, immediately removed, and the final file-scope check shows no root `package.json` remains.

## Static Checks

| Check | Observed |
| --- | --- |
| `/healthz` | HTTP 200, body `ok` |
| Deployment image | `clawmanager:m1-gtmanager-assets-local-20260430120731` |
| Deployed JS path | `/assets/index-BcCrhHyr.js` |
| `GTManager` in JS | present |
| `登录 GTManager` in JS | present |
| `Sign in to GTManager` in JS | present |
| `clawmanager_locale` in JS | present |
| `ClawManager` in JS | absent |
| `Sign in to ClawManager` in JS | absent |
| Protected runtime/image identifiers | `clawmanager-system`, `clawmanager-app`, `clawmanager-gateway`, and `clawmanager-openclaw-image` observed in static deployed bundle or read-only Kubernetes evidence |

Authoritative assertion table: `assets-final-2026-04-30/logs/assertion-status.tsv`.

## `/gtmanager-logo.png`

| Check | Observed |
| --- | --- |
| HTTP | 200 |
| Content-Type | `image/png` |
| SHA256 | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |
| File metadata | PNG image data, 115 x 120, RGBA |
| Browser direct load | complete, naturalWidth 115, naturalHeight 120 |
| Screenshot | `assets-final-2026-04-30/screenshots/gtmanager-logo-browser-load.png` |

Strict-decoder caveat remains unresolved per `2026-04-30-deployment-refresh-assets.md`; browser loading is OK and recorded here.

## Clean `/login`

Screenshot: `assets-final-2026-04-30/screenshots/login-clean-profile-before-submit.png`

| Check | Observed | Result |
| --- | --- | --- |
| `html lang` | `zh` | PASS |
| Browser title | `GTManager` | PASS |
| Visible text | `登录 GTManager` | PASS |
| Language selector default | value `zh`, selected text `中文` | PASS |
| Favicon href | `/gtmanager-logo.png` | PASS |
| Visible image logo | no `img` elements on `/login` | FAIL |
| Product-facing `ClawManager` | absent | PASS |
| `GTClaw` | absent | PASS |

This is the only failing assertion in the final E2E run.

## UI Auth Flow

Playwright used a clean Chromium context with `ignoreHTTPSErrors: true`. Login was submitted through the UI with `admin` / `admin123`.

| Timing point | Timestamp | Delta from submit |
| --- | --- | ---: |
| Submit | `2026-04-30T04:31:01.527Z` | 0 ms |
| Route changed to authenticated route | `2026-04-30T04:31:01.627Z` | 100 ms |
| `access_token` + `refresh_token` observed | `2026-04-30T04:31:01.642Z` | 115 ms |
| Stable non-loading body | `2026-04-30T04:31:02.256Z` | 729 ms |

- Final post-login URL: `https://localhost:30443/dashboard`
- Stable body window: 510 ms
- API token bootstrap was not used.
- `/api/v1/auth/login`: HTTP 200
- `/api/v1/auth/me`: HTTP 200
- Console events: 1 informational `WebSocket connected`
- Console errors: 0
- Page errors: 0
- Request failures: 0

Logs:
- `assets-final-2026-04-30/logs/auth-endpoint-statuses.json`
- `assets-final-2026-04-30/logs/console-events.json`
- `assets-final-2026-04-30/logs/page-errors.json`
- `assets-final-2026-04-30/logs/request-failures.json`

## Route Coverage

Admin routes all passed page-level checks: title `GTManager`, document language `zh`, visible Chinese UI, observable GTManager shell branding, no product-facing `ClawManager`, no `GTClaw`, and no stable loading-only body.

| Route | Screenshot | Result |
| --- | --- | --- |
| `/admin` | `assets-final-2026-04-30/screenshots/admin-dashboard.png` | PASS |
| `/admin/users` | `assets-final-2026-04-30/screenshots/admin-users.png` | PASS |
| `/admin/instances` | `assets-final-2026-04-30/screenshots/admin-instances.png` | PASS |
| `/admin/ai-gateway` | `assets-final-2026-04-30/screenshots/admin-ai-gateway.png` | PASS |
| `/admin/security` | `assets-final-2026-04-30/screenshots/admin-security-center.png` | PASS |
| `/admin/settings` | `assets-final-2026-04-30/screenshots/admin-settings.png` | PASS |

No separate regular-user credentials were provided. User route coverage used the admin authenticated context and is recorded as a limitation.

| Route | Screenshot | Result |
| --- | --- | --- |
| `/dashboard` | `assets-final-2026-04-30/screenshots/user-dashboard.png` | PASS under admin authenticated context |
| `/instances` | `assets-final-2026-04-30/screenshots/user-instances.png` | PASS under admin authenticated context |
| `/openclaw-configs` | `assets-final-2026-04-30/screenshots/user-openclaw-resource-management.png` | PASS under admin authenticated context |
| `/settings` | `assets-final-2026-04-30/screenshots/user-settings.png` | PASS under admin authenticated context |
| `/portal` | `assets-final-2026-04-30/screenshots/user-instance-access-surface.png` | PASS under admin authenticated context |

## Wording and Protected Identifiers

OpenClaw runtime wording remains visible where runtime/resource behavior is named:

- `/admin/settings`
- `/openclaw-configs`

Protected identifiers preserved in evidence:

- `/healthz`
- `/api/v1/auth/login`
- `/api/v1/auth/me`
- `clawmanager_locale`
- `clawmanager-system`
- `clawmanager-app`
- `clawmanager-gateway`
- `clawmanager-openclaw-image`
- `clawmanager-user-1`

## Blockers and Caveats

- **Failure:** clean `/login` has no visible `img` logo using `/gtmanager-logo.png`.
- No separate regular-user credentials were available; user-route checks used admin authenticated context.
- Strict PNG decoder caveat remains unresolved, but Chromium browser load succeeds with the expected dimensions.
- No source, backend, frontend, deployment, docs, longterm, spec/plan/tasks, Close, rollout, build/import, or feature pass-flag write-back was performed.
