# Coordinator stop finalization implementation plan

> **For agentic workers:** Use subagent-driven-development for independent CM readiness and target-template changes; preserve native upgrade gates.

**Goal:** Complete proved slot stops without waiting for the replacement execution container and admit work to healthy sibling slots.

**Architecture:** Reuse the existing stop proof, writer reconciliation and release handlers. A worker holds a local state ownership flock for its entire process lifetime. The coordinator mounts each slot's private state and may reopen and mutate its book only while it exclusively owns the same lock; otherwise it forwards to the live worker. No cached book crosses ownership transfer. CM continues to verify exact kubelet termination proof and persist stopped before requesting release. This narrows the previously proposed ownership change without replacing the assignment journal or adding another state machine.

**Tech Stack:** Go, Kubernetes, existing native Runtime upgrade API, Python lifecycle harness, HTML report.

## Constraints

- All compilation/images/tests on 10.130.81.54; local source and evidence preparation only.
- Runtime image changes only through CM native upgrades with immutable digests; unsupported OpenCode upgrades remain refused.
- Preserve Pod UID, slot, generation, operation, worker epoch, digest, writer locks, quarantine and storage uncertainty checks.
- Do not reset pools between repeated lifecycle cycles; record backoff, deadline failures and blocked checks honestly.
- Keep existing unrelated working tree edits.

## Tasks

- [x] Add pool tests: unavailable replacement still reconciles/releases; live state-owner lock excludes coordinator; stale/changed proof and writer locks fail closed; restart reload retains quarantine until release; released retries preserve newer assignment. Run failing tests remotely before implementation.
- [x] Reuse existing worker lifetime guard and add coordinator offline handler in AgentsRuntime/clawmanager-agent/slots. Reuse serveContainerReconcile/serveContainerRelease; open a fresh book under the lease for each request, close on every response. Run slots regression and race tests remotely.
- [x] Add deterministic target-template state mounts in ClawManager/backend/internal/services/k8s, for new pools and native operation targets only. Test source template unchanged and validation reconstructs the same target.
- [x] Replace whole-Pod readiness gating for ordinary admission with coordinator and selected slot checks. Preserve upgrading/source validation and prove an unhealthy sibling cannot block a healthy slot.
- [x] Independent safety review; fix important findings and repeat relevant tests.
- [x] Build immutable candidates on54 and invoke native upgrade per supported type. Stop/report exact refused type or native safety blocker without external workaround.
- [x] Run real lifecycle tests on successfully upgraded types, with 60s scans/120s idle, repeated cycles, stopped/running/asleep delete, workspace SHA and unaffected-sibling creation. Test worker unavailable300s in controlled automated integration where safely possible. Publish HTML with source/digest, per-case evidence, failures and blocked cases for user review.

## Additional defects reproduced during native deployment and smoke testing

- Initial candidates were imported locally but not published to the existing TLS registry. CM correctly refused manifest inspection. The original failed attempts and preparation state were preserved; publication was verified by content digest, history was reconciled, and the documented preparation-ID recovery was used. DSH operation4 and Hermes operation5 finished.
- OpenClaw operation6 failed isolated validation because its replacement emptyDir workspace root lacked management ownership. Fix only the validation clone: nonrecursive UID/GID10001 and0711; never chown user NFS. Native operation cleaned its target to replicas0 and retained source.
- The synthetic NFS probe cannot use legacy DELETE without a stop intent or fabricate a business SQL assignment. Its dedicated cleanup verifies the complete isolated scope, atomically scales the exact controller to0 and waits for all Pod API objects to disappear. This is isolated resource cleanup, **not** business C1/process-exit proof. Timeout/crash after scaling0 fails closed and can cause validation failure; source upgrade remains blocked.
- Preliminary Hermes restart failed because managed proxy-policy GET still depended on slot0 and PUT required all workers. Coordinator must own the canonical CAS lease; refresh peers with bounded calls and synchronize the selected worker before creation. Forward the exact expiry instead of extending stale trust. Native types without the feature return404 without contacting slot0.
- OpenCode safe upgrade remains explicitly unsupported; do not deploy its new image or count its current-version UI smoke check as new-version lifecycle validation.

## Final deployment and verification — 2026-11-13

- CM source `ab6b1e7dc4806dc9b0049abca11f5800d504f4b6`; AgentRuntime source `51505ec886ec87f5fe05fc64f76490955b56d005`. All builds and compiled tests ran on 10.130.81.54.
- Native upgrades completed: Hermes operation 7, DeepSeek Harness operation 8, OpenClaw operation 9. OpenCode remained refused and retained its prior image; this task does not claim its new-version deployment or lifecycle validation.
- Formal run `qa3-561396`, 09:16:36–10:02:22 UTC: each supported type completed 10 consecutive create / real auto-sleep / wake / workspace SHA / running-delete rounds without resetting pools. Manual stop/start/restart and asleep-delete checks also passed. Idle policy remained 60-second scan / 120-second idle.
- Formal plus independent browser followup: 256 PASS, 1 FAIL (browser executor missing Node; absolute-path followup passed), 1 BLOCKED (OpenCode new-version upgrade). The initial interim-Hermes restart failure and original publication/validation failures remain in report history. No failed record was relabeled PASS.
- Observed 75 worker exits and maximum actual kubelet restart gap 325 seconds. 26 sleep-stop finalizations completed before replacement start; native finalized books also confirmed deletion during long backoff, and 17 healthy-sibling creations completed entirely within a sibling's >=300-second backoff. No synthetic timestamp changes or external pool resets were used.
- Final snapshot 10:02:42 UTC: four ordinary type pools, one Pod each, all 9/9 containers ready; coordinator restarts zero, test instances/bindings/pending idle operations/nonterminal management operations all zero. Empty-pool native validation and OpenCode's existing-version UI smoke are explicitly distinguished from data-bearing upgrade verification.
- Exact CM selected regression: 148 PASS / 0 FAIL / 0 SKIP. Final AgentRuntime module `go test ./... -count=1` passed. Focused coordinator/proxy/native-pool race tests passed with count=5. A broader preview race fixture failed and was reproduced on baseline; retained as a testing limitation.
- User-review artifact: [HTML report](../../reports/2026-11-13-coordinator-finalization-test.html), [sanitized evidence](../../reports/2026-11-13-coordinator-finalization-test-evidence.json). Published at https://10.130.81.54:30443/reports/2026-11-13-coordinator-finalization-test.html.
- Published HTML and downloaded evidence matched SHA256. Browser verification at 1440/1280/768/390 widths passed with no page-level overflow or JavaScript errors; filtering, expandable evidence and JSON download covered all 272 report rows (including initial history).
