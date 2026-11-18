# Migration recovery and slot release implementation plan

> For agentic workers: use test-driven development with independent backend, audit and frontend tasks; review their combined changes before deployment.

**Goal:** Make failed migrations recoverable and make released source slots converge promptly with an explicit pending-release display.

**Architecture:** Keep existing generation fences, immutable stop evidence, SQL admission locks and runtime slot audits. Correct the matched-row check in migration completion; request bounded, coalesced fresh source audits after successful binding release; expose the existing verified release-pending observation as `releasing` without granting capacity or lifecycle actions. Reuse scheduling-history retries for exact failed sources.

**Constraints:** Work on existing `fix-release11.11` changes without overwriting previous repairs. All compilation, tests and image builds run on `10.130.81.54`; local work only edits, packages and transfers source. Preserve all running workloads and workspace data. Do not manually clear live SQL, quarantine records or stop evidence. Main/upstream/origin history remains unchanged.

## 1. Migration completion

- [x] Reproduce the production TIMESTAMP(0) no-op UPDATE with an isolated MySQL regression in `backend/internal/services/runtime_management_mysql_test.go`.
- [x] In `runtime_management_force.go`, verify exact instance generation/status/guard under transaction locks; accept a matched stopped source even if its values do not change. Preserve failures on changed generation, missing guard, stopped/delete intent and binding/Pod identity changes.
- [x] Run the migration, force eviction, retry and concurrent release MySQL regressions remotely.

## 2. Prompt source-slot reconciliation

- [x] Add a bounded, per-Pod coalesced audit request to `RuntimeScheduler.RequestSlotAudit(podID int64)` with leader checks and the existing authenticated audit implementation.
- [x] Wire `instanceService.SetRuntimeSlotRechecker(func(int64))` in server setup and request audits only after successful source binding release. Cover ordinary cleanup and manual-stop paths. Trigger the same request after migration's source-release transaction succeeds.
- [x] Re-observe after a successful quarantine CAS using fresh native and SQL evidence; never publish cached free facts or bypass failed proof/inventory reads. Keep periodic auditing as fallback.
- [x] Test source/new-binding isolation, coalescing, failed proof, leadership and successful release without waiting for the minute timer.

## 3. Recovery entry and pending-release display

- [x] Add `models.RuntimeSlotReleasePendingMessage` for the existing pending-release audit diagnostic. Project only fresh, unbound, stopped/free pending observations as `reconciliation_state=releasing` in `runtime_inventory_view.go`; keep genuine uncertainty unknown and prevent release-pending from becoming capacity or confirmed binding authority.
- [x] Extend frontend runtime status helpers, counts, slot tooltip/legend and translations to show pending release distinctly from unknown and idle.
- [x] Let scheduling-history retry include the exact failed, stopped source reservation shown as Unknown, while rejecting unrelated Pod UID/generation/gateway/instance identities. Preserve the server preflight as authority.
- [x] Test model projection and frontend retry/pending-release behavior, then run remote TypeScript/Vite build and relevant UI checks.

## 4. Integrate and deploy

- [x] Independent review of combined changes; resolve safety and correctness findings.
- [x] Run affected Go packages and real MySQL scenarios, frontend tests and build on server54. Build a source-manifested Manager image containing the reviewed backend and frontend; reuse current runtime images.
- [x] Deploy Manager only with UID/resourceVersion guards; verify runtime Pod identities and all existing workloads remain healthy.
- [x] Use clearly named temporary DSH fixtures to validate successful balance, source-slot release, and cleanup; preserve user-created instances. Save acceptance evidence and remove only task-owned temporary fixtures and forwarding processes.

## Release and verification record

- Manager `v2026.11.11-fix4` is deployed on `10.130.81.54`. Its application source matches manifest SHA-256 `e8efcd465225c131e552f6f69e5c7bca9f76b6e888c4234d7deaa2073dee146e`; the runtime model-configuration repair was deployed earlier and reused for this release.
- Go suites cover `internal/models`, `internal/aigateway`, `internal/repository`, `internal/handlers`, `internal/services` and `internal/services/k8s`; the OpenClaw runtime `lite` suite also passes on Linux.
- Real MySQL regressions use disposable databases, including nine unchanged-timestamp migration cases and 31 unpublished-reservation cases. They do not mutate application database rows.
- Frontend verification includes 71 tests, TypeScript/Vite build and browser checks for retry fencing and pending-release actions.
- Two temporary DSH migration fixtures completed; their source slots became available in 2.491 s and 3.189 s. Both fixtures were deleted. All eight existing runtime Pod identities remained unchanged during the Manager deployment.

## Known follow-up outside this repair

The subsequent ten-instance OpenClaw batch exposed a separate startup failure. Instance 64 logged `ERR_SQLITE_ERROR` / `disk I/O error` while reading its workspace SQLite state database on NFS. Automatic CLI triage then received `no eligible model for automatic routing`, extending recovery time. Instance 68 reported a generic CLI bootstrap failure without enough detail to identify the same cause. Both recovered automatically and all ten instances reached running state at inspection time. The exact SQLite/NFS failure mechanism and the diagnostic auto-model routing failure remain unresolved; this release does not claim to fix them.
