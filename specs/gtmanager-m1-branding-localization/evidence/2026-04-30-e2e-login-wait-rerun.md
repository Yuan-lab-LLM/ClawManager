# GTManager M1 E2E Login Wait Rerun Evidence Packet - 2026-04-30

## Verdict

Final evidence verdict: **E2E text/localization/auth-flow passed; final M1 close remains blocked by Assets C unless user explicitly re-scopes assets.**

This packet does **not** authorize Close/write-back and does **not** write a durable pass flag.

## Context

- Target: `https://localhost:30443`
- Evidence root: `specs/gtmanager-m1-branding-localization/evidence/login-wait-rerun-2026-04-30/`
- Playwright result JSON: `login-wait-rerun-2026-04-30/playwright-login-wait-rerun-result.json`
- Screenshots: `login-wait-rerun-2026-04-30/screenshots/`
- Logs: `login-wait-rerun-2026-04-30/logs/`
- Deployment image: `clawmanager:m1-gtmanager-local-20260430102258`
- LoginRuntimeTriage conclusion used: the previous `加载中...` sample was not a durable stuck state; clean context login reaches `/dashboard` in about 1s, tokens exist, `/auth/login` and `/auth/me` return 200, and there were no console/page/request failures.

## Commands Run

| Command | Exit | Evidence |
| --- | ---: | --- |
| `curl -sk https://localhost:30443/healthz` | 0 | `login-wait-rerun-2026-04-30/logs/healthz.log` |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath=...image...` | 0 | `login-wait-rerun-2026-04-30/logs/deployment-image.log` |
| Playwright clean-context login wait rerun | 0 | `login-wait-rerun-2026-04-30/logs/playwright-login-wait-rerun.log`, `login-wait-rerun-2026-04-30/playwright-login-wait-rerun-result.json` |

## Prerequisite Status

- Frontend build: exit 0, inherited from `evidence/logs/frontend-npm-run-build.log`.
- Frontend lint: exit 1, inherited as pre-existing debt per ReviewWorker from `evidence/logs/frontend-npm-run-lint.log`.
- Backend tests: exit 0, inherited from `evidence/logs/backend-go-test.log`.
- K3S dry-run: exit 0, inherited from `evidence/logs/kubectl-dry-run-k3s.log`.
- K8S dry-run: exit 0, inherited from `evidence/logs/kubectl-dry-run-k8s.log`.
- Latest `/healthz`: `ok`, exit 0.
- Latest deployment image check: `clawmanager:m1-gtmanager-local-20260430102258`, exit 0.

## Strong Login Wait Result

Playwright used a clean Chromium context with `ignoreHTTPSErrors: true` and `locale: en-US`. After submit, the script did not immediately assert against the body; it waited for an authenticated route, both tokens, and a stable non-loading body.

| Timing point | Timestamp | Delta from submit |
| --- | --- | ---: |
| Submit | `2026-04-30T03:25:35.503Z` | 0 ms |
| Route changed | `2026-04-30T03:25:35.590Z` | 87 ms |
| First `access_token` + `refresh_token` observed | `2026-04-30T03:25:35.607Z` | 104 ms |
| Final stable non-loading body | `2026-04-30T03:25:36.226Z` | 723 ms |

- Final post-login URL: `https://localhost:30443/dashboard`
- Stable body window: 515 ms
- Final body was not primarily `加载中...`.
- `/api/v1/auth/login`: HTTP 200.
- `/api/v1/auth/me`: HTTP 200.
- Console errors: 0.
- Page errors: 0.
- Request failures: 0.
- API token bootstrap was **not** used for the successful UI login flow.

## Clean `/login` Coverage

Screenshot: `login-wait-rerun-2026-04-30/screenshots/login-clean-profile-before-submit.png`

- `html lang`: `zh`
- Browser title: `GTManager`
- Visible login text: `登录 GTManager`
- Language selector default: `zh` / `中文`
- Product-facing `ClawManager`: not visible
- Favicon: `/lobster_transparent.png`, HTTP 200
- Asset classification: favicon/logo/loading assets still show the lobster-era asset state because Assets C is BLOCKED; asset replacement is not counted as passed.

## Route Coverage

All required admin routes passed text/localization checks: Chinese UI, title `GTManager`, visible GTManager shell branding, no product-facing `ClawManager`, and no durable loading-only state.

| Admin route | Screenshot | Result |
| --- | --- | --- |
| `/admin` | `login-wait-rerun-2026-04-30/screenshots/admin-dashboard.png` | passed |
| `/admin/users` | `login-wait-rerun-2026-04-30/screenshots/admin-users.png` | passed |
| `/admin/instances` | `login-wait-rerun-2026-04-30/screenshots/admin-instances.png` | passed |
| `/admin/ai-gateway` | `login-wait-rerun-2026-04-30/screenshots/admin-ai-gateway.png` | passed |
| `/admin/security` | `login-wait-rerun-2026-04-30/screenshots/admin-security-center.png` | passed |
| `/admin/settings` | `login-wait-rerun-2026-04-30/screenshots/admin-settings.png` | passed |

No separate regular-user credentials were provided. User route coverage used the admin authenticated context, and this is recorded as a coverage limitation rather than a hidden pass.

| User route | Screenshot | Result |
| --- | --- | --- |
| `/dashboard` | `login-wait-rerun-2026-04-30/screenshots/user-dashboard.png` | passed under admin authenticated context |
| `/instances` | `login-wait-rerun-2026-04-30/screenshots/user-instances.png` | passed under admin authenticated context |
| `/openclaw-configs` | `login-wait-rerun-2026-04-30/screenshots/user-openclaw-resource-management.png` | passed under admin authenticated context |
| `/settings` | `login-wait-rerun-2026-04-30/screenshots/user-settings.png` | passed under admin authenticated context |
| `/portal` | `login-wait-rerun-2026-04-30/screenshots/user-instance-access-surface.png` | passed under admin authenticated context |

## Runtime Wording and Protected Identifiers

- OpenClaw runtime wording remains OpenClaw where runtime behavior is named:
  - `/admin/settings`: `OpenClaw Desktop`
  - `/openclaw-configs`: `OpenClaw 资源管理`
- Protected identifiers remained observable and unchanged where covered:
  - API: `/healthz`, `/api/v1/auth/login`, `/api/v1/auth/me`
  - Storage key: `clawmanager_locale`
  - Runtime/image identifiers: `clawmanager-openclaw-image`, `clawmanager-user-1`
  - Deployment command scope: `clawmanager-system`, `clawmanager-app`

## Asset Status

- Assets C remains **BLOCKED** because exact logo, favicon, and loading source paths have not been provided.
- Favicon still references `/lobster_transparent.png` and returned HTTP 200.
- Logo/favicon/loading replacement is **not** counted as passed in this packet.

## Remaining Blockers

- Assets C remains blocked pending exact user-provided logo/favicon/loading source paths, or explicit user re-scope excluding those assets.
- Separate regular-user credentials remain unavailable; user route coverage is limited to admin authenticated context.
- Frontend lint remains exit 1 due pre-existing debt per ReviewWorker.
- Close/write-back remains unauthorized.
- Durable pass-flag write-back remains forbidden and was not performed.

## Close Gate

Close approval gate can be considered only after asset re-scope or asset paths are provided, followed by explicit user approval for Close/write-back. This packet alone does not authorize Close.
