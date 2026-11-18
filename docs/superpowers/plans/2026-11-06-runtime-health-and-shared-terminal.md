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

## Test-cluster delivery, 2026-11-09

- Connectivity recovered. Runtime commit `d383bab` additionally fixes the trusted intermediate reparent race and establishes private-PID PTY sessions inside the correct namespace. Three complete disposable Linux acceptance rounds pass under production-equivalent `tini`: protected networking/preview, shared primary visibility, sibling/primary survival, private terminal and NFS terminal.
- Final Linux runtime checks: 501 passed / 25 opt-in skipped; focused race: 27 passed / 1 opt-in skipped. Backend validation remains the previously verified 2,890 + 103 race + 15 MySQL passes.
- Manager image `94939b287a91efccb01df191f12b27951150b9b38ba36693685a9b88c3431f8a` is Ready with migration103 recorded. Startup bootstrap image configuration remains unchanged because existing canonical pools require coordinated changes; new scheduling pools use independent pinned candidate images. Automatic scale-out clones eligible new templates.
- New disabled-by-default pools158–161 were created and positively inspected over authenticated slot Unix sockets before enabling them. Old pools were only paused for new scheduling; no old Pod was restarted or writer fence cleared. New runtime images and defaults are documented in `.deploy/runtime-health-20261109/delivery-result.json`.
- Real OpenCode163, DSH164 and Hermes165 canaries passed shared process visibility, cross-terminal HTTP200, sibling/primary survival after close and preview creation. OpenCode163 was deliberately crashed; the platform persisted retry reason/budget, automatically recovered G1→G2 and retained its workspace marker. Confirmed manual stops then released their bindings; cleanup used normal APIs.
- **OpenClaw switch remains pending.** Historical committed default placement15 rejects the newly built image/pool. Full preflight17 includes 43 items and is blocked by the never-started canary162. Excluding all except153 produces passed preflight19, but the existing exclusion mechanism would alter other historical instances' future placement contracts. No rollout confirmation/start was performed. Upgrade scope clarification was requested; no historical instance was silently started or migrated.
- Protected states remain25 error/G14,45 stopped/G21,118 stopped/G9,153 stopped/G9,161 stopped/G3. Protected118 Pod UID remains `a46388e5-fb58-4a3e-8542-3b1ad24ca3dc` with zero restarts. The three disposable namespace/MySQL Pods and owned MySQL Secret were removed. Original42+1 untracked user files remain unchanged.


## Manual wake, audit refresh and slot sizing follow-up, 2026-11-09

- Manual Start now writes desired-running intent under the existing lock/transaction. Explicit retries of proven-unpublished pending starts use a conditional repository transition; source identity, workspace, generation, current binding and newer Stop guards remain mandatory.
- Audit excludes stale/paused historical pools before allocating its bounded cursor budget. Allocation can clear only exact-UID quarantined ports with fresh authenticated positive idle proof; partial observations preserve exclusions and ConfigMap conflicts fail closed. Existing SQL bindings and stop/writer fences remain unchanged.
- Commit `b9b9ebd` passed Linux backend tests (2,921), focused race (169), and disposable real-MySQL repository tests (154), with no failures. Manager `42da99ab23eb5e2272ff92b295d785b9f68c1803c56cc5af5e886b3b8745763e` is deployed; existing supported Kubernetes client QPS/Burst are 50/100, and bootstrap image defaults remain unchanged.
- Earlier 2/2/2/1-slot canary pools were incorrectly retained as serving pools. Replacement pools162–165 are now Ready, authenticated-idle inspected and enabled at **8 slots per Pod** for all four Agent types. Their Deployment templates retain eight worker containers for subsequent replicas. The four owned small pools were paused, checked twice for zero bindings and positively proven idle before scaling to zero. Their user workspaces were not reset.
- Hermes instance161 `abc` now runs at G6 on pool165 through normal Start. Its original workspace is preserved, running binding is published, workbench terminal capability is available and previous stop blocker is cleared. The prior disposable Stop→Start canary also passed with its workspace marker preserved.
- Old one-slot helper pools128/157 remain paused, with historical source records intact. Other historical paused pools were not restarted or force-cleared. Protected118 retains its original Pod UID and zero restarts;25/45/118/153 retain their previous status/generation. The OpenClaw historical placement-policy blocker remains unresolved; no upgrade confirmation or historical instance wake was performed.
- Live evidence: `.deploy/slot-quarantine-20261109/` (manager/Start) and `.deploy/eight-slots-20261109/` (pool registration, idle proofs, owned-small-pool retirement and final verification).


## Hermes Desktop capability correction, 2026-11-09

- Root cause: the ad hoc binary-only runtime overlay omitted the existing Hermes Dockerfile release-regeneration step. The previous release manifest still hashed the old `/usr/local/bin/clawmanager-agent`, so verified capabilities were correctly absent despite a healthy gateway. The regular `hermes/Dockerfile.lite-agent-overlay` already includes the required image-owned `--generate-release --verify-release` step; future builds must use it instead of a generic binary-only overlay.
- Rebuilt from the deployed Hermes image using that exact generation/verification step. New digest `1e652d823b304f75100a3a6c3d2039b800bb62a3ed56fb5d9093dfcbfcefd9e9` preserves all base layers and runtime configuration; its sole added file is the regenerated `release.json`. All 9,751 managed artifacts verify, including the unchanged new runtime binary; signed-release acceptance remains explicitly false rather than inherited or forged.
- New eight-slot Hermes pool166 advertises compatible Desktop Web from its coordinator, and all eight workers were authenticated and positively confirmed idle before admission. Instance161 now runs at G10 on that pool with its original workspace preserved. GET capability and same-origin POST bootstrap return available; bootstrap sets the scoped HttpOnly session cookie.
- Actual local Chrome acceptance passes: Desktop iframe loads `/hermes-desktop-web/`, emits the instance-scoped ready message and shows Hermes Agent/Gateway ready. The unsupported-capability text is absent. TLS was handled only by the browser confirmation; no certificate file was installed. Evidence is in `.deploy/hermes-capability-20261109/`.
- Additional defect observed during normal Restart: `FenceRuntimeRestart` increments generation before the manual-stop path compares it to the old binding, producing `runtime binding identity mismatch`. No guard was loosened and no business SQL was changed. Normal Start safely tore down the older gateway and recovered this instance. This distinct restart/manual-stop incompatibility remains to be fixed and regression-tested.


## Scheduling resume and configurable time units, 2026-11-09

- Reproduced Pod132's Resume response200 with SQL pause cleared but effective scheduling still false: Deployment scheduling metadata remained disabled. Resume now pins the current Pod/controller identity, keeps the selected Pod paused while synchronizing the normal Deployment gate, then uses the existing transactional admission checks to unpause it. Kubernetes errors, conflicts and maintenance rejection retain the SQL pause. No template, image, replica or native writer/stop proof is changed. Upgrade standby/laboratory/unactivated target pools remain excluded. A retained completed-wake journal is not treated as active maintenance, and disabled boolean spellings match inventory parsing.
- Both automatic-stop fields now accept exact positive integer seconds through number+unit controls (seconds/minutes/hours/days). Migration104 adds nullable exact-second columns without rewriting existing day values, deadlines or scan state. Old callers remain supported; explicit seconds drive next checks, candidate SQL and transactional final idle claims. One-second polling avoids full instance initialization except at startup and due checks. Backup schema and both auxiliary binaries were rebuilt from the same final source.
- Final source `48cd653` passed3,004 backend tests/subtests,202 focused race cases,175 disposable real-MySQL repository cases, and the changed handler test (zero failures). Additional real-MySQL scheduling tests passed12/0skip, tagged/normal duration and four-runtime stop/controller tests passed, frontend13tests and TypeScript/Vite build passed. Independent review findings have regression red/green coverage.
- Manager/frontend image `86f591ea89c155dd95fb97d449e20d53b7f9e9cc67541a520b640ebd155ca927` is deployed with migration104. Base layers/runtime configuration and the existing Hermes renderer remain preserved. Runtime Pod UIDs/restart counts and protected instance statuses/generations are unchanged. Existing global policy stays enabled,1-day interval/1-day idle timeout,version7; exact-second columns remainNULL until the administrator saves settings.
- Actual Chrome clicked Resume for132 and confirmed the resulting Pause action/effective scheduling=true with the same PodUID. Both time fields expose all four units; the UI generated exact30-second/7,200-second save payloads using a browser-intercepted save so no global short-duration policy was applied. Actual API rejects zero seconds without changing stored settings. Live evidence: `.deploy/runtime-controls-20261109/`, with backend verification/build artifacts under the worktree directory of the same name.
- The owned emptyDir MySQL test Pod was deleted and its pinned port-forward stopped after verification. The temporary browser bearer was removed. Original42+1 untracked user files and the user's additional scheduling brief/status documents remain untouched.
## Shared-instance workbench delivery, 2026-11-09

- Share pages previously rendered a separate files-only panel. They now reuse `InstanceWorkbench` for files, Git, service previews and terminals, including the native-agent link bridge and per-share restoration of tabs, selected panel, files and Git view. Legacy runtimes without the workbench retain the existing files view.
- Shared requests use a dedicated anonymous client and code-bound session authorization. The instance and owner come from the current share, never caller-supplied identity. Read shares allow Git inspection and preview creation; terminal/process access and Git/file mutations require write permission. Preview capabilities recheck share enablement, binding, expiry and permissions, including established streams; shared lease metadata survives manager replicas in the existing database schema.
- Regression testing exposed cross-tab cookie/CSRF rotation and MySQL microsecond rounding. Matching cookies are reused until renewal; the anonymous client renews and retries once only for the explicit pre-execution `share_csrf_invalid` rejection. Persisted preview deadlines round down so storage never extends access past its session deadline.
- Deployed source `19117949028d4d563abf5c6237ecf77be4a27674`, backend source manifest `b5305e128483d281f501bcccb35d94d831d2906a678c7ec4d830afd708ebac75`. Final Linux checks passed: 3,058 tests/subtests, 181 focused race tests/subtests, and two real-MySQL workbench tests. The deterministic database boundary regression also passed ten consecutive runs. Frontend TypeScript/Vite build, 27 targeted tests and the shared-page/certificate contracts passed; independent review found no remaining blockers.
- Manager-only image `clawmanager.local/clawmanager@sha256:ac2db3f4c74242bd44e23c13b10b8cdf9e34f66fd8754f25b5f83b816c809b2e` is Ready. Existing runtime configuration, native Hermes renderer, backup binaries, Runtime Pod UIDs/restart counts and protected instance generations were preserved across the update. The configured global automatic-stop durations were unchanged.
- A disposable Hermes169 canary passed real Chrome checks without an owner login: Git status/diff and commit, cross-tab CSRF recovery, native-link opening in the right preview, terminal input/output, browser and terminal restoration after reload, read-only restrictions, and preview revocation on permission change/disablement. There were zero owner workbench requests, bearer headers on shared workbench calls, laptop-loopback requests or popups. HTTPS confirmation used Chrome's warning screen; no certificate files were installed. Private evidence is in `.deploy/share-workbench-20261109/` locally and on the test host.
- Hermes169 was stopped and deleted through normal APIs, with its runtime binding confirmed released. The owned emptyDir MySQL fixture and port-forward were removed; temporary credential files were deleted. Original untracked user files remained unchanged (ClawManager42, AgentsRuntime1).

## Idle pool admission and test cleanup, 2026-11-09

- The configured scan interval was correctly 60 seconds with a 300-second instance idle threshold. Automatic empty-pool reclamation was blocked globally by OpenClaw166/167 creating and unconfirmed stops118/156. Scoped admission now retains capacity for pending starts of the same agent type and protects the original stop Pod UID (or Pod ID for a missing-UID receipt). The same checks run under the existing admission lock before publishing source fences; global upgrade/management admission, fresh identities, occupancy and binding checks remain intact.
- Source `c4fe8482f25fc2394cb2337279d0d1669ddef20e`, manifest `872be2436d853fb6ba7d47ed108b560d56913677ef08d7e9732972d8411770d2`, manager image `clawmanager.local/clawmanager@sha256:5dbe2a45fe1153e0a0d3c700138d925c70fdcc64699cbf9a2133cb2bc56fb00d`. Verified 2,773 service/Kubernetes/repository tests, 68 focused race checks and 87 real-MySQL tests. New regressions first reproduced the unrelated-demand failure, then covered related starts/stops, replacement UIDs, missing-UID receipts and last-moment demand. Independent review found no blockers. Two opt-in manual-maintenance fence tests also fail on the unchanged deployed baseline; those unrelated failures were recorded separately rather than treated as passing.
- The manager-only update preserved duration settings, frontend, runtime images and Pod identities. Without manual scale commands, five empty runtime pools completed durable automatic scale-to-zero operations: both DSH pools, the current Hermes Web pool, its old native pool, and the current OpenCode eight-slot pool. Runtime Pod count fell from12 to7, while retained Pods kept their UIDs and restart counts. Occupied/error pools and OpenClaw pending-start capacity were retained. Evidence: `/root/cm-idle-pool-scope-20261109/acceptance-result.json`.
- At the user's explicit request, deleted eight positively owned, unbound test/obsolete Deployments and their children: `opencode-workbench-20261106-{a,b}`, `hermes-runtime-native-20261106`, superseded `hermes-runtime-eight-20261109`, and the four `*-runtime-health-20261109` pools. The one remaining live workbench-test Pod was removed; the test MySQL Pod, Secret, forward and password file were also removed. Promoted eight-slot serving templates and user workspaces/PVCs were retained. No new test Pods were created after the cleanup request. Final runtime count is6; test controller/Pod selectors are empty. Evidence: `test-cleanup-result.json` and `cleanup-final-verification.json` in the same private directory.
