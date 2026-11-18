# NFS 多槽位实例隔离方案与验收条件

日期：2026-11-05（Asia/Shanghai）

状态：讨论确认按此方向继续；所有安全条件通过验收后才可视为可交付。目前仅完成底层机制实验，未修改业务代码，未批准或执行业务部署。

代码基线：ClawManager `b8c53ff`、AgentsRuntime `6a57658`，两仓库均为本地 `mount` 分支。

## 1. 已确认的要求

1. 保留 NFS，保留一个 Runtime Pod 内的多个实例槽位。每个槽位仍对应独立容器，同一时刻最多承载一个实例执行。
2. 保留实例之间的文件、进程、网络及平台管理凭据隔离；不以关闭网络隔离作为兼容方式。
3. 每个实例执行使用独立 net namespace，localhost 独立。同一实例执行的主进程、终端和辅助命令加入同一个网络空间，不是每条命令各建一个互不连通的网络。
4. 实例数据随实例身份保留；槽位复用不得使后来的实例继承、读取或改写前一实例的数据。相同用户的不同实例也必须隔离，明确授权的 Team 共享目录除外。
5. 单个槽位可独立重置，其他槽位的容器不应因此重启；入口就绪与路由变化还须验证，不能只凭其他进程仍在运行就宣称请求无影响。
6. 清理、旧执行停止或存储身份无法确认时，禁止迁移、释放 writer 或把槽位分配给新实例。

本轮仍处于设计与验证阶段，不进入业务代码实现。

## 2. 已确认的根因

测试节点为 `10.130.81.54`，Kubernetes 1.35.0、Linux 6.8.0-71-generic、containerd 2.4.2、runc 1.5.2。

- 普通 Pod 挂载 NFSv4.1 并以非 root 身份读写成功。
- 同一个 NFS PVC 配合 `hostUsers: false` 时，在 OCI 容器初始化阶段报 `failed to set MOUNT_ATTR_IDMAP ... invalid argument`，尚未进入业务程序。
- Pod user namespace 使用本地 ext4 卷时成功。
- 普通 Pod 中 bwrap 绑定 NFS 目录成功；创建新的 PID namespace 并挂载新 `/proc` 时失败。
- Kubernetes 服务端拒绝 `hostUsers: true` 配合 `procMount: Unmasked`。
- bwrap 0.8.0 的 `--disable-userns` 需要写 `/proc/sys/user/max_user_namespaces`，在普通 Pod 的只读 proc 掩码下失败。
- NFS export 启用 root_squash 时，容器 root 对实例目录执行 `chown` 被拒绝。这与 idmapped mount 失败属于不同问题。

对应代码：

- `backend/internal/services/k8s/runtime_slots.go`：Pod user namespace、Unmasked proc 和槽位 capabilities 配置，以及存储准入检查。
- AgentsRuntime `clawmanager-agent/sandbox/sandbox.go`：嵌套 PID/user namespace、新 proc 和 `--disable-userns`。
- AgentsRuntime `clawmanager-agent/gateway/workspace.go`、`slots/persistent_tmp_unix.go`：目录归属初始化和校验。

现有业务测试卷实际为本地 ext4 上的 hostPath，不是 NFS。代码认可 Longhorn 的 provider 身份也不能证明当前隔离方案支持 Longhorn RWX：常规 Longhorn RWX 向客户端提供的是 NFS。

## 3. 方案选择与明确变化

优先方向：普通 NFS Pod，业务使用 rootless user/mount/net namespace，复用槽位容器的 PID 隔离，以槽位容器销毁作为最终进程清理边界。

另一方向是节点级受控沙箱服务，由节点组件创建和管理独立的业务 PID/proc 等 namespace。它适合必须继续隐藏本槽 supervisor 进程条目，或不能接受容器重启周转成本的场景，但引入节点组件与更大的管理权限边界，当前未验证。

优先方向有以下变化，不能隐含视为与原实现完全等价：

- Pod 不再启用触发 NFS idmapped mount 的 UID 重映射，业务进程仍保留独立 user namespace、非特权身份、空 capabilities 和强制 seccomp。可信管理进程失去原 Pod user namespace 的宿主 UID 重映射保护，需审查其权限与输入边界。
- 业务与本槽 supervisor 位于同一个容器 PID namespace。业务可能看到 supervisor 的进程条目；必须不能读取其环境、FD、根目录和控制凭据，不能控制它，也不能看到其他槽位的进程。
- 槽位释放或执行替换以旧槽位容器终止作为最终清理确认。需处理 kubelet 重启退避和 Pod Ready 状态变化，不能承诺立即周转。

如果上述明确边界不能满足最终安全要求，应改选另一方向，不得静默降低要求。

## 4. 文件与 NFS 身份

NFS 共享根目录只提供给可信管理程序。业务沙箱只绑定当前实例目录、实例持久化 tmp 以及明确授权的 Team 目录；不得绑定整个 PVC 根、运行时根或用户根。

实例 workspace 及 tmp 的身份由 runtime/user/instance 派生，不依赖 slot、Pod 或 generation。目录路径来自可信实例绑定，业务不能自选 source、UID、GID 或 Team 授权。

候选权限方式是固定管理 UID 持有管理目录，实例叶目录按独立实例 GID 授权，不依赖容器 root 在 root_squash NFS 上任意 chown。管理父目录不允许业务修改；业务不持有管理 GID 或其他实例 GID。已有数据的权限迁移、home、Team 和持久化 tmp 兼容性仍须验证，不能递归修改现有用户数据来绕过检查。

挂载 source 的祖先保持管理方控制，使用目录描述符约束检查和访问，防止校验后发生路径替换。符号链接、非预期硬链接、嵌套挂载和旧数据不能被当作可信输入。

`..` 和软链接只能在沙箱可见的文件系统中解析，不能沿 bind mount 自动取得 NFS source 的父目录。还须同时做到：

- 启动时不继承管理根目录 FD、无关连接或凭据，工作目录位于授权目录内。
- 阻止借 `/proc/<pid>/root`、`fd`、`environ`、namespace FD 等重新取得管理面的访问能力。
- 禁止业务挂载、重新进入管理 namespace、改变身份或绕过文件权限。
- 文件浏览、下载、上传、解压、升级和备份等管理接口同样按实例授权并限制路径；不能只保护业务进程。
- 业务网络及出口代理不得允许直接访问 NFS/RPC 存储端点，包括 IPv4、IPv6、解析别名及代理绕过。

## 5. 独立网络与受控访问

网络 namespace 按实例执行创建和持有，绑定实例、generation、Pod UID、slot、container ID 与执行 epoch。主进程、终端及辅助进程加入同一执行的网络；槽位换实例或执行被替换后创建新网络，拒绝旧请求重入。

namespace 使用可信进程创建并固定的 FD。目标 net namespace 的 owning user namespace 可能不同于业务进程当前 user namespace；实验通过 `NS_GET_USERNS` 获取对应关系后成功进入。不能只拼接业务可控 PID 路径，也不能假定两者总是相同。

持有 namespace 或转发通道 FD 的辅助进程同样属于可信边界。如果辅助进程与业务映射到相同外层 UID，必须另行防止业务读取其 FD、内存或复用其通道，不能仅凭 UID 相同的进程间默认权限认定安全。所用独立身份或 dumpability、seccomp、FD 关闭策略必须实测，业务启动不得继承这些控制 FD。

保留现有 broker 的身份校验、短期授权和失败拒绝语义，适配到 rootless 网络的传输方式。固定入口转发机制已经验证；以下完整功能尚未验证：

- HTTP/WebSocket 入口、健康检查、终端、浏览器、模型和 Team Redis。
- DNS、透明 TCP、IPv6，以及现行策略允许的其他协议。
- 旧执行权限撤销、代理断连、namespace 重建后的旧请求拒绝。

不能只设置 `HTTP_PROXY` 就认为所有出口受控，也不能用独立网络中暂时没有出口，替代完整业务网络验收。业务无通往 Pod 管理网络的任意直通路径，平台上游密钥留在可信管理侧。

## 6. 停止、槽位重置与迁移

1. 可信管理方持久化停止意图，固定当前执行身份，禁止继续创建辅助工作。
2. 撤销新请求和入口授权，优雅停止业务并等待需要收尾的操作。超时不能视为已完成数据刷新。
3. 结束目标槽位的容器 PID 1，终止其 PID namespace 内的剩余进程；不删除整个共享 Pod。
4. 通过可信运行时状态核对指定旧容器已终止，完成外部辅助操作、namespace/mount FD、代理和连接的关闭确认。不能仅检查 restartCount 增加或新容器 ready。
5. 保留并核对执行账本、停止记录和 writer 状态。单次容器重启不会清除 emptyDir；新 supervisor 必须先完成恢复核对，不能自动覆盖旧的 running 记录。
6. 旧 writer 的停止与清理证明完成后，才允许提交新的执行绑定，建立新的 namespace 与挂载授权，最后发布新入口。

状态缺失、节点失联、NFS 故障、旧容器身份不明或清理结果不确定时维持阻塞。不能因租约计时到期或目标已启动就允许双 writer。

## 7. 数据保留与复用

| 数据 | 退出后的处理 | 后续实例的访问规则 |
| --- | --- | --- |
| 实例 workspace、home、配置 | 在 NFS 保留 | 仅对应实例可重新授权访问 |
| 实例持久化 tmp | 保留，身份不随槽位变化 | 其他实例使用各自目录 |
| Team 共享目录 | 按 Team 生命周期保留 | 按明确授权挂载 |
| 容器可写层 | 容器重建后重新初始化 | 不作为实例持久化存储 |
| emptyDir 内管理账本 | 跨容器重启保留 | 仅管理程序核对，不暴露给业务 |
| 进程、内存、连接及运行期 FD | 结束或关闭 | 不继承到新执行 |

停止和迁移不删除用户的持久化数据。删除实例的数据回收属于另一个明确授权操作。

## 8. 实验事实与证据范围

实验只使用 `clawmanager-nfs-lab` 内的临时 Pod 和专属测试目录。NFS 为单节点回环 NFSv4.1，export 保持 root_squash。镜像为已有 OpenClaw Lite digest `sha256:28524c52a76cc14abed91f80c66f9cc8c4c5bfca379068767f3a07eddd3ccd48`，运行诊断程序，不是新实现的完整 Runtime。

两槽位测试 Pod 未启用 Pod user namespace，容器仅给可信启动程序 SETUID/SETGID；没有宿主 SYS_ADMIN/NET_ADMIN，没有 privileged。外层 AppArmor/seccomp 为 Unconfined，业务通过 bwrap 安装与仓库 amd64 策略一致的 seccomp 过滤器。

| 实验 | 本次结果 | 不能据此宣称 |
| --- | --- | --- |
| 两个槽位同时监听 `127.0.0.1:18789` | 成功，net namespace 不同 | 完整 IPv6、UDP 或跨节点验证完成 |
| 访问 Pod localhost 的测试端口 | 被隔离 | 所有直接存储访问绕过已覆盖 |
| 可信辅助进程加入对应实例网络 | 成功，访问到自己的服务 | 终端与全部辅助业务链路已适配 |
| 固定入口通过受控 FD 转发到实例 | 成功 | 产品级鉴权和 WebSocket 验收完成 |
| 读取本槽 supervisor 的环境、FD、根目录及发送信号权限 | 拒绝 | supervisor 进程条目完全隐藏 |
| 业务 capabilities 与新 user namespace | CapEff 为零，seccomp 拒绝新 user namespace | 完整系统调用和逃逸验收完成 |
| 只终止业务主进程 | 后台写入仍继续 | 主进程退出等于停止完成 |
| 终止 slot-0 容器 PID 1 | 旧后台写入停止；slot-0 容器 ID 改变，restartCount 0→1；slot-1 容器 ID 不变且写入继续 | 服务入口完全无中断或 NFS 崩溃一致性已验证 |
| 管理 UID + 实例 GID 创建 NFS 叶目录 | 本实例可写、另一实例被拒绝，无需关闭 root_squash | 现有数据、Team、home、tmp 全部兼容 |

原始观察在本会话工具输出中，未形成可复用测试套件。新建诊断 Pod 和测试目录已清理，业务部署未改变。

## 9. 必须通过的验收

以下均未完成端到端验收；上节单项实验不能替代它们。

| 类别 | 必测条件 |
| --- | --- |
| 文件越界 | A→B 绝对路径、`..`、软链接、硬链接、目录替换、proc root/FD、继承 FD、管理文件 API 均不能越权 |
| 历史残留 | A 退出→B 迁入同槽→A 迁回；仅恢复各自数据，无旧身份、授权、挂载和连接继承 |
| 网络隔离 | IPv4/IPv6 localhost、同端口并存、抽象 Unix socket、跨实例连接、Pod 管理入口、直接 NFS/RPC 及代理绕过 |
| 实例内协作 | Web、终端、子进程、原生辅助服务加入正确网络，能相互访问，不能加入旧执行或其他实例网络 |
| 凭据隔离 | 环境、参数、文件、proc、FD、日志及代理接口不泄露平台凭据，不接受伪造实例身份 |
| 进程清理 | double-fork、setsid、后台写入、辅助命令和异常退出；旧容器结束与所有外部引用关闭后才确认 |
| 故障恢复 | supervisor/协调器/后端重启、状态更新丢失、旧请求延迟、NFS 断连与重连、节点失联、目标启动失败，始终无双 writer |
| 数据兼容 | 四种 Runtime、既有 workspace/home、持久化 tmp、Team 权限及升级；锁、原子 rename、fsync 和故障语义 |
| 其他槽位 | 单槽停止、重启、退避不重启其他槽位；连续请求测量就绪及路由影响，并核对共享资源压力 |
| 真实部署 | 在目标 NFS 配置下完成双节点 A→B→A 迁移；不能使用 StorageClass 名称或本地 hostPath 代替证明 |

先完成方案评审，再在隔离测试环境推进上述验收。未通过前不发布兼容声明，不切换业务池。

## 10. 参考

- [Kubernetes 1.35 user namespace 与 NFS 限制](https://v1-35.docs.kubernetes.io/docs/concepts/workloads/pods/user-namespaces/#filesystem-support)
- [Kubernetes 1.35 Unmasked proc 的约束](https://v1-35.docs.kubernetes.io/docs/tasks/configure-pod-container/security-context/#managing-access-to-the-proc-filesystem)
- [Kubernetes 卷及 emptyDir 生命周期](https://kubernetes.io/docs/concepts/storage/volumes/)
- [Linux network namespace](https://man7.org/linux/man-pages/man7/network_namespaces.7.html)
- [Linux PID namespace 的 PID 1 退出语义](https://man7.org/linux/man-pages/man7/pid_namespaces.7.html)
- [Linux 路径解析](https://man7.org/linux/man-pages/man7/path_resolution.7.html)
- [bubblewrap 0.8.0 源码](https://github.com/containers/bubblewrap/blob/v0.8.0/bubblewrap.c)
- [Longhorn 常规 RWX 的 NFS 架构](https://longhorn.io/docs/1.13.0/nodes-and-volumes/volumes/rwx-volumes/)
