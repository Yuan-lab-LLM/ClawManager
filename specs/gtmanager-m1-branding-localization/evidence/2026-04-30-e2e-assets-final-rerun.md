# GTManager M1 Final E2E Rerun Evidence - Corrected Assets Criteria

Date: 2026-04-30T13:06:32+08:00

Target: `https://localhost:30443`

Deployment candidate: `clawmanager:m1-gtmanager-assets-local-20260430120731`

Evidence root: `specs/gtmanager-m1-branding-localization/evidence/assets-final-rerun-2026-04-30/`

Result JSON: `assets-final-rerun-2026-04-30/playwright-assets-final-rerun-result.json`

## Verdict

**E2E passed; Close/write-back still requires explicit user approval**

This rerun uses the corrected acceptance criteria: `/login` is not required to contain a visible image logo. `/login` records favicon and design facts; direct logo asset loading and authenticated shell/header logo usage are the logo assertions.

No source, frontend, backend, deployment, docs, longterm, spec/plan/tasks, build/import/rollout, Close, or `passes:true` write-back was performed.

## Commands Run

| Command | Exit | Evidence |
| --- | ---: | --- |
| `curl -skS -w ... https://localhost:30443/healthz` | 0 | `assets-final-rerun-2026-04-30/logs/healthz.log` |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath=...image...` | 0 | `assets-final-rerun-2026-04-30/logs/deployment-image.log` |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o wide` | 0 | `assets-final-rerun-2026-04-30/logs/deployment-wide.log` |
| `kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide` | 0 | `assets-final-rerun-2026-04-30/logs/pods-wide.log` |
| `kubectl get svc,endpoints -n clawmanager-system clawmanager-gateway` | 0 | `assets-final-rerun-2026-04-30/logs/gateway-service-endpoints.log` |
| `curl -skS https://localhost:30443/login -o .../login.html` | 0 | `assets-final-rerun-2026-04-30/logs/login.html` |
| Extract deployed JS path with `rg` | 0 | `assets-final-rerun-2026-04-30/logs/deployed-js-path.txt` |
| `curl -skS https://localhost:30443/assets/index-BcCrhHyr.js -o .../deployed-app.js` | 0 | `assets-final-rerun-2026-04-30/logs/deployed-app.js` |
| JS bundle string checks with `grep -q` | see log | `assets-final-rerun-2026-04-30/logs/check-js-*.log` |
| `curl -skSI https://localhost:30443/gtmanager-logo.png` | 0 | `assets-final-rerun-2026-04-30/logs/logo-head.log` |
| `curl -skS https://localhost:30443/gtmanager-logo.png -o .../gtmanager-logo-deployed.png` | 0 | `assets-final-rerun-2026-04-30/gtmanager-logo-deployed.png` |
| `file .../gtmanager-logo-deployed.png` | 0 | `assets-final-rerun-2026-04-30/logs/logo-file.log` |
| `shasum -a 256 .../gtmanager-logo-deployed.png` | 0 | `assets-final-rerun-2026-04-30/logs/logo-sha256.log` |
| `npm init -y` in `/tmp/clawmanager-e2e-assets-final-rerun-2026-04-30` | 0 | temporary npm workspace |
| `npm install playwright@1.59.1` in `/tmp/clawmanager-e2e-assets-final-rerun-2026-04-30` | 0 | `assets-final-rerun-2026-04-30/logs/npm-install-playwright.log` |
| `node assets-final-rerun-2026-04-30/e2e-assets-final-rerun.mjs` | 0 | `assets-final-rerun-2026-04-30/logs/playwright-assets-final-rerun.log` |

Full command status: `assets-final-rerun-2026-04-30/logs/command-status.tsv`

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
| Product-facing `ClawManager` in JS | absent |
| `Sign in to ClawManager` in JS | absent |
| Technical runtime/image identifier | `clawmanager-openclaw-image` present |
| Kubernetes identifiers | `clawmanager-system`, `clawmanager-app`, and `clawmanager-gateway` preserved in read-only cluster evidence |

Assertion table: `assets-final-rerun-2026-04-30/logs/assertion-status.tsv`.

## `/gtmanager-logo.png`

| Check | Observed |
| --- | --- |
| HTTP | 200 |
| Content-Type | `image/png` |
| SHA256 | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |
| File metadata | PNG image data, 115 x 120, RGBA |
| Browser direct load | complete, naturalWidth 115, naturalHeight 120 |
| Screenshot | `assets-final-rerun-2026-04-30/screenshots/gtmanager-logo-browser-load.png` |

Strict PNG decoder caveat remains recorded as unresolved. Chromium browser load is OK and the caveat is not treated as a failure under the corrected criteria.

## Clean `/login`

Screenshot: `assets-final-rerun-2026-04-30/screenshots/login-clean-profile-before-submit.png`

| Check | Observed | Result |
| --- | --- | --- |
| `html lang` | `zh` | PASS |
| Browser title | `GTManager` | PASS |
| Visible text | `登录 GTManager` | PASS |
| Language selector default | value `zh`, selected text `中文` | PASS |
| Favicon href | `/gtmanager-logo.png` | PASS |
| Product-facing `ClawManager` | absent | PASS |
| `GTClaw` | absent | PASS |
| `/login` visible image logo | no requirement; current design fact recorded | PASS |

Recorded design fact: `/login` had no image elements; this is not a failure in the corrected acceptance criteria.

## UI Auth Flow

Playwright used a clean Chromium context with `ignoreHTTPSErrors: true`. Login was submitted through the UI with `admin` / `admin123`.

| Timing point | Timestamp | Delta from submit |
| --- | --- | ---: |
| Submit | `2026-04-30T05:05:57.002Z` | 0 ms |
| Route changed to authenticated route | `2026-04-30T05:05:57.105Z` | 103 ms |
| `access_token` + `refresh_token` observed | `2026-04-30T05:05:57.117Z` | 115 ms |
| Stable non-loading body | `2026-04-30T05:05:57.735Z` | 733 ms |

- Final post-login URL: authenticated route.
- API token bootstrap was not used.
- `/api/v1/auth/login`: HTTP 200
- `/api/v1/auth/me`: HTTP 200
- Console events: 1 informational `WebSocket connected`
- Console errors: 0
- Page errors: 0
- Request failures: 0

Logs:
- `assets-final-rerun-2026-04-30/logs/auth-endpoint-statuses.json`
- `assets-final-rerun-2026-04-30/logs/console-events.json`
- `assets-final-rerun-2026-04-30/logs/page-errors.json`
- `assets-final-rerun-2026-04-30/logs/request-failures.json`

## Shell Logo

Shell logo evidence: `assets-final-rerun-2026-04-30/logs/shell-logo-images.json`

| Surface | Observed |
| --- | --- |
| Admin shell | visible `/gtmanager-logo.png`, alt `GTManager 标志`, natural size 115 x 120, rendered 36 x 36 |
| User shell | visible `/gtmanager-logo.png`, alt `GTManager 标志`, natural size 115 x 120, rendered 36 x 36 |

Screenshots:
- `assets-final-rerun-2026-04-30/screenshots/admin-shell-logo-check.png`
- `assets-final-rerun-2026-04-30/screenshots/user-shell-logo-check.png`

## Route Coverage

Admin routes all passed page-level checks: title `GTManager`, document language `zh`, visible Chinese UI, observable GTManager shell branding, no product-facing `ClawManager`, no `GTClaw`, and no stable loading-only body.

| Route | Screenshot | Result |
| --- | --- | --- |
| `/admin` | `assets-final-rerun-2026-04-30/screenshots/admin-dashboard.png` | PASS |
| `/admin/users` | `assets-final-rerun-2026-04-30/screenshots/admin-users.png` | PASS |
| `/admin/instances` | `assets-final-rerun-2026-04-30/screenshots/admin-instances.png` | PASS |
| `/admin/ai-gateway` | `assets-final-rerun-2026-04-30/screenshots/admin-ai-gateway.png` | PASS |
| `/admin/security` | `assets-final-rerun-2026-04-30/screenshots/admin-security-center.png` | PASS |
| `/admin/settings` | `assets-final-rerun-2026-04-30/screenshots/admin-settings.png` | PASS |

No separate regular-user credentials were provided. User route coverage used the admin authenticated context and is recorded as a limitation, not a failure.

| Route | Screenshot | Result |
| --- | --- | --- |
| `/dashboard` | `assets-final-rerun-2026-04-30/screenshots/user-dashboard.png` | PASS under admin authenticated context |
| `/instances` | `assets-final-rerun-2026-04-30/screenshots/user-instances.png` | PASS under admin authenticated context |
| `/openclaw-configs` | `assets-final-rerun-2026-04-30/screenshots/user-openclaw-resource-management.png` | PASS under admin authenticated context |
| `/settings` | `assets-final-rerun-2026-04-30/screenshots/user-settings.png` | PASS under admin authenticated context |
| `/portal` | `assets-final-rerun-2026-04-30/screenshots/user-instance-access-surface.png` | PASS under admin authenticated context |

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

## Blockers and Caveats

- No blockers were observed in this corrected E2E rerun.
- No separate regular-user credentials were available; user-route checks used admin authenticated context.
- Strict PNG decoder caveat remains unresolved, but Chromium loaded the image at 115 x 120.
- Close/write-back and `passes:true` still require explicit user approval and were not performed.
