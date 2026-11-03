# Local kind elastic-memory test environment

Run commands from the directory containing `ClawManager` and `AgentsRuntime`.
Use the dedicated Docker context and kubeconfig throughout. This environment
does not change the default Docker context, kubeconfig, or production workloads.

The tested host has 16GiB physical memory. Its dedicated Colima VM has 6 CPUs,
10GiB RAM and a 60GiB disk. Four complete 16GiB runtime Pods cannot run here.
The three profiles deliberately have different purposes:

| Profile | Pod CPU request / limit | Pod memory request / limit | Eight slots, each memory request / limit |
|---|---|---|---|
| Formal deployment | 4 / 4 | 16Gi / 16Gi | 1Gi / 4Gi |
| Local native applications | 1 / 1 | 1536Mi / 2Gi | 128Mi / 1536Mi |
| Disposable allocator test | 25m / 1 | 512Mi / 512Mi | 32Mi / 128Mi |

Each native runtime Pod also has a coordinator with 512Mi request and limit,
inside the Pod budget. The allocator coordinator uses 16Mi. The allocator test
scales the formal **memory** values by 32; it does not simulate native runtime
memory needs or full-scale CPU performance. Requests reserve scheduling capacity;
the tested Kubernetes 1.35 MemoryQoS configuration also writes `memory.min`.
That is reclaim protection, not preallocated memory or immunity from Pod OOM.

## Create the environment

Requires Colima, Docker CLI, kind 0.31+, kubectl **1.35**, and Python with
`pyyaml`, `bcrypt` and `requests`. The pinned node image is Kubernetes 1.35.0,
containerd 2.2.0, with cgroup v2. The registry at `172.16.1.12:5010` must be reachable.

```sh
colima start cm-kind --cpu 6 --memory 10 --disk 60 --activate=false --mount none --ssh-config=false
mkdir -p .deploy/kind-elastic-20261019
DOCKER_CONTEXT=colima-cm-kind kind create cluster \
  --config ClawManager/deployments/k8s/kind-elastic/kind.yaml \
  --kubeconfig .deploy/kind-elastic-20261019/kubeconfig
python3 ClawManager/deployments/k8s/kind-elastic/prepare_vm.py
python3 ClawManager/deployments/k8s/kind-elastic/prepare_node.py \
  --kubeconfig .deploy/kind-elastic-20261019/kubeconfig
```

Pass `--kubectl /path/to/kubectl-1.35` if the default kubectl is older. A VM-reachable
proxy may be passed to `prepare_node.py --proxy http://192.168.5.2:7890`; host
loopback proxy addresses cannot be used inside the VM/node.

`prepare_node.py` verifies the kind identity before changing the node, configures
the private HTTP registry, labels the storage node, and installs two boot services.
They provide a read-only sysfs mount for nested namespace creation and an 8GiB
ext4 loop filesystem for workspace PVC ID mappings. Run it **before** deploying
data; it refuses to hide an existing nonempty workspace directory. Data remains
inside the dedicated VM, including across normal Colima stops, until the cluster
or VM is deleted. The workspace filesystem is development-sized.

`prepare_vm.py` installs a bubblewrap-specific AppArmor userns rule in this VM.
It leaves Ubuntu's global userns restriction enabled. Kubernetes Pod user namespaces
map 1,048,576 IDs per Pod to cover the existing `200000 + instance_id` identities.
Business processes still have private PID/mount/user namespaces, no capabilities,
`no_new_privs`, seccomp, and only their authorized workspace mounts.

MemoryQoS is alpha in 1.35. `memoryThrottlingFactor: 1.0` avoids the observed
early `memory.high` stall on the VM's Linux 6.8 kernel while retaining request
protection and hard `memory.max` limits. Do not infer behavior of newer Kubernetes
versions from this version-pinned test.

## Deploy remotely built images

Build new OCI images on node12, then supply the recorded digest catalogs. The
renderer never reads production Secrets. It generates fresh local credentials,
preserves them across rerenders, and writes private files with mode 0600.

```sh
python3 ClawManager/deployments/k8s/kind-elastic/render.py \
  --images .deploy/kind-elastic-20261019/images.json \
  --backend-image .deploy/kind-elastic-20261019/backend-image.json \
  --out .deploy/kind-elastic-20261019
kubectl --kubeconfig .deploy/kind-elastic-20261019/kubeconfig \
  apply -f .deploy/kind-elastic-20261019/deployment.private.json
```

Visit **https://localhost:18443**. The local TLS certificate is self-signed.
Login details are in `.deploy/kind-elastic-20261019/admin-login.private.txt`.
The four `kind-*` instances are smoke-test fixtures. Their model entry uses a
dummy key and a closed loopback endpoint; configure a real provider in Settings
before testing model conversations. No inference or paid API calls are tested.
OpenCode and DeepSeek Harness use dedicated `*.127-0-0-1.nip.io:18443` origins;
the browser must resolve those names to loopback.

This single-node environment uses local PVCs. Multi-node deployments must use
Longhorn; cross-node migration is not covered by this one-node kind environment.

## Acceptance

Create the four local fixtures once, then run verification:

```sh
python3 ClawManager/deployments/k8s/kind-elastic/seed_smoke.py \
  --out .deploy/kind-elastic-20261019
python3 ClawManager/deployments/k8s/kind-elastic/verify_memory.py \
  --kubeconfig .deploy/kind-elastic-20261019/kubeconfig \
  --image "$(python3 -c 'import json; print(json.load(open(".deploy/kind-elastic-20261019/images.json"))["hermes"]["image"])')" \
  --out .deploy/kind-elastic-20261019
python3 ClawManager/deployments/k8s/kind-elastic/verify_native.py \
  --kubeconfig .deploy/kind-elastic-20261019/kubeconfig \
  --out .deploy/kind-elastic-20261019
```

The memory probe creates temporary Pods without user PVCs and removes them even
after failure. It verifies borrowing, child-only OOM, and aggregate parent OOM via
actual cgroup values/events. The native probe verifies all four existing fixtures,
eight-slot capacity, native pages/assets, workspace access and process isolation;
it also checks OpenCode health and DeepSeek Harness read-only RPC.

Reports under `.deploy/kind-elastic-20261019` include `memory-acceptance.json`,
`formal-admission.json` (API dry-run only), and `native-acceptance.json`.
`backend-formal-patch.yaml` records the intended production budget; do not apply
it to this constrained VM. Changing an existing pool's image/resources requires
the coordinated drain/rollout workflow; backend bootstrap rejects silent drift.

References: [Pod-level resources](https://v1-35.docs.kubernetes.io/docs/tasks/configure-pod-container/assign-pod-level-resources/),
[kind nested user namespaces](https://github.com/kubernetes-sigs/kind/issues/3436),
[Ubuntu userns policy](https://discourse.ubuntu.com/t/understanding-apparmor-user-namespace-restriction/58007).

Current acceptance results: [RESULTS.md](RESULTS.md). For constrained local pool updates, confirmed-stop instances first, then use `maxSurge: 0` and `maxUnavailable: 1`; the VM has insufficient spare resources for an extra complete runtime Pod.
