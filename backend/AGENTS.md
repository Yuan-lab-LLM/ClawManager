# backend/AGENTS.md - Backend Project Rules

> This file applies to `backend/`. Root `../AGENTS.md` and `../.specify/memory/constitution.md` still apply.

## Scope

The backend is the Go REST API, K8S control-plane integration, AI Gateway, audit, and data-access layer.

Keep product-facing brand/localization work out of backend source unless a feature spec explicitly approves a backend API or data-contract change. Product-facing GTManager wording should normally be handled by frontend i18n or approved docs.

## Structure

- `cmd/server/` - primary API server entrypoint
- `cmd/initdb/` and `cmd/fixpassword/` - operational commands
- `internal/handlers/` - HTTP handlers and route-facing request/response logic
- `internal/services/` - business services and K8S/runtime orchestration
- `internal/repository/` - persistence access
- `internal/db/` - database connection and migration loading
- `internal/services/k8s/` - Kubernetes abstraction layer

## Commands

Run from `backend/` unless noted:

```bash
go build ./...
go test ./...
go vet ./...
```

For repository-level deployment validation, run from the repo root:

```bash
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml
kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml
```

These checks are prerequisite evidence only. A feature is not accepted or `passes:true` until E2E evidence exists according to the root constitution.

## Coding Rules

- Use `gofmt` for all Go changes.
- Keep package names lowercase.
- Handle errors explicitly; do not ignore returned errors.
- Preserve existing API prefixes and health endpoints unless an approved spec says otherwise.
- Do not hardcode secrets. Use Kubernetes Secrets or configuration.
- Do not rename protected technical identifiers such as `clawreef`, `clawmanager-system`, `/api/v1`, `/healthz`, database names, table names, or OpenClaw runtime identifiers for branding work.

## Database and Deployment Boundaries

- Database schema changes must use numbered SQL migrations under `internal/db/migrations/` and remain compatible with deployment ConfigMap expectations.
- Do not edit deployment manifests from backend-only work unless the approved feature plan includes deployment ownership and matching docs updates.
- Do not treat build, unit, integration, or dry-run success as final acceptance without E2E evidence.
