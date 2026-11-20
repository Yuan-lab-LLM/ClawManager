# ClawManager K3s Single-Node Install

Use the [single-node manifest](../../../deployments/k3s/single-node/clawmanager.yaml)
on a cluster with one Ready Linux node. Static HostPath volumes hold MySQL,
Redis, MinIO, the workspace and audit data. The installer creates a
`kubernetes.io/no-provisioner` StorageClass if the workspace class is absent.
The PVs retain their files when Kubernetes resources are removed.

## Prepare the node and configuration

- Use Kubernetes 1.33 or newer, a CRI with Pod user namespace support and local
  filesystems that support ID-mapped mounts. Set kubelet
  `userNamespaces.idsPerPod: 1048576` to cover the Runtime IDs that start at
  200000. Review [container slot prerequisites](../../../tools/k8s/lite-slots/README.md)
  and [Kubernetes user namespace support](https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/).
- From the repository root, confirm the context and label the storage node.

  ```sh
  kubectl config current-context
  kubectl get nodes -o wide
  kubectl label node <node> clawmanager.io/storage-node=true --overwrite
  ```

- Review the manifest's `clawmanager-secrets`, Service ports and image references.
  Set deployment-specific credentials, including matching MinIO root and access
  credentials when using the bundled administrator account. Use immutable CM
  and compatible Runtime image references for release or offline installs.
- Use a new namespace and empty HostPath directories. The standard paths are
  `/data/clawmanager/system` for the bundled services and `/data/clawreef` for
  per-user storage. For an isolated copy, update namespaced resources, bundled
  service DNS names, cluster-scoped PV and RBAC names and references, HostPaths,
  and NodePorts together. Leave prior namespaces and retained volumes available
  to the installations that own them.

## Start a fresh installation

```sh
python3 tools/k8s/install_fresh.py \
  --manifest deployments/k3s/single-node/clawmanager.yaml \
  --images-file tools/k8s/single-node/ghcr-images.json \
  --credentials-dir /secure/clawmanager-first-install
```

Choose a new directory under protected persistent storage for the audit key and
history restore credentials. The installer checks the selected node, refuses
overlapping or nonempty HostPaths, prepares the two immutable credential Secrets
and starts the bundled database with CM at zero replicas. It creates the
isolated history account and temporarily enables
`log_bin_trust_function_creators` while CM applies its migrations. It restores
the original value when the CM rollout finishes or fails. Keep the private
credential directory for backup and recovery.

CM creates all four Runtime pools with one container slot each by default.
`--slots` sets the per-pool capacity within the gateway port range. The source
manifest's static Runtime Deployments provide image references to the installer.
They remain available for existing process-pool deployments. The
[GHCR image profile](../../../tools/k8s/single-node/ghcr-images.json) pins
MySQL 8.4.13, Redis 7.4.12, MinIO, Skill Scanner and the four `v2026.11.20`
Runtime images by digest. CI and release tests use the same profile. Update
its Runtime digests when introducing a new version to the install profile.
Repeat `--image DEPLOYMENT=IMAGE` to select a specific CM image or override a
profile entry.

For an existing installation, use the [deployment and upgrade guidance](../../deployment.md).
A move from a process pool to slots requires the
[documented Runtime migration](../../../tools/k8s/lite-slots/README.md#从旧进程池迁移)
and confirmation that old instances have stopped.

## Browser origins

To use the same host for the manager, OpenCode and DeepSeek Harness, pass both
`--browser-origin https://<node-host>:30443` and `--browser-ports 32100-32115`
to the installation command. Pick free NodePorts. The installer creates
`clawmanager-browser-origins` with all ports targeting `8444`. For the
per-instance DNS option, configure the
[public origin templates](../../deployment.md#opencode-lite-public-origin-strategy).

## Verify the installation

```sh
kubectl -n clawmanager-system get deployments,pods,pvc -o wide
kubectl -n clawmanager-system get endpointslice -l kubernetes.io/service-name=clawmanager-browser-origins
```

Expect all five static PVCs to be Bound and CM plus all four Runtime pools to
be Ready. Configure a model and create one Lite instance of each kind. Stop
and start each instance, confirm an uploaded workspace file survives, then
delete the test instance and verify it disappears. For browser origins, check
that every configured NodePort targets `8444`, the EndpointSlice is ready, and
the OpenCode and DeepSeek Harness pages render in Chrome. See
[browser origin verification](../../browser-origin-ports.md).

If the HostPath probe or server-side validation fails, fix the reported cause
and rerun the same command after the installer removes that attempt's
namespace and any staged credentials. Newly created empty HostPath
directories can be reused. If an earlier run left a namespace behind, verify
that it contains only preflight resources before removing it, and move any
staged credentials to a protected backup directory before retrying.

Once the installer starts writing application resources, it preserves the
namespace and credentials for recovery. Inspect the Deployments, Pods, PVCs,
and events and verify the MySQL setting before resolving the failed step. Use the
[release checks](../../deployment.md#release-verification) before promoting a
candidate image.

## Uninstall

Use this order for a disposable single-node test deployment.

### 1. Delete ClawManager Resources

```sh
kubectl delete -f deployments/k3s/single-node/clawmanager.yaml --ignore-not-found
kubectl delete namespace clawmanager-user-1 --ignore-not-found
kubectl delete namespace clawmanager-system --ignore-not-found
```

Check:

```sh
kubectl get ns clawmanager-system clawmanager-user-1 --ignore-not-found
kubectl get pv | grep clawmanager || true
kubectl get pvc -A | grep clawmanager || true
```

The PVs may remain because they use `Retain`. This is expected.

### 2. Remove Retained PVs When Data Is No Longer Needed

Only run this after confirming the data can be discarded:

```sh
kubectl delete pv \
  clawmanager-mysql-pv \
  clawmanager-minio-pv \
  clawmanager-redis-pv \
  clawmanager-workspaces-pv \
  clawmanager-observability-pv \
  --ignore-not-found
```

### 3. Remove HostPath Data When Data Is No Longer Needed

Run this on the labeled storage node only after confirming the data can be
discarded:

```sh
rm -rf /data/clawmanager/system /data/clawreef
```

## Common Issues

| Symptom | Likely Cause | Check Or Fix |
| :--- | :--- | :--- |
| PVC stays `Pending` | Static PV and PVC did not bind | `kubectl get pv`, then `kubectl describe pvc -n clawmanager-system <pvc-name>` |
| Pod stays `Pending` | Storage label or node capacity | Check the node label and Pod scheduling events |
| Pod is `ImagePullBackOff` | Node cannot pull an image | `kubectl describe pod <pod-name> -n <namespace>` |
| Pod is `ContainerCreating` | HostPath mount or permission issue | `kubectl -n clawmanager-system get events --sort-by=.lastTimestamp` |
| Runtime creation fails | Runtime HostPath root is unavailable | Check `K8S_PV_HOST_PATH_PREFIX` and disk space on the labeled node |
