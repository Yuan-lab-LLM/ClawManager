# Runtime cold capacity and historical pool wake prevention

User resource policy: retain each Runtime Pod request of 4 CPU and current memory/slot settings. The operator supplies additional cluster CPU. Never lower requests to hide Kubernetes Pending capacity constraints.

## Trigger and behavior

Batch creation previously woke a historical sleeping Hermes/DeepSeek Harness pool before scaling a full proven serving pool. The historical pool could report all slots Unknown because its image/native contract differs. New unpinned requests now expand the proven serving pool. Existing instances retain their recorded source deployment and image. A genuinely cold wake requires the approved image and compatible frozen native/security/resource/storage contract. Unknown slot proofs remain unavailable.

OpenClaw had no approved live default after a completed orphan retirement. Existing default lineage pointed at the retired controller and correctly refused to revive it. The existing rollout API can now establish a fresh successor using the exact approved immutable image and completed retirement receipt. It verifies the original ReplicaSet template, PVC/PV storage identity, source UID and retirement fence, absence of writers, native validation/activation, and target UID. Only this new successor becomes the default. Retired source fences are retained. Failed operations clean up only their owned probe and successor.

Protected writer inspection now recognizes the native maintenance/upgrading ledger and its stopped tombstone. It continues to reject arbitrary slots, maintenance/running, malformed ownership, and symlinks; records belonging to a different Pod are retained unchanged. This removes an erroneous recovery blocker from legitimate old maintenance records.

## Operator recovery

Use the existing Pod orphan-recovery preflight and eviction workflow for the two accidentally woken unbound historical pools. Never edit protected ledgers or repair business rows directly. Never evict a newly occupied Pod based on an earlier observation; recheck source Pod UID and bindings before submitting.

For a fully retired canonical OpenClaw default, POST `/api/v1/admin/runtime-rollouts` with `runtime_type: openclaw`, its exact approved `target_image_ref`, and `auto_rollback: true`. The server derives cold mode and evidence; callers cannot supply template or authorization proof. Retry never-started failed instances only after rollout verification completes. A Pending Pod whose scheduling event says Insufficient cpu needs cluster resources; additional Pending replicas do not create capacity.

## Verification

Real local MariaDB regression runs cover cold admission/commit/failure isolation, committed controller UID scaling, pinned source/image, full-pool burst behavior, approved cold fallback, conflicting contracts, and existing idle pool behavior. Full K8s tests and protected-ledger race tests pass. Local Darwin service tests use the pre-existing compile overlay for an unrelated Linux-only NFS test.

Deployment and live verification receipts will be appended after staging acceptance.
