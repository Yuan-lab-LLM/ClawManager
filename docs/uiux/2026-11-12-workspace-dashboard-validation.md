# Workspace dashboard — release validation

Status: deployed to 54 and verified against real APIs, running image/binary identity, all emitted assets and loaded browser views.

## Scope and environment

- Approved design: v9 in `2026-11-11-workspace-dashboard-preview.html` at f93e4a8. Production entry: `/dashboard`.
- User refinements: remove Portal from the account menu; repair default-zero instance Workspace sizes using actual measurements.
- All application execution, Go/Node tests, browsers, binaries, frontend bundles and images run on `10.130.81.54`. Local operations are source preparation/review, Git, transfer and viewing captured images.
- Existing production URL: `https://10.130.81.54:30443`.
- Previous image retained for rollback: `clawmanager.local/clawmanager@sha256:e8252ca5c28f37bd30a4adfffe59b01babb6d72da3a39050f1e90487b9c03592`.
- Disposable test namespace: `cm-workspace-20261112`; no production database fixtures or model/lifecycle test mutations.

## Backend and scope verification

Task1 commits through 513c5d4 passed independent spec/quality review. Caller identity comes only from JWT `userID`, including personal admin visits. MySQL integration covers owner/northbound isolation, blocked calls, duplicate cost snapshots, currencies, missing prices, archived usage and attributable terminal task history after physical deletion. Native runtime completion remains `ended`, and historical state/capacity gaps remain null.

The full remote command `go test ./internal/workspacedashboard ./internal/handlers ./internal/db ./cmd/server -count=1` passed. Subsequent history/extraction/retention fixes passed the workspace package and server compile. Native eligibility regressions verified read-only capacity lookup, unchanged existing instance birth fences, and no audit writes. Strict backup registration/fresh-schema/export/unknown-field regressions passed after adding observation history to the explicit registry.

A delivery-only probe against actual Auth + SetUserInfo + dashboard handler passed on 54: `go test ./internal/handlers -run TestWorkspaceDeliveryTwoJWTAccounts -count=1 -v` (0.029s). User11 and admin22 remain caller-scoped despite forged query selectors; refresh/expired/wrong-signature/zero-ID tokens do not reach the reader. This probe used an exact 513c5d4 backend archive plus a temporary test, not production source or data. Log: `/root/cm-workspace-20261112/auth-probe.log`.

## Workspace size repair

Read-only pre-release reproduction found all56 live instance rows reporting0, while bounded file-list traversal found at least12740 logical file bytes in each of three samples. The existing update method had no production collector callers.

Task1b 774c1f4 passed independent spec/quality review. Migration111 adds explicit successful-observation proof and internal reset fences. List/detail/status expose `workspace_usage_observed_at`; null means unavailable, while genuine measured empty space has0 plus a timestamp. Sampling runs in existing credential-scoped helper processes, starts with the actual leader lifecycle, and respects per-tree/pass/process limits. Conditional writes reject identity/reset/older-sample races and preserve business `updated_at`. Failed scans retain prior successful values.

Remote focused Go tests with actual isolated MySQL passed, including NFS helpers, reset erasure, replacement handoff, ordinary stop, stale updates, monotonic publication and API serialization. Race tests passed (5.463s); strict fresh schema/registry tests passed (5.581s). Frontend model2/2, noEmit, new utility/types lint, production build and real-route browser fixtures passed. EN/ZH cover1440/1280/768/390, JA/KO/DE cover390, with unavailable/empty/prior-success cases and no page overflow. The11 existing instance-page hook lint diagnostics were unchanged by exact baseline comparison. b315d95 strengthens the browser harness to fail on uncaught page errors; its covering browser rerun passed.

Supported measurements are native shared Lite workspaces under the configured root, including stopped/sleeping instances. Desktop `/config` and arbitrary legacy paths remain unavailable without new live exec/wake. Samples are logical regular-file bytes, not allocated quota or an atomic filesystem snapshot. Real kernel hard-NFS outages were not induced; at most four stuck helpers can exhaust this process's collection budget until kernel I/O recovers.

## Frontend and combined review

Task2 0a3a13c plus c90fec6 passed independent spec/quality review. The real personal page uses caller/range-keyed queries and account-keyed browser preferences. It includes welcome/resume/pins, separated-currency usage, actual capacity/state histories, annual activity, native dialogs, keyboard access and paused hidden/reduced motion. Portal is removed only from the account menu, preserving language/Console/logout and the route.

Remote model tests passed9/9. Six focused review-browser regressions passed, including retaining Capacity/Agent/currency views and focus while changing period, populated365day unavailable activity, Shanghai UTC-midnight boundaries/exclusive endpoints, and mobile14row calendar movement. The full actual Router/Auth browser suite passed, covering late user/range responses, pins/read acknowledgements/storage failures, three native detail types, stopped-instance navigation without API writes, four widths/five locales, hidden-page polling and null/tiny-value chart geometry. Scoped lint and both noEmit checks were clean; production build passed3.27s with existing large-chunk/data-age warnings. The focused existing account-menu regression passed in both user/admin shells.

Root independently viewed final fixture screenshots for1440/1280/390 and the1280Agent-state view: compact pins, complete currency strings/ranks, all preview nodes, readable axes and one-screen fit. The fixed unavailable fixture retains365unknown cells and shows unknown counts. Native routes open the existing Agent/team pages and do not support arbitrary task/session deep links.

The final combined review found two additional failure-path combinations. Commit6c144b4 independently combines team/runtime task-source outcomes, and clears unknown Token/call history after interrupted row reads while retaining valid period totals. Actual isolated MySQL source combinations and a standard-library SQL driver injecting Scan/stream failures reproduced the bugs before fixes. The focused green command passed the workspace package in0.367s and compiled the server. Four standard-library delivery guards passed, including extra source inventory, lazy asset mismatch, every-replica image identity and positive measurement proof. Bounded final re-review approved the complete fix and release guards; no blockers remained.

## Exact-source build and deployment

| Field | Verified value |
| --- | --- |
| Released source | `6c144b4ae7d69fde56d8083e4a2556cf450647b5` |
| Version | `v2026.11.12-workspace` |
| Build time | `2026-11-12T02:18:35Z` |
| Image | `clawmanager.local/clawmanager@sha256:553416e8edd34c121f3e8f9caac7f19a007527d9e4629517efbfca57a5f11ca9` |
| Archived source SHA256 | `aa27f0c5b4621992bfca2402e210f893ef1f517dade312048d71bdf45d991dd1` |
| Source manifest SHA256 | `68c815744b75bad1b39b99ecca02a191086d88814bc0896f7713f4bf41d05211` |
| Running Pod | `clawmanager-app-d8947746d-5x22x` |
| Deployment generation / ready replicas | `11 / 1` |

The controller prepared only tracked backend/frontend/approved-preview inputs using Git archive. All2041 input hashes, exact file/directory inventory and reviewed script hashes passed on54 before compilation. The source and release directories were fresh. All four Go binaries rebuilt on54; the frontend production build passed3.25s with the existing large-chunk warning. BuildKit used the pinned deployed OCI base, preserved its runtime config/base layers, and imported the resulting image. The preview is included in the immutable image.

The existing application image field was changed through a JSON Patch compare-and-swap. Rollout completed. The first post-rollout guard temporarily rejected the old Ready-but-terminating Pod; after it disappeared, the unchanged reviewed read-only verification block ran successfully without replaying the patch. The single serving Pod's actual imageID equals the candidate digest, and every rebuilt binary matches its built artifact. Startup/nginx, the retained eviction/template helpers and all unrelated Deployment images matched the pre-rollout baseline. The prior image remains available; rollback itself and old backup capture against the migrated schema were not tested.

| Live binary | SHA256 |
| --- | --- |
| clawreef-server | `d2532fb90a069d614ec455e79e97439b237700ced39aae6a690519fcd696d936` |
| clawreef-northbound-gateway | `642ecac063147a4c06a659ca38ea024d9626ab7e4ba10cd58bbb43d50a0c97aa` |
| system-backup-controller | `54e0d22c61395b54649b017a265e4f307b3d6d42effbf0c90a4114dd76c81671` |
| system-backup-artifact | `bf603afc36aac85f9968086a96dffcf834af90eaf69132a840ab9dfd9ae7aabe` |

Authoritative build/rollout/HTTP/browser/schema/cleanup logs and manifests are in `/root/cm-workspace-20261112/release-6c144b4ae7d6/` on54. The tested release is source6c144b4; subsequent repository commits recording this evidence are documentation only.

## Live result

- All161 emitted regular dist files, including lazy/public assets and the approved HTML preview, matched served bytes. Health/version returned200; version reported the exact released commit. Missing auth returned401 and invalid range400.
- Real today/7d/30d responses each reported56 instances. Instance/task/Token/capacity/activity sources were ready. State/capacity history is partial because collection has just started;7d/30d cost coverage is partial because complete price/expense records are missing. Missing prices are displayed honestly rather than assigned a guessed price or comparison.
- All56 retained instances had successful positive Workspace measurements. Sample72 reported41370977bytes;71 reported40436847;70 reported40430155. Each sample had identical successful-proof timestamps/bytes across list, detail and status. A real list row rendered39MB. No stopped/sleeping instances were present for a live sample; those cases are covered by the isolated helper/collector/lifecycle regressions rather than manufactured production transitions.
- Read-only production schema queries confirmed migrations110/111 and all three new instance metadata columns. A real per-user observation row existed, first recorded at`2026-11-12 02:21:54.421553` UTC. No history was backfilled. Strict backup schema/export/unknown-field checks ran on fresh isolated databases; all four deployed consumers have the matching embedded code. No production restore was executed.
- Real authenticated browser checks passed1440x900,1280x800,768x1024 and390x844; period retention, native dialog focus, Portal absence/Console access/account Escape, mobile annual-calendar keyboard, and a positive Workspace list cell passed. Collected page errors, HTTP5xx and unexpected API writes were all empty. The final desktop captures explicitly waited for data counters, correcting an initial loading-state capture; a measurement-column selector was also corrected to the actual Chinese label. Root viewed the loaded1440/1280/390,1280state and real instance-size screenshots.
- Only the guarded owned test namespace`cm-workspace-20261112` and owned Vite9422 processes were removed. The namespace is absent, no task-owned background processes remain, and post-cleanup health/version still return200 with the released commit. Build/evidence artifacts remain for inspection. The normal checkout and `uiux` branch are preserved; unrelated user files remain untouched.
