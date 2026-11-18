# Lite lifecycle repair implementation plan

> Execute the authorized repairs on `fix-release11.11`; use test-driven-development and review each change before completion.

**Goal:** Batch creation and deletion work with real model identifiers, and a failed instance does not strand confirmed idle runtime slots.

**Architecture:** Keep the existing lifecycle locks and authenticated teardown protocol. Treat a stopped assignment as history, not a current worker owner. Reuse retained stop receipts for deletion rather than adding another lifecycle state.

**Global constraints:** Preserve user work and UI display names. Never admit an occupied or unverified slot, bypass owner visibility, synthesize a stop receipt, remove unrelated custom providers, or mutate existing live instances during verification. Deployment and temporary-fixture cleanup are recorded in `2026-11-11-migration-recovery-convergence.md`.

- [x] Concurrent built-in initialization: force four initial reads across two service replicas; after MySQL duplicate-key errors reread the existing resource, preserving unrelated insertion failures. Verify all callers receive the same row.
- [x] Model identities: inject `ProviderModelName`, fixed managed provider `clawmanager`, and matching capability/context keys; preserve namespace slashes and deduplicate duplicate deployments. Resolve real model names in the scoped gateway, keep failover on the same real model, and reconcile persisted managed OpenClaw provider/allowlist entries without removing custom configuration. Test new and existing invalid configurations and visibility/revocation.
- [x] Slot lifecycle: allow a valid stopped assignment from a previous worker epoch only with current authenticated Pod/worker identity and complete idle/cleanup proof. Test both allocation and inventory; retain rejection of active stale owners, malformed identity, missing proof, and pending cleanup.
- [x] Deletion: reconstruct the previous source identity from recovery phase (`source_stopped` or `placing`) and source Pod; validate existing immutable stop evidence instead of unconditional rejection. Test missing/mismatched proof and active publication. Keep batch delete useful under lifecycle contention and preserve actionable partial failures.
- [x] Verification: run targeted red/green regressions, related Go suites, MySQL integration in disposable databases, race checks for initialization, and final code review. Linux suites run as isolated test binaries, not by changing the running deployment.

**Final test environment:** All final compilation, tests and image builds run on `10.130.81.54` with Go 1.26.8. The existing services suite includes Linux-only helpers, so Linux is the authoritative test platform. Local work only prepares, checks and transfers source.

## Verification evidence

- Concurrent four-caller/two-service initialization, historical stopped worker slots, retained recovery deletion proof, and transient batch lock contention all failed before their respective fixes and passed afterward.
- Linux services regression binary passed against disposable MySQL databases; tested allocation, slot audit, deletion, recovery, model injection, and lifecycle guards. Existing application databases were not modified.
- Native `internal/handlers`, `internal/repository`, and `internal/services/k8s` suites passed; targeted batch deletion was repeated after distinguishing transient locks from durable guards.
- Initializer race detector passed. A temporary test-only overlay supplies the existing Linux-only fixture helper for native services compilation; no production source is overlaid.
- Full native services and AI gateway suites passed. Model collisions, owner visibility, exact saved IDs, and persisted invalid managed configuration have regression coverage.
- Full OpenClaw Lite runtime suite passed on Linux after the final migration follow-up. Final whole-change review approved spec compliance and code quality with no remaining findings.
