# Runtime Upgrade Sleep Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Extend existing CM rollouts to admit and safely awaken proven automatic sleeping pools, reconcile interrupted scale-in by native proof, and require future image deliveries through the API.

**Architecture:** Keep ordinary inventory unchanged. Add a persisted sleep preparation stage ahead of the existing rollout executor; use completed automatic scale-in journals and strict Kubernetes/PVC identity checks. Reuse existing rollout mutation, agent verification, admission and idle sleep after termination.

**Tech Stack:** Go, existing Upper DB/MySQL repository and Kubernetes client; Python standard-library delivery CLI.

## Global Constraints

- Build binaries, images and run tests only on10.130.81.54. Local source preparation/static checks/transfers only.
- Do not recover or mutate current business Runtime pools, instances or historical journals in this task.
- Retain OpenClaw data-safe preflight/migration/rollback and OpenCode unsafe-rollout rejection.
- Never clear needs_attention by timeout, synthesize termination proof, mutate old sleep snapshots or use raw external replica/image patches.
- Existing global admission remains enforced, with own-preparation exemption only under the same SQL admission lock and exact owner/status check.

### Task 1: Native proof and interrupted scale-in reconciliation

Files: `backend/internal/services/k8s/runtime_pod_scale_in.go`, `backend/internal/services/runtime_pod_scale_in_worker.go`, their existing/new scale-in tests.
Interfaces: consume RuntimePodScaleInProtection/RuntimePodScaleSnapshot; produce safe observation/cleanup of a native completed resize whose SQL phase was not updated. Existing signatures remain compatible.

- [x] Add tests using existing scaleInMySQLFixture and fake Kubernetes: interrupted protection plus verified same-operation resize converges; foreign operation, UID/template/PVC drift, unexpected Pod or binding retains protection; unchanged original set can cancel normally.
- [x] Run focused test on54 and observe new behavior failing.
- [x] Implement narrowly scoped same-operation proof, reusing native Observe/Release and existing fenced save/finish functions; no external correction of replicas or journal truth.
- [x] Run services/k8s and scale-in MySQL/race regression on54; review task diff.

### Task 2: Sleep-aware upgrade preparation and inventory

Files: new `backend/internal/services/runtime_rollout_sleep.go`, existing management dispatch in `runtime_management.go` and prepare handler/route, services/k8s sleep proof helpers if required, focused rollout/sleep tests.
Interfaces: consume completed runtimePodScaleInJournal, rollout ID/status/checkpoint and RuntimeIdlePoolDeploymentService; produce a durable rollout_wake management operation via POST /admin/runtime-rollouts/prepare, containing frozen sleep candidates with wake progress and deadline, then invoke existing preflight/rollout. The UI and CLI poll exact operation ID from existing runtime-scheduling history.

- [x] Test all-sleeping and mixed inventory, missing/changed proof, own-rollout admission versus unrelated operations, lost wake response, old checkpoint compatibility and leader restart. Assert ordinary inventory continues to exclude zero replicas.
- [x] Run focused tests on54 and verify RED.
- [x] Implement discovery with no mutation during preflight; persist candidates before wake; recheck exact operation admission and native identity for every write; resume only the same candidates.
- [x] Integrate ahead of existing OpenClaw classification/preflight as needed and non-OpenClaw pool freezing; preserve existing compatibility paths and fail closed on unprovable sleep identity.
- [x] Verify GREEN and existing rollout completion/placement/recovery tests on54.

### Task 3: API-only release workflow and durable instructions

Files: `deployments/runtime-upgrade/upgrade-runtime.py` and README, `AGENTS.md`, parent AGENTS if appropriate, small Python CLI tests.
Interfaces: use existing authenticated preflight/create/history endpoints. CLI takes API URL, runtime type, immutable target digest, credentials via environment/file; outputs operation IDs/status without secrets. No Kubernetes or database access.

- [x] Test terminal success/failure/timeout, OpenClaw preflight handling, conflicts and no mutating calls other than supported upgrade API; use local HTTP fixture running on54.
- [x] Implement submission, polling, nonzero exit on blocked/failed/cancelled and preserve operation identity on retry; never silently patch images/settings/replicas.
- [x] Record user requirement in AGENTS and document building on54, immutable image delivery, supported/blocked cases and acceptance.

### Task 4: Committed image defaults and restart consistency

Files: new committed-image service, SystemImageSettingService effective default lookup, server bootstrap and focused tests.

- [x] Resolve only native-verified finished ordinary rollouts as immutable defaults; preserve Pro/extra custom cards and exclude pending/failed/lab/recovery operations.
- [x] Use consistent committed defaults for new-instance cards, backend lookup and server startup; retained source pools need exact frozen UID/image plus native disabled role proof or exact isolated-instance/binding authorization, never arbitrary current image adoption. Missing/replaced retired canonical sources must not be resurrected.
- [x] Verify in-place restart, committed successor/retired source, excluded states, partial data-safe isolation semantics and unknown proof fail-closed on54.

### Task 5: Whole-change verification and review

- [x] Run existing related Go services/repository/handlers/k8s suites with disposable MySQL on54, plus focused race tests and CLI tests. Optional full database fixture issues and exact command scope are recorded in the acceptance note.
- [x] Review all diffs for lifecycle/ownership violations, old checkpoint compatibility and accidental business cluster mutations. Fix important findings and rerun impacted checks.
- [x] Save exact test evidence and limitations in release notes. Do not claim live Runtime recovery; business pools stay unchanged until user requests recovery.

## Completion evidence

Development source: `ed6ea86cdd2340423167070bad563a0946d7bcd3`. See `docs/superpowers/tests/2026-11-12-runtime-upgrade-sleep-acceptance.md`. Candidate built on54; intentionally not deployed so business incident remains intact. No business Runtime recovery or image upgrade was performed.
