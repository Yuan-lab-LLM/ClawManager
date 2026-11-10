# Ubuntu 24.04 ClawManager 离线部署指南

本文是交付部署入口，同一个 Ubuntu 24.04 离线包同时支持 `single-node` 和 `cluster`，
无需为两种模式分别制作安装包。安装时通过 `PROFILE` 和 `nodes.conf` 选择模式。

## 1. 选择部署模式

| 项目 | `single-node` | `cluster` |
| --- | --- | --- |
| 节点 | 1 台 control-plane | 1 台 control-plane + N 台 worker |
| 业务 Pod | 运行在 control-plane | 默认运行在 worker |
| 存储 | 本机 hostPath | Longhorn |
| 数据目录 | `/data/clawmanager/system/*` | `/var/lib/longhorn`，可在安装前修改 |
| SSH 编排 | 不需要 | control-plane 通过 SSH 自动安装 worker |
| 高可用 | 不具备 | 取决于 worker、存储节点和副本数 |

开发、演示或单机交付使用 `single-node`；生产多节点环境使用 `cluster`。双节点 cluster
只能设置一个 Longhorn 副本，不属于存储高可用。三个及以上存储节点建议使用三个副本。

## 2. 构建离线包

如果不自行构建镜像，可以先使用可选的 [GitHub 镜像准备脚本](../GITHUB_IMAGES.md)，
读取统一 release-images.conf 批量拉取镜像、生成输入 tar 并更新 lock。
ClawManager/Runtime 的 latest 会解析为具体版本写入 lock，再继续本节制包流程。

如果已经拿到 `dist/*.tar.gz` 和对应 `.sha256`，跳过本节。首次准备 deb/Calico 缓存的
制包机需要联网，并具备：

```text
bash curl docker sha256sum tar python3
```

镜像输入位于公共 `images/local/`：

- `clawmanager-images-<发布标签>.tar`：本次 ClawManager 镜像，由业务侧替换。
- `runtime-images-<发布标签>.tar`：本次选择的一个或多个 Runtime，由业务侧替换。
- `platform-base-images-<发布标签>.tar`：K8s、存储、MySQL 等固定依赖，默认复用。

每一类都允许拆成多个 tar，因此 GitHub Release 提供“一个镜像一个 tar”也可以使用。
下载后按类别保证文件名匹配以下规则：

```text
clawmanager-images-*.tar
runtime-images-*.tar
platform-base-images*.tar
```

例如 `runtime-images-openclaw-2026.9.1.tar`、`runtime-images-codex-2026.9.1.tar`、
`platform-base-images-mysql-8.4.8.tar`。tar 文件名只决定制包脚本是否加载，镜像名和 tag
仍以 tar 内部 RepoTag 为准；从 GitHub 下载的任意文件名如果不匹配规则，需要先重命名。

镜像已经在本机 Docker 时，可以直接按 lock 生成三个标准输入 tar；缺镜像会明确报错，
脚本不会自动 pull：

```bash
cd deployments/k8s/offline-installer
bash save-images-from-lock.sh --release-tag 2026.9.15 --group all
```

基础包未变化时可只生成业务分组：

```bash
bash save-images-from-lock.sh --release-tag 2026.9.15 \
  --group clawmanager,runtime
```

然后构建 Ubuntu 24.04 总离线包：

```bash
cd deployments/k8s/offline-installer/ubuntu24.04
bash build-offline-bundle.sh --release-tag 2026.9.15
```

如果 Ubuntu 22.04 成品的 `images.lock` 和基础设施版本完全一致，可以复用其中的镜像
归档，减少空间和构建时间：

```bash
bash build-offline-bundle.sh --release-tag 2026.9.15 \
  --reuse-image-bundle ../ubuntu22.04/dist/<Ubuntu-22.04-成品>.tar.gz
```

输出位于：

```text
ubuntu24.04/dist/clawmanager-k8s-offline-<发布标签>-ubuntu24.04-r<修订号>-linux-amd64.tar.gz
ubuntu24.04/dist/clawmanager-k8s-offline-<发布标签>-ubuntu24.04-r<修订号>-linux-amd64.tar.gz.sha256
```

最终只向 control-plane 上传这两个文件。不要把源码目录或镜像输入 tar 分别上传到服务器。

### 镜像 tag 在哪里修改

当前 tag 的权威打包清单是：

```text
deployments/k8s/offline-installer/images/images.lock
```

格式为稳定键和完整镜像引用：

```text
platform.mysql|mysql:8.4.8
platform.minio|minio/minio:RELEASE.2025-09-07T16-13-09Z
platform.clawmanager|clawmanager:2026.8.27
runtime.openclaw.lite|openclaw-lite:2026.8.24
```

修改业务镜像版本时只需：

1. 修改 `images/images.lock` 中对应键的完整镜像名和 tag。
2. 确保本机 Docker 或 `images/local/` 输入 tar 中存在这个精确 RepoTag。
3. 重新生成对应分组 tar 并构建总离线包。

`build-offline-bundle.sh` 会在临时 staging 目录内按稳定键自动渲染 single-node、cluster
两份 YAML，包括平台 Deployment 的 `image:`、Lite 预热 Runtime 的镜像和环境变量，以及
MySQL `system_image_settings` 的 Runtime 映射。仓库中的原始离线 YAML 不会被改写。

`images.lock` 已预留以下可选 Runtime 键：OpenClaw Pro/Lite、Hermes Pro/Lite、
DSH Pro/Lite、OpenCode Pro/Lite 和 Workbuddy Internal。空值（例如
`runtime.dsh.pro|`）表示本次不打包、不渲染这个镜像，但固定键继续保留，且不会修改
数据库已有项的 `is_enabled`。Pro 使用 desktop；Lite 在 MySQL 引导阶段使用 shell，并由
ClawManager 现有数据库迁移转换为 gateway。

## 3. 目标服务器前置条件

- Ubuntu Server 24.04 amd64，安装时已启用 OpenSSH Server。
- 固定 IP、时间同步、hostname 允许修改。
- 不存在 kubeadm、K3s、MicroK8s、K0s 等其他 Kubernetes 环境或残留。
- 每台建议至少 50 GiB 可用空间；Runtime 较多时相应增加。
- `10.244.0.0/16` Pod CIDR 和 `10.96.0.0/12` Service CIDR 不与现场网络冲突。
- cluster 模式下 control-plane 可通过 SSH 访问所有 worker，节点内网互通。
- 至少放行节点间 TCP 22、6443、10250 和 UDP 4789，访问端放行 TCP 30443。

目标服务器不需要提前安装 Docker、containerd、Kubernetes 或 Longhorn，也不要求访问
互联网。安装器不会清理非本安装器管理的已有 Kubernetes；检测到后会停止。

## 4. 上传、校验和解压

在管理机上传到 control-plane：

```bash
scp clawmanager-k8s-offline-*.tar.gz \
  clawmanager-k8s-offline-*.tar.gz.sha256 \
  <管理用户>@<CONTROL_PLANE_IP>:/home/clawmanager/
```

登录 control-plane 后执行：

```bash
cd /home/clawmanager
sha256sum -c clawmanager-k8s-offline-*.tar.gz.sha256
tar -xzf clawmanager-k8s-offline-*.tar.gz
cd clawmanager-k8s-offline-*/
cp config/install.env.example config/install.env
cp config/nodes.conf.example config/nodes.conf
```

SHA256 校验必须显示 `OK`。

## 5. 配置 single-node

`config/install.env` 的关键内容：

```bash
PROFILE=single-node
POD_CIDR=10.244.0.0/16
SERVICE_CIDR=10.96.0.0/12
CLAWMANAGER_NODE_PORT=30443
```

`config/nodes.conf` 只能保留一行：

```text
control-plane|k8s-master|local|auto|false
```

`LONGHORN_*` 参数可以保留默认值，single-node 不读取这些参数，也不会部署 Longhorn。
它使用四个 `manual` hostPath PV 保存 MySQL、MinIO、Redis 和 workspace 数据。

## 6. 配置 cluster

`config/install.env` 的关键内容：

```bash
PROFILE=cluster
SSH_USER=root
SSH_PORT=22
SSH_PRIVATE_KEY=/root/.ssh/clawmanager-installer
POD_CIDR=10.244.0.0/16
SERVICE_CIDR=10.96.0.0/12
LONGHORN_DATA_PATH=/var/lib/longhorn
LONGHORN_REPLICAS=3
CLAWMANAGER_NODE_PORT=30443
```

双节点验证环境应将 `LONGHORN_REPLICAS` 改为 `1`。`config/nodes.conf` 示例：

```text
control-plane|k8s-master|local|auto|false
worker|k8s-worker1|<WORKER_1_IP>|auto|true
worker|k8s-worker2|<WORKER_2_IP>|auto|true
worker|k8s-worker3|<WORKER_3_IP>|auto|true
```

第三列 `ssh_target` 填每台 worker 的实际 IP 或可解析主机名，不在脚本中写死。最后一列
为 `true` 表示该 worker 承载 Longhorn 数据。

## 7. 统一安装流程

只在 control-plane 执行：

```bash
sudo bash prepare-ssh.sh
sudo bash preflight.sh
sudo bash verify-bundle.sh
set -o pipefail
sudo bash install.sh 2>&1 | tee install.log
install_rc=${PIPESTATUS[0]}
echo "install exit code: ${install_rc}"
```

每条命令的作用：

| 命令 | 是否修改服务器 | 作用 |
| --- | --- | --- |
| `sudo bash prepare-ssh.sh` | cluster 会配置 SSH；single-node 不修改 SSH | cluster 生成安装专用密钥并复制到所有 Worker；single-node 只提示没有 Worker |
| `sudo bash preflight.sh` | 否 | 只读检查 Ubuntu 版本、架构、IP、磁盘、端口、时间同步、SSH 和已有 Kubernetes 状态 |
| `sudo bash verify-bundle.sh` | 否 | 校验发行版、架构、deb、manifest、镜像清单、镜像 tar 和 SHA256 是否一致 |
| `set -o pipefail` | 仅影响当前 shell | 确保 `install.sh` 失败时，不会被后面的 `tee` 错误地显示成成功 |
| `sudo bash install.sh 2>&1 \| tee install.log` | 是 | 安装软件包、配置 containerd、导入镜像、安装 Kubernetes/Calico，并按 profile 部署 ClawManager；同时保存完整日志 |
| `install_rc=${PIPESTATUS[0]}` | 否 | 立即取得管道中 `install.sh` 的真实退出码，而不是 `tee` 的退出码 |
| `echo ...` | 否 | 显示退出码；只有 `0` 代表安装成功 |

`PIPESTATUS[0]` 必须在安装管道之后立即保存；中间插入其他命令会覆盖它。

- single-node：`prepare-ssh.sh` 会提示没有 worker，不要求输入 SSH 密码。
- cluster：首次 `prepare-ssh.sh` 需要各 worker 的 SSH/sudo 密码，之后正式安装免密执行。
- `install exit code` 必须为 `0`。
- `skill-scanner:latest` 是当前已知可变标签警告；正式发版建议改为不可变 tag。

## 8. 安装结果检查

通用检查：

```bash
export KUBECONFIG=/etc/kubernetes/admin.conf
kubectl get nodes -o wide
kubectl get pods -A -o wide
kubectl -n clawmanager-system get deploy
kubectl -n clawmanager-system get pvc
```

single-node 额外检查：

```bash
kubectl get pv
kubectl get namespace longhorn-system
test ! -e /var/lib/longhorn && echo "Longhorn 未部署"
```

预期只有一个 Ready 节点、PV/PVC 全部 Bound，且 `longhorn-system` 不存在。

cluster 额外检查：

```bash
kubectl -n longhorn-system get volumes.longhorn.io
```

预期全部节点 Ready、PVC 全部 Bound、Longhorn 卷为 `attached/healthy`。

页面地址：

```text
https://<CONTROL_PLANE_IP>:30443/login
```

## 9. 失败续跑

先保存并查看日志：

```bash
tail -n 200 install.log
kubectl get pods -A -o wide 2>/dev/null || true
journalctl -u containerd -n 200 --no-pager
journalctl -u kubelet -n 200 --no-pager
```

排除磁盘、网络或配置问题后，直接重新执行同一个安装入口：

```bash
set -o pipefail
sudo bash install.sh 2>&1 | tee install-retry.log
echo "retry exit code: ${PIPESTATUS[0]}"
```

安装器会校验实际状态并跳过已完成步骤。不要手动删除 PVC、MySQL 数据、
`/etc/kubernetes` 或 `/var/lib/clawmanager-installer`。本方案不提供重装/清库入口；需要
完全重装时应使用新服务器或恢复干净系统快照。

## 10. 实机验收记录

- `single-node`：见 `SINGLE_NODE_VALIDATION.md`。
- `cluster`：见 `VALIDATION.md`。

两种模式均已在 Ubuntu 24.04.3 LTS amd64 上完成 r2 实机安装和完整重跑验证。
