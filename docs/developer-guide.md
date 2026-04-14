# Developer Guide

This guide is the codebase orientation page for contributors. ClawManager spans frontend, backend, deployment assets, and supporting product documentation, so the fastest way to get productive is to start from the subsystem you want to change.

## Repository Map

- `frontend/`: React application, admin surfaces, portal views, and product UI
- `backend/`: Go services, handlers, repositories, migrations, and platform logic
- `deployments/`: Kubernetes manifests, container bootstrap, and nginx config
- `docs/`: product-facing guides and screenshots

## Suggested Entry Points

- AI governance work: [`docs/aigateway.md`](./aigateway.md)
- runtime orchestration work: [Agent Control Plane Guide](./agent-control-plane.md)
- reusable resource workflows: [Resource Management Guide](./resource-management.md)
- security scanning work: [Security / Skill Scanner Guide](./security-skill-scanner.md)

## Common Areas of Change

- frontend pages and navigation for product surfaces such as AI Gateway, Security Center, and Config Center
- backend services for agents, commands, resources, and scanning
- migrations and repository logic when new control-plane state is introduced
- deployment manifests when platform components or images change

## Related Guides

- [Deployment Guide](./deployment.md)
- [Admin and User Guide](./admin-user-guide.md)
- [AI Gateway Guide](./aigateway.md)
