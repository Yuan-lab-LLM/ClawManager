# Runtime reconciliation and recovery implementation plan

**Goal:** Resolve the ten reviewed scheduling/scaling/recovery gaps and retain explicit administrator recovery paths when automatic repair cannot finish.

**Approved design:** The user approved the preceding review and remedies. Reuse lifecycle locks, generation/Pod UID/assignment fencing, durable management journals and Runtime observations. Do not introduce an independent scheduler or bypass source-stop proof. Automatic retries are bounded; administrators can request a fresh check/retry and see blockers. No workspace/PVC deletion, SQL reset or force-release based on missing heartbeat.

**Global constraints:** Preserve instance ownership and stopped/deleting desired state, exact generations, current Kubernetes Pod UID and assignment identity. Treat unknown as unavailable. Never infer stopped from missing report. Keep manual recovery authenticated/admin-only and audited. Do not change live cluster during implementation. Preserve existing untracked user documents.

## Tasks

- [x] Scaling safety and continuation: reproduce manual shrink with occupied/unknown observations; obtain fresh physical proof for paused sources before all replica deletion. Resume interrupted post-PATCH operations, including same-owner failures; administrator retry continues observation/cleanup without replaying uncertain external mutations. Try another sleeping pool when a registered candidate is unusable. Include focused failure-injection tests.
- [x] Scheduling liveness: reproduce unpublished reservations, physical capacity exhaustion, stale failover first-stop failure and fixed-page starvation. Preserve timeout origin, retry publication only with authoritative absence, account for physically allocatable slots, persist/resume failover, and rotate bounded scans. Do not weaken published assignment/generation fences. Test each failure and normal paths.
- [x] Agent reports: prevent omitted reports deleting protected running ownership; preserve suspicion/quarantine. Separate physical slot count from effective admission limit or reject mismatched configuration explicitly. Restore ownership only with strict current authority and conflict checks. Test omission, genuine stop and routing mismatches.
- [x] Recovery phases and manual instance path: persist source-stopped/continuation independently of retry budget, including last-attempt DB failure. Add administrator inspect/recheck/retry path reusing locks and stop validation; preserve stopped/deleting/upgrade and storage fences. Return blocking reason and no false success. Expose accessible actions within existing Runtime UI, with busy/error states and localization.
- [x] Verification and documentation: focused regressions, backend package/race tests where supported, frontend tests/typecheck/build, independent review. Document actual manual steps, retained blockers and tested limits. Commit reviewed changes without PR.

## Verification commands

Use /usr/local/go/bin/go on this host; default go/GOROOT mismatch is known. Darwin services package has a pre-existing linux-only nfsTeamGuard reference in browser_worker_nfs_test.go; use a temporary Go overlay excluding only that test for service verification. Run real-MySQL repository cases in isolated local containers when possible; do not create cluster test Pods. Run frontend package scripts from frontend/package.json.

## Manual fallback contract

Inspect is read-only and returns desired state, binding/source identity, observations, retry count, and management phase/blocker. Retry reacquires lifecycle/management locks, re-reads all identities, and reuses existing fenced recovery. Stopped/deleting instances cannot be resurrected. Exhausted automatic retries may be re-armed explicitly; uncertain source ownership cannot be cleared by that action. Interrupted management operations resume the persisted phase, never replay a possibly executed mutation. Older Runtime protocols may still require controlled source repair/upgrade; explain the blocker and keep capacity unavailable.
