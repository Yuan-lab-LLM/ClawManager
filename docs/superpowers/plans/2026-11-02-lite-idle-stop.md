# Lite Idle Stop Implementation Plan

**Latest execution status (2026-11-03, user correction):** All four Lite types must use the same Gateway model-activity window. Removed native task inspection and type/version-specific idle hooks; the common supervisor retains command admission, graceful-only stop and exact cleanup receipts. OpenClaw, Hermes, OpenCode and DeepSeek Harness passed17 live isolated cases covering activity, stop, slot reuse and persistent restart. Previous OpenClaw-only qualification below is historical and superseded. No local tests or builds; all verification and image builds use test node10.130.81.54. See the [unified model activity validation report](../tests/2026-11-03-lite-model-activity-unified-validation.md) for final images and rollout scope.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Independent repository changes may be developed without overlapping files; all integration is reviewed before completion.

**Goal:** Implement the approved Lite-only automatic stop policy with default-off OpenClaw autonomous work, durable Gateway activity, administrator controls and manual recovery.

**Architecture:** Persist per-instance model activity and request scopes before forwarding. A database-coordinated day scheduler selects idle Lite instances and uses the existing lifecycle lock, durable storage proof and confirmed graceful-only teardown. Keep stopped instances stopped until an explicit Start; unknown execution or teardown never releases resources.

**Tech Stack:** Existing Go/MySQL/Gin backend, Go Runtime supervisor, React/TypeScript frontend; no new runtime dependencies or model classifiers.

## Global Constraints

- Only Lite; enabled by default; Pro unaffected.
- `check_interval_days` and `idle_timeout_days`: integers, default1, minimum1. Reject zero, negatives, fractions and overflow. One day is24h, check anchor04:00 Asia/Shanghai.
- OpenClaw Lite defaults: `agents.defaults.heartbeat.every="0m"`, `plugins.entries.memory-core.config.dreaming.enabled=false`, `skills.workshop.autonomous.mode="off"`. Preserve explicit overrides and existing data.
- All authenticated valid instance-attributed model requests count, including failures and background requests. No Jev, purpose Header or prompt parsing.
- Persist request admission before upstream; failures fail closed. Unknown outstanding work blocks stop. Model completion is not proof of native task completion.
- Same stopped state and Start button. No request wake, no indefinite capacity retry, no forced automatic kill. Release only with matching teardown proof. Retain per-instance storage.
- Respect existing untracked documents. Develop on feat/lite-idle-stop in the two repositories; no external messaging, secret files in version control, main-branch changes or pushing. Test-cluster updates were explicitly authorized on2026-11-03.
- No local tests, image builds or temporary local clusters. All tests/builds run on10.130.81.54 using cached Linux toolchain images; do not change the laptop's global toolchain configuration.

## Task 1: Runtime defaults and graceful-only teardown

**Files:** AgentsRuntime/openclaw/lite/config_writer.go and managed_defaults_test.go; clawmanager-agent/gateway existing graceful/isolated lifecycle; slots/process.go, worker.go and coordinator routing/tests.

- [ ] Add tests requiring the three absent defaults to be off while explicit user settings survive rewrite; run `go test ./lite -run 'Defaults|Managed'` and observe failures before changes.
- [ ] Supply defaults via existing `setDefaultObjectValue`/ensureObject; do not blanket-disable cron or memory/skills plugins. Verify repeated writes preserve false/off.
- [ ] Add tests where a process ignores SIGTERM: automatic stop must return an error and retain writer/assignment, never call Kill. Reuse ManagedProcess.GracefulStop and native isolated cleanup semantics; do not change manual Stop policy.
- [ ] Add a separate authenticated `POST /v1/gateways/{gateway_id}/idle-stop` operation, routed through coordinator to its owning slot. Request body binds instance_id/generation/pod_uid. Unknown route on an old worker must be harmless. Response must identify gateway_id, instance_id, generation, pod_uid and stopped/confirmed/unmounted/graceful=true. Missing common graceful-stop capability or an active supervisor command blocks signaling; native task/cron proof is not required.
- [ ] Ensure graceful semantics survive retries/recovery; no fallback through the forceful DELETE route. CM alone selects idle instances from durable model activity. All four Runtime types share command admission and graceful teardown, with no native task hook or version whitelist.
- [ ] Run relevant Runtime tests, Linux cross-build and report API/capability details for backend integration.

## Task 2: Durable policy, activity and Gateway admission

**Files:** backend/internal/models/instance_auto_stop.go; repository/instance_auto_stop.go; db/migrations/096_add_instance_auto_stop.sql; repository/instance_repository.go; aigateway activity hook and public inference entry points/tests.

- [ ] Add policy tests for1/2/7-day valid settings and invalid0/negative/fractional input; idle anchors max(last activity, readiness, monitoring), keep-running/Pro/active-scope protection,24h boundary and next check scheduling.
- [ ] Add singleton `instance_auto_stop_settings` with enabled/check_interval_days/idle_timeout_days/version/next_check_at; per-instance `instance_model_activity`, `instance_model_activity_requests`; instance fields auto_stop_disabled, stopped_reason and start deadline/recovery data. Use additive migration and the existing MySQL integration-test environment.
- [ ] Use atomic row locks for request Begin/Finish, policy updates, keep-running and stop-fence claims. Validate current owner/generation/status in SQL, with persistent request ID and timestamps. Do not hold a DB transaction during upstream I/O.
- [ ] Before first upstream/model routing call, register a request scope. Wrap every public Chat/Responses/Messages/image inference entry point; validation occurs before registration, model/budget failures after registration still count. Registration failure must prevent provider calls. Finish errors keep scope open and block stop.
- [ ] Add HTTP stub tests proving no provider call on failed admission, failed upstream still touches activity, stopped/stale requests rejected, cancelled stream remains protected until cleanup. Ensure tests use real hook paths rather than duplicating implementation.

## Task 3: Scheduler, lifecycle, persistence and HTTP administration

**Files:** services/instance_auto_stop_service.go, instance_service.go and lifecycle/retry helpers; services/runtime_slot_agent_client.go; handlers/instance_auto_stop_handler.go; cmd/server/main.go; instance heartbeat/recovery boundaries; tests.

- [ ] Add service tests for scan due/disabled/period changes, live request vs stop race, missing durable storage/teardown, manual stopped reason preservation and crash recovery.
- [ ] Implement database-coordinated next-check claim and paged selection; initialize all eligible instances at deployment/start rather than using missing history as idle evidence.
- [ ] Stop under existing lifecycle lock. Recheck activity version, policy version and shared active scopes, set a persistent stopping fence before external I/O, and preserve fixed gateway/generation/Pod identity. Use idle-stop endpoint only; errors retain resources. Reconciliation uses the same mode and cannot convert a graceful idle stop into forced cleanup.
- [ ] Validate PVC/NFS storage identity, original workspace and compatibility. Known emptyDir or unconfirmed teardown produces a visible blocker. No blank workspace may be created when restarting an automatically stopped instance.
- [ ] Reuse confirmed binding release and existing Start. Explicit Start gets a120s capacity deadline; preserve previous stopped reason on confirmed failure; do not leave delayed autonomous restart. Gate old heartbeats and Runtime recovery by stopped target/generation.
- [ ] Add admin GET/PUT `/api/v1/admin/runtime-auto-stop` and admin PUT `/api/v1/admin/instances/:id/auto-stop` (auto_stop_disabled). Normal instance JSON exposes last_gateway_activity_at, monitoring_since, auto_stop_disabled, stopped_reason and auto_stop_blocker. Enforce admin auth and optimistic config version.
- [ ] Wire Gateway activity and idle controller in main. Use common instance/repository behavior so all lists and lifecycle events report consistent state.

## Task 4: Administrator controls and instance presentation

**Files:** frontend/src/services/runtimePoolService.ts, types/runtimePool.ts, types/instance.ts, pages/admin/RuntimePodsPage.tsx and new RuntimeAutoStopDialog.tsx; instance list/detail presentation; locale dictionaries; relevant Node tests.

- [ ] Add API types for settings (enabled/check_interval_days/idle_timeout_days/version/next_check_at) and per-instance activity fields. Settings PUT requires version; keep-running endpoint accepts auto_stop_disabled.
- [ ] Add a Runtime management settings dialog, existing styling, with enabled switch and independent integer-day inputs (default/min1), next-check display and actionable API errors.
- [ ] Add admin per-instance keep-running control and display stopped reason, model activity and blockers. Preserve existing Start behavior and permissions, never add wake on access.
- [ ] Write focused validation/service tests, run frontend runtime-management tests, TypeScript build and relevant lint.

## Task 5: Integration review and verification

- [ ] Review full diffs in both repositories against A01–A29 of the approved spec. Check no missing public inference paths, lock ordering, fail-open persistence, forced teardown retry, leaked leases, late generation or capacity retry.
- [ ] Run backend tests, MySQL-backed concurrency/migration tests where available, Runtime tests/race checks and Linux builds; frontend production build/tests. Fix issues and rerun affected checks.
- [ ] Record actual unsupported native execution capabilities as blockers, not fabricated success. Document default migration/override behavior and known limitations in the design and implementation report.
- [ ] Mark task progress and evidence here; deliver code and test results for review. No deployment or pushing.

## Progress

- Initial source state: no tracked modifications; unrelated untracked documents retained.
- Frontend baseline build passed. Go baseline initially blocked by mismatched `/usr/local/bin/go`; matching `/usr/local/go/bin/go` selected and baselines rerun.

- Task 1 Runtime: implementation agent active; defaults and graceful-only fenced endpoint implemented; OpenClaw2026.10.2 native suspend/handoff adapter supported; other runtimes/version paths block as unsupported. Tests/race review in progress.
- Task 2 parent: policy model tests green; additive096 schema, durable request admission/finish, optimistic settings/check claims and stop state machine implemented. Real local MySQL admission/concurrency tests and Gateway fail-closed/provider503 tests passed. Teardown proof persistence added, pending retest.
- Task 3 parent in progress: native protocol client, live PVC/NFS verifier, controller and admin HTTP routes written; manual Start120s deadline, manual reason, heartbeat fences, JSON metadata integration written, compiling/testing next. No deployment/commit.
- Local test VM: dedicated Colima idle-tests (default context untouched); disposable MySQL cm-idle-stop-mysql at127.0.0.1:33519; use TEST_IDLE_MYSQL_PORT and TEST_RUNTIME_MYSQL_PORT. Go tool is /usr/local/go/bin/go with GOTOOLCHAIN=local.
- Runtime task independent review found two Important defects (confirmed idle stop blocked restart; auxiliary cleanup uncertainty could release guard). Implementer reproduced RED, fixed, and passed gateway/slots/control race regressions; re-review requested against frozen fixed diff.
- Task 4 frontend worker implemented settings and shared Lite status/admin keep-running; production build and focused tests passed, final lint baseline comparison/report pending.
- Backend targeted live MySQL + native receipt rejection/storage/workspace tests passed. Full backend suite found new backup table registry omission; parent fixed registry + isolated-restore normalization, added no-replay regression, suite rerunning.
- Backend independent review in progress found deadline not cleared after successful Start, first manual stop failure fence gap, native task admission race, legacy Agent session/desired-state fences and actual workspace mount coverage gaps. These are open Important issues; do not mark ready or deploy until fixed and re-reviewed. Parent reserving systembackup and new integration test files; next fixer owns Backend production fixes otherwise.
- Runtime task 1: independent re-review APPROVED spec + quality after both fixes; no remaining Critical/Important in reviewed scope. Runtime full race + covering post-fix race passed; parent Linux amd64 library build and OpenClaw Lite binary cross-build passed. Native binary acceptance still outstanding and must block rollout qualification; Hermes/OpenCode/DSH/unreviewed versions remain visibly unsupported.
- Frontend task 4: independent review APPROVED spec + quality, no Critical/Important. Worker85 tests + production build passed; reviewer10 focused tests passed; only pre-existing lint10 diagnostics, unchanged from HEAD.
- Parent Backend storage fix5: actual instance workspace coverage, shadow mounts, consistent slot views validated; all new storage regressions passed. Original-home test and real MySQL controller unsupported/failure/resume/slot release/data retention flow passed before subsequent fixes.
- Parent Backend scan fix7: migration097 adds durable scan token/cursor/5min lease. Due time advances only on completion; crash resumes cursor after lease expiry; stale scanner denied; configuration version unaffected. Real MySQL crash recovery regression + all AutoStop repository tests passed. Pending recovery fair oldest-updated batch10/3workers, manual-kind routing integrated; Start deadline recovery independent5s loop.
- Backend single fix agent owns Important1-4 and6 (deadline completion/manual stop journal/Agent authority/native task admission/public rate limit activity). Parent owns storage/scan controller and systembackup. Await complete fix report, re-review, full final tests. Full backend pre-fix suite only failed backup registry, now systembackup full suite passes.

- USER OVERRIDE (latest instruction): no local testing or builds. Stop current local tests and disposable containers/VM; thereafter develop and source-review only. Testing must wait for explicit future user request and run in test cluster. Interrupted final full/race/native smoke checks are NOT passing evidence. Earlier results are historical only; later changes remain untested.
- Final source review open Important issues: repeated Start during creating resets deadline/generation (fixer code-only fix in progress); model-egress historical coverage qualification absent (parent resolving code-only). Native protocol smoke build interrupted per user, no native acceptance claimed.
- Cleanup verified: `colima delete idle-tests --data --force` removed the separate VM AND retained runtime data disk; no paths matching idle-tests remain under ~/.colima. Existing default profile remains stopped/untouched. Task temporary binaries removed; local tests/builds terminated.
- Final code-only follow-ups: duplicate creating Start rejected transactionally; Team rejects every stopped reused instance; idle candidate and recovery use existing WithInstanceMutation. Runtime/Backend qualification defaults unsupported/unknown, SQL rechecks current/fresh generation-bound coverage; loss/change resets monitoring. Persisted default policy marker and API/UI states expose pending/source-unknown honestly. Recovery retry timestamp touched before locks; upgrade rejection persists blocker/fence, no starvation by first batch.
- These latest corrections and their new regression cases are UNTESTED under explicit user prohibition. No further local tests, builds, VM, cluster access or deployment.
- Required product decision pending via async question: use existing Gateway activity as observation boundary and exclude known direct external-provider instances (recommended, changes strict qualification), OR require provable all-model enforcement (currently blocks every runtime). Do not silently pick an answer or claim actual automatic reclamation is delivered while no runtime can qualify. Current safe code and status report are reviewable; dependent changes await user answer.

- 2026-11-03 USER CORRECTION: CM forbids Agent direct external-model access; reuse existing Gateway model routing and LLM egress blocks. Earlier direct-provider exemption and universal coverage-proof assumption were incorrect (missed backend egresspolicy/policy.go). Async policy choice is withdrawn/resolved by this constraint. Removed all model_coverage/enforced qualification fields, stop gates, table and UI blocker.098 now stores default-policy display status only. Native execution/activity/storage/graceful-stop protections retained. Updated design/status accordingly. Code-only, no tests/builds run.
