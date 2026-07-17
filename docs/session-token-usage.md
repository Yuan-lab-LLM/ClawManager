# Session Token Usage

Instance-level reporting for LLM calls routed through the platform AI Gateway.

## Data flow

1. Runtime or user calls `POST /api/v1/gateway/llm/chat/completions`.
2. AI Gateway persists:
   - `model_invocations` (tokens, model, status, `instance_id`, `session_id`)
   - `cost_records` (estimated cost per trace)
   - `audit_events` (including `gateway.session.fallback` when session key is missing)
   - `chat_sessions` (optional title from first user message)
3. Instance detail UI calls:
   - `GET /api/v1/instances/:id/session-usage`
   - `GET /api/v1/instances/:id/session-usage/detail?session_id=...`
4. Admin overview UI calls:
   - `GET /api/v1/admin/session-usage/overview`

## Session ID rules

| Source | Stored `session_id` |
|--------|---------------------|
| Header `x-openclaw-session-key: main` on OpenClaw | `agent:openclaw:main` |
| Header on Hermes | `agent:hermes:{key}` |
| Missing stable key (user JWT or non-managed callers) | `sess_trc_{traceId}` (fallback) |
| Missing key on **instance gateway token** for OpenClaw/Hermes | `agent:{type}:main` (managed default) |

Display keys are derived via `FormatOpenClawSessionKey` (e.g. `main`).

## API

### List session usage

`GET /api/v1/instances/:id/session-usage?page=1&limit=20&search=main&since=2026-07-01T00:00:00Z`

Query parameters:

- `since` / `until`: optional RFC3339 timestamps (`until` must be after `since`); filter on invocation `created_at` (cost aggregates join non-blocked invocations on the same window)
- `search`: filters session rows by session id/key/title (summary totals ignore search)

Response highlights:

- `summary`: totals across all sessions on the instance
- `compliance`: fallback session count and recent fallback audit events
- `items`: paginated per-session rows

### Session detail

`GET /api/v1/instances/:id/session-usage/detail?session_id=agent:openclaw:main&since=2026-07-01T00:00:00Z`

Accepts the same optional `since` / `until` bounds as the list endpoint. Detail rows, model breakdown, and recent traces respect the time window and exclude blocked invocations.

Returns model breakdown (tokens + cost) and recent traces for one session.

### Admin cross-instance overview

`GET /api/v1/admin/session-usage/overview?page=1&limit=20&search=openclaw&since=2026-07-01T00:00:00Z`

Returns managed OpenClaw/Hermes running instances sorted by total tokens, with per-instance summary and global totals.

## UI features

- **Time range presets**: all time, 24h, 7d, 30d (instance panel and admin overview)
- **Auto refresh**: optional 15s polling
- **CSV export**: instance panel exports all filtered session rows; admin page exports instance summary rows

## Limits

- **Gateway only**: direct external LLM calls that bypass the platform gateway are not included.
- **Blocked invocations** are excluded from token aggregates.
- Supported instance types in UI: `openclaw`, `hermes`.

## Database indexes

Migration `038_add_session_usage_indexes.sql` adds:

- `cost_records(instance_id)`
- `cost_records(session_id)`
- `model_invocations(instance_id, session_id, created_at)`

## Local verification

1. Apply migrations (including `038`).
2. Open an OpenClaw or Hermes instance detail page (Lite or Pro).
3. Send a gateway chat completion with `x-openclaw-session-key: main`.
4. Refresh the **Session Token Usage** panel and confirm token totals increase.
5. Open **Admin → AI Gateway → Session Usage** for the cross-instance overview.

Optional E2E:

```bash
cd e2e
npx playwright test tests/instances/session-token-tracking.spec.ts
```

Requires a running stack, configured gateway models, and DB access for `fixtures/dbClient.ts`.

## E2E coverage

| Spec | Scope |
|------|-------|
| `session-token-tracking.spec.ts` | Instance session usage API, gateway aggregation, fallback compliance, instance gateway token |
| `session-usage-admin.spec.ts` | Admin overview API, `since` query validation, non-admin 403 |

Run all session usage specs:

```bash
cd e2e
npx playwright test tests/instances/session-token-tracking.spec.ts tests/instances/session-usage-admin.spec.ts
```

## Pre-commit checklist

When preparing the standalone session-usage PR:

1. Branch: `feat/session-token-usage` (from Skill Hub baseline)
2. Include migration `038_add_session_usage_indexes.sql`
3. Exclude unrelated WIP: egress policy, local deployment yaml, debug `_*.json` artifacts
4. Suggested commit split:
   - `feat(session-usage): add session usage APIs, indexes, and admin overview`
   - `feat(session-usage): add instance/admin UI with filters, refresh, and CSV export`
   - `test(session-usage): add handler, service, repository, and e2e coverage`
   - `docs(session-usage): add session token usage guide`
5. Verify locally:
   - `go test ./internal/services/... ./internal/handlers/... ./internal/repository/... -run SessionUsage`
   - Playwright P1 specs above (P0 gateway aggregation may skip when upstream LLM unavailable)

## Phase 9 staging file list

Include (session usage only):

**Backend**
- `backend/cmd/server/main.go` (session-usage routes only — review diff before staging)
- `backend/internal/db/migrations/038_add_session_usage_indexes.sql`
- `backend/internal/db/migrations_test.go` (038 test)
- `backend/internal/handlers/session_usage_query.go`
- `backend/internal/handlers/session_usage_query_test.go`
- `backend/internal/handlers/instance_handler.go`
- `backend/internal/handlers/instance_handler_test.go`
- `backend/internal/handlers/ai_observability_handler.go`
- `backend/internal/handlers/ai_observability_handler_test.go`
- `backend/internal/repository/session_usage_filter.go`
- `backend/internal/repository/session_usage_filter_test.go`
- `backend/internal/repository/model_invocation_repository.go`
- `backend/internal/repository/cost_record_repository.go`
- `backend/internal/services/ai_observability_service.go`
- `backend/internal/services/ai_observability_session_usage_test.go`

**Frontend**
- `frontend/src/components/InstanceSessionUsagePanel.tsx`
- `frontend/src/pages/admin/SessionUsageOverviewPage.tsx`
- `frontend/src/pages/instances/InstanceDetailPage.tsx`
- `frontend/src/components/AdminLayout.tsx`
- `frontend/src/pages/admin/AIGatewayPage.tsx`
- `frontend/src/router/index.tsx`
- `frontend/src/services/instanceService.ts`
- `frontend/src/services/adminService.ts`
- `frontend/src/types/instance.ts`
- `frontend/src/utils/sessionUsageExport.ts`
- `frontend/src/lib/i18n.ts`

**E2E & docs**
- `e2e/fixtures/apiClient.ts`
- `e2e/fixtures/dbClient.ts`
- `e2e/tests/instances/session-token-tracking.spec.ts`
- `e2e/tests/instances/session-usage-admin.spec.ts`
- `docs/session-token-usage.md`

Exclude (do not stage for session-usage PR):

- `backend/internal/egresspolicy/**`
- `backend/internal/handlers/egress_proxy_handler*.go`
- `deployments/k8s/**/instance-egress-networkpolicy.yaml`
- `deployments/scripts/**`
- `e2e/tests/instances/llm-governance.spec.ts` (unless bundled intentionally)
- Root `_*.json`, `pr138.patch`, debug artifacts
