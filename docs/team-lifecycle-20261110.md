# Team lifecycle integration — implementation notes

Updated 2026-11-11. The authoritative deployment digests, live acceptance results,
rollback prerequisites and handoff are in
[`TEAM_LIFECYCLE_REPAIR_HANDOFF_20261110.md`](../TEAM_LIFECYCLE_REPAIR_HANDOFF_20261110.md).
Earlier unresolved checkpoint descriptions have been replaced by this document.

## Merge and scope

Team repositories only, branch `codex/team-personal-integration`. App merges
`upstream/main@646833710b3fed1a1dbc3b89436a744db3445b1c` in `4eee25fa`;
Runtime merges `upstream/main@3f1cd721cee1880849cd9abc26771c0cc476ac29` in `5fbe896`.
Conflicts were resolved individually, retaining both sides' functions rather
than replacing entire files. Keep the owner-history scheduler, isolated reader,
OpenClaw archive acknowledgements, DSH cursors, Team-compatible placement,
credential-scoped NFS reads and per-slot readiness/identity checks.

Migration101 introduces the separate Team runtime lifecycle record. Existing
published migrations retain their filenames and content; the final repair adds
no migration. The lifecycle row is backup metadata/reference-only; copied
operations must not execute in another installation.

## Whole-Team admission

Ordinary Lite instances retain upstream idle settings and Pro behavior. Team
members are excluded from individual idle-stop candidates and claims. The Team
uses the same idle duration but stops as one unit only after the last real user
root task formally succeeded and no active task, work item, review, delivery,
model request, command or write remains. Bootstrap introduction is not task
completion. Unknown business states prevent automatic stop.

Heartbeat, LastSeenAt, member updated_at, agent presence and page polling do not
extend business idle time. Existing Teams receive an observation grace period.
Expired model-request leases reconcile independently of the daily idle scan;
valid renewed leases and legacy requests lacking process-exit proof remain held.

Manual stop first requires business tasks, commands and writes to finish. It can
freeze the Team lifecycle row while an existing inference request drains. New
inference is rejected through the same admission lock; no native stop is sent
until runtime preflight and active-model checks pass. Automatic idle stop still
requires zero active inference before admission. Bounded waiting retains the
original operation and exposes recovery_required instead of false success.

## Operations, state and APIs

CM and northbound use POST /teams/:id/start and /stop through their respective
authentication paths. Northbound requires teams:manage and owner authorization.
Repeated requests recover the existing operation. Direct member lifecycle APIs
cannot bypass Team management. Stopped cards offer Start Team; member status is
intentionally stopped. History/files stay readable and drafts survive.

Operations freeze original member/instance IDs, generations and workspaces, and
reuse ordinary durable instance stop/start. Workers start before the Leader.
Startup validates runtime and collaboration readiness and sends no task. A
completed historical start ledger does not hide a currently stopped/error
member; the read projection offers recovery without rewriting the ledger.
Heartbeat-only unknown does not manufacture a lifecycle fault.

Leader/Worker dispatch, dependencies, revisions, formal completion and final
root ownership remain unchanged. No new Worker required fields are introduced.

## Stop proof and recovery

Native teardown, exact kubelet container exit, writer-lock reconciliation and
external-write fences are separate evidence. Pod Ready, missing registration,
404, SQL stopped or lease expiry alone do not prove termination.

A pure sandbox container-reset requirement is reported as
container_reset_required and handled by the existing source-recovery path.
Joined genuine failures retain gateway_stop_failed; they are not made broadly
retryable. The original writer reservation stays held until confirmed release.

A retained stopped source can cold-resume the original instance after validating
its finalized container proof, ownership, workspace, native version, generation
and absence of newer/unknown publication. This nondestructive resume does not
require destructive cleanup of every historical credential. Delete/reset still
use the full historical proof and are not relaxed. New execution never writes
the retained storage before the original execution has been fenced.

Explicit administrator fallback uses immutable
runtime-slot-fence-<existing-stop-operation> requests. The request is not proof.
It fixes the exact old ContainerID, Pod UID, stop intent and immutable replacement
image before replacement. The coordinator then verifies actual kubelet
termination after request creation, a different container in the same slot,
closed PID namespaces, no host mounts/namespaces, no privilege escalation and
only SETUID/SETGID serving capabilities. The proof is explicitly
kubelet-container-fence-v1, never a fabricated graceful C1 receipt. Retain request
and proof for audit. Owner/model endpoints cannot issue this recovery request.

## Deployment prerequisites and data

Upstream NFS/sandbox defaults and security requirements remain unchanged. The
HXC-only Localhost AppArmor profile is explicitly provisioned where needed for
restricted rootless user namespaces; the global sysctl is not disabled. Empty
RUNTIME_SLOT_APPARMOR_PROFILE retains upstream behavior. Missing profiles fail.
Local Landlock ABI1 cannot establish optional ABI3 test/profile acceptance.

Hermes0.22.5 migrated the retained0.21 workspace after a stopped-state backup;
three sessions and37 messages preserved key hashes. Historical private-group
normalization changed only reviewed known GID1000 files to mapped primary GID,
preserving owner, mode and content. The durable journal is
/workspaces/.clawmanager-admin-migrations/team-182-private-groups-20261110.json.
Do not repeat this migration or downgrade native Hermes over newer data.

Final image overlays update both the serving manager and recovery helper.
Hermes release inventory is regenerated and verified. Native payloads remain
OpenClaw2026.10.2, Hermes0.22.5, OpenCode2.0.27 and DeepSeek Harness0.2.1rc3.

## Verification and rollback

Necessary tests cover real MySQL admission/concurrency, lease expiry versus
renewal, active work/model/write rejection, manual inference drain, exact-source
cold resume, destructive-history protection and lifecycle fault projection.
Runtime tests distinguish pure reset from joined real failure and retain writer
fences. Earlier affected frontend/build and ordinary Hermes restart tests are
retained; unchanged layers are not repeatedly tested.

Live evidence is recorded in the root handoff, including final-image task and
stop/start results. No SQL state or completion receipts are edited to pass tests.
Reconcile issued operations before rollback; preserve schema, retained data and
native stop proofs. Use a lifecycle-aware compatible App and matching Runtime
helpers. Do not force-restart occupied pools or revert Hermes to0.21.
