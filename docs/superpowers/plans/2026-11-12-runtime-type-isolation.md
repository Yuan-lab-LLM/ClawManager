# Runtime Type Isolation Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or executing-plans; perform targeted review and then whole-change review.

**Goal:** Isolate Runtime control operations by type and allow changed runtime images when waking verified sleeping capacity.

**Architecture:** Keep short atomic SQL admission serialization. Replace lifetime global blockers with explicit runtime-type scope. Sleep proof preserves identity and storage, accepts image changes and captures the current native configuration for upgrade preparation.

**Tech Stack:** Go, MySQL, Kubernetes client-go; existing CM admin APIs and Python release tool.

## Global Constraints
- All tests/builds run on10.130.81.54; local source and transfers only.
- Preserve existing user dirty files and legacy journals. Never patch Runtime image/replicas/SQL externally.
- Keep native upgrade compatibility and verified target digest checks. Unknown historical scope fails closed with a clear reason.

### Task 1: Type-scoped admission
- [x] Trace all admission, preparation reservation, upgrade, scheduling, scale-in/out and orphan-recovery callers.
- [x] Add MySQL regressions for held DSH allowing OpenClaw and blocking DSH, including legacy item-derived operation scope and ready preparation reservations.
- [x] Run RED on54, implement shared type resolution/admission, wire all real callers, run GREEN plus related tests.
- [x] Record commits and exact tests; review diff.

### Task 2: Image-tolerant sleep restoration
- [x] Add Kubernetes regressions for changed images/image env/generation accepted and UID/PVC/command/security/storage/unowned activity still rejected.
- [x] Run RED on54; implement shared normalized template comparison and remove generation equality that treats image revisions as foreign rescaling; retain operation/UID/CAS checks.
- [x] Freeze current validated template/image for prepare and normal allocation. Old journal remains immutable; verify native registration against current approved image.
- [x] Run GREEN for idle wake, sleep preparation, readiness, native identity and interrupted scale-in tests; review.

### Task 3: Integration and deployment
- [x] Run ordinary packages and relevant MySQL regression/race on54; correct stale assertions for the new explicit contract.
- [x] Whole-change review; fix Important/Critical findings and rerun affected tests.
- [x] Commit only task files, archive exact source, rebuild candidate on54, deploy CM with guarded app-only CAS.
- [x] Verify health/version/assets and browser origin endpoints; observe existing OpenClaw creation and actual ready/registered native capacity.
- [x] Use CM API release path for any Runtime image upgrades; keep unrelated DSH protection. Record remaining blockers and actual results.

## Completion evidence

Implemented in7257e9c,13cddc6 andcc781a9; final exact archive tests and54 deployment verified. See [acceptance](../tests/2026-11-13-runtime-type-isolation-acceptance.md) for concrete results and remaining environment blockers. RealRuntime image publication remains blocked by the non-resolvable local image host; the own unused preparation was cancelled through CM after confirming no rollout exists.
