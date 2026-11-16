# Team runtime M0–M1: compatibility boundary

The Team engine keeps its existing dispatch order, serial member execution,
assignment revisions, dependency/review checks and acknowledged finalization.
Workers retain the existing minimal tools; they do not supply execution tokens,
new mandatory IDs or artifact hashes.

## M0 inventory, 2026-11-03

The team branch contains Runtime upstream `8a0dddb` and App upstream `66c5c4de`.
The 32543 test deployment still uses the legacy process-pool path, not
`container-slots-v1`. Its two nodes run Kubernetes 1.31.14, containerd 2.2.x and
Linux 6.8. Runtime workspaces use direct NFS mounts. The existing manual workspace
PVC is not Longhorn; available Longhorn StorageClasses do not prove that this
claim satisfies the multi-node slot storage contract.

Real probes subsequently ran in a separate tenant-owned test namespace, with no
business mounts or credentials. Both master-pinned probes were rejected because
that node had reached its 110-Pod capacity. On the other node, the unchanged
shared sandbox Linux gate failed with `bwrap: setting up uid map: Permission
denied` under the requested Pod user-namespace/security context. The independent
Longhorn RWX test claim bound successfully; a cross-node result is not available
because the master-side probe could not start. A bound claim is not cross-node
acceptance. These are explicit M0 blockers for slot activation, not reasons to
weaken the isolation policy or alter cluster-wide settings during M1.

Run the read-only inventory against explicitly selected deployments:

```sh
python3 deployments/k8s/lite-slots/preflight.py --namespace YOUR_NAMESPACE \
  --deployment openclaw-runtime --deployment opencode-runtime
```

This prints no credentials or environment values. It does not create probes,
change scheduling, or claim that sandbox execution has passed. Candidate-image
execution gates, real RWX verification, stopped-writer data copy and slot
activation belong to the later migration stages. Do not enable slots merely
because this inventory completed. Do not weaken the upstream storage check.

## M1 release boundary

Fix dedicated OpenCode access configuration, explicit completion validation and
read-only current-workflow context while preserving the existing serving mode.
The legacy shared task files remain dispatch/recovery snapshots, not a second
current ledger. Redis workflow snapshots are advisory projections of SQL facts;
missing snapshots cannot imply completion or authorize new assignments.

No default forced Leader interruption, second model execution, automatic
assignment replay, data migration or container-slot activation is part of M1.
Before a Runtime replacement, preserve the actual image digest and require the
existing confirmed-stop/data-safe lifecycle. A busy instance remains held; do
not manually rewrite bindings or task states to make a rollout pass.

M1 rollback keeps the same workspace and schema. Retain the previous App and
Runtime digests, deployment configuration and instance identities. Never restore
an old filesystem snapshot over subsequent user writes.
