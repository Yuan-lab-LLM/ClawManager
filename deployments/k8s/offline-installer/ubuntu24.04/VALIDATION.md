# Ubuntu 24.04 双节点验收

适用拓扑：一台 control-plane + 一台 worker。此拓扑用于验证多节点安装流程，Longhorn
只能配置单副本，不代表生产高可用验收。

## 1. 验收前提

- 两台机器均为全新的 Ubuntu 24.04 amd64，且没有其他 Kubernetes 安装。
- 两台机器 IP 固定、时间同步，control-plane 可通过 SSH 访问 worker。
- 默认 `10.244.0.0/16` Pod 网段和 `10.96.0.0/12` Service 网段不与现场网络重叠。
- 节点间允许 Kubernetes API TCP 6443、kubelet TCP 10250 和 Calico VXLAN UDP 4789。
- 访问端允许连接 ClawManager NodePort TCP 30443。
- 每台机器预留足够空间保存安装包、解压镜像和 containerd 镜像；建议至少 50 GiB
  可用空间，Runtime 越多需要的空间越大。
- 强离线验收应在上传完成后切断公网，但必须保留两台节点之间以及管理端 SSH 的局域网。

两台机器分别执行并保存输出：

```bash
cat /etc/os-release
dpkg --print-architecture
uname -r
ip -br address
df -h /
free -h
timedatectl status
swapon --show
command -v kubeadm || true
test ! -e /etc/kubernetes/admin.conf
```

## 2. 配置双节点

只在 control-plane 解压最终离线包并执行：

```bash
cp config/install.env.example config/install.env
cp config/nodes.conf.example config/nodes.conf
```

`config/install.env` 的关键配置：

```bash
PROFILE=cluster
SSH_USER=clawadmin
SSH_PORT=22
SSH_PRIVATE_KEY=/root/.ssh/clawmanager-installer
POD_CIDR=10.244.0.0/16
SERVICE_CIDR=10.96.0.0/12
LONGHORN_DATA_PATH=/var/lib/longhorn
LONGHORN_REPLICAS=1
CLAWMANAGER_NODE_PORT=30443
```

`config/nodes.conf` 只保留两行，其中 `<WORKER_IP>` 替换为真实地址：

```text
control-plane|k8s-master|local|auto|false
worker|k8s-worker1|<WORKER_IP>|auto|true
```

## 3. 离线安装

```bash
sudo bash prepare-ssh.sh
sudo bash preflight.sh
sudo bash verify-bundle.sh
set -o pipefail
sudo bash install.sh 2>&1 | tee install-ubuntu24-cluster.log
```

预期四个命令退出码均为 0。首次 `prepare-ssh.sh` 可以提示输入 worker 的 SSH/sudo
密码；`install.sh` 正式执行时不应再提示密码，也不应访问公网下载内容。

## 4. 自动化结果检查

```bash
kubectl get nodes -o wide
kubectl get pods -A -o wide
kubectl get deploy -A
kubectl get storageclass
kubectl get pvc -A
kubectl get pv
kubectl get events -A --sort-by=.lastTimestamp | tail -n 100
```

通过标准：

- 恰好两个节点，均为 `Ready`。
- `calico-system`、`longhorn-system` 和 `clawmanager-system` 没有持续
  `Pending`、`ImagePullBackOff`、`CrashLoopBackOff` Pod。
- ClawManager 所有 Deployment 的 READY 数等于期望副本数。
- ClawManager PVC 全部 `Bound`，Longhorn 卷为 Healthy/Attached。
- 业务 Pod 被调度到 worker，Longhorn 数据目录位于 `/var/lib/longhorn`。

页面探测：

```bash
curl -k -sS -o /dev/null -w '%{http_code}\n' \
  https://<CONTROL_PLANE_IP>:30443/login
```

预期返回 `200`。随后登录 UI，至少创建一个本次打包包含的 Runtime 实例，确认其 Pod
进入 Ready、页面状态正常。模型提供商的现场 DNS、证书和出口策略单独验证，不属于通用
Ubuntu 安装器验收。

## 5. 幂等与续跑检查

完整安装成功后再次执行：

```bash
set -o pipefail
sudo bash install.sh 2>&1 | tee install-ubuntu24-cluster-rerun.log
```

预期退出码为 0、两个节点仍为 Ready、PVC 仍为 Bound、已有 MySQL 数据和管理员账号不被
重置。若环境可恢复虚拟机快照，可另做一次中断测试：在安装中途终止脚本，恢复网络或
故障条件后重新运行 `install.sh`，确认从未完成步骤继续。

## 6. 故障取证

```bash
kubectl get pods -A -o wide
kubectl describe node k8s-master
kubectl describe node k8s-worker1
kubectl get events -A --sort-by=.lastTimestamp | tail -n 200
journalctl -u containerd -n 200 --no-pager
journalctl -u kubelet -n 200 --no-pager
```

worker 上额外执行：

```bash
sudo journalctl -u containerd -n 200 --no-pager
sudo journalctl -u kubelet -n 200 --no-pager
sudo ls -lah /var/lib/longhorn
```

## 7. 2026-09-09 实机结果

已在两台 Ubuntu 24.04.3 LTS amd64 服务器完成一次真实集群验收：

- 最终验收前，两台节点均执行集群重置、Kubernetes/containerd 软件包清理、安装器状态
  清理、Longhorn/MySQL/PVC 数据清理和重启；同时确认 `/opt/clawmanager-offline`
  Worker 缓存不存在。首次清理遗漏该缓存后，安装在 kubeadm 初始化前主动中止；补充清理
  并重新预检后，最终验收实际向 Worker 传输了 `22,927,010,816` 字节镜像归档。
- Kubernetes `v1.36.4`，containerd `2.3.4`；一个 control-plane 和一个 worker
  均为 `Ready`。
- Calico、Longhorn 和 ClawManager 全部工作负载就绪；4 个 Longhorn 卷均为
  `attached/healthy`，双节点验收配置为单副本。
- MySQL 从空盘初始化出 58 张 `clawmanager` 表和一条默认管理员记录；管理员 API
  登录返回 HTTP 200。
- 登录页返回 HTTP 200；ClawManager、MySQL、MinIO、Redis、skill-scanner 和包内两个
  Runtime Deployment 均达到期望副本数。
- 完整第二次执行 `install.sh` 返回 0，跳过已完成的软件包、镜像、control-plane 和
  worker 初始化；MySQL Pod UID、全部 PVC UID、所有容器重启计数、表数和管理员记录
  均保持不变。
- 现场发现并修复 containerd 2.3 CRI 配置兼容问题，以及 installer-owned
  `kubeadm reset` 后 containerd 延迟重启造成的 CRI socket 竞态。最终成品必须使用包含
  这两项修复的 `r2` 脚本重新制包，旧的 Ubuntu 24.04 `r1` 成品不能作为本次验收结论。

最终验收使用的归档 SHA256 为
`ce3caedfa180dfccb6587a1ebe306eb76188a9422f5dbf1046905536582b07fc`。首次完整安装从
`2026-09-09 03:10:48 UTC` 执行至 `03:25:26 UTC`；完成态重跑从 `03:29:58 UTC`
执行至 `03:30:33 UTC`。

本次没有切断公网做强离线验证；安装过程使用包内 deb、manifest 和镜像，没有在目标机
执行下载。模型供应商的 DNS、证书和出口策略仍按现场单独配置，不写入通用安装器。
