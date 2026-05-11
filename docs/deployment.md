# Deployment Guide

ClawManager is packaged as a Kubernetes-first platform. This guide is the operational entry point for deploying the control plane, locating the relevant manifests in the repository, and understanding which services are expected to come up in a working environment.

## Deployment Paths

Choose the deployment path that matches your environment:

- Standard Kubernetes: [`deployments/k8s/clawmanager.yaml`](../deployments/k8s/clawmanager.yaml)
- K3s or lightweight clusters: [`deployments/k3s/clawmanager.yaml`](../deployments/k3s/clawmanager.yaml)
- End-to-end first-use walkthrough: [User Guide](./use_guide_en.md)

## What Gets Deployed

- ClawManager frontend and backend
- MySQL for application state
- MinIO for object storage-backed features
- `skill-scanner` for skill analysis workflows
- Kubernetes Services used for portal, gateway, and supporting traffic paths

## Repository Entry Points

- Kubernetes manifest: [`deployments/k8s/clawmanager.yaml`](../deployments/k8s/clawmanager.yaml)
- K3s manifest: [`deployments/k3s/clawmanager.yaml`](../deployments/k3s/clawmanager.yaml)
- Container startup script: [`deployments/container/start.sh`](../deployments/container/start.sh)
- Nginx config: [`deployments/nginx/nginx.conf`](../deployments/nginx/nginx.conf)

## Deployment Workflow

1. Choose the deployment path: standard Kubernetes or K3s/lightweight.
2. Prepare the cluster, storage strategy, and image source strategy for that environment.
3. Review the bundled manifest and adjust secrets, images, storage classes, and ingress exposure for your environment.
4. Deploy the platform components into the cluster.
5. Wait for the core services to become ready.
6. Validate frontend access, AI Gateway management pages, Security Center connectivity, and runtime creation flows.

## Operational Notes

- ClawManager is designed around in-cluster services and platform-mediated access rather than direct pod exposure.
- Resource Management features depend on object storage and `skill-scanner` being available.
- Production environments should review images, credentials, TLS, persistence, and networking policies before rollout.

## Related Guides

- [Admin and User Guide](./admin-user-guide.md)
- [Agent Control Plane Guide](./agent-control-plane.md)
- [AI Gateway Guide](./aigateway.md)
- [Security / Skill Scanner Guide](./security-skill-scanner.md)
- [Resource Management Guide](./resource-management.md)
- [Developer Guide](./developer-guide.md)
