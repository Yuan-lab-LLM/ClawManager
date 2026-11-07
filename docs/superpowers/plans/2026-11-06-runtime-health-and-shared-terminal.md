# Runtime health and shared terminal implementation plan

> **For agentic workers:** Execute independent domains in parallel using dispatching-parallel-agents; root integrates lifecycle hooks and reviews the combined result.

**Goal:** Detect falsely idle slots, recover unexpectedly failed instances with bounded retries, and make workbench terminals observe the instance's actual execution environment.

**Architecture:** Reuse the elected scheduler, distributed instance lifecycle lock, immutable assignment records and confirmed teardown/reset protocol. Keep detection distinct from permission to release a writer. Attach terminals through pinned instance identities and retain a separate terminal cleanup boundary.

**Tech Stack:** Existing Go services, MySQL persistence, Kubernetes and Linux namespace primitives; no new service or dependency.

## Global constraints

- All supported Lite agents use the common implementation.
- User stop, sleep, delete and migration take precedence over recovery.
- Unknown ownership, unreadable state and unreachable workers never count as free or stopped.
- Never release an old generation or recreate its writer before confirmed cancellation/stop.
- Work only in the existing isolated CM and runtime worktrees. Preserve original user files.
- No local OCI builds. Build and test Linux artifacts on test node 10.130.81.54 only.
- Do not restart protected OpenCode118 or OpenClaw153/its clock service during implementation and canary tests.
- Do not clear historical instance25 fences or directly rewrite lifecycle SQL/slot books.

### Task 1: Periodic slot consistency audit

**Files:** Add `backend/internal/services/runtime_slot_audit.go` and focused tests; extend common runtime snapshot proof only if existing fields cannot establish the required state. Root owns scheduler hook integration.

**Consumes:** Current Pod UID/endpoint, SQL bindings, persisted desired assignments and authenticated per-slot runtime snapshots.
**Produces:** Bounded periodic reconciliation that excludes inconsistent slots and reports why; owner-aware cleanup uses existing confirmed teardown only.

- [ ] Write regression cases before implementation: SQL-free/runtime-occupied; stale stopped record with cleanup pending; unreachable worker; changed Pod UID; concurrent start after scan; genuinely free slot.
- [ ] Prove failures against the existing code.
- [ ] Reuse a 60-second audit cadence, bounded per-pass work and existing authority checks. Persist inspection/recovery outcome where needed so manager restart cannot silently clear a quarantine.
- [ ] Keep live owner records intact. Recover desired-running owners via Task2; clean stopped owners through normal stop/reset. Unknown owners stay unavailable with a specific reason.
- [ ] Run focused unit/race tests. Provide root the minimal scheduler hook and an operational canary plan; do not mutate the cluster independently.

### Task 2: Durable bounded instance recovery

**Files:** Add `backend/internal/services/runtime_instance_recovery.go`, corresponding repository persistence/migration and focused regression tests. Root owns shared scheduler hook integration.

**Consumes:** Desired-running instance snapshot, failure classification, current binding and source-stop authority.
**Produces:** Idempotent recovery under the instance lifecycle lock; retry budget survives backend restart and heartbeat updates.

- [ ] Write failing cases: unexpected exit recovers; manual stop/sleep/delete does not; unrelated running instance untouched; stale heartbeat does not authorize duplicate writers; stop uncertainty blocks placement; concurrent user stop wins; three failures exhaust the budget across manager restart.
- [ ] Implement attempts with 30s/120s/300s backoff. Reset budget only after five minutes of stable health or explicit user restart. Permanent credential/model/configuration failures remain actionable errors.
- [ ] Reuse normal confirmed stop/reset and conditional generation transition, then the existing scheduler. Preserve the persistent workspace and prefer an eligible alternate slot after slot failure.
- [ ] Retain the failure reason, attempt number and next retry time in persistent state and existing inspection responses. No new frontend dashboard is required for this patch.
- [ ] Run focused tests and repository integration checks on Linux54; no user-instance crash injection.

### Task 3: Shared instance terminal environment

**Files:** Common `clawmanager-agent/sandbox`, `slots` and gateway workbench process handles, with focused Linux namespace acceptance tests.

**Consumes:** The admitted current gateway's pinned process and namespace identities, sanitized business environment, workspace path and PTY.
**Produces:** Terminal `ps` observes the Agent's Web server with consistent PID semantics. Shared instance mounts/dependencies/network remain available; terminal lifecycle cannot kill primary or sibling sessions.

- [ ] Reproduce the current distinct PID namespaces in a disposable fixture, with a primary server and two terminals.
- [ ] Prototype the smallest attach mechanism using existing Linux/bubblewrap primitives; pin handles and verify owner/generation before launch. Never infer a target from a user-supplied PID.
- [ ] Establish an independently verifiable terminal cleanup scope before replacing the existing private-namespace cleanup. Do not use whole-instance namespace kill for an individual terminal.
- [ ] Test same-instance process visibility and workspace/dependency access; terminal close preserves server and sibling terminal; instance stop drains all; stale generation/reused PID/cross-instance targets reject.
- [ ] Keep existing terminal-session persistence and fresh reattachment unchanged.

### Task 4: Integration and test-cluster delivery

- [ ] Root reviews all changes and integrates only small scheduler hooks.
- [ ] Run combined backend unit/race tests and Linux runtime namespace/teardown tests, then required builds.
- [ ] Build pinned images remotely with file/hash/config verification; validate on disposable owned instances before any existing runtime rollout.
- [ ] Verify idle-audit detection, crash recovery/backoff and terminal visibility/close isolation in the test cluster.
- [ ] Verify protected118/153 Pod UIDs/generations/restart counts and the clock HTTP200 remain unchanged. Report any rollout of an active instance as a separate concrete deployment step if it would interrupt its services.

## Verification and delivery status

- Backend source `f8d9ef791deb181724feec02d017ee30ef4269a223221a1c10934a35de035b23` passed the complete Linux backend run (2,890 tests/subtests), focused race run (103), and disposable MySQL integration (15). Fresh backup schema matched all 86 tables. The source manifest still matches the working files.
- The shared-terminal disposable Linux test passed for primary process visibility, common tmp/environment, detached terminal cleanup, and survival of the primary and sibling terminal. Final private-network and NFS acceptance runs remain pending.
- The current runtime code compiles the Linux slots test binary. Targeted sandbox/slot/workbench unit and race tests pass locally. A full Darwin run encounters existing Linux-only tests and does not replace the required full Linux run.
- Read-only review found no further blocking identity/cleanup issues after the NFS, legacy capabilities and uncertain-cleanup corrections.
- Test-node SSH (22) and management (30443) are now unreachable from this workspace. No new images or lifecycle changes were deployed. Resume with final Linux gates and disposable canaries before rollout; existing user instances still use the prior runtime.
- Original untracked user files remain unchanged: ClawManager 42, AgentsRuntime 1. Disposable lab/MySQL cleanup requires restored cluster access.
