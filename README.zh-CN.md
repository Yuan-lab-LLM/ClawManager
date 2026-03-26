# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  一个面向团队与集群规模场景的 Kubernetes-first 控制平面，用于统一管理 OpenClaw 和 Linux 桌面运行时。
</p>

<p align="center">
  <strong>语言:</strong>
  <a href="./README.md">English</a> |
  简体中文 |
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

## 新闻

- `2026-03-26`: AI Gateway 文档与总览已更新，涵盖模型治理、审计追踪、成本核算和风险控制。参见 [AI Gateway](#ai-gateway)。

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager Admin" width="32%" />
  <img src="./docs/main/portal.png" alt="ClawManager Portal" width="32%" />
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="32%" />
</p>

## 它是什么

ClawManager 帮助团队在 Kubernetes 上统一部署、运维并访问桌面运行时。

它适合这些场景：

- 需要为多个用户创建桌面实例
- 需要集中管理配额、镜像和生命周期
- 希望桌面服务始终保留在集群内部
- 希望通过安全的浏览器访问方式，而不是直接暴露 Pod

## 为什么选择它

- 一个管理后台统一管理用户、配额、实例和运行时镜像
- 支持 OpenClaw，并提供记忆与偏好设置的导入导出
- 通过平台提供安全桌面访问，而不是直接暴露服务
- AI Gateway 提供受控模型访问、审计追踪、成本分析和风险控制
- 天然适配 Kubernetes 的部署与运维方式
- 同时支持管理员统一发放和用户自助创建


## 快速开始

### 前置条件

- 一个可用的 Kubernetes 集群
- `kubectl get nodes` 可以正常执行

### 部署

直接应用仓库自带清单：

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## 从源码构建

如果你想从源码运行或打包 ClawManager，而不是直接使用仓库自带的 Kubernetes 清单：

### 前端

```bash
cd frontend
npm install
npm run build
```

### 后端

```bash
cd backend
go mod tidy
go build -o bin/clawreef cmd/server/main.go
```

### Docker 镜像

在仓库根目录构建完整应用镜像：

```bash
docker build -t clawmanager:latest .
```

### 默认账户

- 默认管理员账户：`admin / admin123`
- 导入管理员用户时的默认密码：`admin123`
- 导入普通用户时的默认密码：`user123`

### 首次使用

1. 使用管理员账户登录。
2. 创建或导入用户，并分配配额。
3. 在系统设置中查看或更新运行时镜像卡片。
4. 使用普通用户登录并创建实例。
5. 通过 Portal View 或 Desktop Access 访问桌面。

## 核心能力

- 实例生命周期管理：创建、启动、停止、重启、删除、查看和同步
- 支持的运行时类型：`openclaw`、`webtop`、`ubuntu`、`debian`、`centos`、`custom`
- 后台运行时镜像卡片管理
- 用户级 CPU、内存、存储、GPU 和实例数量配额控制
- 节点、CPU、内存和存储的集群资源总览
- 基于令牌的桌面访问与 WebSocket 转发
- AI Gateway：模型管理、可追溯审计、成本核算与风险控制
- 基于 CSV 的批量用户导入
- 多语言界面

## AI Gateway

AI Gateway 是 ClawManager 中负责模型访问治理的控制平面。它为 OpenClaw 实例提供统一的 OpenAI 兼容入口，并在上游 Provider 之上增加策略、审计和成本控制。

- 面向普通模型与安全模型的模型管理，以及 Provider 接入、启停、端点配置和价格策略
- 面向请求、响应、路由决策和风险命中的全链路审计与追踪记录
- 内置 Token 统计与估算分析的成本核算能力
- 基于可配置规则的风险控制，并支持 `block` 与 `route_secure_model` 等自动动作

如需查看截图、完整功能拆解以及模型选择与路由流程，请参阅 [docs/aigateway.md](./docs/aigateway.md)。

## 产品流程

1. 管理员定义用户、配额和运行时镜像策略。
2. 用户创建 OpenClaw 或 Linux 桌面实例。
3. ClawManager 创建并跟踪 Kubernetes 资源。
4. 用户通过平台访问桌面。
5. 管理员通过仪表盘监控健康状态和容量。

## 架构

```text
Browser
  -> ClawManager Frontend
  -> ClawManager Backend
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

## 配置说明

- 实例服务保留在 Kubernetes 集群内部网络
- 桌面访问通过已认证的后端代理转发
- 运行时镜像可以在系统设置中覆盖
- 后端最好部署在集群内部

常用后端环境变量：

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

### CSV 导入模板

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

说明：

- `Email` 为可选项
- `Max GPU Count (optional)` 为可选项
- 其他列均为必填项

## 许可证

本项目基于 MIT License 发布。

## 开源

欢迎提交 issue 和 pull request。
