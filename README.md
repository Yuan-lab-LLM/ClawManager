# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  A Kubernetes-first control plane for managing OpenClaw and Linux desktop runtimes at team and cluster scale.
</p>

<p align="center">
  <strong>Languages:</strong>
  English |
  <a href="./README.zh-CN.md">中文</a> |
  <a href="./README.ja.md">日本語</a> |
  <a href="./README.ko.md">한국어</a> |
  <a href="./README.de.md">Deutsch</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Virtual%20Desktop%20Platform-e25544?style=for-the-badge" alt="ClawManager Platform" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

## News

- `2026-03-26`: AI Gateway documentation and overview were refreshed, including model governance, audit and trace, cost accounting, and risk control. See [AI Gateway](#ai-gateway).

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager Admin" width="32%" />
  <img src="./docs/main/portal.png" alt="ClawManager Portal" width="32%" />
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="32%" />
</p>

## What It Is

ClawManager helps teams deploy, operate, and access desktop runtimes on Kubernetes from one place.

It is built for environments where you need to:

- create desktop instances for multiple users
- control quotas, runtime images, and lifecycle centrally
- keep desktop services inside the cluster
- give users secure browser access without exposing pods directly

## Why Users Pick It

- One admin panel for users, quotas, instances, and runtime images
- OpenClaw support with import/export for memory and preferences
- Secure desktop access through the platform instead of direct pod exposure
- AI Gateway governance for controlled model access, audit trails, cost analysis, and risk controls
- Kubernetes-native deployment and operations flow
- Works for both admin-managed rollout and self-service instance creation


## Quick Start

### Prerequisites

- A working Kubernetes cluster
- `kubectl get nodes` works

### Deploy

Apply the bundled manifest:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## Build From Source

If you want to run or package ClawManager from source instead of using the bundled Kubernetes manifest:

### Frontend

```bash
cd frontend
npm install
npm run build
```

### Backend

```bash
cd backend
go mod tidy
go build -o bin/clawreef cmd/server/main.go
```

### Docker Image

Build the full application image from the repository root:

```bash
docker build -t clawmanager:latest .
```

### Default Accounts

- Default admin account: `admin / admin123`
- Default password for imported admin users: `admin123`
- Default password for imported regular users: `user123`

### First Use

1. Log in as admin.
2. Create or import users and assign quotas.
3. Review or update runtime image cards in system settings.
4. Log in as a user and create an instance.
5. Access the desktop through Portal View or Desktop Access.

## Main Capabilities

- Instance lifecycle management: create, start, stop, restart, delete, inspect, and sync
- Runtime types: `openclaw`, `webtop`, `ubuntu`, `debian`, `centos`, `custom`
- Runtime image card management from the admin panel
- User quota control for CPU, memory, storage, GPU, and instance count
- Cluster resource overview for nodes, CPU, memory, and storage
- Token-based desktop access with WebSocket forwarding
- AI Gateway for model management, traceable audit logs, cost accounting, and risk control
- CSV-based bulk user import
- Multilingual interface

## AI Gateway

AI Gateway is the governance plane for model access inside ClawManager. It gives OpenClaw instances a single OpenAI-compatible entry point while adding policy, audit, and cost controls on top of upstream providers.

- Model management for regular and secure models, provider onboarding, activation, endpoint configuration, and pricing policy
- End-to-end audit and trace records for requests, responses, routing decisions, and risk hits
- Built-in cost accounting with token tracking and estimated usage analysis
- Risk control with configurable rules and automated actions such as `block` and `route_secure_model`

For screenshots, the full feature breakdown, and the model selection and routing flow, see [docs/aigateway.md](./docs/aigateway.md).

## Product Flow

1. An admin defines users, quotas, and runtime image policies.
2. A user creates an OpenClaw or Linux desktop instance.
3. ClawManager creates and tracks the Kubernetes resources.
4. The user accesses the desktop through the platform.
5. Admins monitor health and capacity from the dashboard.

## Architecture

```text
Browser
  -> ClawManager Frontend
  -> ClawManager Backend
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

## Configuration Notes

- Instance services stay on Kubernetes internal networking
- Desktop access goes through the authenticated backend proxy
- Runtime images can be overridden from system settings
- Backend deployment is best kept inside the cluster

Common backend environment variables:

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

### CSV Import Template

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

Notes:

- `Email` is optional
- `Max GPU Count (optional)` is optional
- all other columns are required


## License

This project is licensed under the MIT License.

## Open Source

Issues and pull requests are welcome.
