# NFS Multi-slot Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Support NFS-backed multi-slot Runtime pools with independent instance localhost, narrow filesystem grants, preserved data and positively confirmed container teardown.

**Architecture:** Opt in through `RUNTIME_SANDBOX_PROFILE=nfs-rootless-v1`; the default existing profile is unchanged. Ordinary Pods mount NFS, business sandboxes retain user/mount/seccomp isolation and reuse the slot container PID boundary. A trusted helper manages private networks inside a mapped user namespace; broker socket descriptors cross to the credential-holding supervisor, and container termination messages bind reset proof to actual old container identity.

**Tech Stack:** Existing Go modules, Linux namespaces/seccomp, bubblewrap, existing netguard/netbroker/netrelay, Kubernetes Pod status and ConfigMap CAS. No new third-party Go dependency.

## Global Constraints

- Specification: `docs/superpowers/specs/2026-11-05-nfs-multislot-isolation-design.md`.
- NFS and multiple slot containers per Pod remain mandatory; same-instance commands share their execution network, different instances have independent localhost.
- Business processes have no platform management credentials or control descriptors, no host SYS_ADMIN/NET_ADMIN, no access to other instance directories.
- Namespace helpers may have capabilities only inside their owned user namespace. Reserved management UID/GID is 10001; instance IDs retain stable distinct UID/GID; Team groups are explicit supplemental grants.
- No root_squash disable, recursive ownership repair of existing NFS data, generic NFS root grant, or implicit fallback to weaker isolation.
- Unknown teardown, stale identity, missing termination proof, NFS faults or uncertain source writer keep the slot quarantined.
- All builds and tests run on `10.130.81.54` in `/root/cm-nfs-multislot-20261105`; macOS only edits/transfers source and reads evidence.
- Continue both existing `mount` branches; preserve unrelated untracked files. Do not switch existing business pools during implementation validation.
- Native profiles, upgrade/file helpers, manual stop and automatic idle stop must retain their existing behavior and authorization, or visibly reject an unsupported unsafe operation; a rejection is not completion of that compatibility requirement.

## Task 1: Profile wiring and storage admission

**Owner:** Parent.

**Files:**
- Modify: `ClawManager/backend/internal/config/config.go`.
- Modify: `ClawManager/backend/internal/services/k8s/runtime_deployment_service.go`, `runtime_slots.go`, `runtime_slot_bootstrap.go` and matching profile tests.
- Modify: `ClawManager/backend/internal/services/runtime_scheduler.go` and identity tests.
- Modify: `AgentsRuntime/clawmanager-agent/gateway/types.go`, `agent/config.go` and matching config tests.
- Create: `AgentsRuntime/clawmanager-agent/sandboxprofile/profile.go`.

**Interfaces:**

```go
// New fields in gateway.Config, RuntimePoolConfig and RuntimeDeploymentSpec.
SandboxProfile string
// Shared runtime constants.
const NFSRootlessV1 = "nfs-rootless-v1"
const ManagementUID = 10001
const ManagementGID = 10001
```

- [x] Add a failing config test that accepts the new profile only with slots, strong control credentials, network isolation and no Pod user namespace; reject unknown profiles and preserve existing profile constraints.
- [x] Run focused tests remotely and record the expected failure before implementation.
- [x] Wire environment/YAML through deployment and agent config. Set Pod/Deployment label `clawmanager.io/sandbox-profile=nfs-rootless-v1`, no Pod user namespace and no shared PID namespace. Use masked proc, File termination-message policy at `/dev/termination-log`, and slot env `RUNTIME_SLOT_TERMINATION_MESSAGE_PATH=/dev/termination-log`.
- [x] Grant only trusted supervisor capabilities needed by the reviewed launcher; never retain the legacy host SYS_ADMIN/NET_ADMIN additions for the new profile. Resolve management UID execution and TLS/state-file access together with Task 3.
- [x] Validate live bound writable direct NFS RWX or recognized NFS CSI RWX for the new profile, including exact PVC/PV/claim/provider identity. Preserve legacy Longhorn/local rules and require real idmap-compatible storage for the legacy userns path.
- [x] For the new profile, assign primary GID equal to stable instance UID-derived GID and keep Team GID supplemental.
- [x] Run focused config/deployment/storage/identity regressions and record reviewed work in the final per-repository feature commit.

## Task 2: Rootless network ownership and broker sockets

**Owner:** Network worker.

**Files:** `AgentsRuntime/clawmanager-agent/netguard/`, `netrelay/`, `slots/network_runtime.go`, new `slots/rootless_network*.go`, and the fixed app helper-command dispatch as necessary. Broker changes must preserve current authorization behavior.

**Interfaces:**
- Consume `gateway.Config.SandboxProfile` and the shared profile constants.
- Preserve existing `networkSession` assignment identity, wrap/release and helper-lifetime semantics.
- Transfer only explicitly typed user/net namespace and pre-bound broker/DNS socket FDs over a private Unix socketpair; validate count, type, address and namespace ownership before readiness.
- Use existing `net.FileListener` and `net.FilePacketConn` in the outer supervisor so accepted sockets retain isolated network source identity while upstream dials remain outside.

- [x] Add failing tests for profile dispatch, malformed or extra descriptors, missing identity, wrong broker endpoints and namespace cleanup uncertainty.
- [x] Run remote RED. Verify the actual reduced-capability Pod launcher under node AppArmor policy before selecting Go clone versus bubblewrap-mediated namespace creation.
- [x] Implement the fixed helper command with an explicit UID/GID mapping: root→10001, business UID/GID→stable outer identity, relay 65534→65534 and authorized Team GIDs only. Map namespace root to nonroot outer UID; do not map outer UID0.
- [x] Create helper-local private state/tmpfs rather than weakening netguard's existing arbitrary-path ownership checks.
- [x] Reuse existing veth, firewall, transparent relay, DNS, model and Team broker behavior. Forward bound sockets by SCM_RIGHTS, not generic outbound access from business to Pod network.
- [x] Pin namespace identity and keep helper FD/credential access separate from business. A helper sharing an outer UID must have independently enforced FD/memory protection; business never inherits control FDs.
- [x] Run focused network regressions plus real ordinary-Pod NFS tests: independent IPv4/IPv6 localhost, controlled ingress/egress, DNS, proxy denial, direct NFS denial, helper joining, WebSocket and cleanup. Record reviewed work in the final per-repository feature commit.

## Task 3: NFS credential-scoped storage

**Owner:** Storage worker, with parent configuring the trusted supervisor identity.

**Files:** `AgentsRuntime/clawmanager-agent/workspaceio/`, `gateway/workspace.go`, `gateway/team_workspace.go`, `slots/path_security.go`, `slots/persistent_tmp*.go`; native storage/config/upgrade wrappers in `openclaw/lite`, `hermes/lite`, `opencode/lite`, `deepseek-harness/lite`; bounded native helper dispatch and operations.

**Interfaces:**
- Consume the profile field/constants and stable primary instance GID.
- Keep management-owned parents private to management; accept only explicit root/management ownership policies, never arbitrary nonroot owners.
- Prepare new workspace leaves as management UID + instance GID `0770`, tmp as `01770`; preserve correctly owned legacy instance data and its existing permissions.
- Native/file/upgrade operations run under instance credentials with exact instance/Team/operation capsule grants. Do not serialize the full management Config or pass its secrets to the helper.

- [x] Write failing tests for A/B grants, private 0600 data, wrong-owner ancestors, symlink/replacement/nested-mount attempts, tmp and metadata preservation.
- [x] Run remote RED, then implement descriptor-bound management and instance grant validation.
- [x] Ensure NFS writer locks/JSON execute as management UID, either by the reviewed supervisor identity or narrowly scoped management helper; root-squashed UID0 is not a management identity substitute.
- [x] Implement native prep/config/file/upgrade execution through an instance-scoped helper. Its synthetic ancestors can be read without exposing real NFS management roots. Upgrade capsules are granted only for the authorized operation.
- [x] Replace recursive ownership repair only in the explicit NFS profile with caller/ownership validation and preservation; do not change legacy behavior. Team preparation validates authorized top-level grants without rewriting existing member data.
- [x] Verify all four native profiles, private files, existing data, Team, upgrade and A→B→A tmp persistence on real root-squash NFS. A root-owned 0755 export must fail with setup guidance instead of permission broadening. Record reviewed work in the final per-repository feature commit.

## Task 4: Container lifecycle and termination proof

**Owner:** Lifecycle worker.

**Files:** `AgentsRuntime/clawmanager-agent/sandbox/{sandbox,process}*.go`, `slots/{worker,pool,run,command,process,assignment,writer}*.go`, scoped gateway lifecycle preparation methods; `ClawManager/backend/internal/services/runtime_slot_agent_client.go`, new `runtime_slot_termination*.go`, and `services/k8s/runtime_assignment_stop.go`.

**Interfaces:**

```go
// Explicit shared-container mode; existing nested-PID behavior remains default.
ContainerLifecycle bool // sandbox.Spec
// Never substitute primary process exit for complete execution teardown.
var ErrContainerResetRequired error
```

- [x] Run existing new RED tests: `go test ./sandbox -run '^TestContainer' -count=1` and backend `go test ./internal/services -run '^TestRuntimeSlotTerminationRequiresMatchingActualContainer$' -count=1`.
- [x] Build NFS profile commands using readonly existing proc and slot PID namespace; replace `--disable-userns` with the mandatory existing seccomp namespace denial policy. Preserve exact startup identity and listener attribution without falsely requiring nested PID1.
- [x] Persist a random worker epoch with the assignment before business launch. Persist a backend stop intent via existing ConfigMap CAS, including operation, epoch, slot, instance, generation, request digest and Pod UID.
- [x] Prepare reset by fencing creates/auxiliary work, graceful business shutdown and external resource cleanup; never mark writer/book stopped while the old container lives.
- [x] Write a bounded explicit termination message to writable-layer `/dev/termination-log`, respond pending, then exit through tini/PID1. The message is intent/cleanup evidence; actual PID teardown comes only from the matching Kubernetes Terminated object.
- [x] Match the exact termination message and actual ContainerID in `State.Terminated` or `LastTerminationState.Terminated`; do not bind launch identity to possibly stale Running status. Missing/truncated/wrong-epoch proof stays blocked.
- [x] New worker remains quarantined until authenticated reconciliation verifies the old writer record under the NFS lock, records stopped state and returns an identity-matching receipt. Do not clear emptyDir running records automatically.
- [x] Implement manual, migration, idle graceful and crash-recovery paths together. Same-instance successor launch also requires the old execution boundary to finish; auxiliary main-command completion never releases the execution writer.
- [x] Run tests for stale status/messages, delayed requests, missing proof, NFS failure, failed native idle commitment, double-fork writers and exact slot restart. Record reviewed work in the final per-repository feature commit.

## Task 5: Integration, review and isolated acceptance

**Owner:** Parent with independent review workers.

- [x] Sync tracked and task files into isolated Linux source trees, preserve raw RED/GREEN outputs per task, and run each focused check before broad regression.
- [x] Review each task's diff for specification compliance and correctness, resolve Important/Critical findings and verify changes after fixes.
- [x] Build all four Runtime images and backend on the test node, retaining existing business pools and their images.
- [ ] Complete the entire physical failure matrix on target deployment infrastructure. The dedicated single-node NFS namespace, private inode/file/credential tests, actual container teardown and continuous peer traffic passed; dual-node/node-fencing/production-NFS reconnect and real upstream service gates remain external, as recorded in the acceptance report.
- [x] Run dual-node A→B→A against the target NFS configuration when a second test node is available. If it is not, record that exact external limitation; never call single-node testing a cross-node pass.
- [x] Update deployment instructions and acceptance report with actual image digests, results and unresolved external prerequisites. Do not publish an NFS compatibility claim before the required tests pass.
- [x] Run final code review and verification. Report actual implementation/validation status, branch commits and any remaining external limit without switching business pools implicitly.

## Execution result

Feature work and independent task/final reviews completed on both `mount` branches. Code commits are grouped by repository after final integration review; the earlier per-task commit wording was superseded by this integration-safe commit strategy. Unrelated untracked documents are preserved.

Final corrected images were built only on Linux54: [image digests](../../nfs-multislot-images-20261105.json). Actual final OpenCode A/B reset/reuse passed65.219s with330 fresh authenticated B probes and no failures; Hermes/DeepSeek exact-container lifecycle passed; OpenClaw six actual CM methods including real migration/restore passed368.463s with1255 source readiness probes and no failures. Private workspace/tmp/capsule and browser leaves reject persisted foreign inodes while preserving same-instance hardlinks and explicit Team sharing. The host-credential directory-FD audit passed the real reduced-capability native bootstrap and management0700 case.

[Acceptance record and exact external gates](../../nfs-multislot-acceptance.md), [deployment and historical-browser prerequisites](../../../deployments/k8s/lite-slots/nfs-rootless.md). This is single-node amd64 acceptance, not blanket production or cross-node compatibility approval. Historical browser Deployment cutover remains refused without reliable external fencing; browser retirement keeps terminal name tombstones and has a documented finite history/object budget. Existing disabled full snapshots are not counted as passed.
