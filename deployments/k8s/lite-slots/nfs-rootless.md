# NFS 多槽位部署与迁移

`nfs-rootless-v1` 保留共享 NFS PVC 和一个 Pod 内的多个槽位容器。每个运行实例有独立的 localhost、文件挂载和业务身份；同实例终端、工具及辅助命令加入同一执行网络。每个槽位只承载一个实例。默认配置不变；必须同时更新后端与四类 Runtime 镜像，再显式启用。

## 部署前提

- 配置 `RUNTIME_CONTAINER_SLOTS>0`、`RUNTIME_NETWORK_ISOLATION=true`、`RUNTIME_POD_USER_NAMESPACES=false` 和真实 `RUNTIME_NETWORK_POD_CIDRS`。control token 至少32字节，保留现有 report token 和管理 HTTPS CA/证书校验。可参考 [配置补丁](backend-nfs-patch.example.yaml)，替换全部占位值后合并，不能直接 apply 成完整部署。
- PVC/PV 必须真实绑定且可写，访问模式为 RWX、卷模式为 Filesystem。支持静态直接 NFS、`nfs.csi.k8s.io` 和 `driver.longhorn.io` 的 NFS RWX；CSI 的 StorageClass provisioner 必须匹配。直接 NFS/NFS CSI 的 server 必须是固定的 IPv4/IPv6 字面地址，避免 DNS 变化使内核客户端和出口拒绝规则指向不同服务器。卷、claim UID、StorageClass 或 provider 不一致会拒绝启动。
- 节点提供非特权 user/mount/net namespace、支持映射 FD 的 bubblewrap、iproute2、nftables 和强制 seccomp。节点 AppArmor 策略须允许受信任的 bubblewrap 路径。实际策略不支持时保持启动失败。CM 读取私有原生会话还要求 Landlock ABI≥3 和镜像内固定 `/usr/bin/sqlite3`。
- 后端与 Runtime 可信程序需要 SETUID/SETGID，以执行固定的实例身份辅助程序。Runtime 使用普通 Pod（`hostUsers:true`），不共享容器 PID namespace，保留默认 masked proc。固定 OCI root 引导只持 SETUID/SETGID，立即切换为10001后执行 tini/agent。槽位不使用外层 SYS_ADMIN/NET_ADMIN/KILL/SYS_PTRACE；业务及 relay 有效 capabilities 为空。仅本地 emptyDir/TLS 初始化有 CHOWN 等本地能力，完全不挂载 NFS。
- RBAC 除现有 Pod/Deployment/ConfigMap/Secret/存储读取权限，还需要运行命名空间的 StatefulSet 和 ReplicaSet get/list/watch/create/update/patch/delete。标准 k3s 部署清单已补充 apps 资源；自建角色也需检查。
- 管理 TLS 源 Secret 只交给本地 init，复制到管理专用内存 emptyDir 并设为10001只读。业务槽位没有后端 report/upstream/管理证书凭据，不挂载 ServiceAccount token。

## NFS 身份与已有数据

export 保持 `root_squash`。存储管理员需预先提供属于10001的管理根，且禁止 group/other 写入；推荐为本配置单独提供受管子目录。客户端 UID0 被 squash 后不能替代管理身份。运行时不会递归 chown 现有卷，也不会为了通过检查扩大已有文件权限。

实例 UID/GID 为 `200000 + instanceID`，不随槽位、generation 或 Pod 改变。新实例叶目录由10001持有，primary GID 为实例 GID，权限0770；持久 tmp 叶目录为01770。workspace/home/tmp 内既有数据保留其所有者和权限，包括0600文件。管理祖先不进入业务挂载；业务只看当前实例叶目录、自己的持久 tmp 和经过授权的 Team 目录。A 退出后数据保留；B 复用该槽位只挂 B 的目录；A 再迁入重新得到 A 的数据。`/run`、`/dev/shm`、连接、进程与内存状态在执行重建时重新创建。

私有数据不能仅凭“位于本实例目录”获得信任。原生准备、启动及维护会只读核对实际私有 inode 身份，后端文件读取与技能接口也核对实际打开的文件。历史/导入数据中属于其他实例的硬链接或单链接文件会被拒绝，同实例自己的正常硬链接保留；已授权 Team 共享目录单独处理。检查不自动删除文件或改所有者，发现不一致需先由存储管理员审查。

启动/维护的目录核对需要读取私有目录树的元数据，开销随条目数量增长；不能为了跳过大项目而省略子目录。已有旧版本执行需经停止、重新准入后才具备这一检查保证。服务端管理员和管理祖先必须可信，检查不防御恶意 NFS 管理员在运行中篡改数据。

Team GID 是显式附加组，不替换实例 primary GID。若 NFS 服务端启用了 `manage-gids`，必须通过服务端 NSS/受审查的 ACL 正确配置这些实例 UID 的 Team 组成员身份；客户端 supplementalGroups 本身不保证服务端接受。测试中用临时高 UID/nologin 用户验证了该条件并在测试后删除。不要关闭服务端组校验来绕过失败。现有实例数据归属不可信、硬链接/符号链接异常或祖先可写时，先由存储管理员审查，自动路径保持拒绝。

业务网络不能访问 NFS/RPC 服务：绑定卷的服务器字面地址被加入所有端口的拒绝列表，集群 Pod、Service、控制地址和代理同样受保护。HTTP、DNS、模型和 Team Redis 通过固定 broker 校验授权；共享卷挂进了可信容器不代表业务拥有整个卷的目录或网络访问权。

## 停止、恢复和写入追踪

停止单个实例会结束对应槽位容器，由 kubelet 单独重启该容器。CM 先持久化包含 operation、worker epoch、instance、generation、slot、Pod UID 的停止意图；旧容器写出绑定意图的终止回执。只有 Kubernetes 实际 Terminated 状态中的旧 ContainerID、回执及意图完全匹配，且外部资源清理和 NFS writer 锁对账完成后，才能确认停止及释放。

新容器在对账前保持隔离。主 PID 退出、restartCount 增加、响应超时、Pod 消失或租约过期都不能代替证明。Kubernetes 的容器重启退避可能使一连串原生维护操作等待数分钟；不能通过清账本、清 stop intent、删除保护 finalizer 或换 operation 绕过等待。

单槽位重置期间，协调器自己的就绪检查不等待该槽位锁。已有相邻实例使用精确分配槽位、Pod UID、镜像及授权的实时校验继续服务；不把整个 Pod 的临时 Pending 状态当作相邻实例失去授权。普通新实例放置仍受池容量和就绪检查约束。

后端私有文件操作、Team 文件写入、原生升级和浏览器都有持久写入记录。没有收到可信完成回执的记录在后端重启后继续阻塞删除、替换或维护；NFS EIO/ESTALE/取消等不确定结果不会因为辅助进程已退出就被视为完成。Team 共享写入不会要求停止无关的正常 Team 同伴，但不确定的共享写入必须先解决才能改成员身份或接纳替代执行。

后端的私有文件和原生历史读取由固定 Go 辅助程序执行，环境不包含后端管理凭据。历史读取额外以 Landlock 逐个授予已验证的实例所有、单链接常规 inode；不能通过把 B 的可读文件硬链接到 A 或在检查后替换路径读取 B。

## 浏览器与旧部署迁移

NFS 浏览器每个执行 epoch 使用独立名称、OnDelete 的单副本 StatefulSet，Pod 创建时就带保护 finalizer。停止后保留已证实全部容器终止、带删除时间的旧 Pod 及零副本旧控制器，占住旧名称，阻止超时后才落地的旧创建请求启动 writer。新 epoch 使用新控制器和新名称；每次操作都核对旧对象 UID、finalizer 及容器终止摘要。浏览器资料保存在 `browsers/user-<userID>/instance-<instanceID>/{profile,downloads}`；业务自己的私有 home 权限不变。镜像初始化使用本地 `/config`，只把两个准确资料目录绑定到 Chromium/Downloads。

这些终止对象不能自动清理。它们不再运行进程，测试集群也确认不占终止后的调度 CPU/内存预算，但仍计入 API 对象数量配额并消耗 etcd/SQL 空间。64KiB 注册记录上限达到时保持拒绝，不截断历史。部署应预算对象数量；只有外部可靠 fencing 能证明所有旧创建请求都不可能执行后，才能设计受审查的回收流程。当前没有自动回收或手动清记录的旁路。

旧 Deployment/ReplicaSet 的历史创建请求可能在超时后才落地。即使当前模板有 finalizer、observedGeneration 已更新且最后一次列举没有 Pod，也不能证明这类请求不会再启动 writer。因此不自动接管这类历史控制器并宣布停止；保留已知身份记录，等待外部可靠 fencing 后另行迁移。当前实现不提供通过普通“已确认”布尔值跳过该限制的入口。实际 Pod 身份缺失、已被删除且没有终止证据、或出现未对账的新 writer，同样保持阻塞。

已离线导入、没有历史控制器暴露的新受管资料可按准确目录整体迁移到新布局。该文件转换和新 StatefulSet 的正常启停都可自动执行；现有历史 Deployment 的原地切换是单独的发布前提，不能用这两项测试代替。

## 启用与回退

1. 在测试集群构建同一批次后端和四类镜像，记录实际 digest；使用专用命名空间、PVC 和控制服务运行验收。不要在资源不足的本地构建。此实现的开发验收均在81.54节点执行，未切换现有业务池。
2. 备份数据库、实例数据、Team 数据及浏览器资料。数据库迁移099/100由后端启动机制执行，新增写入追踪表不使用级联删除。原集群恢复不能清除未知 writer 的证明和记录；系统备份对这些活执行协调表采用引用元数据策略，不把旧安装的记录复制到另一安装或重放执行。跨安装启动仍需要可靠的源端停止或外部 fencing。
3. 在实际目标 NFS/节点策略下验证权限、文件锁、rename/fsync、四类 Runtime 启停、双实例并行及故障封锁；双节点部署还要完成真实双节点 A→B→A。单节点测试不能替代此项。
4. 停止旧实例并取得旧执行全部退出的证明，再通过受控流程排空旧池并建立新配置池。不要只修改现有 Deployment 的 profile、hostUsers 或镜像。启动检查会拒绝不兼容的原地覆盖。
5. 按原有实例生命周期启动并逐项验证。回退同样先排空并确认停止；保留持久数据和证明，使用兼容旧配置的镜像与数据恢复方案。不要在新 writer 活动期间直接启动旧池。

本次测试环境只有一个节点。跨节点迁移、实际生产 NFS 故障重连、arm64、容量及真实模型/Team 消息链路仍需各自环境的发布验收。测试控制服务只提供注册/租约等最小协议，模型上游返回403；它不证明真实模型调用成功。
