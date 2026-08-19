[← Back to README](../README.md)

# Deployment Guide

ClawManager is packaged as a Kubernetes-first platform. This guide is the operational entry point for deploying the control plane, locating the relevant manifests in the repository, and understanding which services are expected to come up in a working environment.

## Deployment Paths

Choose the deployment path that matches your environment:

- K3s single-node HostPath: [`deployments/k3s/single-node/clawmanager.yaml`](../deployments/k3s/single-node/clawmanager.yaml)
- K3s multi-node CSI/RWX: [`deployments/k3s/cluster/clawmanager.yaml`](../deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes single-node HostPath: [`deployments/k8s/single-node/clawmanager.yaml`](../deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes multi-node CSI/RWX: [`deployments/k8s/cluster/clawmanager.yaml`](../deployments/k8s/cluster/clawmanager.yaml)
- End-to-end first-use walkthrough: [User Guide](./use_guide_en.md)

The cluster profile is validated with Longhorn as the example CSI implementation. It uses `longhorn` for RWO control-plane and instance volumes, and `longhorn-rwx` for the RWX workspace volume. These names are not a project requirement; replace them with compatible StorageClasses for your storage provider.

## What Gets Deployed

- ClawManager frontend and backend
- MySQL for application state
- MinIO for object storage-backed features
- `skill-scanner` for skill analysis workflows
- Team Redis and shared workspace storage services
- Shared Lite Runtime pools for OpenClaw, Hermes, OpenCode, and DeepSeek Harness
- Kubernetes Services used for portal, gateway, and supporting traffic paths

## Repository Entry Points

- Kubernetes single-node manifest: [`deployments/k8s/single-node/clawmanager.yaml`](../deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes cluster manifest: [`deployments/k8s/cluster/clawmanager.yaml`](../deployments/k8s/cluster/clawmanager.yaml)
- K3s single-node manifest: [`deployments/k3s/single-node/clawmanager.yaml`](../deployments/k3s/single-node/clawmanager.yaml)
- K3s cluster manifest: [`deployments/k3s/cluster/clawmanager.yaml`](../deployments/k3s/cluster/clawmanager.yaml)
- Container startup script: [`deployments/container/start.sh`](../deployments/container/start.sh)
- Nginx config: [`deployments/nginx/nginx.conf`](../deployments/nginx/nginx.conf)

## Deployment Workflow

1. Choose the Kubernetes distribution: `k3s` or `k8s`.
2. Choose the storage profile: `single-node` or `cluster`.
3. Check the storage prerequisites for that profile.
4. Review the bundled manifest and adjust secrets, images, StorageClass names, and ingress exposure for your environment.
5. Deploy the platform components into the cluster.
6. Wait for the core services to become ready.
7. Validate frontend access, AI Gateway management pages, Security Protection connectivity, and OpenClaw/Hermes/OpenCode/DeepSeek Harness runtime creation flows.

Single-node example:

```bash
kubectl get nodes
kubectl label node <node> clawmanager.io/storage-node=true --overwrite
kubectl apply -f deployments/k8s/single-node/clawmanager.yaml
kubectl get pvc -n clawmanager-system
kubectl get pods -n clawmanager-system
```

Cluster example:

```bash
kubectl get storageclass longhorn longhorn-rwx
kubectl apply -f deployments/k8s/cluster/clawmanager.yaml
kubectl get pvc -n clawmanager-system
kubectl get pods -n clawmanager-system
```

## DeepSeek Harness Runtime

DeepSeek Harness is available in both runtime modes:

- Lite runs `dsh web` as an isolated process in the shared
  `deepseek-harness-runtime` pool. Its persistent home is
  `<workspace>/home/.dsh`.
- Pro runs a dedicated Webtop Deployment on port `3001`; `dsh web` listens on
  loopback port `3080` inside the desktop and Chromium opens it automatically.
  Its persistent home is `/config/.dsh`.

The runtime image source is owned by the
[AgentsRuntime repository](https://github.com/Iamlovingit/AgentsRuntime/tree/main/deepseek-harness)
under `deepseek-harness/`, not by ClawManager. It pins `@deepseek-ai/dsh` to an
explicit release candidate and publishes the `deepseek-harness` and
`deepseek-harness-lite` images. Both modes receive the ClawManager-managed
OpenAI-compatible base URL, instance credential, and model list. The bundled
Cordis patch exposes only that managed provider and disables the direct public
DeepSeek web-search provider.

The DeepSeek Harness browser uses root-relative HTTP and websocket routes. Lite
therefore requires a dedicated origin template:

```text
CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=https://deepseek-harness-{instance_id}.172-16-1-12.nip.io:39443/
```

For an offline deployment, use the same wildcard DNS and certificate strategy
described above, for example:

```text
CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=https://deepseek-harness-{instance_id}.clawmanager.test:39443/
```

The `{instance_id}` placeholder is required. The bundled Nginx configuration
authenticates the short-lived bootstrap token, promotes it to an origin-scoped
cookie, and forwards HTTP and websocket traffic through the control plane to
the assigned Lite process.

## Storage Profiles

### Single-Node

The `single-node` profile is the official HostPath validation path. Label exactly one node with `clawmanager.io/storage-node=true` before installation. The manifest pins HostPath PVs through node affinity and runs `clawmanager-app` as a single replica. MySQL, Redis, MinIO, workspace, and runtime data are all backed by persistent volumes; durable data must not use `emptyDir`.

### Cluster

The `cluster` profile is the official multi-node CSI/RWX validation path. The bundled manifest uses Longhorn as an example only:

- `longhorn`: RWO MySQL, Redis, MinIO, and instance volumes
- `longhorn-rwx`: RWX workspace volume shared by ClawManager and runtime Pods

Set these environment variables in the ClawManager app deployment when replacing the storage provider:

- `CLAWMANAGER_STORAGE_PROFILE=cluster`
- `K8S_HOSTPATH_FALLBACK_ENABLED=false`
- `K8S_PVC_BIND_TIMEOUT=2m`
- `K8S_CONTROL_PLANE_STORAGE_CLASS=<rwo-storage-class>`
- `K8S_INSTANCE_STORAGE_CLASS=<rwo-storage-class>`
- `K8S_WORKSPACE_STORAGE_CLASS=<rwx-storage-class>`
- `K8S_WORKSPACE_ACCESS_MODE=ReadWriteMany`

Unsupported combinations:

- multi-node HostPath as a production or shared workspace strategy
- `local-path` or other node-local storage pretending to provide RWX across nodes
- cluster-internal Service DNS such as `workspace-store.clawmanager-system.svc.cluster.local` as an NFS server for kubelet-mounted PVs
- durable MySQL, Redis, MinIO, workspace, or object data on `emptyDir`
- cluster profile with implicit HostPath fallback

## ARM64 Deployment

The official ClawManager and Skill Scanner images are published for `linux/arm64`, but a complete installation also uses MySQL, Redis, MinIO/workspace services, and the selected OpenClaw, Hermes, OpenCode, or DeepSeek Harness Runtime images. Verify the manifest of **every pinned image** before deploying to ARM nodes; platform support does not make a custom Runtime image ARM64-compatible.

For mixed-architecture clusters, use architecture-compatible tags together with node selectors or affinity. The shared Lite profiles include OpenClaw, Hermes, OpenCode, and DeepSeek Harness pools, so validate every enabled pool image even when users initially see only one Runtime. Use SSD-backed persistent storage, sufficient memory, and reproducible tags rather than `latest`, then perform the same PVC, Runtime creation, desktop, and model acceptance checks as on amd64.

## Operational Notes

- ClawManager is designed around in-cluster services and platform-mediated access rather than direct pod exposure.
- Resource Management features depend on object storage and `skill-scanner` being available.
- The deployment profiles include first-start MySQL initialization through the `clawmanager-mysql-init` ConfigMap. Existing database volumes do not re-run those initialization scripts.
- Lite instances are processes/workspaces inside the corresponding shared Runtime Pod; they do not create one Pod per user instance.
- Runtime workspace `.openclaw` and `.hermes` archive import/export size is controlled by `CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB`. The default is `500` MiB; set the env var on the ClawManager app deployment when a larger or smaller limit is needed.
- For install issues, collect `kubectl get storageclass`, `kubectl get pvc -n clawmanager-system`, `kubectl get pods -n clawmanager-system`, `kubectl get events -n clawmanager-system --sort-by=.lastTimestamp`, and `kubectl describe pvc -n clawmanager-system <pvc-name>` output before filing an issue.
- Production environments should review images, credentials, TLS, persistence, and networking policies before rollout.

## Related Guides

- [User Manual](./use_guide_en.md)
- [AI Gateway Guide](./aigateway.md)
- [Security Protection Platform](./security-platform.md)
- [Resource Management Guide](./resource-management.md)
- [Skill Hub Guide](./skill-hub-guide_en.md)
