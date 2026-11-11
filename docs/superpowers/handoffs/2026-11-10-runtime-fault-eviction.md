# Scoped Runtime fault eviction — staging acceptance, 2026-11-10

Feature commit: `92d455bc1f5271f51fdb79287dc8ec3c1bbb0e15`; native-stop integration fix: `609135f431007b80932f414a2e2c88c03510de3a`. Both are on `preview`; no PR was created.

Environment: `https://10.130.81.54:30443/admin/runtime-pods`.

Final image: `clawmanager.local/clawmanager@sha256:32b5176410ecdf753bcab427f8e770550056617aa6ee10e2a6bb3e1aea3e7068`.

The application image contains all five verified Linux executables and current frontend assets. Runtime images and the TLS Secret were retained. The final application rollout and `/api/v1/version` were checked against the built payload.

## Product-path recovery

Recovery was submitted through authenticated Pod eviction APIs with explicit instance IDs; no application business SQL was manually rewritten.

| Source | Intent | Result |
| --- | --- | --- |
| Instance 156, old Pod 116 already deleted with no retained controller | deleting, generation 2 | Exact protected ledger + original management PVC + node/CRI receipt retired its old binding. The native manual-stop journal consumed that receipt; normal instance deletion completed. |
| Instance 118, Pod 135 / slot-0 | stopped, generation 9 | Independent helper stopped only the pinned old container, retired the binding, and completed the old idle-stop journal. The instance remains stopped at generation 9 with no active stop operation. |

Operation IDs: `runtime_mgmt_c1b0aba2-47c3-47f5-8b32-b760439f42a1` (156) and `runtime_mgmt_1a1eb950-1889-4135-8671-cf6bd7dd6c51` (118). Both reached `completed` / `released` with independently confirmed receipts. The native controller did not synthesize graceful Agent acknowledgements.

The original five Runtime Pod UIDs were preserved. Only the selected OpenCode slot-0 container restarted once; the other 44 container identities/restart counts were unchanged. Seven project-file hashes, the workspace PVC UID and HTTPS certificate fingerprint matched the before snapshot. Existing Team instance 166 retained its original creating/generation-1 state.

Live acceptance covers stopped/deleting continuation. Running-intent requeue, generation replay and new-source protection are covered by SQL/controller regressions.

## Verification and limits

Broad backend regression before the small native-stop integration fix: 4,876 passed, 392 skipped, 26 packages passed, zero failures. Targeted final native proof/reconciliation tests and ordinary idle/manual baselines passed; the latter used a documented test-fixture-only missing-column overlay. An unrelated Pro registration fixture still lacks `desired_runtime_action` before reaching the recovery code. Frontend tests: 72 passed; TypeScript/Vite build passed. Chrome desktop component acceptance passed; additional locale/mobile runs were blocked by harness teardown and were not counted as passing.

Fault eviction targets verifiable database-bound sources. Orphan execution without a database binding and other legacy Unknown slots remain explicit blockers; this rollout does not claim all old Pods are empty or reusable. The source remains paused until separate physical recheck/maintenance. See [the operator guide](../../runtime-reconciliation-recovery.md).

Only the two owned completed helper Jobs/Pods were cleaned up, using exact UID preconditions after durable receipts were retained. Private SQL fixtures and browser harness processes were stopped and their owned data directories removed. Deployment image content is retained; scratch OCI/layer files were removed. Projects/PVCs were not deleted by recovery.
