# Ubuntu 24.04 单节点验收

适用拓扑：一台 control-plane，同时承载 ClawManager 工作负载和本地数据。该模式使用
hostPath PV，不部署 Longhorn，也不提供节点或存储高可用。

## 配置

`config/install.env`：

```bash
PROFILE=single-node
POD_CIDR=10.244.0.0/16
SERVICE_CIDR=10.96.0.0/12
CLAWMANAGER_NODE_PORT=30443
```

`config/nodes.conf` 只能包含 control-plane：

```text
control-plane|k8s-master|local|auto|false
```

`LONGHORN_*` 配置可以保留通用默认值，single-node profile 不读取这些值。为复用同一份
离线镜像归档并避免维护两套镜像锁，归档内可以包含 Longhorn 镜像，但安装后不应存在
Longhorn namespace、CRD、StorageClass、Pod 或 `/var/lib/longhorn` 数据目录。

## 安装

```bash
sudo bash prepare-ssh.sh
sudo bash preflight.sh
sudo bash verify-bundle.sh
set -o pipefail
sudo bash install.sh 2>&1 | tee install-r2-single.log
```

single-node 模式下 `prepare-ssh.sh` 应提示没有 Worker，不要求 SSH 密钥或 Worker 密码。

## 通过标准

- Kubernetes 恰好一个 Ready 节点，control-plane taint 已移除。
- 节点具有 `clawmanager.io/storage-node=true` 标签。
- MySQL、MinIO、Redis 和 workspaces 使用 `manual` StorageClass，对应 hostPath 为
  `/data/clawmanager/system/*`，所有 PV/PVC 均为 Bound。
- `longhorn-system` namespace、Longhorn CRD 和 Longhorn StorageClass 均不存在。
- ClawManager 所有 Deployment 就绪，没有持续异常 Pod。
- MySQL 从空目录完成初始化，默认管理员可以通过登录 API 登录。
- 完整重跑 `install.sh` 返回 0，PV/PVC、MySQL Pod、数据和重启计数保持不变。

## 2026-09-09 r2 实机结果

在 Ubuntu 24.04.3 LTS amd64 服务器完成从零验收。安装前已删除原 Kubernetes、
containerd、Longhorn、安装器状态及 `/data/clawmanager`，卸载相关软件包并重启；确认
相关目录、CNI 网卡和端口为空后开始安装。

- 首次安装从 `07:52:37 UTC` 执行至 `07:58:14 UTC`，退出码为 0。
- Kubernetes `v1.36.4`、containerd `2.3.4`；唯一节点为 Ready，CRI plugin 状态为 ok。
- 4 个 `manual` PV/PVC 全部 Bound，hostPath 均位于 `/data/clawmanager/system`。
- Longhorn namespace、CRD、StorageClass 和数据目录均不存在。
- ClawManager 及包内 Runtime Deployment 全部就绪，异常 Pod 数为 0。
- MySQL 初始化出 58 张业务表和一条启用的默认管理员记录；登录 API 返回 HTTP 200。
- 完整第二次执行从 `08:00:29 UTC` 至 `08:00:54 UTC`，退出码为 0；PV/PVC、MySQL
  Pod UID 和所有容器重启计数均未变化。

本次没有物理切断公网；软件包、manifest 和镜像均从 r2 离线包读取，目标机未执行在线
下载。`skill-scanner:latest` 仍会产生可变标签警告。
