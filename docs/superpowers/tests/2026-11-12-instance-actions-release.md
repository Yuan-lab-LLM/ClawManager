# Instance list actions and deletion recovery release

Source: `ca94476a7d59d9766c01355521ba47c0c21df7ca` (includes list actions commit `45e8869876bbd4bc33301e83f72476aabc6fbb8a`).
Environment: `https://10.130.81.54:30443/instances`.
Version: `v2026.11.12-instance-actions`.
Image: `clawmanager.local/clawmanager@sha256:1b6dd87dd6cd0453bb6c6fcc9cd90bcb60f9d34e0838d13150a4456d3ff1293c`.
Deployment: `clawmanager-system/clawmanager-app`, generation27, ready1/1.

## Delivered behavior

The instance list header places Hide immediately before Expand management. A collapsed list can be restored from the Agent header; restoring from Agent fullscreen exits fullscreen first and returns visible keyboard focus. The first two list actions are Create Instance and Instance management, followed by search and actual instances. They are also available in the narrow-screen instance picker. Creation is absent from the page top-right. The empty workspace has a centered creation prompt/button and does not automatically open the management dialog.

Deleting the current instance, including deletion from expanded management or another browser tab, no longer strands the workbench on Instance not found. A metadata404 immediately clears that account's deleted resume target and stale list cache and replaces the route through the inventory entry. Recovery does not wait for a hung status request. Search/page/scroll preferences remain; pending deletions are skipped, including across inventory pages. A server503 still surfaces an error instead of pretending the instance was deleted.

Batch deletion retries busy instances in rounds within the existing three-second total budget. A busy first instance cannot prevent later available instances from being queued. Ownership checks, lifecycle locks, durable guards and termination proofs are preserved. The management dialog shows each failed instance name/reason and keeps failed selections for retry; its summary says queued for deletion rather than claiming asynchronous deletion already completed.

Stop/drain assignment requests now use the existing30-second control timeout; creation retains10seconds and shorter parent cancellation is respected. This fixes the separate non-graceful request-budget mismatch with the Agent's20-second primary shutdown grace period. It does not escalate a previously accepted graceful-only stop.

## Verification on54

Application execution, browser tests, Go/frontend builds and image builds ran on54. Local work was source editing/static inspection/transfer.

- TypeScript passed; focused list/split/dialog/entry lint passed. InstanceListPage retains its same four pre-existing set-state-in-effect lint findings, verified against the prior commit; no claim of full lint cleanliness.
- Nine navigation unit tests passed, including deletion cache removal/account isolation/view retention.
- Added metadata404 regression failed before the fix. Full real-route browser regression passed list action ordering, no top-right creation, runtime and management deletion, last-instance creation, pending-deletion exclusion,503 behavior, batch failure details/retained retry selection, resizes, iframe retention, snapshots, account isolation, five widths and five locales. A final deletion subset passed against the simplified metadata Promise path, holding status pending while404 restores navigation.
- Administrator and Dashboard browser regressions passed. Source review/rereview found no remaining important issues after fixing fullscreen focus and404 recovery blocked by status.
- Batch starvation regression failed against old code and passed after repair. All handlers tests passed91.960s; final combined BatchDeleteLiteInstances tests passed3.176s. Lifecycle/queue/DeleteV2/slot reconciliation/stop-proof tests passed0.047s. Stop deadline regression failed for draining/stopped at10seconds before the fix and passed with30seconds; six transport-level cases preserve creation and short-parent budgets. Gofmt and diff whitespace checks passed.
- Exact2064-file source inventory, four rebuilt binaries, deployed serving image and all163 frontend files matched. Stable runtime helper/config hashes and other deployment images were preserved.
- Actual deployed browser verified hide placement, action order, expanded management, creation form, narrow-screen picker, five responsive widths, search/switch/resume, skills/menu, resizing, usage period isolation and Dashboard. Browser-only response fixtures verified current-instance404 fallback and empty-workspace creation without deleting a real instance. No real lifecycle writes or test instance creation.
- Browser-origin Service retains128 NodePorts31600–31727, target8444, matching selector and ready endpoint. All current instances were stopped, so this release did not start instances to exercise native composers.
- Final health200 at10:47:40UTC:32 instances,31 sleeping and1 offline. Existing stopped instances were not restarted.

## Confirmed live failure and remaining instance46

Read-only evidence before release: batch/delete returned207 in3.048seconds;32 instances remained,31 without a held lifecycle lock and only46 locked. No matching persistent manual guard, rollout or Team ownership explained the batch-wide failure.46's slot0 was stuck in reset preparation while slot1 remained healthy. The original shared-budget loop allowed the first busy item to starve the remaining31.

Further inspection corrected an initial hypothesis:46's persisted stop intent has `graceful=true`. The20-second non-graceful escalation timer therefore does not apply to46. After release, read-only90-second observation still showed draining generation3, held lock and no termination proof. Slot0 state queries intermittently returned200 between reset attempts, while cancellation/deadline events continued.46 has not been proven recovered or deleted; no force-stop, new deletion attempt, database mutation or proof bypass was performed. The retry fairness/UI fixes are deployed independently of this remaining graceful-stop investigation.

Remote evidence: `/root/cm-instance-create-20261112/release-ca94476a7d59/`; read-only observer: `/root/cm-instance-create-20261112/observe-stop46.py`. Private credentials, response snapshots and screenshots are not committed.
