# Runtime consistency and administrator recovery

Runtime desired state and observed state are separate facts. An omitted heartbeat/report is not proof that a process exited. Unknown, residual and conflicting slots remain unavailable until a fresh observation and matching authority resolve them. Workspace/PVC content is not deleted by reconciliation or recovery.

**Fault eviction:** Inspect/Recheck/Retry retain their conservative checks. The Pod eviction flow now also offers mode `recovery`: an independent node/container stop path for an exact database-bound source. It does not rebuild an entire pool. See the [accepted fault eviction design](superpowers/specs/2026-11-10-runtime-terminal-recovery-design.md).

## Administrator controls

Open **Runtime → Recovery / 异常恢复**. Enter an instance ID and choose **Inspect instance / 检查实例** to read the current generation, binding, recovery phase, attempts and blocker. Inspection is read-only. Editing the ID invalidates the previous inspection.

Choose a Runtime Pod and **Recheck physical slots / 重新核验现场槽位** to run the same exact-UID physical audit immediately, without waiting for the periodic timer. This can repair proven stopped reservations and clear resolved quarantine. Some cleanup requires a second fresh check after the first pass has confirmed teardown; unknown writers are never released just because the user clicked Recheck.

Use **Retry instance recovery / 重试实例恢复** after addressing the displayed cause. This explicitly re-arms exhausted crash/health recovery, or continues the original failover intent. The request is bound to the inspected generation and reacquires the distributed lifecycle lock. An already confirmed source stop continues from its checkpoint without another stop or another generation increment. A failed replacement allocation can be retried in the same generation. Manual stop, sleep, delete, upgrade and pending stop guards retain priority.

Open **Scheduling records / 调度记录** and choose **Recheck and continue / 重新核验并继续** for interrupted management operations. The request is persisted, so any API replica can accept it; the elected scheduler consumes it. An active worker is not taken over by the API. A replica write with an uncertain outcome is observed, not replayed. Terminal operations require a new preflight. Generic moves without an exact published successor remain blocked rather than replaying an uncertain source stop.

If a source is unreachable, repair the source/network/control plane and recheck. If an older Runtime cannot supply complete Pod UID/worker epoch/exit proof, use scoped fault eviction below when its independent node/storage identity can be verified, or repair/upgrade that Runtime through existing maintenance before retrying. The separately restricted `recover-stopped-source` API remains for an exact unpublished successor with independent stopped-source/storage/external-writer proofs; it is not a general force-release API. Do not clear bindings, counters, assignment ConfigMaps or writer fences by SQL to make the screen appear idle.

## Fault eviction

Choose **Fault recovery / 故障恢复** in the existing Pod eviction dialog. With no explicit IDs the server selects only existing abnormal bindings (`creating`, `error`, `stopped`, `deleting`); healthy running neighbors are excluded. Explicit IDs must belong to that source and require acknowledging restart impact. Sources without SQL bindings use the separately preflighted orphan recovery mode below; missing database rows alone never prove that execution stopped.

The source is paused for new placement. Normal Agent stop is attempted first; a node-pinned helper Job supplies the fallback. Only the exact source container is terminated; its matching management writer ledger is reconciled without deleting projects/Git/PVC. Assignment retirement and binding release require matching evidence. Future placement retains original storage/image/resource constraints. Stopped/deleting instances keep their intent; capacity shortage does not block source cleanup. The native stop controller can consume the matching released recovery receipt to complete its old manual/idle stop journal, so a missing binding no longer strands deletion or a later manual start. It preserves the original stop reason and never manufactures an Agent teardown acknowledgement.

Follow **Scheduling records → Recheck and continue** for the same durable operation. Pending helper Jobs continue automatically; structural conflicts retain their fence and reason. Failures before fencing can be cancelled and inspected again. Once fencing begins cancellation is refused. Application restarts retain progress and the pinned helper image. The old Pod stays paused until separately rechecked/maintained: releasing a binding does not claim all its slots are reusable.

When the original Pod/controller history is already gone, only stopped/deleting instances may use an exact protected management ledger plus the verified management PVC to identify and retire the old execution. Independent node/CRI stop verification is still mandatory. This fallback never supplies restart-placement provenance.

Requirements: a reachable source node, containerd socket `/run/containerd/containerd.sock`, verified workspace PVC/PV, and RBAC/admission permitting the restricted platform helper Job with socket/boot mounts. The image must contain `/usr/local/bin/runtime-eviction-helper`; its digest is pinned before submission from `clawmanager-app` or `CLAWMANAGER_RUNTIME_RECOVERY_IMAGE`. Missing history, changed node identity, protected Team/upgrade writers and unverified file helpers are explicit blockers, not force-release authorization. Do not bypass these with SQL or lock-file deletion.

## Orphan Pod recovery

Use **Fault recovery → Orphan Pod recovery** when an old Pod has residual or Unknown slots but no current SQL binding. Check the source first and acknowledge the exact Pod UID. Ordinary instance eviction retains its existing eligibility checks. The card shows **Bound** from SQL separately from residual runtime reports and Unknown physical observations.

Preflight verifies the exact source Pod/controller, a single-replica controller without an HPA, Node UID/boot, pinned helper image and original workspace PVC/PV. Current running/creating publishers, valid bindings, protected Team/upgrade owners, unknown external writers and changed identities block recovery. A stopped/deleting unbound instance may obtain a new stop receipt without requiring that same receipt before recovery starts.

Submission atomically registers the operation, source fence and instance guards. It takes the same lifecycle and workspace locks as normal Start/file mutations so already admitted work cannot race source retirement. The controller is scaled to zero and disabled for scheduling while retaining its template. The source Pod is deleted with exact-UID checks; positive terminal evidence and an independent node helper must confirm all old source containers stopped before publication retirement. API absence alone is insufficient. Only source-attributed publication/credential records are annotated; unattributed historical generations remain unchanged.

Follow the same scheduling record through `planned`, `fencing`, `terminating`, `helper_pending`, `source_stopped`, `sources_retired` and completion. Retry continues the persisted scope; cancellation is available only before fencing. Partial failures retain their blocker and guard. A same-Node reboot may refresh boot provenance after revalidation; replacing the Node UID requires investigation. Completed retirement marks the old Runtime row deleted for audit, excludes it from inventory and prevents late heartbeats from reviving it. It does not resume stopped instances or delete project/PVC data.

Instance deletion also verifies all retained source history even if the current binding is absent. Missing historical proof keeps the instance in deleting with an explicit blocker instead of silently losing the remaining cleanup record.

## Admin APIs

All routes require the normal authenticated administrator middleware. Mutations also require an actor identity. Unknown fields/trailing JSON are rejected for identity-bearing recovery requests.

- `POST /api/v1/admin/runtime-pods/:id/evict`, body `{"mode":"recovery","instance_ids":[]}`: fault eviction of abnormal bound instances or explicitly selected IDs.
- `POST /api/v1/admin/runtime-scheduling/preflight`, body `{"mode":"orphan-recovery","source_pod_id":132,"source_pod_uid":"exact-uid","instance_ids":[]}`: inspect eligibility for one source Pod.
- `POST /api/v1/admin/runtime-pods/:id/evict`, body `{"mode":"orphan-recovery","source_pod_uid":"exact-uid","instance_ids":[]}`: create its durable orphan recovery operation.
- `GET /api/v1/admin/instances/:id/runtime-recovery`: read inspection.
- `POST /api/v1/admin/instances/:id/runtime-recovery`, body `{"generation":3}`: retry the exact inspected generation.
- `POST /api/v1/admin/runtime-pods/:id/recheck`, body `{"pod_uid":"current-uid"}`: immediate physical audit of that exact registered Pod.
- `POST /api/v1/admin/runtime-scheduling/:id/retry`: persist an operation recheck request; response includes `retry_requested`, phase and blocker.

A successful request means the check/retry was accepted or performed; it does not promise that the instance is already running. Follow instance status or scheduling records. A 409 retains its explicit blocker, and no unknown source ownership is discarded.

## Implemented safeguards

- Manual/automatic scale-in and managed-pool deletion require fresh physical emptiness, with exact observation rows locked through the destructive write. Paused source Pods receive an explicit fresh scan after moves.
- Interrupted processing journals, including same-owner post-PATCH save failures, continue through phase-aware observation/cleanup.
- Capacity uses allocatable physical slots; a ready but unusable awakened pool does not hide other eligible sleeping pools. Unsupported physical proof cannot justify cloning an incompatible pool.
- First-generation unpublished reservations may retry only with authoritative assignment/credential absence. Repeated retries do not extend reservation cleanup age.
- Source-bound failover intents retain the original UID, endpoint, gateway, owner and generation. Stop retries are bounded; the administrator can re-arm the same intent.
- Scheduler scans rotate through a fixed high watermark instead of repeatedly reading only the first 1,000 instances.
- Protected report omissions retain binding/health/state/capacity. Missing binding adoption requires fresh current authority plus transactional owner, profile, generation, workspace, desired-power, admission and port-conflict checks.
- Physical capacity above the configured admission limit is rejected explicitly instead of silently truncating the physical layout.
- Recovery source-stop receipts and binding release commit together; replacement generation and placement checkpoint also commit together. Exhausted attempts cannot prevent continuation of an already completed stop.

## Database changes and rollout

Migrations 106–108 add recovery phase, exact-source failover intent, and management retry-request state. They preserve existing workspaces, retry budgets and lifecycle state. Deploy the normal migration-enabled application with these migrations; do not run a new controller against an unmigrated database. This change does not upgrade older Runtime images or grant legacy observations authority.

## Verification (2026-11-10)

Independent review found and verified fixes for the additional awake-pool/full-pool physical-capacity paths, incomplete allocator layout, stale queued stop phase, and superseded failover intent. The omission rule was extended to all profiles and unavailable metadata; healthy adoption also clears stale error codes.

- Backend broad regression: 4839 tests/subtests passed, 373 skipped, zero failures, 25 packages passed. On Darwin the existing linux-only `browser_worker_nfs_test.go` compile issue was excluded with a temporary Go overlay; the existing native-script relative sibling path was provided for this linked worktree.
- Focused recovery race: 79 passed; handler race after all-profile omission fix: 144 passed.
- Real isolated SQL: recovery checkpoint/manual placement 10 passed; report/adoption 28 cases plus healthy-error-code regression; source failover/current-read/supersession, same-owner post-PATCH SQL failure, final proof invalidation, follower retry queue, unknown/quarantined wake, and concurrent maintenance admission passed.
- Actual Linux + MySQL 8.4.8: all four fresh-schema/full restore/recovery-normalization/failover-reference-policy tests passed. The same full-restore fixture exceeded its 120-second deadline over a forwarded remote SQL connection; running the Linux test binary beside the disposable database passed full restore in 13.06 seconds and schema validation in 6.90 seconds. No product timeout was relaxed.
- Frontend: 13 service tests and 16 management tests passed; TypeScript/Vite build passed. Chrome component regression passed English/Chinese at widths 1280 and 390, including failed-stop retention, exact generation/UID requests, invalidated inspection after ID edits, operation continuation and fitting layout. Mechanical UI detector found no issues.

SQL fixtures were isolated databases, not application business databases. No Kubernetes test Pods were created. Deployment receipt and cleanup verification are recorded separately after rollout.

## Fault eviction verification (2026-11-10)

- Final broad backend regression: 4,876 tests/subtests passed, 392 skipped, 26 packages passed, zero failures. The temporary Darwin overlay excludes the pre-existing Linux-only NFS test compile issue.

- Independent final review identified and verified fixes for three crash/concurrency edges: completion and lifecycle-guard removal are one transaction; pre-fence cancellation from `needs_attention` re-enters the processing queue; delayed legacy OpenClaw reports cannot recreate a retired source generation.
- Native stop integration additionally verifies independent recovery completion for manual/idle × stopped/deleting, and that already confirmed native proofs bypass unrelated malformed recovery records. The existing manual SQL fixture omits `desired_config_revision_id`; its baseline run uses a temporary fixture-only overlay adding that column. The unrelated Pro registration baseline also omits `desired_runtime_action` and remains blocked during fixture setup before any auto-stop recovery hook. No production schema/code change was made for either fixture.
- Real isolated SQL covered helper continuation, exact binding release, stopped/deleting intent, generation replay, completion rollback on guard-delete failure, cancellation, old-report adoption and existing maintenance admission fences. No test wrote application business tables.
- Node helper and Kubernetes adapter tests cover exact container identity, receipts, ledger ownership, stale Jobs, retry and immutable helper-image/storage provenance. Linux amd64 and arm64 helper builds were verified.
- Frontend service/selection/component tests: 72 passed; TypeScript/Vite build passed. Actual Chrome desktop component acceptance passed for the inspected offline-source flow, explicit/unverified scope boundaries, impact acknowledgement and same-operation history controls. Additional locale/mobile browser runs were blocked by harness teardown, not counted as passing.

[Staging acceptance](superpowers/handoffs/2026-11-10-runtime-fault-eviction.md) records the deployed image, exact recoveries, preserved neighbors/data/certificate and cleanup. Unbound sources now use the separate orphan recovery path described above. Unknown slots are not declared reusable merely because their database owner is absent.

## Orphan recovery verification (2026-11-10)

- Broad backend regression: 4,912 tests/subtests passed, 422 skipped, 27 packages passed. Darwin uses the existing Linux-only NFS compile overlay. `TestWorkspaceTextConcurrentSave` is explicitly excluded after its `openat .clawmanager-editor-lock: no such file or directory` flake was reproduced with the unchanged HEAD workspace mutation implementation (200-run baseline); it is not counted as passing.
- Final isolated SQL orphan flow passed in 136.588s; final queued lifecycle/reset admission plus real Start/workspace/atomic rollback group passed in 78.402s. Queued original-source, replacement-waiting, batch and retry cases failed without the new admission predicate, then passed with it.
- Replacement reset execution and existing reset regressions passed in 17.728s. Start, workspace writes, replacement creation/cutover/cleanup share the orphan guard; admission serializes with lifecycle operation/batch producers and atomically persists guard, source fence and operation.
- Kubernetes adapter, CRI helper and helper command suites passed; Linux amd64 and arm64 helper builds were checked. Source/controller/Node substitution, same-Node reboot, external writer exclusion (all four Runtime types), failed proof continuation and late heartbeat retirement have dedicated coverage.
- Frontend: 61 tests and TypeScript/Vite build passed. Actual Chrome component checks at widths 1280 and 390 verified SQL Bound versus residual counts, Unknown recovery availability, exact UID/preflight/acknowledgement, changed-UID invalidation, blockers, phases and pre-fence cancellation, with no page errors or horizontal overflow.

[Orphan staging acceptance](superpowers/handoffs/2026-11-10-runtime-orphan-cleanup.md) records all five completed source retirements, preserved data/certificate/neighbor, cleanup and the remaining lack of schedulable business capacity. At the latest observation 118 is running in a new eight-slot OpenCode Pod with one confirmed binding and zero Unknown slots; 166 still lacks schedulable OpenClaw capacity.
