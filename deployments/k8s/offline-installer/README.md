# ClawManager Ubuntu 22.04 / 24.04 离线安装器

本目录提供面向 Ubuntu 22.04（Jammy）和 Ubuntu 24.04（Noble）的 Kubernetes +
ClawManager 离线安装框架，支持：

- 单节点：一个 control-plane，同时承载业务 Pod，使用 HostPath。
- 集群：一个 control-plane + N 个 worker，默认由 worker 承载 Longhorn 存储。
- 在 control-plane 上执行一次脚本，通过 SSH 配置全部 worker。
- 完全离线安装 `.deb`、Kubernetes、Calico、Longhorn 和 ClawManager。
- 所有容器镜像合并为一个 Docker 兼容 tar，共享基础层只保存一次。
- 安装中断后重新执行，按实际状态和安装器状态标记继续。
- 只清理由本安装器刚刚创建、但未完成的 `kubeadm init/join` 状态。
- 发现非本安装器管理的 Kubernetes 时立即停止，不提供重装能力。

> 本目录是安装框架。大体积 `.deb` 和镜像 tar 不提交到 Git，需在联网的 Linux amd64
> 制包机上运行对应版本目录中的 `build-offline-bundle.sh`，生成发行版专用离线包。
> Ubuntu 22.04 与 24.04 的 deb 不混装，安装器会校验目标系统。

## 为什么 Worker IP 需要填写

安装器必须先知道向哪台机器发起 SSH，所以 Worker 的 SSH 目标 IP 或 DNS 名需要写入
`config/nodes.conf`。这个值不是写死在脚本里的 Kubernetes 节点 IP。

连接 Worker 后，安装器会通过到 control-plane 的实际路由自动获取 Kubernetes
`node_ip`。只有多路由环境无法唯一判断时，才需要手工填写 `node_ip`。

节点格式：

```text
角色|hostname|SSH目标|node_ip|是否作为Longhorn存储节点
```

示例：

```text
control-plane|k8s-master|local|auto|false
worker|k8s-worker1|192.168.1.101|auto|true
worker|k8s-worker2|192.168.1.102|auto|true
worker|k8s-worker3|192.168.1.103|auto|true
```

- 单节点：把 `PROFILE` 改为 `single-node`，节点文件只保留 control-plane 行。
- 多节点：把 `PROFILE` 保持为 `cluster`，按真实 Worker 数量增删 worker 行。
- `storage=true` 的节点数必须不少于 `LONGHORN_REPLICAS`；例如只有一个存储 Worker
  时应把副本数设为 1，常规 3 个以上存储 Worker 保持 3。

## 目录

```text
offline-installer/
├── ubuntu22.04/                # Jammy 专属版本锁、deb、输出和制包入口
│   ├── versions.env
│   ├── packages/amd64/
│   └── build-offline-bundle.sh
├── ubuntu24.04/                # Noble 专属版本锁、deb、输出和制包入口
│   ├── versions.env
│   ├── packages/amd64/
│   └── build-offline-bundle.sh
├── build-offline-bundle.sh     # 两个版本入口共同调用的核心制包逻辑
├── save-images-from-lock.sh    # 按 images.lock 分组导出本机 Docker 镜像
├── install.sh                  # 制包时放入成品的统一安装入口
├── prepare-ssh.sh              # 制包时放入成品的 SSH 配置入口
├── preflight.sh                # 安装前只读检查全部节点环境
├── verify-bundle.sh            # 制包时放入成品的完整性校验
├── config/
│   ├── install.env.example     # 安装参数
│   └── nodes.conf.example      # 节点清单
├── images/
│   ├── images.lock             # 唯一镜像清单
│   └── local/                  # 可选：本地镜像 tar 输入
├── manifests/                  # 离线专用 ClawManager YAML；制包时加入 Calico YAML
└── scripts/lib/                # 安装器内部函数
```

`images/`、`manifests/`、`config/` 和 `scripts/` 是两个 Ubuntu 版本的公共资源，只保存
一份。每个版本生成的最终 `.tar.gz` 仍然是完整、自包含的离线安装包。

项目原有的默认清单保持不变，离线安装器不会读取或改写它们：

- `../single-node/clawmanager.yaml`
- `../cluster/clawmanager.yaml`

离线部署使用本目录内独立维护的两套清单：

- `manifests/clawmanager-single-node.yaml`
- `manifests/clawmanager-cluster.yaml`

制包脚本会把这两套离线专用清单复制到同一个离线包；安装时由 `PROFILE` 选择其中一套。

## 一、配置 SSH 免密

推荐所有节点使用相同的普通管理用户，例如 `clawadmin`，并具备免密 sudo。

在每台 Worker 上创建用户：

```bash
sudo adduser clawadmin
sudo usermod -aG sudo clawadmin
```

在 control-plane 解压离线包并配置节点：

```bash
cp config/install.env.example config/install.env
cp config/nodes.conf.example config/nodes.conf
vim config/install.env
vim config/nodes.conf
sudo bash prepare-ssh.sh
```

`prepare-ssh.sh` 会生成安装专用密钥，然后逐台调用 `ssh-copy-id`。首次仍需输入各
Worker 的现有登录密码；如尚未配置免密 sudo，还会再提示一次 sudo 密码，用 `visudo`
校验后写入 `/etc/sudoers.d/clawmanager-installer`。密码不会保存，完成后正式安装不再
需要 SSH 或 sudo 密码。该用户必须已经属于 `sudo` 组。

## 二、制作离线包

不自行构建镜像时，可选用 [发布镜像准备方案](GITHUB_IMAGES.md)：统一维护
`images/release-images.conf`，执行 `bash pull-github-image.sh` 批量 pull、save 并更新
`images.lock`。业务 latest 会解析成摘要一致的具体版本，基础镜像保持固定版本。此步骤独立可选，
不会被构建或安装脚本自动执行。

要求：

- 联网的 Linux amd64 主机；具体制包机发行版不限，依赖由对应 Ubuntu 容器下载。
- Docker 可用。
- ClawManager 和 AgentRuntime 自建镜像已经存在于本机 Docker 中，且名称与
  `images/images.lock` 完全一致。

如果这些镜像已经整理成 tar，可放入 `images/local/*.tar`；制包脚本会先 `docker load`
这些输入包，再与 Kubernetes、Calico、Longhorn 镜像合并成最终一个 tar。输入 tar
本身不会重复放入最终离线包。

本地输入分为“每次可替换的业务镜像”和“默认固定的基础镜像”：

- `clawmanager-images-<版本>.tar`：由业务侧按本次 ClawManager 版本提供。
- `runtime-images-<版本>.tar`：由业务侧按本次需要提供，可以包含一个或多个 lock 中
  已定义的 Runtime。
- `platform-base-images-<版本>.tar`：通用安装方案的固定基础依赖，普通业务发版默认不修改。

Ubuntu 22.04 与 24.04 共用这些输入，不复制镜像。你只需要按需替换前两个业务 tar 并
维护 `images/images.lock`；制包时会自动渲染两份成品 YAML，基础依赖 tar 继续复用。
详细约定见 `images/local/README.md`。

当前本地输入包中的业务镜像版本为：

```text
clawmanager:2026.8.27
openclaw-lite:2026.8.24
hermes-lite:2026.8.24
workbuddy-linux:2026.8.27
```

`skill-scanner` 仍由两套 YAML 使用。应用镜像必须已存在于本机 Docker，
或能从 `images/local/` 的分组 tar 加载；缺失时停止。只有 `kubernetes.*`、
`calico.*`、`longhorn.*` 基础设施镜像允许由制包脚本按 lock 固定版本自动补拉，
已有本地镜像则复用。自动下载发生在联网制包机，服务器安装不联网。

基础设施升级时分别更新两个版本目录内的 `versions.env`。镜像映射是公共资源，只需修改：

- `images/images.lock`

构建 Ubuntu 24.04/Noble 离线包：

```bash
cd deployments/k8s/offline-installer
bash save-images-from-lock.sh --release-tag 2026.9.15 \
  --group clawmanager,runtime  # 基础分组未变化时
cd ubuntu24.04
bash build-offline-bundle.sh --release-tag 2026.9.15
```

如果 Ubuntu 22.04 成品使用相同的 `images.lock`、Kubernetes、Calico、Longhorn 和
`amd64` 架构，可以直接复用它里面已经校验过的完整镜像 tar。该模式不调用 Docker，
也不读取 `images/local/runtime-images-*.tar`：

```bash
cd ubuntu24.04
bash build-offline-bundle.sh \
  --reuse-image-bundle ../ubuntu22.04/dist/<Ubuntu-22.04-成品>.tar.gz
```

脚本会依次校验 22.04 成品 SHA256、内部镜像 tar SHA256 和两边的 `images.lock`；任意一项
不一致都会停止，不能用旧镜像覆盖新清单。

构建已经验收过的 Ubuntu 22.04/Jammy 离线包：

```bash
cd ubuntu22.04
bash build-offline-bundle.sh --release-tag 2026.9.15
```

输出：

```text
ubuntu<版本>/dist/clawmanager-k8s-offline-<发布标签>-ubuntu<版本>-r<修订号>-linux-amd64.tar.gz
```

制包时只读取所选版本的 deb；镜像总 tar 可以由 Docker 输入重新生成，也可以从另一版本的
已验证成品直接复用。为节省磁盘，不需要同时长期保留两个 `dist/` 输出，交付或校验完成后
可归档到外部存储。

制包过程会：

1. 下载 Ubuntu/Kubernetes 离线 deb 及依赖并建立本地 apt 仓库。
2. 下载固定版本的 Calico operator YAML。
3. 加载 `images/local/` 三个分组的 tar；自动补拉缺失的 Kubernetes/Calico/Longhorn 固定版本镜像，应用镜像缺失则停止。
4. 按 `images.lock` 渲染两份 staging YAML 和 Runtime 数据库映射。
5. 将全部镜像导出为一个 OCI/Docker 兼容 tar。
6. 校验 YAML 中的镜像引用都存在于清单和 tar 内。
7. 将两套 ClawManager 清单都放进同一个总包，安装时再选择模式。

生成的 deb 仓库带有 `target-release.env`，固定记录目标版本、代号和架构；
`verify-bundle.sh` 会在安装前拒绝发行版或架构不匹配的包，避免 Jammy/Noble 包混装。

制包脚本只改写临时 staging 副本，不修改仓库源 YAML。它会为非空 `runtime.*` 生成
`system_image_settings` 的 image/display_name 更新或缺失行插入；不修改已有行的
`is_enabled`。空 Runtime 不打包、不渲染，固定键仍保留。

## 三、离线安装

将生成的 `.tar.gz` 复制到 control-plane：

```bash
tar -xzf clawmanager-k8s-offline-*.tar.gz
cd clawmanager-k8s-offline-*
cp config/install.env.example config/install.env
cp config/nodes.conf.example config/nodes.conf
vim config/install.env
vim config/nodes.conf
sudo bash prepare-ssh.sh
sudo bash preflight.sh
sudo bash verify-bundle.sh
sudo bash install.sh
```

安装成功后访问：

```text
https://<任一节点IP>:30443
```

ClawManager、数据库和 runtime token 沿用现有 YAML 默认值，安装器不会随机生成或
修改这些值。

## 四、续跑规则

安装器在每个节点记录：

```text
/etc/clawmanager-installer/owner
/var/lib/clawmanager-installer/state/
```

重新执行 `sudo ./install.sh` 时：

- 已完成且实际状态正确的步骤会跳过。
- 文件复制使用校验和；镜像大文件在安装 rsync 后使用 `--partial` 续传。
- `kubeadm init/join` 中断时，仅在确认 owner 和 bundle id 一致后执行受控清理重试。
- Calico、Longhorn、ClawManager 使用 `kubectl apply`，可安全重复执行。
- 状态标记与实际集群状态不一致时，以实际状态为准并重新执行该步骤。
- 发现其他安装来源的 `/etc/kubernetes` 或 kubeadm 状态时立即退出。

## 五、Longhorn 简明说明

- 三个及以上存储节点默认使用 3 副本。
- 8 个 worker 做存储节点时仍然是 3 副本，不是 8 副本。
- 默认数据目录是 `/var/lib/longhorn`。
- 三副本意味着业务数据的磁盘占用大约是原始数据的三倍。
- 如需独立数据盘，安装前修改 `LONGHORN_DATA_PATH`，不要在产生业务数据后直接改路径。

## 六、镜像更新

新版本镜像建议使用不可变 tag，例如：

```text
clawmanager.local/openclaw-lite:2026.8.31
clawmanager.local/hermes-lite:2026.8.31
clawmanager.local/clawmanager:2026.8.31
```

更新时只修改 `images/images.lock`、准备精确 RepoTag 并重新制包。安装器不接受
`imagePullPolicy: Always`，离线清单统一使用 `IfNotPresent`。

镜像名可以是 `agentruntimename:2026.8.31` 这种短名称，也可以带私有仓库地址；无论
哪一种，都必须有明确 tag。YAML 和 Runtime 数据库映射由稳定键自动生成，不能依赖镜像
创建时间猜选“最新”版本。

## 七、版本升级和架构边界

- 当前脚本负责全新离线安装和同一离线包的失败续跑，不自动升级已有 Kubernetes。
- 即使已经人工确认旧数据可删除，安装器仍不会自动清理来源不明的旧 Kubernetes；
  旧环境清理必须作为单独、明确授权的一次性操作完成。
- 修改 Kubernetes、Calico、Longhorn 任一版本后应同时核对兼容矩阵、deb 版本、镜像
  和 YAML，再修改 `BUNDLE_ID`。旧集群遇到不同 bundle id 会停止，避免误升级。
- Ubuntu 22.04 与 Ubuntu 24.04 使用两个独立 amd64 总包；不得跨版本安装。
- 目录和配置保留了 `TARGET_ARCH`，但增加 arm64 前必须分别准备 arm64 deb 与多架构
  镜像，并新增对应架构的实机验收，不能直接复用 amd64 总包。
