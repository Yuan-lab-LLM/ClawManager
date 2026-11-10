# Ubuntu 24.04 制包与主机准备

## 基础 Ubuntu 环境必须事先具备

使用 Ubuntu Server 24.04 amd64 安装两台机器。安装系统时启用 OpenSSH Server，并保证：

- control-plane 能通过 SSH 访问 worker；
- 使用 root，或使用已加入 `sudo` 组的统一管理用户；
- 节点 IP 固定、主机时间同步；
- 没有 kubeadm、kubelet、K3s、MicroK8s、K0s 或其他 Kubernetes 残留；
- Pod/Service CIDR 不与物理网络重叠；
- 双机验证时 worker 至少有 50 GiB 可用空间，Longhorn 使用
  `/var/lib/longhorn`，并将副本数设为 1。

OpenSSH 是安装器把离线包发送到 worker 之前的传输通道，因此必须在 Ubuntu 系统安装时
事先启用。除此之外，不要求提前联网安装 Kubernetes、containerd、Longhorn 或 Docker。

## Ubuntu 24.04 离线包内置的软件包

Noble 离线 apt 仓库由 `scripts/build-offline-packages.sh` 自动生成，包含目标软件及其
完整依赖：

- `containerd.io`；
- `kubelet`、`kubeadm`、`kubectl` 和 `kubernetes-cni`；
- `conntrack`、`socat`、`ebtables`、`ipset`、`iproute2`、`iptables`；
- `open-iscsi`、`nfs-common`、`cryptsetup`、`dmsetup`；
- `ca-certificates`、`curl`、`gpg`、`openssl`、`openssh-client`；
- `rsync`、`jq`、`python3-minimal`；
- apt 解析出的全部 Noble amd64 依赖。

安装器为 containerd 2.x 写入明确的 config v3，并在配置后检查 Kubernetes CRI v1
插件处于 `ok` 状态。不要在安装前另行覆盖 `/etc/containerd/config.toml`；如果现场必须
使用自定义 containerd 配置，应先验证 `crictl info` 和 kubeadm dry-run，再安装集群。

版本由 `versions.env` 锁定。Ubuntu 22.04 的 Jammy deb 不会复制或混入本目录。

## 联网制包机

制包机需要 Linux amd64 或带 Linux container 支持的 Docker Desktop，并具备：

```text
bash curl docker sha256sum tar python3
```

业务侧只需要修改 `images/images.lock`，把对应的 ClawManager 和本次 Runtime 精确 tag
准备到本机 Docker，再按 lock 导出变化的分组；固定的 platform-base tar 默认复用：

```bash
cd deployments/k8s/offline-installer
bash save-images-from-lock.sh --release-tag 2026.9.15 \
  --group clawmanager,runtime
```

然后执行：

```bash
cd deployments/k8s/offline-installer/ubuntu24.04
bash build-offline-bundle.sh --release-tag 2026.9.15
```

如果现有 Ubuntu 22.04 成品的 `images.lock` 和基础设施版本完全相同，可以不导入或重新
导出任何镜像，直接复用成品中的完整镜像 tar：

```bash
bash build-offline-bundle.sh \
  --reuse-image-bundle ../ubuntu22.04/dist/<Ubuntu-22.04-成品>.tar.gz
```

本地容器断网安装测试是可选的辅助检查；本次以真实 Ubuntu 24.04 节点上的预检、安装、
重跑和功能验收为最终结果。

最终只向 control-plane 上传 `dist/*.tar.gz` 和对应 `.sha256`。

## 目标主机预检

解压最终包并配置 `config/install.env`、`config/nodes.conf` 后执行：

```bash
sudo bash prepare-ssh.sh
sudo bash preflight.sh
sudo bash verify-bundle.sh
```

`preflight.sh` 只读检查两台机器的系统版本、架构、SSH、已有 Kubernetes、CPU、内存、
磁盘、cgroup v2、内核模块、时间同步和端口占用，不会安装或清理软件。
