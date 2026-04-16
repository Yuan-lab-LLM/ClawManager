# ClawManager — Project Stable Facts (app_spec)

**Project**: ClawManager
**Workspace**: clawmanager
**Repo**: /Users/eduardogan/Desktop/GHJProject/ClawManager
**Upstream**: https://github.com/Yuan-lab-LLM/ClawManager
**Owner**: eduardogan
**Last Updated**: 2026-04-14

---

## Product Goals

ClawManager is a Kubernetes-first control plane that lets teams deploy, manage, and access OpenClaw desktop runtimes (Linux virtual desktops) at scale. The platform provides:

1. A central admin portal for provisioning desktop instances, managing user quotas, and configuring runtime images
2. A user portal for accessing personal desktop instances via browser
3. An AI Gateway layer for routing LLM requests with cost tracking, audit, and risk control
4. Full lifecycle management: create → start → stop → backup → restore → delete instances

**Primary use case (local testing)**: Run K3S on a single Mac machine, deploy ClawManager, and use it to schedule and manage 2+ OpenClaw instances on the same machine.

---

## User Workflows (Testable Scenarios)

1. **Admin provisions an instance**
   - Admin logs in → navigates to Instances → creates a new instance for a user → K3S schedules a Pod → user can access their desktop via browser

2. **User accesses their desktop**
   - User logs in → sees their instance → clicks "Connect" → opens desktop in browser (Webtop)

3. **Admin monitors resource usage**
   - Admin views dashboard → sees CPU/memory/disk usage per instance → can stop/restart instances

4. **K3S local multi-instance test**
   - K3S running on local Mac (via k3d) → deploy ClawManager → create 2 OpenClaw instances → both accessible at the same time

---

## Out of Scope (V1 Local Testing)

- Multi-cluster federation
- GPU scheduling (may test later)
- Backup/restore to remote storage (local emptyDir only for now)
- LDAP/SSO integration
- Production TLS (self-signed acceptable for local testing)

---

## Technical Baseline

| Item | Value |
|------|-------|
| Backend language | Go 1.21+ |
| Backend module | `clawreef` (see `backend/go.mod`) |
| Frontend | React 19 + TypeScript + Vite |
| Database | MySQL 8.4.x |
| Object storage | MinIO (K3S) / EmptyDir fallback |
| K8S client | `k8s.io/client-go` |
| Deployment target | K3S via `deployments/k3s/clawmanager.yaml` |
| Local K3S tool | k3d (`brew install k3d`) |
| Container registry | `ghcr.io/yuan-lab-llm/clawmanager:latest` |
| NodePort (HTTPS) | 30443 |

---

## Command Contract

> These commands must stay in sync with `longterm/workspace/init.sh`

```bash
# Backend: build and test
cd backend && go build ./...
cd backend && go test ./...

# Frontend: install and run
cd frontend && npm install
cd frontend && npm run dev

# K3S local cluster (k3d)
k3d cluster create clawmanager --port "30443:30443@loadbalancer"
k3d cluster delete clawmanager

# Deploy ClawManager to K3S
kubectl apply -f deployments/k3s/clawmanager.yaml

# Check deployment
kubectl get pods -n clawmanager-system
kubectl get svc -n clawmanager-system

# Access
# https://localhost:30443
# Default admin: admin / admin123 (first login, must change)
```

---

## Quality and Non-Functional Requirements

- **Startup time**: ClawManager app Pod ready within 60 seconds of deployment
- **Instance creation**: K3S Pod scheduled within 30 seconds of API request
- **Security**: No secrets in source code; all credentials via K8S Secrets
- **Observability**: All admin actions logged in `audit_logs` table

---

## Migration Constraints (from upstream)

- `deployments/k3s/clawmanager.yaml` is the primary deployment artifact — do not restructure without updating docs
- Database schema is managed via numbered SQL migration files in ConfigMap — do not alter existing files, only append new ones
- `backend/internal/services/k8s/` is the K8S abstraction layer — all K3S interactions go through here

---

## Definition of Done (per feature)

- [ ] spec.md written and reviewed
- [ ] plan.md written with technical approach
- [ ] tasks.md created and all tasks checked off
- [ ] Tests run and pass (`go test ./...` or `npm test`)
- [ ] Deployment verified (`kubectl apply --dry-run=client`)
- [ ] `feature_list.json` updated with `passes: true` and evidence
- [ ] `claude-progress.txt` updated
