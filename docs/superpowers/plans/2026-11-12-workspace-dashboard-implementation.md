# Workspace dashboard implementation

**Goal:** Replace the real JWT `/dashboard` with user-approved v9, using authentic user-scoped data and preserving existing routes and permissions.
**Architecture:** One bounded backend aggregate reader for data that existing APIs cannot aggregate safely; existing React shell, native dialogs and SVG charts; user-keyed browser persistence for pin/result preferences; additive observation storage for state/capacity history. No IEI dashboard replacement.
**Base:** f93e4a8 on ongoing `uiux` branch; preserve all unrelated dirty inputs. Continue this approved working checkout; no additional worktree needed for this continuous branch task.

## Global constraints
- All runtime, tests, builds, binaries, browsers and container images run on 10.130.81.54. Local source preparation/read/review/Git/transfer only.
- Match approved v9 composition and CM DESIGN.md, no new UI/chart/motion dependency; accessible keyboard, native dialogs, focus return, reduced-motion, paused hidden-page animations; one-screen desktop and grouped narrow layout without clipping.
- Server-verified `userID` only. A personal admin view remains caller-scoped. Never accept owner/user identity from request parameters; no cross-owner data, secrets or conversation payloads in aggregate.
- Real data only; no inferred task completion from model calls, invented busy/idle/progress, fabricated history, assumed pricing, mixed currencies or quota-as-free-capacity. Nullable/unavailable sources must be represented honestly.
- Preserve existing team/instance/session navigation and lifecycle behavior; no automatic start/wake/retry/model calls. User-scoped pin/result preferences independent of task and instance state.
- Additive migration with rollback-compatible old binary. Retain previous image and verify exact source/build/deployed bytes. Do not merge or push.

## Task 1 — User-scoped aggregate and observation history
Owner: backend implementer. Scope: backend only plus API contract documentation.
- Read `/tmp/cm-workspace-20261112/contract.md` and implement its exact public shape (optional source coverage fields may extend it).
- Add `backend/internal/workspacedashboard/` reader/DTO/tests; handler `backend/internal/handlers/workspace_dashboard_handler.go` and tests; wire GET `/api/v1/dashboard/workspace` behind Auth + SetUserInfo in `backend/cmd/server/main.go`.
- Aggregate caller-owned CM instances and teams (exclude northbound team scope), native team/gateway tasks, scoped gateway invocations and recorded costs. Use SQL-side bounds/aggregation; no N+1 per invocation/session. Query cancellation/deadlines, bounded payloads and explicit partial sources.
- Add `110_add_workspace_dashboard_observations.sql` for per-user timestamped state/quota/eligible capacity snapshots. Reuse scheduler-native read-only eligibility where available. Sample on a bounded background cadence with context cancellation; a current successful dashboard read may seed the initial real sample. Missing historical bins remain null and source coverage explains collection start. Do not alter resource lifecycle/scheduling.
- Tests first: range/UTC-Shanghai boundary; unauthenticated/invalid identity/invalid range; current owner versus other owner including admin; blocked/duplicate invocations; actual versus estimate and mixed currency/missing prices; task status semantics; gaps; source failure; migration embedded. Use existing MySQL fixture conventions on54 for aggregate integration.
- Exact remote targeted command: `go test ./internal/workspacedashboard ./internal/handlers ./internal/db` (narrow test selection allowed for large unrelated suites; report exact commands and results). Go tools only remote. Commit only owned files; report interface caveats and test proof.
- Independent task review for spec and quality before task2.

## Task 1b — Repair instance workspace usage collection
Added after the user's live all-zero report on 2026-11-12. Root confirmed all56 instances report0 despite real file bytes; the repository update method has no production callers. Implement bounded automatic read-only size collection using the existing credential-scoped NFS helper architecture. Persist successful measurements with availability proof, reject publication after identity/path/generation/reset changes, and never update business activity timestamps or replace a failed scan with0. No filesystem walks in the long-lived app against NFS, symlink escapes, synchronous scans on list requests, Agent wake-ups or lifecycle changes. Genuine measured empty space is0; unmeasured is unavailable. Preserve last successful values on failure. Add an additive migration if required, backend/API and instance list/detail availability fields, and meaningful failing-then-passing tests only on54. Fresh implementer, followed by independent spec/quality review. Exact scoped handoff brief: `/tmp/cm-workspace-20261112/workspace-size-task.md`.

Execution refinement after Task1 approval: the new independent size bug and Task2 UI can proceed in parallel under `dispatching-parallel-agents`, with disjoint source ownership, remote snapshots/ports and test resources. Task1b owns backend and instance types/list/detail/size helpers plus the disposable DB. Task2 owns only UserDashboard and dashboard-local files plus the requested Portal menu link/test; it uses browser service fixtures and no DB. Neither edits central i18n.ts. Commits name exact owned files; each task retains independent review and the combined feature receives final review/build/verification before deployment.

## Task 2 — Real Workspace React screen
Owner: fresh frontend implementer. Scope: UserDashboard and new dashboard-local components/service/types/translations/CSS/tests only; shell public props may be used unchanged.
- Use approved spec/v9, the final task1 API DTO and existing route entry points. Create `frontend/src/services/workspaceDashboardService.ts`, `frontend/src/types/workspaceDashboard.ts`, `frontend/src/lib/workspaceDashboard.ts`, dashboard-local components/CSS. Replace `frontend/src/pages/dashboard/UserDashboard.tsx`; leave OwnerDashboard/IEI/admin intact.
- Apply the user's subsequent account-menu refinement: remove only the Portal link in `ManagementHeader.tsx`, update the corresponding browser regression, and preserve language, Console/admin, logout, focus and `/portal` route behavior. This is the sole shell change authorized for this task.
- Test first using Node strip-types model tests and browser fixtures on54: null versus zero/partial source, currencies, period comparison, correct instance/team/session routes, stopped instance behavior, stable pin membership/max6/empty/recovery/user-key isolation, result acknowledgement independent of tasks, stale responses/auth switch, chart gaps/keyboard, reduced-motion.
- Use React Query or robust abort/sequence with visible-page polling/focus/reconnect. Preserve stale valid content on errors and show retry; no refresh button/time-stamp dashboard chrome. Render unsupported metrics as unavailable, never synthetic.
- Main resume defaults to actionable failures and unread results; running filtered list; pins quick-enter existing routes; native preferences dialog. Operations has period + metric choices, real costs/currency/coverage, availability versus quota and current per-Agent states. Annual heatmap from actual returned activity. All visible text localized EN/ZH and compatible JA/KO/DE.
- Node tests, app/node tsc, changed-file ESLint and remote production build; browser tests at1440/1280/768/390, actual sparse/empty/error, long labels, focus/keyboard/navigation and motion. Commit only owned files, report exact commands and API dependencies.
- Independent task review for spec and quality.

## Task 3 — Integration, real environment checks and deployment
Owner: root/integration. Record task2 head, run full-branch independent review over this feature base and resolve findings in one fix wave. Build backend/frontend/image on54 from exact committed source. Validate two-account scope, current data/empty/history/currency/task/pins with isolated fixtures. Check real current environment read-only login and Workspace/instances/team routes and no unexpected lifecycle/model mutations. Deploy only after source and image checks, retain previous image, verify rollout/version/health/assets and UI desktop/mobile screenshots. Record exact build/deploy/test evidence in `docs/uiux/2026-11-12-workspace-dashboard-validation.md`. Keep branch uiux; no merge/push.

## Checklist
- [x] Task 1: API, samples, focused tests, independent review (513c5d4; history/extraction/retention and backup compatibility fixes reviewed).
- [x] Task 1b: actual workspace sizes, unavailable/zero distinction, focused tests, independent review (774c1f4; no blocking findings, live measurements remain part of Task3).
- [x] Task 2: React UI, real data bindings, focused tests, independent review (0a3a13c..c90fec6; period retention, activity availability, timezone and mobile calendar fixes reviewed).
- [x] Task 3: whole feature review, cluster build, validation and81.54 rollout (source6c144b4, image553416e8; actual56positive workspace measurements, all161dist files and four-view browser checks verified).
