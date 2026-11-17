# Database remediation completion implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement and review each task. Do not stop at a batch boundary; the user explicitly requested the entire approved scope.

**Goal:** Complete all open requirements in the approved database capacity/correctness design, beyond the first delivery at `71d723ac`.

**Architecture:** Preserve core budget, task and identity transactions. Add bounded durable observability storage and maintenance, atomic Team projection, verified history archival/month routing, and reliable Redis admission/retention. Existing interfaces remain compatible; new features are wired into serving and worker paths, not left as unused utilities.

**Tech Stack:** Existing Go/MySQL 8.4/Redis 7/object storage and React stack; no new service dependency.

## Global constraints

- Work on `database` in the existing checkout; preserve the first delivery.
- Local source/document edits and permitted no-output checks only. All Go tests, Python/Node test execution, binaries, frontend production bundles and images run on `10.130.81.54`.
- Use the server's Go 1.26.8 toolchain and existing caches. Use isolated MySQL/Redis fixtures with explicit marker checks; no test deletion of application data.
- All new retention deletes require verified archive/state/recovery checks. Keep budget ledger, business idempotency and unfinished operations. Never drop pending messages or claims to meet a quota.
- Do not deploy application changes, change Runtime images, or bypass native upgrade protections as part of code verification. If a Runtime protocol change is necessary, prepare it and validate compatibility; actual upgrades use CM native flow.
- Preserve authorization, backup writer gate, current runtime/storage safeguards, scoped encryption and current API payload compatibility. No silent body/history truncation.
- Tests precede implementation and reproduce intended failures remotely. Independently review every work package and fix important findings.

## Task 1: Atomic Team projection and durable cursor (E3)

Files: new projection transaction repository/service files, targeted Team consumer and replay paths; migration 112 reserved if necessary.

- [x] Trace all projection writes and Redis/external effects. Put repository business writes, dedup identity and cursor in a recoverable transaction/owned operation. A transaction rollback must also roll back source-event dedup markers.
- [x] Preserve original business IDs on retry. Serialize inline and recovery projection for a Team; no unowned intent may race a live worker or replay successful ledger effects.
- [x] Defer outbound side effects until commit or record them in existing transaction outbox. Never hold a global cross-Team long lock.
- [x] Add real MySQL rollback/commit-failure/restart/concurrent-worker regressions and existing lifecycle/backup regression coverage.
- [x] Prove the committed cursor is a safe maintenance watermark before exposing it to Redis trimming.

## Task 2: Durable observability events and encrypted body references (E4 / redundancy)

Files: focused `backend/internal/observability/` package, model/cost/audit/chat repository adapters, serving initialization hooks; migration 113 reserved.

- [x] Provide stable event identity, request admission/recovery identity, bounded durable retry and explicit overload errors. Completed upstream work must never be described as unexecuted because a log write failed.
- [x] Store large bodies once within an owner/encryption boundary, with digest, length, encryption version and reference metadata. Use existing object storage; verify before replacing original text. Retain true-detail authorized reads.
- [x] Wire invocation/audit/chat/fee persistence and retry worker into server startup. Respect backup gate. Bound outstanding events globally/per owner and retain completion capacity for admitted work.
- [x] Add fault tests for SQL/object outage, duplicate/out-of-order retry, restart, quota exhaustion and two owners with identical content; verify no plaintext public objects, double accounting or false success.
- [x] Migrate historical inline bodies by batches; verify object bytes/hash before clearing the original. Add body/orphan retention with current references and backup/recovery dependencies preserved.

## Task 3: Verified history maintenance, monthly data and backup adaptation

Files: focused maintenance/archival repository/service, SQL migrations (114 reserved), registered backup tables and archive references, deployment maintenance entrypoints.

- [x] Implement leased bounded maintenance (500 rows / 30 seconds), recorded progress and backup-gate pause. Preserve incomplete/ambiguous recovery records.
- [x] Complete finished model-activity and delivered-outbox retention with explicit safe conditions; archive eligible invocation/audit/chat/risk and terminal Team history before deletion.
- [x] Create and route registered UTC monthly log tables with stable public IDs and supported cross-month queries, prepare two future months, and provide verified backfill/cutover/rollback jobs.
- [x] Register schemas, archive/body objects and watermarks for backup and isolated restore; prove restore before allowing source data removal. Do not confuse same-schema movement with physical I/O isolation.
- [x] Expose retention/expiry/archival status to authorized detail queries and bounded export; preserve historical metadata lookup.
- [x] Add actual disposable MySQL/object-store archive/restore/corruption/interruption tests and query plans with representative synthetic history.

## Task 4: Backup partial-attempt identity and reclaim (E6)

Files: production backup script/config and dedicated fault tests.

- [x] Use an owned attempt marker/lock/heartbeat and bounded cleanup. An active holder or unknown legacy partial directory must never be removed based only on age.
- [x] Reclaim only verified inactive owned attempts, with path/symlink guards and recorded cleanup results. Preserve successful backup retention and avoid concurrent backup/cleanup races.
- [x] Run failure, live-attempt, stale-attempt, unsafe-path and retry regressions on server54.

## Task 5: Reliable Redis quotas, watermarks and deleted-Team cleanup

Files: Redis bus/managed bridge, Team admission/maintenance, narrow Runtime protocol compatibility changes if required; migration 115 reserved.

- [x] Atomically bound reliable stream entries and bytes, single payload and Team totals. Preserve a completion/ack/error reserve; apply backpressure to new work and durable outbox rather than trimming unprocessed data.
- [x] Register required consumer groups. Compute and trim only the prefix safe for all delivered/non-pending groups, or all committed projection/recovery watermarks. Reject unregistered historical group creation after trimming.
- [x] Keep terminal dedup until durable idempotency and replay-source windows permit reclamation. Archive DLQ before removal. Bound runtime/local and SQL outboxes.
- [x] For deleted Teams, revoke producers, settle/explicitly terminate work, archive and confirm state, then cursor-based SCAN/UNLINK the exact namespace; record recoverable progress.
- [x] Add Runtime UI per-message/time bounds and apply measured Redis/MySQL container/connection budgets. Test Redis-full recovery, AOF peak/restart, group registration races and deletion interruption without message loss.

## Task 6: Complete query limits, summaries, cursors and detail UI

- [x] Maintain persistent session/instance/day/trace summaries with stable-event dedup and rebuild/reconciliation. Replace repeated historical CTE scans on ordinary queries.
- [x] Add stable keyset cursors and default 30-day / maximum 90-day synchronous windows; long history uses an explicit bounded archive/export workflow.
- [x] Implement Trace pages (maximum 200 rows / 8 MiB) and separate body fetch, with frontend pagination so existing content is not silently lost. Read repository DESIGN.md and applicable UI skills before interface changes.
- [x] Add appropriate existing-index-aware query indexes, actual plans, and 10,000-instance / 90-day-equivalent capacity scenarios. Record measured P95; do not invent acceptance results.

## Task 7: Complete verification and delivery

- [x] Review the complete approved design against implemented serving/worker paths, migrations, deploy config, tests and restore jobs. No unchecked requirement may be relabeled as complete without evidence.
- [x] Run relevant full package, race, real MySQL/Redis, object, frontend and browser checks on server54 as applicable. Compare failures with baseline and fix regressions.
- [x] Record source manifests, exact commands, test counts/skip reasons, capacity metrics and remaining operational rollout actions. Commit reviewed code, keep worktree clean, and do not publish without authorization.
- [x] User requirement (2026-11-18): after development is complete, execute a full functional regression on server54, emphasize changed functionality, and generate a standalone HTML report for the user to review. The report must include actual results, commands, evidence, source fingerprints, failure/recovery scenarios, skips and coverage gaps; a planned case is not a passed test.

### Final functional test matrix

1. Normal/admin/session/instance/trace queries, authorization, default/range boundaries, cursor stability, body fetch, complete pagination, cost totals and mixed currencies.
2. Persistent summaries: duplicate/out-of-order observations, restart, rebuild/reconcile, hot/cold transitions, 10,000-instance representative load, actual EXPLAIN and measured P95.
3. Durable observations: simultaneous SQL/object outage after upstream execution, filesystem restart, replay identity, quota rejection before execution, encrypted owner isolation, historical body migration and verified retention.
4. Atomic Team projection: rollback, source dedup rollback, cursor write failure, COMMIT lost before/after commit, concurrent workers/recovery, outbound replay identity, legacy audited reconciliation.
5. Reliable Redis: admission count/bytes/key/cardinality limits, completion reserve, full capacity and recovery, pending/unread group protection, registration race, verified projection trim, DLQ archive, terminal receipt lifetime, deleted-Team cleanup interruption.
6. Runtime protocol: supported plugin contracts and durable retry without repeating business execution, UI payload/time/count limits, health delivery behavior.
7. History/backup: monthly routing, backfill, cutover, rollback refusal after deletion, restore into isolated DB, corruption/missing object, backup gate, dynamic schema inventory, active/stale/unknown/symlink partial-attempt cleanup.
8. Integrated backend packages/race/build, frontend typecheck/build/tests and real browser interactions on the isolated test build. Tests and builds run only on54; do not replace actual checks with source inspection.

## Progress

- [x] User explicitly requested all remaining work on 2026-11-18.
- [x] Task 1.
- [x] Task 2.
- [x] Task 3.
- [x] Task 4.
- [x] Task 5.
- [x] Task 6.
- [x] Task 7.

## Completed verification — 2026-11-18

The [HTML report](../../reports/2026-11-18-database-functional-test-report.html) is the final v5 acceptance record, with adjacent JSON and complete evidence ZIP. All execution occurred on server54 after the application/test freeze: CM `34d9da18b768ee089894fd321db656e3260cdf95`, isolated Runtime `8ddd567b65119e1ca91afa07359290f59e4300d7`, 4,302-file manifest SHA-256 `1eeaef647c3cf25f322f1b3e537bb42b384116a71612a9d5e6b00c9adc36d35f`. The original AgentsRuntime checkout was preserved.

Final results: 42 completed command suites, 6,516 passing execution records, zero failures or incomplete records, all 15 required feature groups and all 10 fixed performance targets passed. Counts include repeated dedicated/race executions; non-Go suites count commands rather than inventing test counts. Of 636 SKIP records, 554 exact same-name cases passed under dedicated fixtures; 82 remain unexecuted with their specific environment/diagnostic requirements retained. These remaining optional cases are not marked passed and are outside the completed change acceptance matrix.

The actual 10,000-instance/900,000-source audit test executed the production backfill CLI, interruption recovery and 20 samples per query. P95: default30 94.670ms, all90 279.973ms, search30 332.079ms, search90 960.585ms. Actual Redis 1GiB/noeviction/AOF rewrite and same-Pod container restart recovered all 13,079 payload keys / 857,145,344 bytes, zero evictions, measured cgroup peak 2,001,629,184 bytes under its 3GiB container. This is a finite synthetic workload, not a 10,000-concurrent-client or production-traffic certification.

Report generation correctly rejected four mismatched performance label presets. A reporting-only correction at `df1a5d88` was independently reviewed and passed nine integrity tests in a separate delivery-tool copy. The original frozen tree/logs were not changed. Report-tool manifest SHA-256 `c1e82cb6d98757cc8f1e45ac49e910581b686dc80feb60e65246efefe4566ab4`, full source diff and RED/GREEN evidence are disclosed separately in the report and ZIP.

The delivered HTML itself passed actual offline Chromium checks of all 7,152 records/pages, filters, search, log details, exact JSON download, evidence ZIP hash and 390px display, with zero external requests or page errors. All 758 indexed evidence entries and the 4,302 frozen files were reverified. Local artifact hashes match the verified server outputs. Desktop/mobile screenshots and the separate [delivery validation](../../reports/2026-11-18-database-report-delivery-validation/validation.json) preserve the result. Static design-detector warnings were retained and manually triaged rather than represented as a clean detector run.

Code/configuration and reviewed rollout jobs are delivered on `database`; no application deployment, container image build, Runtime upgrade, push, merge or deletion of existing application history was performed. Production measurements, persistent WAL/key/object configuration, migrations/backfill and native Runtime upgrade/rollout acceptance remain operational steps before enabling the new deployment.
