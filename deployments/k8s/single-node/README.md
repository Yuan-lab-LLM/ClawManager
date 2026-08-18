# ClawManager Kubernetes Single-Node Manifest

This directory contains the all-in-one Kubernetes manifest for a single-node
ClawManager deployment:

- `clawmanager.yaml`: ClawManager workloads plus static HostPath PVs.
- No Longhorn dependency.
- No dynamic StorageClass dependency.

Use this profile only when ClawManager workloads and user runtimes can run on
one storage node. For multi-node clusters, use `deployments/k8s/cluster` instead.

## Storage Model

The manifest uses static PV/PVC binding:

- StorageClass name: `manual`
- System HostPath root: `/data/clawmanager/system`
- Runtime HostPath root: `/data/clawreef`
- Storage node label: `clawmanager.io/storage-node=true`

The `manual` value is only a matching name between bundled PVs and PVCs. It does
not require a Kubernetes `StorageClass` object or dynamic provisioner.

The bundled PVs use `persistentVolumeReclaimPolicy: Retain`, so deleting the
manifest does not erase HostPath data.

## Install

### 1. Enter This Directory

```sh
cd deployments/k8s/single-node
```

Check that `kubectl` points to the target cluster:

```sh
kubectl config current-context
kubectl get nodes -o wide
```

Expected result: the target node is `Ready`.

### 2. Choose And Label One Storage Node

Label exactly one node before installing:

```sh
kubectl label node <node> clawmanager.io/storage-node=true --overwrite
kubectl get nodes -l clawmanager.io/storage-node=true
```

Expected result: only one node is listed.

If more than one node has this label, remove the label from the extra nodes:

```sh
kubectl label node <node> clawmanager.io/storage-node-
```

### 3. Check For Old Installation State

```sh
kubectl get ns clawmanager-system clawmanager-user-1 --ignore-not-found
kubectl get pv | grep clawmanager || true
kubectl get pvc -A | grep clawmanager || true
```

Expected result for a fresh install: no old ClawManager namespace, PV, or PVC
remains.

If old resources exist, run the uninstall flow in this README before installing
again.

### 4. Check HostPath Directories

The kubelet can create the HostPath directories automatically, but checking the
target path first makes permission and disk issues easier to diagnose:

```sh
df -h /data || true
ls -ld /data || true
```

If `/data` is not the right disk for this node, edit these paths in
`clawmanager.yaml` before applying it:

- `/data/clawmanager/system/mysql`
- `/data/clawmanager/system/minio`
- `/data/clawmanager/system/redis`
- `/data/clawmanager/system/workspaces`
- `K8S_PV_HOST_PATH_PREFIX=/data/clawreef`

### 5. Check Image Availability

The manifest uses the images listed in `clawmanager.yaml`. Make sure the node
can pull from the referenced registry before installing:

```sh
grep -n 'image:' clawmanager.yaml | head -n 40
```

If this is an offline or private-registry deployment, load or push the images to
the registry used by the manifest before continuing.

### 6. Apply The Manifest

```sh
kubectl apply -f clawmanager.yaml
```

### 7. Check ClawManager

```sh
kubectl -n clawmanager-system get pvc
kubectl -n clawmanager-system rollout status deployment/clawmanager-app --timeout=15m
kubectl -n clawmanager-system rollout status deployment/openclaw-runtime --timeout=15m
kubectl -n clawmanager-system rollout status deployment/hermes-runtime --timeout=15m
kubectl -n clawmanager-system rollout status deployment/opencode-runtime --timeout=15m
kubectl -n clawmanager-system get deploy,pod,pvc -o wide
```

Expected result:

- `mysql-data`, `redis-data`, `minio-data`, and `clawmanager-workspaces` are `Bound`.
- `clawmanager-app`, `openclaw-runtime`, `hermes-runtime`, and `opencode-runtime` are available.
- MySQL, Redis, MinIO, and skill scanner pods are running.

If PVCs stay `Pending`, check the static PV binding:

```sh
kubectl get pv | grep clawmanager
kubectl -n clawmanager-system describe pvc <pvc-name>
```

If pods stay `Pending` with node affinity errors, check the storage node label:

```sh
kubectl get nodes -l clawmanager.io/storage-node=true
kubectl describe pod <pod-name> -n clawmanager-system
```

If pods stay `ContainerCreating`, check recent events:

```sh
kubectl -n clawmanager-system get events --sort-by=.lastTimestamp | tail -n 80
```

## Uninstall

Use this order for a disposable single-node test deployment.

### 1. Delete ClawManager Resources

```sh
kubectl delete -f clawmanager.yaml --ignore-not-found
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
| Pod stays `Pending` | Missing or wrong `clawmanager.io/storage-node=true` label | `kubectl get nodes -l clawmanager.io/storage-node=true` |
| Pod is `ImagePullBackOff` | Node cannot pull an image | `kubectl describe pod <pod-name> -n <namespace>` |
| Pod is `ContainerCreating` | HostPath mount or permission issue | `kubectl -n clawmanager-system get events --sort-by=.lastTimestamp` |
| Runtime creation fails | Runtime HostPath root is unavailable | Check `K8S_PV_HOST_PATH_PREFIX` and disk space on the labeled node |
