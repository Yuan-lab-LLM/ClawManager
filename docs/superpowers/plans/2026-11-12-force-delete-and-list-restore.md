# Force deletion and persistent list restore implementation plan

> **For agentic workers:** Use subagent-driven-development with isolated file ownership and review before deployment.

**Goal:** Confirmed instance deletion terminates that instance and its tasks immediately, including a previously accepted graceful stop. A hidden instance list always has a visible restore entry.

**Approved behavior:** The user explicitly selected force deletion, regardless of running tasks. No second force-confirmation workflow is required. Ordinary Stop and idle sleep retain their existing semantics. Creation and management remain the first two list actions.

**Architecture:** Persist deletion before asynchronous cleanup; fence stale reports and cancel superseded graceful work. A dedicated force-delete request uses the existing slot execution identity and a `force-delete` operation kind. Only that explicit operation may supersede an unfinished normal stop. The Agent immediately kills the pinned target execution, then performs resource cleanup and publishes the real container termination commitment. Other slots retain their processes, assignments and reservations. CM deletes database/workspace records and releases slots only after matching real stop/unmount proof.

**Constraints:** Source editing/preparation locally; all application tests, frontend builds, binary/image builds and deployment run on10.130.81.54. No local app servers or images. Preserve ownership, Team and upgrade guards; generation/PodUID/slot/worker-epoch/digest checks; do not synthesize completion evidence or kill the shared Pod.

## Tasks

- [x] UI: reproduce missing restore after collapse/reload, provide a Split-owned persistent text/icon restore control, including content errors and fullscreen; test Lite/Pro, running/stopped/error,1440/1280/768/390 and focus.
- [x] CM deletion admission: persist `deleting` independently from busy task/stop cleanup; let explicit deletion take priority over old graceful waiting; queue single and batch identically. Cancel old stop attempts when durable deletion supersedes them. Preserve safety/ownership guards.
- [x] CM runtime protocol: implement `ForceDeleteGateway`; only this method requests `kind=force-delete,graceful=false`. Supersede the same stop operation/execution with a CAS; normal retries never upgrade graceful. Continue actual stop-proof and unmount reconciliation. Resolve the race where the previous normal stop already published a matching kubelet termination: accept its actual same-operation proof rather than falsely asserting forced or graceful completion.
- [x] Runtime Agent: support explicit force-delete, cancel a same-execution graceful prepare, kill that primary immediately without waiting20seconds, keep admission fencing/resources until real cleanup/termination, prevent stale in-flight stop receipt writes. Cover unknown/stale identities, maintenance, replay, parallel requests and unaffected sibling slots.
- [x] Verify red/green and related suites on54; review both repos and the cross-repo protocol.
- [x] Build on54; deploy only required CM/runtime artifacts. Verify real force deletion using an isolated disposable instance and the already-authorized stuck46 target, preserving other slots. Verify collapsed-list restore on deployed assets and browser-origin Service/actual supported native pages where available. Record concrete evidence and remaining limitations.
