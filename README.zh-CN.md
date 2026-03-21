# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  全球首款面向 OpenClaw 集群批量部署与运维的平台。
</p>

<p align="center">
  <strong>语言：</strong>
  <a href="./README.md">English</a> |
  中文 |
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

- [03/20/2026] **ClawManager 全新发布** - ClawManager 正式发布，作为虚拟桌面管理平台，提供批量部署、Webtop 支持、桌面 Portal 访问、运行时镜像设置、OpenClaw 记忆/偏好 Markdown 备份迁移、集群资源总览和多语言文档。

## 👀 Overview

ClawManager 是一个面向 Kubernetes 的虚拟桌面管理平台，提供完整的桌面运行时运营平面，覆盖运行控制、用户治理和安全的集群内访问。

ClawManager 将批量部署、实例生命周期管理、管理员控制台、基于代理的桌面访问、运行时镜像控制、集群资源可视化，以及 OpenClaw 记忆/偏好备份迁移能力统一到一个平台中。

ClawManager 适用于这些环境：

- 需要为多个用户创建和管理虚拟桌面实例
- 管理员需要集中治理 quota、镜像和实例
- 桌面服务需要保留在 Kubernetes 内部，并通过鉴权代理对外访问
- 运维需要统一查看实例健康、集群容量和运行状态

简而言之，ClawManager 是：

- OpenClaw 与 Linux 桌面运行时的集中运维控制台
- Kubernetes 上的多用户桌面管理平台
- 基于令牌鉴权代理的内部桌面安全访问层

## ✨ At a Glance

- 多租户桌面实例管理
- 面向用户或运行时模板的桌面实例批量部署
- 针对 CPU、内存、存储、GPU 和实例数量的用户 quota 控制
- 支持 OpenClaw、Webtop、Ubuntu、Debian、CentOS 和自定义运行时
- 通过令牌生成和 WebSocket 转发实现安全桌面代理访问
- OpenClaw 记忆、偏好和 Markdown 配置数据的备份与迁移
- 管理员后台提供用户、实例、镜像卡片和集群资源视图
- 多语言界面：英文、中文、日文、韩文、德文

> 🧭 ClawManager 将管理员控制、安全桌面访问和运行时运维整合到一个控制平面中。

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

这些是 ClawManager 的主要能力：

- 🖥 `webtop` 运行时支持，用于浏览器内桌面访问
- 📦 支持大规模桌面实例分发的批量部署能力
- 🚪 Desktop Portal 页面，可在一个入口中切换运行中的实例
- 🔐 基于令牌的实例访问端点和反向代理路由
- 🔄 桌面会话和状态更新的 WebSocket 转发
- 🧠 OpenClaw 记忆、偏好和 Markdown 配置数据的备份/导入 API
- 🧩 面向各实例类型的运行时镜像卡片管理
- 📊 节点、CPU、内存和存储的集群资源总览
- 👨‍💼 支持跨用户筛选与控制的全局管理员实例管理
- 📥 带默认密码生成的 CSV 用户导入
- 🌍 提供 5 种语言的国际化前端

## 🛠 Key Features

- ⚙️ 实例生命周期管理：创建、启动、停止、重启、删除、查看和强制同步
- 📦 面向大规模桌面发放场景的批量部署支持
- 🧱 支持运行时类型：`openclaw`、`webtop`、`ubuntu`、`debian`、`centos`、`custom`
- 🔒 通过鉴权代理端点提供安全桌面访问
- 📡 基于 WebSocket 的实时状态更新
- 📝 OpenClaw 记忆、偏好和 Markdown 配置数据归档备份/导入
- 📏 用户级 quota 管理：实例数、CPU、内存、存储和 GPU
- 🖼 管理后台中的运行时镜像覆盖管理
- 🛰 管理员仪表盘提供集群资源总览和实例健康视图
- 👥 CSV 批量用户导入和集中 quota 分配
- 🌐 多语言 UI 与管理员/普通用户双角色视图

## 🔄 Typical Workflow

1. 👨‍💼 管理员登录并配置用户、quota 和运行时镜像设置。
2. 🖥 用户创建一个桌面实例，例如 OpenClaw、Webtop 或 Ubuntu。
3. ☸️ ClawManager 创建对应的 Kubernetes 资源并持续同步运行状态。
4. 🔐 用户通过 Portal 或基于令牌的代理端点访问桌面。
5. 📊 管理员在后台监控实例健康和集群资源。

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

- 前端：React 19 + TypeScript + Tailwind CSS
- 后端：Go + Gin + upper/db + MySQL
- 运行环境：Kubernetes
- 访问层：带 WebSocket 转发的鉴权反向代理
- 数据层：MySQL 存储业务数据，PVC 存储实例持久化数据

## 🗂 Project Structure

```text
ClawManager/
├── backend/            # Go 后端 API
├── frontend/           # React 前端
├── deployments/        # 容器和 Kubernetes 部署文件
├── dev_docs/           # 设计与实现文档
├── scripts/            # 辅助脚本
├── TASK_BREAKDOWN.md   # 详细任务拆解
└── dev_progress.md     # 开发进展记录
```

## 💻 Tech Stack

### Backend

- Go 1.21+
- Gin
- upper/db
- MySQL 8.0+
- JWT 鉴权

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

ClawManager 是一个 Kubernetes-first 项目。只有当被管理节点加入 Kubernetes 集群后，ClawManager 才能进行实例调度、资源检查和统一运维。

在安装 ClawManager 之前，请准备好可用的 Kubernetes 环境，并确认 `kubectl` 可以访问集群：

```bash
kubectl get nodes
```

### Linux 安装示例

使用 `k3s`：

```bash
curl -sfL https://get.k3s.io | sh -
sudo kubectl get nodes
```

使用 `microk8s`：

```bash
sudo snap install microk8s --classic
sudo microk8s status --wait-ready
sudo microk8s kubectl get nodes
```

### Kubernetes 基础命令

```bash
kubectl get nodes
kubectl get pods -A
kubectl get pvc -A
kubectl cluster-info
```

### 最低建议配置

- 1 个 Kubernetes 节点
- 4 CPU
- 8 GB RAM
- 20+ GB 可用磁盘

如果你计划同时运行多个桌面实例，请分配更多 CPU、内存和存储。

## 📦 Installation

安装前请确认：

- Kubernetes 可用
- `kubectl get nodes` 正常工作

直接使用仓库内置的 Kubernetes YAML 部署：

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## ⚡ Quick Start

### Default Accounts

- 默认管理员账号：`admin / admin123`
- 导入的管理员用户默认密码：`admin123`
- 导入的普通用户默认密码：`user123`

### First Login

1. 👨‍💼 使用管理员账号登录。
2. 👥 创建或导入用户，并分配 quota。
3. 🧩 可选地在系统设置中配置运行时镜像卡片。
4. 🖥 使用普通用户登录并创建实例。
5. 🔗 通过 Portal View 或 Desktop Access 访问桌面。

## ⚙️ Configuration

ClawManager 遵循清晰的安全模型：

- 实例服务运行在 Kubernetes 内部网络中
- 桌面访问统一经过 ClawManager 后端鉴权代理
- backend 最佳部署位置是在集群内部
- 运行时镜像可通过系统设置集中管理
- 被管理节点应全部属于同一个 Kubernetes 集群

常用后端环境变量：

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

前端开发模式会通过 Vite 将 `/api` 代理到后端。

### CSV Import Template

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

说明：

- `Email` 是可选项
- `Max GPU Count (optional)` 是可选项
- 其他列都是必填
- quota 数值应与你的集群容量规划保持一致

## 📄 License

本项目基于 MIT License 发布。

## ❤️ Open Source

欢迎提交 issue 和 pull request，包括功能、文档和测试方面的改进。
