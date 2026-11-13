# Workspace follow-up and OpenClaw recovery validation

Status: released and verified on54. App source03e7e72bc569c6b7be3b501797270291553d1613; native source correction remains separately committed for a controlled worker upgrade.

## Requested behavior

Continue working now prioritizes recent Agents from recorded model invocations, conversation activity, team tasks and runtime tasks, sorted/deduplicated over90days with at most6 entries. Each source and the metadata query enforce the personal caller's current CM ownership, including administrators. Lifecycle/heartbeat timestamps do not manufacture recency. Opening a detail page alone is not tracked. Missing activity sources have explicit partial/unavailable coverage.

The desktop dashboard shows three independent panels: Token total/input/output plus history and recorded costs; assigned CPU/memory/storage against quotas in a speedometer; full Agent state aggregate in a donut with total and labeled counts. Physical eligible slots remain separate from quota. Unknown samples/limits remain unknown, histories contain real observations, currencies remain separate. Welcome, pins, task result/failure/running details, read acknowledgement and annual activity remain available. Narrow screens retain grouped navigation; desktop1440x900/1280x800 fits the content area.

## OpenClaw findings

Read-only diagnosis found26 OpenClaw instances:25running and instance65 in error. Its authenticated worker state retained the exact assignment, showed a positive automatic restart attempt, quarantine and no idle confirmation; CM had no recovery journal for that failure.

The native post-ready retry path called Stop on a process whose native isolation contract requires an external container reset. It replaced the original retryable failure with gateway_stop_failed. The existing CM controller deliberately does not retry arbitrary stop failures, so this particular automatic handoff was lost. Automatic recovery already existed: persisted30/120/300-second backoff, maximum3 attempts and budget reset after5minutes of continuously fresh healthy observations.

The native source correction delegates crash recovery to the confirmed external reset and retains the original cause/PID/reservation. The CM compatibility correction admits an old worker's lost cause only using a fresh authenticated, exact native Pod/slot/assignment/owner/generation snapshot with positive automatic-restart provenance, no idle/maintenance intent, and existing lifecycle/desired-power/upgrade guards. Existing actual old-container termination, cleanup, writer-release receipts and atomic generation checks remain required. A stopped or uncertain writer is never inferred merely from a failed HTTP request. No database/state clearing or force-start is used.

Repeated SQLite disk I/O log messages are observed, but their underlying extended errno and connection to each process exit remain unproven. Deployed code and sampled headers show rollback journaling on NFS; this is not evidence of WAL mode. No storage mount, journal mode, permissions or production SQLite file was changed. No current node-pressure/OOM evidence established a resource-exhaustion cause.

## Source, review and verification

- Dashboard CM commit f32008e; recovery initial CM921f0a4; native source c450acb on AgentsRuntime fix-release11.11. No branch merge/push.
- Dashboard independent review approved the source, caller scope, full aggregates, quota semantics, regressions and54-generated rendered screenshots. Recovery independent review identified a pre-intent interrupted-stop continuation gap. Follow-up03e7e72 adds only exact durable-owned draining continuation with complete fresh provenance checks and truthful exhausted-budget copy; both findings independently verified resolved.
- Native affected-package race on54:359pass,20explicit skips,0fail; CM initial focused recovery race including dedicated MySQL:105pass,25unrelated opt-in skips,0fail. Parent independently counted the JSON result records. Final focused correction race:116pass,25explicit unrelated skips,0fail, including the real isolated recovery SQL and new interruption/negative controls. Independently counted from the final54 JSON output.
- All application tests, browser sessions, binaries and container builds run on54. Local work is limited to source preparation/review/Git/transfer and viewing remotely generated image artifacts.
- Disposable fixture namespace cm-workspace-v2-20261112, separate dashboard and recovery databases. No production database fixture writes. Private credentials/config remain only in restricted remote files.

## Release evidence

- URL: https://10.130.81.54:30443/dashboard
- Exact released CM source:03e7e72bc569c6b7be3b501797270291553d1613 (Dashboard f32008e; compatibility921f0a4 plus review correction03e7e72).
- Tracked archive2045files, SHA2564fb7d157c06ace8bc489b1c4a409fa0e6e836acb1d03f3b0ae9013dcbbe0c985; manifest SHA256f418786241e5cf9e69e2380c8c5fe09e54a5ae5d7b4eae9d48d1126930f4a83b. Strict inventory/hash verification preceded building.
- App immutable image:clawmanager.local/clawmanager@sha256:3463717fb973a92bb407c7c12c8d035a1c7432400cc88c7b2db10a6f7b62c1b6; base553416e8. Built on54 at2026-11-12T03:35:58Z, versionv2026.11.12-workspace-v2. OCI base layers/runtime config preserved; all4 app binaries rebuilt from the same exact source.
- Deployment clawmanager-system/clawmanager-app, generation12,1/1ready, Podclawmanager-app-bf5bc88cd-flwwm. Every serving Pod imageID and all4 deployed binary hashes matched the candidate. Startup/nginx/runtime-helper files and other Deployment image references remained unchanged. Old image retained.
- HTTP/auth verification:161regular dist/public/lazy/preview files byte-identical to candidate, versioncommit correct, unauthenticated401, invalidrange400. Today/7d/30d personal API checked:56instances, full state sums,365activity cells, history lengths, real recent ordering/deduplication/coverage. Current retained history yields1recentAgent, without fabricated extra rows; recorded native tasks currently0.
- Workspace measurement regression:all56 current instances had observed positive bytes; sampled list/detail/status agree. No stopped/sleeping live samples existed during this check; those cases remain covered by isolated tests.
- Live browser passed1440x900/1280x800/768x1024/390x844, desktop fit, three independent visible panels, recent order matching its actual API response, selected resource retained after period change, state/resource histories in individual details, native-dialog Escape/focus return, annual365-cell keyboard navigation, account Portal removal/Console preservation, measured-positive Workspace column. Zero browser errors/HTTP5xx/unexpected API writes. Parent inspected actual1280production screenshot after deployment.
- Production schema remains110/111; no new migration was introduced.

### Observed automatic recovery

The observer performed SELECT/Kubernetes GET requests only; no manual recovery/start/reset/SQL edits or test-model calls. At03:36:09Z,65 was generation1/error with no recovery journal. The new leader acquired its lease at03:37:15Z. At03:37:44Z, the existing controller had independently admitted the exact native source with attempts0 and a30-second retry deadline. Old slot6 terminated normally at03:37:49Z with its cleanup commitment; the retained generation1 stopped-source audit matches instance65/gatewaygw-65-1/PodUID47eb44c4-dea6-4e9c-873a-35f36d59c083.

At03:38:00Z the same automatic attempt had advanced exactly once to generation2, with replacement PodID8/gatewaygw-65-2 and phaseplacing. At03:38:13Z the replacement passed readiness. The03:38:48Z snapshot shows instance and bindingrunning, fresh health, attempts1 retained, no pending retry/phase and healthy_since set. All26OpenClaw instances are running; the other25 retain their previous generation/status. This verifies the existing failing instance recovered through the deployed CM compatibility path, without restarting healthy Runtime Pods.

The native worker fix c450acb is committed/tested source, not a deployed worker image. Existing worker image references are retained; CM compatibility handles the verified legacy automatic-reset symptom. The underlying SQLite I/O errno remains unproven; this result does not claim restart repairs storage or that all future errors are eliminated.

Authoritative build/rollout/HTTP/browser/source-reset/schema evidence is under /root/cm-workspace-20261112-v2/release-03e7e72bc569/ on54. Implementation/independent review reports and same-source fixture screenshots are mirrored locally under /tmp/cm-workspace-20261112-v2/. Follow-up documentation commits contain no executable changes.

Owned fixture cleanup completed: namespacecm-workspace-v2-20261112 is absent, exact Vite9432 PID2043426 terminated, no owned port-forward found, source/build/evidence retained. The production namespace, normal checkouts, existing image/cache artifacts and unrelated input files were preserved.

Final post-cleanup check at2026-11-12T03:40:28Z: health200, exact released version, public status65running/generation2, all26OpenClaw running, other25status/generation unchanged, Dashboard56running/no failed. Real recent count remains1. Evidence: final-health.json. This is an observed recovery and short healthy window, not a claim that the unresolved storage symptom cannot recur.
