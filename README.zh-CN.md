# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  一个面向团队和集群规模场景的 Kubernetes-first 控制平面，用于统一管理 OpenClaw 与 Linux 桌面运行时。
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

## 它是什么

ClawManager 帮助团队在 Kubernetes 上统一部署、运维和访问桌面运行时。

它适合这些场景：

- 需要为多个用户创建桌面实例
- 需要集中管理配额、镜像和生命周期
- 希望桌面服务保持在集群内部
- 希望通过安全的浏览器访问方式，而不是直接暴露 Pod

## 为什么用户会选择它

- 一个后台统一管理用户、配额、实例和运行时镜像
- 支持 OpenClaw，并提供记忆与偏好设置的导入导出
- 通过平台提供安全桌面访问，而不是直接暴露服务
- 天然适配 Kubernetes 的部署和运维方式
- 同时支持管理员统一发放和用户自助创建实例

<p align="center">
  <img src="frontend/public/clawmanager_overview.png" alt="ClawManager Overview" width="100%" />
</p>

## 快速开始

### 前置条件

- 有一个可用的 Kubernetes 集群
- `kubectl get nodes` 可以正常执行

### 部署

直接应用仓库自带的清单：

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

### 默认账号

- 默认管理员账号：`admin / admin123`
- 导入管理员用户时的默认密码：`admin123`
- 导入普通用户时的默认密码：`user123`

### 首次使用

1. 先使用管理员账号登录。
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
- 基于 CSV 的批量用户导入
- 多语言界面

## 产品使用流程

1. 管理员配置用户、配额和运行时镜像策略。
2. 用户创建 OpenClaw 或 Linux 桌面实例。
3. ClawManager 创建并跟踪对应的 Kubernetes 资源。
4. 用户通过平台访问桌面。
5. 管理员通过仪表盘查看健康状态和容量。

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

- 实例服务运行在 Kubernetes 内部网络中
- 桌面访问通过已认证的后端代理转发
- 运行时镜像可在系统设置中覆盖
- 后端最佳部署位置是集群内部

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
