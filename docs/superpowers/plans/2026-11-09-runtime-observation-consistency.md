# Runtime observation consistency implementation plan

> **For agentic workers:** Use dispatching-parallel-agents for the nonoverlapping storage, observation and frontend domains; root integrates the common inventory interpretation and verifies the deployment.

**Goal:** Persist fresh Runtime facts in SQL and make inventory, admission and reclamation distinguish known occupancy, confirmed emptiness and unresolved divergence. Missing database bindings must never imply that a Runtime slot is empty.

**Architecture:** Retain instance bindings as allocation/desired-state authority. Persist a separate sanitized observation under the current Pod UID with a monotonic observation timestamp. Reuse the elected slot auditor and existing lifecycle/fencing operations; observations alone never fabricate binding, cleanup or writer-exit authority. UI shows both the reported owner and any conflicting database owner, with unsafe actions disabled while reconciliation is unresolved.

**Constraints:** Work in the existing linked CM worktree. Do not directly edit business SQL, clear writer fences, restart protected user Runtime Pods, or delete workspaces/PVCs. Build Linux artifacts on54 in `clawmanager-system`; create no test Kubernetes Pods. Use an owned temporary containerd MySQL fixture and clean it afterwards. Preserve user files and existing native-agent rendering. Commit and push the preview branches without creating a PR, as requested for the computer handoff.

## Shared interfaces

- `models.RuntimePod.ObservationJSON *string` (`observation_json`, not exposed raw) and `ObservationAt *time.Time` (`observation_at`).
- `models.RuntimePodObservation{PodUID string, ObservedAt time.Time, Slots []RuntimeSlotObservation, Error string}`. Each slot has `ContainerName string, GatewayPort int, State string` (`free`, `occupied`, `unknown`), `InstanceID int, UserID int, GatewayID string, Generation int, InstanceState string, IdentityVerified bool, Error string` with snake-case JSON keys. No credentials or raw runtime payloads are persisted.
- Optional `repository.RuntimePodObservationStore.SaveRuntimeObservation(context.Context, int64, string, models.RuntimePodObservation) (bool,error)`. Reject stale timestamps or a changed Pod UID; no occupancy-counter or binding writes through this API.
- `models.RuntimeSlot` gains `ReconciliationState string` (`consistent`, `residual`, `conflict`, `unknown`), `ExpectedInstanceID *int`, `BindingConfirmed bool`, and `ObservationMessage string`. Runtime-sourced owner identity remains observational and never authorizes a lifecycle action.
- `models.RuntimePod` gains derived `ReconciliationState string`, `ReconciliationMessage string` and `UnknownSlots int` (`db:"-"`). Root implements shared interpretation with physical slot ports and the existing SQL binding enrichment.

## Storage and backup

- [x] Add migration105, matching backup schema and typed fields. Preserve observations across heartbeat updates and clear them if registration replaces the Pod UID.
- [x] Implement conditional observation persistence and tests for stale observations, replacement UIDs and unchanged allocation counters.
- [x] Verify backup compatibility and rebuild backup helpers if their compiled schema changes.

## Runtime observation and safe reconciliation

- [x] Extend the existing slot auditor to persist sanitized facts on successful and failed inspections. Preserve visible reported owner identity even when legacy protocol fields are insufficient for cleanup authority; never upgrade weak evidence into exit proof.
- [x] Keep strict validation and existing stopped-owner cleanup. Add regressions for legacy/malformed owners, orphaned ownership, read failures and observation persistence.
- [x] Root uses SQL observations consistently in inventory and empty-pool admission, retaining current UID/binding/claim fences. Unbound Creating rows must not indefinitely reserve unrelated physically empty pools; concrete reservations remain protected.

## Inventory and UI

- [x] Merge observed slot ownership and SQL bindings without losing either side of a conflict. Missing/stale evidence produces an explicit unresolved state instead of idle.
- [x] Update slot colors, details, summaries and action enablement to reflect reconciliation state. Show reported and expected instance IDs and the reason when they disagree.
- [x] Add tests for the live Hermes25 case (reported owner without a binding), OpenCode118, true emptiness, stale evidence and concurrent binding publication.

## Verification and delivery

- [x] Run focused red/green, unit/race, isolated MySQL, frontend checks and an independent review. Preserve any unrelated baseline failures as separately identified limitations.
- [x] Build pinned manager/backup binaries and frontend on54; inspect image changes and schema completeness.
- [x] Update the manager only and verify SQL observations match live reported identities, unknown slots are no longer shown as idle, protected Pods do not restart, and verified empty capacity can be reclaimed.
- [x] Remove the temporary container/database/credentials, verify no test Pods remain, document evidence and push `preview` without creating a PR.

## Handoff checkpoint

Implementation and delivery are complete as of 2026-11-10. See [the verified deployment record](../handoffs/2026-11-10-runtime-observation-consistency.md) for Linux/MySQL/frontend/Chrome results, the final image and live acceptance. The owned temporary container and credentials were removed. Older protocols remain explicitly unknown when they cannot provide writer-exit proof; no binding or cleanup authority is fabricated. No PR was created.
