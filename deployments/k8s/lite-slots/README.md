# Lite container slots

此目录用于新 Lite 模式的显式启用。默认 `RUNTIME_CONTAINER_SLOTS=0` 继续使用原进程池；设置为 1–256 后，后端检查存储并初始化 OpenClaw、Hermes、OpenCode、DeepSeek Harness 四类池。每个 Pod 有一个协调 Agent 和 N 个固定容器槽位，空槽不启动 gateway。实例数据在 PVC；每个业务进程只挂载自己的目录及授权 Team 目录。

## 启用条件

- 使用同一 `container` 分支构建的后端和四个 `Dockerfile.lite` 镜像，构建方法见 AgentsRuntime 的 `docs/lite-images.md`。禁止把旧 Lite 镜像混入槽位池。
- PVC 已绑定且 PV、StorageClass、claim 身份一致。单节点可用有节点亲和性的本地 PV 或 Longhorn；集群存在两个及以上节点时，必须使用真实 Longhorn CSI + RWX，检查包括暂不可用节点。仅把 StorageClass 命名为 longhorn 不够。
- 节点支持 bubblewrap 需要的非特权 user namespace、PID namespace 和 seccomp。业务进程安装架构校验的 syscall 策略；可信 supervisor 的 SYS_PTRACE/KILL 用于监听端口归属检查及跨 UID 停机，业务进程没有这些能力。不能降级到直接执行 gateway。Linux 实机验收通过后再部署业务。
- 后端有节点/PV/PVC/StorageClass 只读权限，以及运行命名空间内 ConfigMap、Secret、Deployment 和 Pod 的必要权限；两个标准部署清单中的 runtime-manager 角色已补充相应规则。Hermes Web 还读取管理 Service 的 Endpoints。
- 保留现有 `RUNTIME_AGENT_CONTROL_TOKEN`、`RUNTIME_AGENT_REPORT_TOKEN` 配置。后端将其写入运行命名空间的 `clawmanager-lite-agent` Secret。槽位只有本地控制凭据，不注入报告凭据，也不挂载 ServiceAccount token。管理 HTTPS 使用独立 `clawmanager-lite-agent-tls` Secret，只有协调器挂载服务端证书及私钥，后端固定校验私有 CA 和服务身份，禁止明文、跳转和跳过证书验证。

先检查示例中的命名空间、PVC 和四个镜像引用，再以 strategic merge patch 合并 `backend-patch.example.yaml`。该文件不会单独创建一个完整后端 Deployment。不要直接使用占位镜像。

后端启动时保留现有槽位池副本数，不重启正在服务的 Pod。改变镜像、槽位数或管理凭据需要排空/受控升级，启动检查会拒绝直接覆盖。默认每个槽位 requests=100m/256Mi、limits=2CPU/4Gi，可通过示例中的四个环境变量调整；协调器 requests=100m/128Mi、limits=1CPU/512Mi。这些是调度预算，尚不是实测容量承诺。端口区间默认 20000–20299；OpenClaw/OpenCode 每槽占 3 个端口，其他类型每槽 1 个。配置数量不能超过端口预算。

## 容器运行时和旧集群策略

所有池使用专用 `clawmanager-lite-runtime` ServiceAccount，禁止自动挂载 API token。槽位监督进程需要 AppArmor `unconfined`、seccomp `Unconfined` 以及 `procMount: Unmasked`，才能在非特权 user namespace 内创建 bubblewrap 的挂载和独立 `/proc`；协调器保留默认 AppArmor/proc，使用 RuntimeDefault seccomp，默认 capabilities 全部删除，仅保留目录初始化需要的 CHOWN/DAC_OVERRIDE/FOWNER。业务 gateway 仍使用非 root 身份、空 capabilities、独立 PID/user/mount 命名空间和强制 syscall 策略，不继承监督进程上下文。新建的 `/proc` 内还会重新屏蔽 kcore/keys/timer_list 等固定敏感节点；`/sys` 不挂载。

在启用了 PodSecurityPolicy 的 Kubernetes 1.24 及以前集群，可审查并应用 `legacy-psp.yaml`，只向专用 ServiceAccount 授予该策略的 use 权限。不要修改现有全局策略或给业务授权宿主目录、宿主 PID/网络、privileged 或 SYS_ADMIN。现代集群不应应用已移除的 PSP API。旧版 API Server 还必须启用 `ProcMountType`；该操作需要按集群维护流程备份配置。

必须在实际容器检查 `/proc/self/mountinfo`。字段被 API 接受并不证明 runtime 已取消外层屏蔽：本次 Kubernetes 1.20.5 + Docker 24 实测仍保留 Docker 默认 proc 子挂载，bubblewrap 因而拒绝启动，生产切换必须等待适配层修复并通过 Linux gate。独立 PID `/proc` 也不代表所有内核全局信息隔离；应检查敏感 proc 节点不可读取或写入、`/sys` 不可见以及外层 PID/root/FD 不可达。

参考：[Kubernetes 1.20 proc 字段语义](https://raw.githubusercontent.com/kubernetes/kubernetes/v1.20.5/pkg/securitycontext/util.go)、[Docker 适配层](https://raw.githubusercontent.com/kubernetes/kubernetes/v1.20.5/pkg/kubelet/dockershim/security_context.go)。

## 从旧进程池迁移

现有 `openclaw-runtime` 等 Deployment 不会原地改成另一种协议。先通过现有生命周期流程停止旧实例并确认进程退出，保留 PVC 和实例数据，再在维护窗口替换旧池 Deployment 并启用槽位模式。启用前备份持久数据。不要直接编辑正在运行实例的目录、assignment 或 writer ledger 来绕过停机确认。

已有旧数据的运行根、runtime 类型目录及 Team 管理根必须属于可信管理 UID。槽位启动只初始化这些固定管理目录，不递归改动其他用户数据；实例准备过程用受目录描述符约束的文件操作。无法证明目录归属或路径安全时拒绝启动，需要管理员先审查旧目录权限。

## 配置、重试和迁移

- `lite-instance-<id>` ConfigMap 保存当前实例身份、generation、Pod UID、运行端点、非敏感请求和 `running/draining/stopped` 期望状态。
- `lite-instance-<id>-g<generation>` 不可变 Secret 保存业务环境变量；gateway 收到明确授权的业务环境和镜像白名单变量，不继承管理程序环境。
- 后端每 10 秒重新读取未完成操作的期望状态，以最多 8 个并发任务恢复中断的创建或排空操作；超时后续轮从后续实例继续，损坏配置单独报错，不阻塞其他实例。成功应用会记录 generation/Pod 身份，避免周期性重放所有健康实例。相同 generation 的配置、端点或 Pod UID 变更会被拒绝；停止状态不会被创建重试改回运行。
- 迁移必须先收到旧槽位匹配 gateway ID 的 `stopped + confirmed + unmounted` 回执，然后持久化源端 stopped，提交更高 generation 和目标 Pod 身份，再启动新槽位。已有调度器在健康报告通过后切换绑定。这里的 unmounted 是私有挂载命名空间随全部子进程退出而销毁，不是在容器中卸载整个 PVC。
- PVC 内隐藏的共享 writer ledger 及生命周期文件锁阻止陈旧配置复活。源端失联或 supervisor 重启后无法证明旧 writer 已退出，会保持隔离/阻塞状态；没有“超时后假设安全”的迁移。需要外部 fencing/人工确认的故障恢复暂不自动放行。原生升级写操作同样持有整个操作期间的实例写入锁，退出确认不明会留下持久 `upgrading` 隔离状态。

未开启网络隔离时，旧槽位配置共享 Pod 网络。开启 `RUNTIME_NETWORK_ISOLATION=true` 后，每次执行创建独立网络命名空间，包括 localhost 与抽象 Unix socket；本地转发器只能接入可信父进程的固定 broker，原生入口先验证当前实例凭据。配置缺失、租约过期、身份变化或清理不确定时拒绝运行/复用，不回退到共享网络。管理 HTTPS 仍需控制凭据及可信证书；槽位 Unix socket、管理账本和签名密钥不在业务挂载中。

业务 `/tmp` 绑定同一 PVC 上专属目录 `<WorkspaceRoot>/.clawmanager-instance-tmp/<runtime>/user-N/instance-N/tmp`，身份不随槽位、generation 或 Pod 改变。固定父目录由 supervisor 持有并保持 0711，不暴露给业务；仅实例叶目录以分配的 UID/GID、01777 权限挂载到 `/tmp`。主进程与辅助业务命令共用该目录，其他实例复用槽位时挂载各自的目录。符号链接、不可信归属或不安全权限会阻止启动，不回退到临时目录，也不递归修改已有文件。

`/run`、`/dev/shm` 每次启动仍创建新的 tmpfs。停止确认包含全部子进程退出，其私有挂载随之销毁，PVC 中的普通文件保留。迁移通过确认源端停止后在目标重新绑定同一实例目录实现；内存、活动 socket、PID、文件锁和连接需要重建。无实例目录授权的独立维护沙箱仍使用临时 `/tmp`。

升级前已运行的实例仍使用旧 tmpfs；需要保留其中的数据时，必须先导出再停止，不能依靠更新镜像找回已销毁的数据。跨节点仍强制 Longhorn RWX；单节点本地 PVC 的跨 Pod 测试不等于跨节点验收。

## 可选的网络与平台凭据隔离

该增强要求现代 Kubernetes/containerd 支持 Pod user namespace，配置 `RUNTIME_POD_USER_NAMESPACES=true`，并正确设置 `RUNTIME_NETWORK_POD_CIDRS` 及实际 Service CIDR。可信槽位监督进程在 Pod user namespace 内管理网络所需能力；业务进程和 relay helper 的有效 capabilities 为空。旧 PSP 示例不能直接作为此模式的部署方案。这仍然共享宿主内核，不等同于虚拟机隔离。

平台 control/report/signing、模型上游、Browser Basic、Team Redis 管理凭据保留在可信端。业务访问通过绑定 instance/user/generation/Pod UID 的短期授权及固定本地代理完成；原生启动前清理已确认的 CM 生成配置中的旧平台凭据，保留独立用户 provider 配置。不能据此声称已删除任意用户复制的历史密钥。Team Redis 必须通过当前集群内目标校验；曾经暴露到公网的旧入口和密钥仍需单独关闭或轮换。

`RUNTIME_CONTAINER_SLOTS`、`RUNTIME_POD_CPU_REQUEST/LIMIT`、`RUNTIME_POD_MEMORY_REQUEST/LIMIT` 及四个 `RUNTIME_SLOT_*` 参数均可配置，8 槽位和 4 核 16Gi 只是示例。Pod 总上限与容器上限同时生效；设置每槽 requests=1Gi、limits=4Gi 不会消除单槽超过 4Gi 时的 OOM，也不能把 requests 解释为随时可用的内存保证。保留协调器和负载波动预算。

隔离模式的短期权限校验需要多次读取 Kubernetes 身份和策略。后端 Kubernetes 客户端默认使用 100 QPS / 200 burst，可通过 `K8S_API_QPS`、`K8S_API_BURST` 或 `kubernetes.apiQPS/apiBurst` 调整；其他模式未配置时保留 client-go 默认值。该配置只调整客户端排队预算，2 秒校验期限、实时校验和失败即拒绝保持不变。按实际并发连接数和 API Server 容量压测后调整，默认值不代表海量部署容量承诺。

启动检查不会原地改变已有池的安全配置。切换网络隔离、镜像或槽位配置仍需走前述停止确认及受控升级。OpenClaw 停止维护使用无外网的独立网络和操作范围；清理不确定时保留隔离标记。

## Runtime 手动扩缩容

维护页面按实例类型调整当前默认 Deployment 的副本数，槽位和资源沿用该 Deployment；不再创建独立 Deployment 或接受资源覆盖参数。历史自建池保留只读列表及确认清空后的删除入口。

启用缩容前，在支持 `admissionregistration.k8s.io/v1` ValidatingAdmissionPolicy 的集群中应用：

```sh
kubectl apply -f deployments/k8s/lite-slots/runtime-scale-in-guard.yaml
```

后端 ServiceAccount 需要现有的 Pod get/list/patch/delete 权限；delete 用于服务器端 dry-run 验证。策略仅拦截带 `clawmanager.io/scale-in-protected` 标签的 Pod 删除。工作任务会先验证真实拒绝回执，再迁移实例；保护不可用时停止执行。删除成本只辅助 ReplicaSet 选择已清空的 Pod。

缩容预览展示所选 Pod、节点、实例迁移和副本变化，确认时重新校验指纹。至少保留一个默认 Pod；兼容容量不足、迁移失败或源端停止无法确认时不降低副本数。任务在调度记录中持续更新，删除已开始后会保持保护直到指定源 Pod 的 UID 确认消失。不要手动移除活动任务的保护标签。

## 验收

开发工作区是 macOS；镜像构建和 Linux 验收在目标节点执行。实际构建、Longhorn 文件系统测试及部署证据记录在发布工作区。离线 Go 测试、交叉构建和 Dockerfile 合约检查不能替代以下验收：

1. 构建四类 amd64/arm64 镜像并运行 AgentsRuntime 提供的 smoke；逐项验证 Web、终端、文件、插件、浏览器、Team、升级和模型调用。
2. 在实际 Pod security context 运行 sandbox Linux gate，包括非 root 和 `--supervisor` 两种模式；验证相邻目录不可见、管理凭据不可见、所有子进程退出、优雅停止及强制停止。
3. 两个节点使用 Longhorn RWX 做 A→B→A 迁移，并制造停止超时、断网、Pod 重启、延迟旧请求、目标启动失败，确认没有双 writer，路由只在目标健康后切换。
4. 验证文件锁/原子 rename/fsync 在所用 Longhorn RWX 配置下的实际语义，尤其 NFS 故障和重连；异常时必须保持阻塞。
5. 测量四类镜像体积、每槽空闲/负载 CPU、内存、I/O、启动和迁移时间，依据结果调整槽位数和资源预算。
