# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  The world's first platform purpose-built for batch deployment and operations of OpenClaw at cluster scale.
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

<p align="center">
  <img src="https://img.shields.io/badge/OpenClaw-Desktop-f97316?style=flat-square&logo=linux&logoColor=white" alt="OpenClaw Desktop" />
  <img src="https://img.shields.io/badge/Webtop-Browser%20Desktop-0f766e?style=flat-square&logo=firefoxbrowser&logoColor=white" alt="Webtop" />
  <img src="https://img.shields.io/badge/Proxy-Secure%20Access-7c3aed?style=flat-square&logo=nginxproxymanager&logoColor=white" alt="Secure Proxy" />
  <img src="https://img.shields.io/badge/WebSocket-Realtime-2563eb?style=flat-square&logo=socketdotio&logoColor=white" alt="WebSocket" />
  <img src="https://img.shields.io/badge/i18n-5%20Languages-db2777?style=flat-square&logo=googletranslate&logoColor=white" alt="5 Languages" />
</p>

## 🚀 News

- [03/20/2026] **ClawManager New Release** - ClawManager is now released as a virtual desktop management platform featuring batch deployment, Webtop support, desktop portal access, runtime image settings, OpenClaw memory/preferences Markdown backup and migration, cluster resource overview, and multilingual documentation.

## 👀 Overview

ClawManager is a virtual desktop management platform built for Kubernetes. It provides a complete control plane for desktop runtime operations, user governance, and secure in-cluster access.

ClawManager combines batch deployment, instance lifecycle management, admin console capabilities, proxy-based desktop access, runtime image control, cluster resource visibility, and OpenClaw memory/preferences backup and migration into one platform.

ClawManager is designed for environments where:

- virtual desktop instances need to be created and managed for multiple users
- administrators need centralized quota, image, and instance governance
- desktop services should remain inside Kubernetes and be exposed through authenticated proxying
- operators need a unified view of instance health, cluster capacity, and runtime status

In short, ClawManager is:

- a centralized operations console for OpenClaw and Linux desktop runtimes
- a multi-user desktop management platform on Kubernetes
- a secure access layer for internal desktop services through token-authenticated proxying

## ✨ At a Glance

- Multi-tenant desktop instance management
- Batch deployment of desktop instances across users or runtime profiles
- User quota control for CPU, memory, storage, GPU, and instance count
- OpenClaw, Webtop, Ubuntu, Debian, CentOS, and custom runtime support
- Secure desktop proxy access with token generation and WebSocket forwarding
- OpenClaw memory, preferences, and Markdown configuration backup/migration
- Admin dashboards for users, instances, image cards, and cluster resources
- Multilingual UI: English, Chinese, Japanese, Korean, and German

> 🧭 ClawManager combines admin control, secure desktop access, and runtime operations in one control plane.

<p align="center">
  <img src="frontend/public/clawmanager_overview.png" alt="ClawManager Overview" width="100%" />
</p>

## 📚 Table of Contents

- [News](#news)
- [Overview](#overview)
- [ClawManager New Features](#clawmanager-new-features)
- [Key Features](#key-features)
- [Typical Workflow](#typical-workflow)
- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [Tech Stack](#tech-stack)
- [Kubernetes Prerequisites](#kubernetes-prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [License](#license)

## 🆕 ClawManager New Features

These are the major capabilities of ClawManager:

- 🖥 `webtop` runtime support for browser-based desktop access
- 📦 Batch deployment workflows for large-scale desktop provisioning
- 🚪 Desktop Portal page for switching between running instances from one place
- 🔐 Token-based instance access endpoint and reverse proxy routing
- 🔄 WebSocket forwarding for desktop sessions and status updates
- 🧠 OpenClaw memory, preferences, and Markdown configuration backup/import APIs
- 🧩 Runtime image card management for each supported instance type
- 📊 Cluster resource overview for nodes, CPU, memory, and storage
- 👨‍💼 Global admin instance management with cross-user filtering and control
- 📥 CSV-based user import with default password generation
- 🌍 Internationalized frontend with 5 languages

## 🛠 Key Features

- ⚙️ Instance lifecycle management: create, start, stop, restart, delete, inspect, and force sync
- 📦 Batch deployment support for large-scale desktop rollout
- 🧱 Supported runtime types: `openclaw`, `webtop`, `ubuntu`, `debian`, `centos`, `custom`
- 🔒 Secure desktop access through authenticated proxy endpoints
- 📡 WebSocket-based real-time status updates
- 📝 OpenClaw memory, preferences, and Markdown configuration archive backup/import
- 📏 User-level quota management for instances, CPU, memory, storage, and GPU
- 🖼 Runtime image override management from the admin panel
- 🛰 Admin dashboard for cluster resource overview and instance health
- 👥 CSV-based bulk user import and centralized quota assignment
- 🌐 Multilingual UI and role-based admin/user views

## 🔄 Typical Workflow

1. 👨‍💼 An administrator logs in and configures users, quotas, and runtime image settings.
2. 🖥 A user creates a desktop instance such as OpenClaw, Webtop, or Ubuntu.
3. ☸️ ClawManager creates the Kubernetes resources and keeps runtime status synchronized.
4. 🔐 The user accesses the desktop through the portal or token-based proxy endpoint.
5. 📊 Administrators monitor instance health and cluster resources from the admin dashboard.

## 🏗 Architecture

```text
Browser
  -> ClawManager Frontend (React + Vite)
  -> ClawManager Backend (Go + Gin)
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

### High-Level Design

- Frontend: React 19 + TypeScript + Tailwind CSS
- Backend: Go + Gin + upper/db + MySQL
- Runtime: Kubernetes
- Access layer: authenticated reverse proxy with WebSocket forwarding
- Data layer: MySQL for business data, PVC for persistent instance storage

## 🗂 Project Structure

```text
ClawManager/
├── backend/            # Go backend API
├── frontend/           # React frontend
├── deployments/        # Container and Kubernetes deployment files
├── dev_docs/           # Design and implementation documents
├── scripts/            # Helper scripts
├── TASK_BREAKDOWN.md   # Detailed task breakdown
└── dev_progress.md     # Development progress log
```

## 💻 Tech Stack

### Backend

- Go 1.21+
- Gin
- upper/db
- MySQL 8.0+
- JWT authentication

### Frontend

- React 19
- TypeScript 5.9
- Vite 7
- Tailwind CSS 4
- React Router

### Infrastructure

- Kubernetes
- Docker
- Nginx

## ☸️ Kubernetes Prerequisites

ClawManager is Kubernetes-first. Managed nodes must join a Kubernetes cluster before ClawManager can schedule instances, inspect resources, or provide unified operations.

Before installing ClawManager, prepare a working Kubernetes environment and verify that `kubectl` can access it:

```bash
kubectl get nodes
```

### Linux Setup Examples

Using `k3s`:

```bash
curl -sfL https://get.k3s.io | sh -
sudo kubectl get nodes
```

Using `microk8s`:

```bash
sudo snap install microk8s --classic
sudo microk8s status --wait-ready
sudo microk8s kubectl get nodes
```

### Basic Kubernetes Commands

```bash
kubectl get nodes
kubectl get pods -A
kubectl get pvc -A
kubectl cluster-info
```

### Minimum Recommendation

- 1 Kubernetes node
- 4 CPU
- 8 GB RAM
- 20+ GB available disk

If you plan to run multiple desktop instances simultaneously, allocate more CPU, memory, and storage.

## 📦 Installation

Before installation, make sure:

- Kubernetes is available
- `kubectl get nodes` works

Deploy with the bundled Kubernetes manifest:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## ⚡ Quick Start

### Default Accounts

- Default admin account: `admin / admin123`
- Default password for imported admin users: `admin123`
- Default password for imported regular users: `user123`

### First Login

1. 👨‍💼 Log in as admin.
2. 👥 Create or import users and assign quotas.
3. 🧩 Optionally configure runtime image cards in system settings.
4. 🖥 Log in as a user and create an instance.
5. 🔗 Access the desktop through Portal View or Desktop Access.

## ⚙️ Configuration

ClawManager follows a clear security model:

- instance services use Kubernetes internal networking
- desktop access goes through the authenticated ClawManager backend proxy
- backend is best deployed inside the cluster
- runtime images can be managed centrally through system settings
- managed nodes should all belong to the Kubernetes cluster

Common backend environment variables:

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

Frontend development mode proxies `/api` to the backend through Vite.

### CSV Import Template

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

Notes:

- `Email` is optional
- `Max GPU Count (optional)` is optional
- all other columns are required
- quota values should match your cluster capacity planning

## 📄 License

This project is licensed under the MIT License.

## ❤️ Open Source

Issues and pull requests are welcome, including improvements to features, docs, and tests.
