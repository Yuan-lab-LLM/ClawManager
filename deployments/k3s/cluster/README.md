# ClawManager K3s Cluster Manifest

This directory contains the all-in-one K3s manifest for a test or small cluster
deployment:

- `clawmanager.yaml`: Longhorn v1.12.0 plus ClawManager workloads.
- `uninstall.yaml`: Longhorn v1.12.0 uninstall job.
- StorageClasses `longhorn` and `longhorn-rwx`.

The manifest includes Longhorn. Follow the install and uninstall order below;
do not delete the whole manifest as the first uninstall step.

## Install

### 1. Enter This Directory

```sh
cd deployments/k3s/cluster
```

Check that `kubectl` points to the target cluster:

```sh
kubectl config current-context
kubectl get nodes -o wide
```

Expected result: every node that may run Longhorn or ClawManager workloads is
`Ready`.

If a node is not `Ready`, fix the Kubernetes node first. Longhorn will not be
stable on an unhealthy cluster.

### 2. Check For Old Installation State

```sh
kubectl get ns longhorn-system clawmanager-system clawmanager-user-1 --ignore-not-found
kubectl get sc longhorn longhorn-rwx --ignore-not-found
kubectl get crd | grep longhorn.io || true
```

Expected result for a fresh install: no `longhorn-system`,
`clawmanager-system`, `clawmanager-user-1`, Longhorn StorageClass, or Longhorn
CRD remains.

If old Longhorn or ClawManager resources exist, run the uninstall flow in this
README before installing again.

### 3. Check Image Availability

The manifest uses the images listed in `clawmanager.yaml`. Make sure every node
can pull from the referenced registry before installing:

```sh
grep -n 'image:' clawmanager.yaml | head -n 40
```

If this is an offline or private-registry deployment, load or push the images to
the registry used by the manifest before continuing.

If pods later show `ImagePullBackOff`, locate the failing image with:

```sh
kubectl get pods -A | grep ImagePullBackOff || true
kubectl describe pod <pod-name> -n <namespace>
```

Then verify registry access from the node that runs the pod.

### 4. Prepare Every Node

Longhorn uses iSCSI for block volumes and NFS for RWX volumes. Install the
required packages on every node that may run workloads.

Ubuntu or Debian:

```sh
apt-get update
apt-get install -y open-iscsi nfs-common cryptsetup dmsetup
systemctl enable --now iscsid || systemctl enable --now open-iscsi
```

RHEL, CentOS, Rocky, or AlmaLinux:

```sh
yum install -y iscsi-initiator-utils nfs-utils cryptsetup device-mapper
systemctl enable --now iscsid
```

Check on every node:

```sh
command -v iscsiadm
command -v mount.nfs
command -v mount.nfs4
systemctl is-active iscsid || systemctl is-active open-iscsi
```

If `mount.nfs` or `mount.nfs4` is missing, pods that mount
`clawmanager-workspaces` can stay in `ContainerCreating` with an event like:

```text
bad option; you might need a /sbin/mount.<type> helper program
```

### 5. Check `multipathd`

Longhorn volumes can be blocked by `multipathd` if Longhorn devices are not
blacklisted.

Check on every node:

```sh
systemctl is-active multipathd || true
multipath -t 2>/dev/null | grep -F 'devnode "^sd[a-z0-9]+"' || true
```

Expected result: either `multipathd` is not active, or the blacklist contains:

```text
devnode "^sd[a-z0-9]+"
```

If `multipathd` is active and the blacklist is missing, either disable it if
the node does not need multipath:

```sh
systemctl disable --now multipathd
```

Or add the Longhorn blacklist to `/etc/multipath.conf`, then restart
`multipathd`:

```conf
blacklist {
    devnode "^sd[a-z0-9]+"
}
```

```sh
systemctl restart multipathd
multipath -t | grep -F 'devnode "^sd[a-z0-9]+"'
```

Common symptom when this check is skipped:

```text
Waiting for volume share to be available
is apparently in use by the system
```

### 6. Check Pod Network Health

Longhorn manager, CSI, and share-manager pods require cross-node Pod networking.
First check for duplicate Pod IPs:

```sh
kubectl get pods -A \
  -o custom-columns=IP:.status.podIP,NS:.metadata.namespace,NAME:.metadata.name,NODE:.spec.nodeName \
  --no-headers \
| awk '$1 != "<none>" { pods[$1] = pods[$1] "\n" $0; count[$1]++ } END { for (ip in count) if (count[ip] > 1) print pods[ip] }'
```

Expected result: no output.

If this command prints any IP, fix the CNI before installing. For Calico, common
checks are:

```sh
kubectl -n calico-system get pods -o wide
kubectl get blockaffinities.crd.projectcalico.org 2>/dev/null || true
kubectl get ippools.crd.projectcalico.org 2>/dev/null || true
```

If a single DaemonSet or Deployment pod has a stale or wrong IP, deleting that
pod and letting its controller recreate it is often enough:

```sh
kubectl -n <namespace> delete pod <pod-name>
```

Do not continue until the duplicate IP check has no output.

### 7. Match Longhorn Replica Count To Storage Nodes

The manifest defaults Longhorn StorageClasses to `numberOfReplicas: "3"`.
Use this default only when at least three Longhorn storage nodes are available.

Check the current value:

```sh
grep -n 'numberOfReplicas:' clawmanager.yaml
```

For a two-node test cluster, change both StorageClass values to `2` before
installing:

```sh
sed -i.bak 's/numberOfReplicas: "3"/numberOfReplicas: "2"/g' clawmanager.yaml
grep -n 'numberOfReplicas:' clawmanager.yaml
```

If this is not adjusted, volumes may still work but will stay `degraded` with
replica scheduling errors.

### 8. Apply The Manifest

```sh
kubectl apply -f clawmanager.yaml
```

### 9. Check Longhorn

Wait for Longhorn manager and CSI components:

```sh
kubectl -n longhorn-system rollout status daemonset/longhorn-manager --timeout=10m
kubectl -n longhorn-system rollout status deployment/longhorn-driver-deployer --timeout=10m
kubectl -n longhorn-system rollout status daemonset/longhorn-csi-plugin --timeout=10m
kubectl -n longhorn-system get deploy,ds,pod -o wide
kubectl get csidriver driver.longhorn.io
kubectl get sc longhorn longhorn-rwx
```

Expected result:

- `longhorn-manager` is ready on every node.
- `longhorn-driver-deployer` is `1/1`.
- `longhorn-csi-plugin` is ready on every node.
- `driver.longhorn.io`, `longhorn`, and `longhorn-rwx` exist.

If `longhorn-driver-deployer` is `CrashLoopBackOff`, inspect it:

```sh
kubectl -n longhorn-system logs -l app=longhorn-driver-deployer --all-containers --tail=200
kubectl -n longhorn-system describe pod -l app=longhorn-driver-deployer
```

If the log contains `connect: connection refused` for
`http://longhorn-backend:9500/v1`, check duplicate Pod IPs and cross-node Pod
networking again. If the log mentions `MountPropagation`, also check that the
Longhorn manager pod has `mountPropagation: Bidirectional` on
`/var/lib/longhorn/`.

### 10. Check ClawManager

```sh
kubectl -n clawmanager-system get pvc
kubectl -n clawmanager-system rollout status deployment/clawmanager-app --timeout=15m
kubectl -n clawmanager-system rollout status deployment/openclaw-runtime --timeout=15m
kubectl -n clawmanager-system rollout status deployment/hermes-runtime --timeout=15m
kubectl -n clawmanager-system rollout status deployment/opencode-runtime --timeout=15m
kubectl -n clawmanager-system get deploy,pod,pvc -o wide
```

Expected result:

- All PVCs are `Bound`.
- `clawmanager-app`, `openclaw-runtime`, `hermes-runtime`, and `opencode-runtime` are available.
- MySQL, Redis, MinIO, and skill scanner pods are running.

If PVCs stay `Pending`, check CSI and StorageClass:

```sh
kubectl get sc longhorn longhorn-rwx
kubectl -n longhorn-system get pods | grep csi
kubectl -n clawmanager-system describe pvc <pvc-name>
```

If pods stay `ContainerCreating`, check recent events:

```sh
kubectl -n clawmanager-system get events --sort-by=.lastTimestamp | tail -n 80
```

### 11. Check Longhorn Volumes

```sh
kubectl -n longhorn-system get volumes.longhorn.io \
  -o custom-columns=NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness,REPLICAS:.spec.numberOfReplicas,SHARESTATE:.status.shareState,ENDPOINT:.status.shareEndpoint
```

Expected result: volumes are `healthy`. The workspace RWX volume should also
show `SHARESTATE` as `running` and an NFS endpoint.

If volumes are `degraded`, check whether `numberOfReplicas` is greater than the
number of available storage nodes.

If the RWX volume is stuck at `shareState: starting`, inspect the share-manager:

```sh
kubectl -n longhorn-system get pods | grep share-manager
kubectl -n longhorn-system logs <share-manager-pod> --tail=200
kubectl -n longhorn-system describe pod <share-manager-pod>
```

Common causes are missing NFS client packages, `multipathd` interference, or
cross-node Pod network problems.

## Uninstall

Use this order for a disposable test deployment. The Longhorn uninstall job must
run before deleting the all-in-one manifest.

### 1. Delete ClawManager Workloads First

```sh
kubectl delete namespace clawmanager-user-1 --ignore-not-found
kubectl delete namespace clawmanager-system --ignore-not-found
```

Check:

```sh
kubectl get ns clawmanager-user-1 clawmanager-system --ignore-not-found
kubectl get pvc -A | grep clawmanager || true
```

If a namespace stays `Terminating`, inspect the remaining resources:

```sh
kubectl get all,pvc -n clawmanager-system 2>/dev/null || true
kubectl get events -n clawmanager-system --sort-by=.lastTimestamp 2>/dev/null | tail -n 80 || true
```

### 2. Enable Longhorn Deletion

```sh
kubectl -n longhorn-system patch settings.longhorn.io deleting-confirmation-flag \
  --type=merge \
  -p '{"value":"true"}'
```

Check:

```sh
kubectl -n longhorn-system get settings.longhorn.io deleting-confirmation-flag -o yaml
```

Expected result: `value: "true"`.

### 3. Run The Longhorn Uninstall Job

```sh
kubectl create -f uninstall.yaml
kubectl wait --for=condition=complete job/longhorn-uninstall -n longhorn-system --timeout=10m
```

Check:

```sh
kubectl -n longhorn-system logs job/longhorn-uninstall --tail=200
```

If the job already exists from a previous attempt:

```sh
kubectl delete -f uninstall.yaml --ignore-not-found
kubectl create -f uninstall.yaml
kubectl wait --for=condition=complete job/longhorn-uninstall -n longhorn-system --timeout=10m
```

### 4. Delete The All-In-One Manifest

```sh
kubectl delete -f clawmanager.yaml --ignore-not-found
kubectl delete -f uninstall.yaml --ignore-not-found
```

Final check:

```sh
kubectl get ns longhorn-system clawmanager-system clawmanager-user-1 --ignore-not-found
kubectl get sc longhorn longhorn-rwx --ignore-not-found
kubectl get crd | grep longhorn.io || true
```

Expected result: no Longhorn or ClawManager resources remain.

## Recovery For A Stuck Uninstall

If `kubectl delete -f clawmanager.yaml` was run first and the uninstall is now
stuck, check whether the Longhorn webhook service is gone:

```sh
kubectl -n longhorn-system get svc longhorn-admission-webhook --ignore-not-found
kubectl get validatingwebhookconfiguration longhorn-webhook-validator --ignore-not-found
kubectl get mutatingwebhookconfiguration longhorn-webhook-mutator --ignore-not-found
```

If the webhook service is gone but webhook configurations remain, remove the
stale webhook registrations:

```sh
kubectl delete validatingwebhookconfiguration longhorn-webhook-validator --ignore-not-found
kubectl delete mutatingwebhookconfiguration longhorn-webhook-mutator --ignore-not-found
```

If Longhorn CRDs still cannot be deleted after that, use this only when you
intend to wipe Longhorn state from the cluster:

```sh
NAMESPACE=longhorn-system
for crd in $(kubectl get crd -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | grep longhorn.io); do
  kubectl -n "${NAMESPACE}" get "${crd}" -o yaml | sed 's/- longhorn.io//g' | kubectl apply -f -
  kubectl -n "${NAMESPACE}" delete "${crd}" --all --ignore-not-found
  kubectl delete "crd/${crd}" --ignore-not-found
done
```
