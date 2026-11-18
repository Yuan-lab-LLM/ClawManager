# OpenClaw 历史回执只读核验

## 新增：损坏操作审计的独立审批与原件保留

默认普通恢复仍拒绝存在但损坏的 `operation.json`。当且仅当完整的签名 `prepared.json`、原维护目录物理身份及原队列/归档快照全部匹配时，可另行预检：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id" --repair-operation
```

返回专用摘要，绑定坏文件的原始字节哈希与物理元数据；不输出坏文件内容，也不把它重新解释成有效操作。确认全部管理器和维护进程已停止并完成外部隔离后，显式批准：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id" --repair-operation --apply --approval "坏审计保留专用摘要" --confirm-manager-stopped --confirm-maintenance-stopped --fence-reference "隔离工单" --reason "保留原件并从可信准备副本恢复"
```

该步骤只创建签名 `operation-repair.json` 授权审计，不替换原件、不解除维护保护。随后必须重新执行普通 `--resume` 预检，确认 `damaged_operation_preservation_required:true`，再用新摘要批准普通恢复。

恢复将原坏文件整体移入归档的 `damaged-operation.json`，保留原字节及物理身份，之后才从原签名准备副本独占写回缺失的操作记录。后续检查持续核对坏文件与授权快照；原件变化、签名/准备副本损坏、已有其他修复审计或无法归属的现场均拒绝。不会覆盖坏文件，不支持没有可信准备副本的“补签”。坏文件移动后恢复进程退出时，仍由签名恢复 owner 的受控归档及普通恢复继续完成，不能手动删除 `resuming`。

## 新增：双凭据轮换预检（尚不能执行迁移）

`scripts/plan-runtime-shutdown-credential-rotation.mjs` 只读核验原队列与两个凭据的审批绑定：

```sh
node scripts/plan-runtime-shutdown-credential-rotation.mjs
```

原环境沿用 `CLAWMANAGER_SHUTDOWN_OUTBOX_DIR/KEY/SCOPE`，新增 `CLAWMANAGER_SHUTDOWN_OUTBOX_TARGET_KEY`、`CLAWMANAGER_SHUTDOWN_OUTBOX_TARGET_SCOPE`。只接受同一完整 scope 的密钥轮换，源文件必须全部通过原签名及标准编码验证，目标命名空间与维护标记必须不存在。不合并已有目标队列，不改变原实例、所有者和代次。

输出 `source_approval_digest`、`target_approval_digest`，分别绑定完整原字节/身份快照、目标命名空间及两个凭据；不输出密钥、路径或原文件内容。明确返回 `executable:false` 和 `ROTATION_EXECUTOR_AND_DURABLE_HANDOFF_REQUIRED`。**目前没有发布目标队列、重签记录或迁移执行能力**；原签名留存、双边日志和中断恢复执行器仍需补齐。

不同端点返回 `CONTROL_PLANE_IDENTITY_REQUIRED`，不能因为两个服务上实例/用户数字 ID 一样就认定同一平台。端点迁移还需要可验证的平台身份及授权契约，不会猜测或自动跨平台发送历史记录。

## 新增：平台验签与 Kubernetes 隔离检查组件（尚未启用自动恢复）

- `backend/internal/runtimeevidence.CleanupVerifier` 独立核验原生回执签名、原占用原始字节摘要、命名空间和代次，拒绝错误密钥、重复 JSON 字段及非标准编码。`EvidenceSource` 必须读取受保护、固定的存储；`OriginalKeySource` 必须从服务端绑定解析原密钥版本。缺少任一提供者就失败，不回退到管理器布尔报告或新密钥。已加入 Node/Go JSON/HMAC 黄金值。
- 此处原密钥指生成原生回执时的 `CLAWMANAGER_LLM_API_KEY`（实例 Gateway 凭据），不是诊断 outbox 的 bootstrap/report token。原生命名空间绑定历史 canonical Gateway URL、该实例凭据与代次，不能用当前地址/新密钥代替，也不能混用诊断队列命名空间算法。
- 原版原生回执的 MAC **不包含 PodUID/PVCUID**，因此这些物理身份必须由可信存储适配器验证，不能仅靠请求中的 `Binding` 声明。组件当前只定义并校验这个适配契约，真实只读存储适配器、历史密钥解析及服务入口尚未接线。签名通过只返回历史 `native_cleanup_verified:true`，释放占用与生命周期授权仍为 false。
- `ReceiptWriterIsolationInspector` 固定 Deployment/PVC/PV/旧 Pod/旧 Node 的 UID，检查副本已归零且控制器观察到最新代次、独占 RWOP 存储及其原绑定、所有命名空间的 Pod 消费者、VolumeAttachment、HPA 和检查前后的资源版本。任何 Pod 消费者（包括 Pending、终止中或 Succeeded）、残留挂载或 UID/版本变化均拒绝。
- 检查前后必须由服务端 `WriterFenceAuthority` 独立验证同一隔离 epoch；该提供者负责实际准入隔离、旧节点/进程隔离和旧卷访问撤销。**没有默认成功实现**；缩容、Pod 删除、空列表或数据库屏障本身不能替代它。当前只有只读检查器及 fake-client 测试，尚未实现实际隔离提供者、控制器执行及其数据库/审计联动。结果不授予重启、复制或解除屏障权限。

以上新增组件不注册公共 API，不自动运行，不改变现有实例路径或协议开关，不宣称跨 Pod/跨卷 fencing 已完成。实际提供者接线前需明确目标存储类型和节点/存储隔离接口；不存在通用的“删 Pod 就保证旧节点不能继续写盘”的实现。

## 异常停机补送队列的受控隔离归档（2026-11-13）

`scripts/reconcile-runtime-shutdown-outbox.mjs` 支持一个明确的窄范围：将**一个原凭据/原作用域的整个诊断补送目录**保留原样移入本机归档区，解决坏记录持续阻塞这个补送目录的问题。它不是确认/删除已送达记录，也不只挑坏记录：该作用域内尚未发送的正常记录也一并归档，之后不会自动补送。执行前必须审阅这一取舍。没有网络请求，不修改数据库、用户权限、实例代次、原生回执、任务状态、端口或 writer/任务维护锁；所有结果均不授予生命周期操作或自动回放权限。

### 必须满足的前提

- 可信管理员负责操作，保留备份和工单；明确原 `AgentDataDir`、原凭据、原完整 scope，沿用下节三个 `CLAWMANAGER_SHUTDOWN_OUTBOX_*` 环境变量。密钥不得放命令行、共享日志或报告。
- **停止并隔离所有可能写入这个目录的管理器、自动重启控制器及其他维护进程**，提供外部隔离凭据编号。工具不访问 Kubernetes，不能替你确认隔离，也不根据 PID 或时间判断原生工具已停止。
- 运行时应升级到支持 `.maintenance-<namespace>` 的版本；旧版本不识别这个标记，必须由外部隔离保证不会并发写入。这个目录标记不是跨 Pod fencing，也不能隔离旧进程。
- 只支持管理器专用、可信单写入目录及同一文件系统内重命名；不支持共享可写工作区。生产目录/卷权限与同步语义需独立验收。

### 预检与执行

默认只读，不创建锁或目录：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs
```

预检返回本次 `approval_digest`、作用域哈希与文件数量，不输出原文件名、密钥或本地路径。摘要绑定原作用域、数据目录路径哈希及物理身份、队列物理身份、每个文件的名称/元数据/原始字节摘要；文件变化需重新预检。摘要不是平台权限凭据，只防止管理员执行了过期的本地计划。

确认隔离后显式执行（此处仅为命令示例）：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --apply --approval "预检摘要" --confirm-manager-stopped --fence-reference "隔离工单编号" --reason "人工核查后隔离诊断队列"
```

执行会独占创建 `shutdown-outbox/.maintenance-<namespace>`，同步保存带 HMAC 的 `operation.json`。新版 Pro/Lite 队列发现任何形式的这个标记时，拒绝读取、入队、确认删除和缺失队列重建；标记损坏或只是部分文件也不能绕过。检查器新增 `maintenance_present`，即便原队列已被移动也显示阻塞。

复验批准快照后，整体原子移动到 `AgentDataDir/shutdown-outbox-archive/<archive_id>/queue`，保留所有原文件，不修改或重签它们。在同一归档目录保留签名 `prepared.json` 和 `completed.json`，记录原作用域摘要、快照、隔离引用及原因。Linux 同步文件与相关目录。释放诊断维护标记前再次校验原位置没有新队列、归档字节/身份不变、审计完整以及维护标记仍属于本次操作。

成功返回 `action:review_before_manager_restart`，**不执行或批准重启**。管理员仍须检查原实例及其他任务/writer 保护，再按原部署流程处理管理器。诊断维护标记释放不等于原生停机保护被释放。新管理器后续可创建新的诊断队列，历史归档不会被自动读取或发送。

### 归档过程退出后的恢复

失败返回 `archive_id` 时保留该编号。若进程直接退出，可信管理员可以从仍保留的 `operation.json` 取得本次编号；不能按目录顺序猜测。继续确认原管理器和旧维护进程都已停止并隔离，使用原凭据/原 scope 只读预检：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id"
```

返回阶段为 `before_move`（包括归档根目录还未创建）、`after_move`、`before_unlock` 或 `already_quarantined`。恢复使用**新的专用摘要**，不能复用最初执行摘要：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id" --apply --approval "恢复预检摘要" --confirm-manager-stopped --confirm-maintenance-stopped --fence-reference "本次隔离凭据" --reason "恢复中断的诊断归档"
```

恢复不重签队列、不重新投递，不覆盖已有审计；已有审计必须先验证。已完成归档可幂等确认，且不操作后来新建的活动队列。两个同时恢复的进程不能共享 `resuming` 保护。新版恢复进程在 `resuming/owner.json` 保存签名身份；正常失败仅移除本次创建、身份与签名均未变的保护，外层诊断维护标记保留。恢复进程被强杀留下的保护不自动接管，只有符合下一节条件时才支持单独受控归档。

### 新版恢复保护的受控归档（新增）

新 `owner.json` 绑定随机 owner token、PID、原操作 MAC、原维护目录及恢复保护目录的物理身份、本次审批摘要，以及 Linux 主机/启动批次/PID namespace 指纹。不含明文凭据；签名使用原队列凭据、独立域。Windows 正常恢复及自己的保护清理仍可工作，但不支持遗留进程不存在的证明核验。

仅当 owner 签名和物理身份完整匹配、指纹与当前 Linux 环境完全相同，且 `kill(pid, 0)` **明确返回 ESRCH** 时，允许预检。活进程/复用 PID、EPERM/EACCES/未知错误、不同主机/boot/PID namespace、Windows 或旧版空保护一律拒绝。不按超时判断进程已经结束；这个判断只涉及维护恢复进程，不证明原生 Agent/工具子进程已停止，仍需管理员确认完整外部隔离。

先用原环境只读预检：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id" --retire-recovery-guard
```

返回专用 `approval_digest` 和 `owner_token`，与正常恢复摘要不能混用。完成管理员停止及隔离确认后执行：

```sh
node scripts/reconcile-runtime-shutdown-outbox.mjs --resume "archive_id" --retire-recovery-guard --owner-token "owner_token" --apply --approval "保护归档专用摘要" --confirm-manager-stopped --confirm-maintenance-stopped --fence-reference "隔离工单" --reason "原恢复进程已退出，保留保护证据"
```

执行前再次核验 PID 和全部证据，将整个保护目录移入该归档的 `retired-guards/<owner_token>`，保留原 owner，并保存签名 `retirement.json`。**外层诊断维护标记继续保留**，不移动或重放队列、不启动管理器、不清理任务/writer 锁；返回 `next_step:review_recovery` 后必须重新执行普通 `--resume` 预检和审批，才能继续原归档。

移动保护后、写处置审计前若进程退出，可用原 owner token 重做此处置的预检与执行，补上缺失的审计。已完成处置可幂等确认；部分/损坏审计拒绝覆盖。后续正常恢复重新验证整个签名保护历史，历史最多 32 个，达到上限不追加、不回收，也不移走当前保护。并发处置不能覆盖原 owner 或另一份已保存审计。

### 缺失操作记录的有证据恢复（新增）

如果 `operation.json` **不存在**，但归档中仍有完整有效的签名 `prepared.json`，且其中绑定的维护目录/数据目录/作用域/源或归档队列物理身份与当前现场全部匹配，可以正常执行 `--resume` 只读预检。结果增加 `operation_audit_restore_required:true`；执行仍要求新的恢复摘要、双重停止确认和外部隔离引用。

恢复会先保存签名 `operation-repair.json` 授权审计，再将原准备副本按原签名、原内容独占写回缺失的 `operation.json`，不会用新身份重签操作或队列。支持最初移动前、移动后，以及删除操作文件后尚未移除维护目录的实际退出临界区。恢复结果包含 `operation_audit_restored:true`。

默认普通恢复不适用于部分写入或签名损坏的操作文件；仅本文件顶部新增的独立 `--repair-operation` 审批流程可在有完整证据时保留原件后恢复。准备副本缺失/损坏、不同物理身份的空维护目录或未知保护仍不能认领。缺少完整绑定时继续保留现场；不能根据文件名/目录顺序猜测操作归属。

### 硬性限制与仍需保留现场的情况

- 每个源目录最多 1024 个普通文件、每个最多 2048 字节；不遍历子目录，不跟随符号链接，拒绝硬链接。签名操作审计最多 512 KiB。超过限制保留原位置，不做部分归档。
- 每个数据目录归档历史最多 256 项；上限计算包含未知项，不自动删除、压缩或回收旧证据。达到上限时只读新计划就拒绝。
- 原凭据缺失、作用域不符、原队列与归档在维护期间同时存在/同时缺失、内容/物理身份变化、审计部分写入/损坏、维护标记被替换，均拒绝继续。
- 旧版空/无签名 `resuming`、部分/损坏操作或保护文件、缺少完整签名准备副本或物理绑定的现场仍不能接管，须保留。新增恢复能力只覆盖上述有完整证据的范围；没有 `--force` 或按超时删除保护的选项。
- 尚不支持凭据轮换后把旧记录迁入新队列，也不支持所有者变更后的自动认领、归档自动回放或无原生清理证明的任务恢复。不能用本功能绕过发布门禁或任务保护。

## 停机补送队列只读检查（2026-11-13）

新增 `scripts/inspect-runtime-shutdown-outbox.mjs`，用于定位补送异常，不连接平台，不创建目录，不发送确认、不删除或重签文件，也不操作任务、进程、端口、绑定和 Kubernetes。应在停止该目录写入或取得管理员确认的一致副本后运行；文件系统读取可能更新访问时间，工具不是分布式锁。

通过受保护的维护环境注入以下值，**不要把密钥写入命令行、报告或仓库**：

| 环境变量 | 内容 |
| --- | --- |
| `CLAWMANAGER_SHUTDOWN_OUTBOX_DIR` | 原管理器 `AgentDataDir` 的绝对路径，不是某个哈希子目录，也不是工作区或整个文件系统根目录 |
| `CLAWMANAGER_SHUTDOWN_OUTBOX_KEY` | 生成记录时的原始凭据：Pro 为 bootstrap token，Lite 为 report token |
| `CLAWMANAGER_SHUTDOWN_OUTBOX_SCOPE` | 原管理器使用的完整作用域字符串，见下方；不猜测或归一化历史地址 |

Pro 的 scope 为去除末尾 `/` 的原 `ControlPlaneBaseURL` 加 `/pro/` 加原实例 ID；Lite 为去除末尾 `/` 的原 `BackendURL` 加 `/lite/` 加原 `RuntimeType`。原地址中如果有 API 路径，必须保留。该队列命名空间算法与任务回执命名空间不同，不混用配置。

```sh
node scripts/inspect-runtime-shutdown-outbox.mjs
```

输出固定为 `diagnostic_only:true`、`action:retain`、`lifecycle_authorized:false`。`verified_local_record` 仅表示本地签名及编码校验通过，**不表示已入库、当前所有者有效或当前实例已安全停止**；`server_acceptance:not_queried` 表示未查询服务端。其他状态：

| 状态/字段 | 含义 |
| --- | --- |
| `invalid_envelope` | 部分 JSON、字段缺失/未知或格式非法 |
| `authentication_failed` | 原凭据/作用域签名不匹配，不输出未验证事件 |
| `identity_mismatch` | 文件 ID、内容 ID 或事件摘要不一致 |
| `noncanonical_encoding` | 重复字段、额外空白或重排等非生产者标准编码；新运行时也会拒绝，保留原文件 |
| `unsafe_file` / `unknown_entry` | 链接、目录、大文件或非队列文件；不会跟随链接或读取未知名称 |
| `blocked_locally:true` | 所选队列有异常记录或超过 256 条，运行时会拒绝正常读写；不是自动修复许可 |
| `queue_at_capacity:true` | 已达 256 条；合法记录仍能补送/确认，但不能新增不同记录 |
| `other_namespace_ids` | 其他作用域目录的哈希清单，不验证、不读取、不导入这些目录内的记录 |
| `unknown_root_entries` | 非法根目录条目的名称哈希，避免直接输出敏感名称；不会自动处置 |

每次最多检查 256 个命名空间条目，以及所选队列 1024 个条目/每个 2048 字节。达到扫描上限或发现读取期间目录/文件变化时报错，不输出部分成功报告；1024 是诊断扫描上限，不提升运行时 256 条容量。错误仅输出固定代码，不含路径、系统异常或凭据。退出码：`0` 检查完成且所选队列无已检测阻塞（包括所选作用域不存在）；`2` 检查完成但本地队列阻塞；`1` 配置、扫描或读取失败。请同时检查 `scope_present`，退出码 0 不代表存在记录或所有旧作用域均健康。

两份 Go 队列现统一要求生产者标准编码，防止重复 JSON 字段被最后值覆盖规则解释；已有生产者生成的文件编码不变。入队重试重新同步命名空间、其父目录与 `AgentDataDir`，不要求同步数据目录之外的只读挂载。生产部署仍应预先配置管理器专用持久目录及其权限，实际卷目录同步/断电语义仍待验收。

检查器本身不提供任何 `--apply` 或 `--force` 参数；有限的整体隔离归档改用上节独立维护工具，不要据检查报告手工删除坏文件、旧命名空间或修改 MAC/所有者。凭据轮换迁移仍未实现，需要保留原凭据与原 scope 才能核验原记录。没有原凭据时仍保留现场，不能用新凭据重签成已验证历史。

## 平台停机观察历史（仅诊断）

管理端 OpenClaw 实例详情页新增“运行时停机观察（只读）”折叠面板，按平台接收时间倒序展示，每页最多 50 条，支持刷新最新记录和查看更早记录。默认接口为 `GET /api/v1/instances/:id/runtime-shutdown-audits`，更早记录使用 `before_id` 游标。实际 API 前缀沿用管理端实例接口配置。请求最长等待 15 秒，切换实例、页码或关闭面板时取消原请求，不将旧请求的结果写到新页面。

基线版本通过认证 Agent 状态和 gateway snapshot 提交结构化观察；新增持久化补送的版本使用下述独立批量接口。观察字段为原代次、原 writer owner 字节的 SHA-256 占用标识及下述结果；补送另外绑定原实例和原所有者。不会上传密钥、原生签名、文件路径或 Gateway URL。平台使用接收时间，不将 Agent 时间当作清理发生时间。

| 结果 | 含义 |
| --- | --- |
| `cleanup_unconfirmed` | 管理器无法确认原生清理完成 |
| `cleanup_confirmed_release_unconfirmed` | 管理器验签清理记录成功，但未观察到占用消失 |
| `cleanup_confirmed_release_observed` | 管理器验签清理记录成功，且当时观察到占用消失 |

这些是**受信管理器的历史观察，不是平台独立读取原回执验签的结果**。即使代次与当前实例相同，也不能证明现在无写入者、所有任务已取消或可以升级；不能用于归档工具的审批证明、强制解锁、释放端口或自动恢复任务。面板不提供这些操作。

迁移 `080_add_runtime_shutdown_audits.sql` 只新增诊断表，不回填旧日志、不更改实例或开启新协议。平台在事务内再次核对当前所有者、代次、用户有效性及 OpenClaw 类型；同一占用/结果/来源重复上报不会新增记录或刷新原接收时间。查询只允许所有者或管理员，且仅返回当前所有者对应的历史，所有者变更不会将旧所有者记录展示给新所有者。普通快照仍只接受当前代次；独立补送可以接收同一原所有者的过去代次，但只存历史诊断，不影响当前实例或任务状态。

没有记录不能视为安全：旧运行时没有结构化字段、未捕获原占用、尚未上报、在队列提交前退出或持久卷丢失都可能没有记录。新增队列覆盖已成功持久化的观察在管理器重启后的补送；不能追溯重建旧版本从未入队的记录，不能据此宣称全量停机审计完整。

数据库失败返回通用不可用错误，不将失败报告为接受，不泄漏数据库细节。只读面板失败显示“状态未知”，不回退为清理成功。平台记录失败不更改运行时停止、归档、任务取消或授权协议。

本地隔离数据库回归：`scripts/test-runtime-shutdown-mysql.ps1` 使用已缓存 MySQL 镜像、仅绑定本机随机端口、tmpfs 临时数据及专用空测试 schema；结束核对专用容器标签后删除该测试容器。不会连接现有业务数据库，不拉镜像、不更新 K8s。

### 持久化补送、确认与边界

- Pro 的独立接口为 `POST /api/v1/agent/shutdown-deliveries`，沿用实例 Agent 会话认证。旧缓存会话可选用只读 `GET /api/v1/agent/shutdown-deliveries/scope` 取得平台绑定的实例/所有者，不强制重新注册；旧服务端不支持时保留原状态上报兼容行为，不伪造原所有者。
- Lite 使用 `POST /api/v1/runtime-agent/shutdown-deliveries`，沿用受信 Runtime pool 报告凭据。此接口只写诊断表，不要求已删除 gateway 继续伪装为存活，不创建或修改绑定、实例、代次或任务。它不是 Pod 身份证明或跨 Pod 隔离授权。
- 事件携带原实例、原所有者、原代次、占用标识和结果，确定性 SHA-256 ID 绑定全部字段。服务端先校验整个批次（最多 32 条、64 KiB），持久化成功才返回 `version:1`、`diagnostic_only:true` 和 `accepted_ids`。未来代次、原所有者已变更或用户失效时不确认该记录；数据库异常不返回伪成功。确认丢失时重试，数据库唯一约束去重。
- Pro/Lite 在各自 `AgentDataDir/shutdown-outbox/<作用域摘要>` 保存文件，每个作用域最多 256 条。文件带本地 HMAC，绑定端点、凭据作用域及完整事件，不保存明文凭据；切换凭据/端点不会自动采用旧队列。此目录必须由单个受信管理器独占，不能位于租户可随意修改的共享工作区。
- 目录采用惰性初始化：只读查询不会创建目录，旧模式启动不强制要求新增目录可写。第一次实际入队才逐级验证并创建所需私有目录；不能创建或同步时保留待写防护，不回退为内存成功。
- 入队同步文件及 Linux 目录；已有同一记录的重试也重新同步文件。部分、篡改、未知、超限记录保留且阻塞，不按超时删除或覆盖。只有成功应用响应且明确确认属于本次发送的 ID 才能移出队列；空 200、裸 JSON、错误 envelope、错误版本、重复/外来确认 ID 和重定向均不能清理记录。
- Lite 在原生清理/释放核验成立后、删除 gateway 和释放管理器端口之前入队。写入失败保留绑定/端口及可重试状态；网络确认不在停止临界路径中等待。Pro 固定子进程启动时的所有者，在清理观察生成时入队，保存失败保留待写观察；重复停止/重启不能绕过待写防护。
- 补送每 5 秒独立执行，不阻塞心跳或状态上报；批次轮换，未获确认的旧记录不会永久挡住其他记录。已绑定的新 Pro 通过队列发送历史，避免旧代次的 inline 观察阻塞当前状态上报；没有范围协商的旧服务端继续原 inline 路径。
- 该保证以成功提交的队列文件及持久化目录仍在为前提。跨 Pod 保留还要求实际为 AgentDataDir 配置持久卷；只有容器可写层或临时目录时不能保证 Pod 重建后仍有记录。Windows 不执行目录 fsync；隔离 Linux/tmpfs 测试也不代替真实 PVC/CSI 或断电验收。
- 凭据轮换、所有者改变、损坏/部分队列文件、达到容量上限及原始记录缺失仍需受控检查；不自动删除未知证据，不重签或猜测原凭据，不取消原运行时的占用保护。观察与其确认均不能代替原生回执或强制解锁审批。

可用 `scripts/test-runtime-shutdown-outbox-linux.ps1` 复现队列 Linux 回归：脚本先比较 Pro/Lite 四个队列源码/测试文件的哈希，交叉编译同一组件，在断网、只读、非 root 容器中运行同步、路径/权限和真实进程退出测试，仅挂载测试二进制，结束自动移除该二进制。不拉镜像、不构建应用镜像、不操作 K8s。

## 有签名清理证明的遗留 writer 离线归档

`scripts/reconcile-openclaw-writer.mjs` 仅处置“原生已确认清理，但在释放占用前中断”的目录：要求原占用记录完整、匹配的签名清理记录有效、没有遗留执行标记。不能处置无证明的崩溃、损坏记录或仍有执行不确定性的情况。

由管理员在受控维护环境执行。先在平台侧停止并隔离所有可能写入该目录的旧运行时，保留隔离凭据编号；工具不会根据 PID、时间或文件存在状态替你完成隔离，也不访问平台授权服务。显式确认是管理员提供的外部授权/隔离保证，不能把签名或持有旧 Key 当成当前实例授权。

通过上述环境变量注入目录和原来源凭据；命令行不接受密钥。以下代次 7 仅为示例，必须改成原占用的真实代次。

```powershell
node scripts/reconcile-openclaw-writer.mjs --source-generation 7
```

默认只读，返回 `approval_digest`，不允许仅凭该结果自动接管。确认隔离仍有效、预检内容未变后，才可执行：

```powershell
node scripts/reconcile-openclaw-writer.mjs --source-generation 7 --apply --approval "预检返回的64位摘要" --confirm-runtime-stopped --fence-ref "平台隔离凭据编号" --reason "维护工单及确认说明"
```

工具在维护锁内复验，将原 `.task-writer.lock` 原子移入 `z-runtime-writer-archive/<archive_id>/writer`，保存签名 prepared/completed 审计，不删除原 owner 内容，不重放任务回执，不修改数据库。成功返回 `restart_required`；它不热恢复旧进程，下一次启动仍要独占准入。原签名清理记录留在原位置。

任何步骤中断可能留下维护阻塞和归档证据。看到 `maintenance_lock_retained:true` 时保留目录、记录 archive_id，**不要手工删维护锁或重跑普通 apply 猜测恢复**。使用下述受控恢复入口；它不是后台自动续作。工具不会自动解除外部隔离。实际 Linux/CSI 与断电语义仍待验收，新协议继续关闭。

### 归档中断后的预检与受控恢复

维持原运行时的外部隔离，并明确停止上一次维护进程；不能只看 PID 不存在就假定它及派生进程已停止。仍使用原代次与原签名 Key。

```powershell
# 只读检查：不解锁、不移动目录
node scripts/reconcile-openclaw-writer.mjs --source-generation 7 --resume "上次返回的archive_id"
```

预检返回原审批摘要、阶段和两个确认要求。确认原运行时及上一次维护均停止后：

```powershell
node scripts/reconcile-openclaw-writer.mjs --source-generation 7 --resume "archive_id" --apply --approval "恢复预检返回的64位摘要" --confirm-runtime-stopped --confirm-maintenance-stopped --fence-ref "当前有效的隔离凭据编号" --reason "恢复维护工单及确认说明"
```

支持审计尚未写全、移动前、已移动但完成记录缺失、完成记录已写但未解锁四个阶段。只补写缺失审计，原占用和停机签名均保留；重复确认已完成的同一归档不会再次移动。审计绑定原目录、维护锁身份和来源证据，签名不符、目录被替换、出现新占用或新增执行标记时拒绝。

每次恢复有独占 `writer-resuming` 保护及签名审计，正常失败只清理本进程自己的保护，外层维护锁保留，可重新预检后重试。最多保留 8 条恢复尝试，达到上限继续阻塞，不自动删历史。若恢复进程自身被强杀，普通恢复仍不自动接管；只有符合下一节完整证明条件时，才支持管理员显式归档保护。保护记录不完整、旧 version 1 操作不带维护锁身份时仍保留现场。不能通过修改操作版本或清除保护来绕过。

### writer 恢复进程被强杀后的保护处置（Linux）

新创建的 `writer-resuming/owner.json` 使用签名 version 2，绑定维护操作、随机 token、保护目录身份、PID，以及 Linux 主机/启动批次/PID 命名空间指纹。只有同一范围内 `kill(pid, 0)` 明确返回 `ESRCH` 时才允许处置。存活或复用 PID、权限不足、查询异常、跨主机/重启/命名空间均拒绝。Windows、不含这些字段的旧 owner、缺失或损坏签名均不支持，不进行自动升级。

这项探测只证明维护进程在该范围内不存在，**不证明原运行时及其派生工具已经停止**；仍必须由可信管理员维持外部隔离，确认原运行时、子工具及此前维护均停止。工具不验证 Kubernetes 隔离凭据真实性，也不会结束任何进程。

先只读获取本次保护处置专用摘要和 owner_token：

```sh
node scripts/reconcile-openclaw-writer.mjs --source-generation 7 --resume "archive_id" --retire-recovery-guard
```

核对后执行，不能复用普通恢复的摘要：

```sh
node scripts/reconcile-openclaw-writer.mjs --source-generation 7 --resume "archive_id" --retire-recovery-guard --owner-token "预检返回的owner_token" --apply --approval "本次处置专用摘要" --confirm-runtime-stopped --confirm-maintenance-stopped --fence-ref "有效隔离凭据编号" --reason "维护工单及确认说明"
```

执行前重新验证证据和进程不存在，将整个保护目录原子移至 `z-runtime-writer-archive/<archive_id>/retired-guards/<owner_token>`，保留原 owner 并写入签名 retirement.json。固定且非空的目标目录防止并发处置覆盖另一份保护。历史上限为 32 条，达到上限返回 `WRITER_GUARD_HISTORY_LIMIT`，不追加、不删除旧记录；这与普通恢复最多 8 次的限制分别计算。

**成功只移走内层恢复保护，外层维护锁仍保留**；不重放回执、不完成任务、不启动实例。返回 `next_step: review_recovery` 后，须重新执行上一节普通 `--resume` 只读预检，再以该预检摘要显式恢复。

如果保护已移动但尚未写处置审计就退出，原证据及外层维护锁继续保留。携带原 owner_token 可重新只读预检并显式补写缺失审计；原处置摘要未变时也可重试原命令。已写完整审计可幂等确认，部分或损坏审计不会被覆盖。后续普通恢复会检查全部处置历史签名，历史被篡改时继续阻塞。不得手工清除 owner、修改 PID/scope 或按时间强行解锁。

`restart_required` 表示回到受控启动流程，不会热恢复旧进程或清除现有管理器的内存停机防护；必要的管理器重新初始化仍须走原授权部署流程。

## 写入者占用提示

结果中的 `execution_barriers.writer_claim_present: true` 表示发现 `.task-writer.lock`，同时 `requires_manual_reconciliation` 为 true。即使 `pending: 0`，也不表示目录空闲、工具已停止或可以升级接管。

核验工具只检查该项是否存在，不解析其 owner 记录来判断进程存活，不跟随该项、不删除、不接管。占用是部分文件或损坏记录时仍保守提示。没有发现占用时省略新增字段，保持原结果兼容。

不要手工删锁来“恢复可用”；当前受控归档也会拒绝存在写入占用的目录。正常停机的原生清理证明及崩溃占用处置尚未接通；在这些链路完成并验收前，新任务协议继续关闭。

本工具仅帮助管理员对照“本地已签名回执”和“服务器已记录的任务状态”。它不是迁移、重放、取消任务或删除回执的工具。所有结果均为 `action: retain`、`archive_allowed: false`。

## 使用前提

- 部署包含 `POST /api/gateway/llm/runtime/tasks/inspect` 的 ClawManager 后端。
- 在有 Node.js 22 或更新版本的受控维护环境运行。必须能够读取目标实例的专用回执目录；不能指向整个 Lite 池根目录或用户工作区。
- 停止该目录的并发写入，或使用管理员确认的一致副本。工具不提供跨进程锁，也不能防御不可信用户持续替换父目录。停止写入不等于证明工具子进程已经停止。
- 明确原代次、原 API Base URL、原实例 Key 和当前实例身份。缺少原 Key 无法验证 HMAC，不要尝试修改文件或用新 Key 重签。
- 管理员负责确认历史资料归属。当前凭据只能查询当前实例/当前所有者名下记录，不能用其他用户的凭据绕过所有者变更。

## 环境变量

| 变量 | 含义 |
| --- | --- |
| `CLAWMANAGER_TASK_RECEIPT_DIR` | 该实例的专用目录，包含各命名空间哈希子目录；不是某个哈希目录本身 |
| `CLAWMANAGER_LLM_BASE_URL` | 当前 Gateway API 根地址，通常以 `/api/gateway/llm` 结尾 |
| `CLAWMANAGER_LLM_API_KEY` | 当前实例 Key，唯一会发送到当前后端的凭据 |
| `CLAWMANAGER_GATEWAY_GENERATION` | 当前运行代次 |
| `CLAWMANAGER_RECEIPT_SOURCE_BASE_URL` | 可选，生成历史回执时的原 Base URL；未设置则使用当前地址 |
| `CLAWMANAGER_RECEIPT_SOURCE_API_KEY` | 可选，生成历史回执时的原 Key；未设置则使用当前 Key，仅用于本地签名验证 |

通过受控环境/密钥注入提供凭据，不写入仓库、命令参数或共享日志。不要为了调用工具修改真实实例的代次。远程连接优先使用 HTTPS；HTTP 只用于可信内网。

在 ClawManager 仓库根目录执行（示例原代次为 5，必须替换为实际值）：

```sh
node scripts/inspect-openclaw-receipts.mjs --source-generation 5
```

单次默认最多扫描 256 个文件，可用 `--max-entries 1024` 提高上限；超过上限会报错，不输出部分成功结论。查询每批最多 32 条，默认单批超时 2.5 秒，总时限 30 秒。读取可能更新文件系统访问时间，但不写入文件内容或主动更改时间戳。

## 结果解释

| status | 含义与操作 |
| --- | --- |
| `completion_recorded` | 服务器已记录正常完成；仍保留文件，不代表工具清理证明 |
| `cancellation_recorded` | 服务器已记录取消确认且本地标记相符；仍不自动归档 |
| `cancellation_unconfirmed` | 本地取消或服务器撤权尚未对应取消确认；人工核查旧进程 |
| `server_task_unfinished` | 服务器任务未完成；不能推断任务仍在执行或已停止 |
| `server_record_missing` | 当前作用域下没有记录；不能生成完成回执补齐 |
| `receipt_state_conflict` | 本地普通完成与服务器取消确认不一致；保留证据核查 |

签名损坏、未知文件、内容冲突、源目录变化时，在查询前停止；老服务器不支持接口、当前身份失效或响应异常时也会退出失败。不会退回调用 finish。错误信息不输出密钥和路径，但正常报告包含 session/task ID，仍应按运维数据保护。

## 执行遗留标记与重启保护

配置专用持久目录的新版运行时，在授权通过后、原生操作调度前，将执行标记写入实例目录下的 `z-task-executions`。完成工具清理并保存已签名回执后，原进程才能移除自己的标记。它独立于回执的 Key/地址/代次命名空间，因此轮换凭据或重启不会跳过检查。

核验报告增加：

```json
"execution_barriers": {
  "pending": 1,
  "maintenance_locked": false,
  "requires_manual_reconciliation": true
}
```

这表示存在需要核查的条目，不代表已确认有一个仍在执行的工具。任何未知、部分写入或异常条目都视为不确定状态。工具只计数，不读取其内容作为完成证据；目录不安全或条目超限会停止检查。

运行时启动发现遗留标记，会拒绝新任务，暂停自动回执发送，诊断显示 `TASK_EXECUTION_RECOVERY_REQUIRED`。处理时应保留存储与日志，核查旧 Pod/进程及其子工具，并使用只读任务查询对照。**不要手工删除标记、替换空卷、修改代次或回滚旧镜像来绕过。** 即使服务器显示 finished，也不能直接判定本地工具已被清理。仅满足下述全部条件的场景可以使用受控归档工具。

限制：仅支持平台控制的专用目录和单写入进程；无持久目录时仍只有进程内保护，升级前的无标记任务无法追溯。Linux 要求文件与目录同步成功；Windows 本地测试不覆盖目录 fsync 和断电语义。此版本未做真实 Linux/CSI 验收。

## 已确认结束任务的受控归档

`scripts/reconcile-openclaw-executions.mjs` 只处理一种情况：**整个执行标记目录内的任务，均有原凭据验证通过的完整签名回执，且当前后端已记录完全相符的完成状态。** 正常完成要求 `finished=true, revoked=false, runtime_acknowledged=false`；取消完成要求本地 `cancelled=true` 且服务器的三项状态均为 true。

不满足条件的任务不支持强制解锁。缺少签名回执、未明确提供全部来源的混合凭据/代次标记、部分或未知文件、重复身份、服务器无记录或状态冲突，都必须保留。不得把 `--confirm-runtime-stopped` 当作跳过这些校验的开关。多来源目录必须使用下节显式多来源模式，不按文件名猜测原凭据。

维护前管理员必须：

1. 停止并核查该实例的运行时及其子工具，阻止自动重启和其他控制者启动同一目录的写入进程。工具不会代你操作 Kubernetes，也不能远程证明进程已经停止。
2. 确认使用本次包含维护锁支持的运行时版本、单写入的专用目录和一致数据，而不是共享工作区。维护锁是防护措施，不是跨 Pod 协调或隔离旧版本写入者的分布式锁。
3. 保留目录备份与工单记录，按上文配置原凭据和当前实例凭据；不可跨用户使用凭据。

先只读预检查（仍以原代次 5 为例）：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5
```

通过后返回 `eligible` 和 `approval_digest`。检查任务列表，再将本次摘要原样带入执行命令：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --approve <approval_digest> --confirm-runtime-stopped --reason <工单编号>
```

执行时会独占创建实例目录的 `.task-maintenance.lock`，重新检查签名、服务器状态及文件快照。摘要绑定当前身份、源代次、目录、文件标识/内容和状态；任何变化都要求重新预检查。通过后在锁内保存带 HMAC 的 `operation.json`，绑定归档 ID、目录、凭据作用域、代次、审批摘要及锁的文件系统身份，并在归档审计中保留该绑定。不会调用 finish、修改数据库或重签旧回执。

成功后，将整个 `z-task-executions` 原子移动至 `z-task-execution-archive/<archive_id>/z-task-executions`，保留原文件，并写入 `prepared.json`、`completed.json` 审计记录；回执文件不移动、不删除。同步后释放自己的维护锁。返回 `restart_required=true`：旧进程内的阻塞不会热解除，应在维护流程结束后启动/重启运行时，再验证授权和模型列表。此命令自身不会启动实例，也不启用新任务协议。

若校验阶段失败且尚未写入操作记录，工具仅释放自己创建的空锁。若已存在其他维护锁，绝不接管/删除。操作记录写入开始后失败，保留锁、原目录或归档副本及审计记录，返回 `maintenance_lock_retained` 与可用的 `archive_id`；不要直接删锁重试或覆盖归档，使用下述恢复预检查。核验报告的 `maintenance_locked=true` 在执行目录已移动时仍要求人工处理。

## 多代次与凭据轮换后的混合执行目录

同一个实例的 `z-task-executions` 可能含多个原代次、Base URL 或密钥命名空间。现在可以在**同一次整目录维护**中核验这些来源，但仍要求每个标记同时具备原签名回执和当前服务器相符的完成状态；不分批移除标记，不凭新 Key 重签或重放旧任务。

主来源仍使用 `--source-generation` 和前述原来源环境变量。额外来源通过受控环境变量 `CLAWMANAGER_RECEIPT_ADDITIONAL_SOURCES_JSON` 注入，并明确加上 `--additional-sources` 才会读取。该变量是非空 JSON 数组，每项恰有 `baseURL`、`apiKey`、`generation` 三个字段；必须显式提供每个来源的原值。最多 7 个额外来源（连主来源共 8 个），JSON 不超过 64 KiB。**它含原始密钥，不要放在命令参数、仓库、截图或日志中。** 不带开关时不会隐式启用多来源模式。

先只读预检查：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --additional-sources
```

结果增加 `source_count`、`source_generations`，不输出来源密钥、URL 或命名空间哈希。审查全部任务后，使用本次摘要执行：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --additional-sources --approve <approval_digest> --confirm-runtime-stopped --reason <工单编号>
```

安全边界：

- 原 Key 仅用于本地 HMAC 验证；所有网络请求只发往当前 Gateway，且只携带当前实例 Key。
- 每个标记绑定其原命名空间。不能拿其他来源中同名任务的回执来证明当前标记；跨凭据出现相同的完整任务身份时也保守拒绝。
- 主/额外来源重复、未来代次、未知字段、任一来源缺失/签名不符或任一任务未完成，都拒绝整批归档。
- 文件扫描预算按所有来源合计，默认 256；服务器查询沿用每批 32 条限制，多来源共享总查询时限，不给每个来源各开一个完整总时限。
- 审批摘要包含完整来源集合和逐标记证据。额外来源换序不改变摘要；添加、删除或更换来源需要重新预检查。执行前、服务器请求后及归档后仍做文件核验。
- 归档仍只移动执行标记目录，所有原代次回执保留原处。恢复命令必须继续提供同一主来源、同一额外来源集合和 `--additional-sources`，不能降为单来源恢复。

中断后示例：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --additional-sources --resume <archive_id>
```

后续显式恢复/遗留恢复保护处置也保留 `--additional-sources`。该功能不是跨用户、跨实例或跨 Pod 的迁移，不改任务数据库、回执内容、代次或密钥；缺失/不可信回执仍不允许解锁。现有单来源命令和签名操作记录格式保持兼容。

## 归档中断后的恢复

新版本支持完整操作记录下的以下阶段：已有签名操作记录但尚未写 prepared 审计、移动前中断、移动后尚未写 completed 审计、完成记录已写但锁未释放。已完全成功的同一归档可只读确认并幂等返回，不重复移动文件。

1. 确认实例及其子工具、先前的归档/恢复维护进程均已停止，并阻止自动启动和其他写入者。恢复工具不代替进程检查。
2. 使用错误结果中的 `archive_id`；若原进程直接退出而没有结果，可在受控环境从锁内完整 `operation.json` 的 `operation.archive_id` 查得。不要凭目录顺序猜测归档 ID。
3. 沿用原维护的当前实例凭据、当前代次和源回执凭据，先执行只读恢复预检查：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --resume <archive_id>
```

检查返回的 `stage`（`before_move`、`after_move` 或 `already_archived`）及 `approval_digest`，再显式执行：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --resume <archive_id> --approve <approval_digest> --confirm-runtime-stopped --confirm-maintenance-stopped --reason <恢复工单编号>
```

恢复会验证操作记录 HMAC、锁的原始身份、源目录/归档目录唯一性、签名回执及服务器最新状态，并在取得独占恢复保护后再次检查。只补写缺失的完整审计记录，不覆盖已有记录；不创建完成回执或调用 finish。等待服务器响应期间锁、审计或目录状态改变时会拒绝继续。

成功后保留归档和回执、释放此次维护锁，仍要求重启运行时。若恢复调用正常抛错，只核验并释放本调用创建的 `resuming` 保护及其 owner 记录，保留外层维护锁与证据，符合条件时可重新预检查后重试。损坏或未知内容不会自动删除。

以下情况仍不自动恢复：旧版没有签名操作绑定的空锁；操作/审计文件部分写入或损坏；锁被替换；源和归档目录同时存在或同时缺失；凭据/代次变化；服务端撤权或完成状态冲突；已有 `resuming` 保护。普通恢复命令不会接管已有保护，只有满足下节条件时才可以先归档保护记录。单机文件锁不代表跨 Pod 协调，Windows 逻辑测试不代表断电持久化验收。

## 恢复进程退出后遗留的保护记录

新版恢复进程在 `resuming/owner.json` 保存签名记录，绑定 PID、原维护操作、随机操作标识、目录身份，以及 Linux 主机/启动批次/PID 命名空间指纹；不包含 API Key。

仅当原记录与当前范围完全匹配，且 `kill(pid, 0)` 明确返回 `ESRCH`（进程不存在），才支持受控处置。存活或复用 PID、权限不足、其他查询异常均拒绝，不按超时解锁。核验过程中仍检查原回执、服务器状态和维护锁归属。

确认实例及其他维护进程都已停止后，先只读预检查：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --resume <archive_id> --retire-stale-recovery
```

使用此次预检查返回的专用摘要执行，不能复用普通恢复摘要：

```sh
node scripts/reconcile-openclaw-executions.mjs --source-generation 5 --resume <archive_id> --retire-stale-recovery --approve <approval_digest> --confirm-runtime-stopped --confirm-maintenance-stopped --reason <工单编号>
```

执行前再次检查进程不存在，然后原子移动整个保护目录到归档内的 `recovery-guards/<随机操作标识>`，保留原 owner 并补充签名处置审计。固定且非空的目标目录防止并发处置覆盖另一份记录。**外层维护锁继续保留，不删除回执、不调用 finish、不启动实例。** 成功返回 `next_step: review_recovery` 后，重新走上一节普通恢复预检查。

移动后审计写入失败仍保留原 owner 和外层锁；后续恢复验证保留下来的记录。若记录部分写入或损坏则继续拒绝，不覆盖修复。旧版空保护目录、缺失/损坏 owner、跨主机/系统重启/PID 命名空间、凭据变化和 Windows 均不支持此处置。工具不会结束任何进程。

2026-11-11 已在隔离 Linux 容器中执行此前的 6 项专属用例，连同容量与并发测试共 **55/55 通过**；再与实际 runtime writer/guard 及固定原生源码夹具联合执行，共 **157/157 通过，0 跳过**。这不是生产实例或断电持久化验收。

恢复保护历史最多保留 256 条。达到上限时，处置返回 `RECOVERY_HISTORY_FULL` 并保留当前保护，不追加到后续无法读取的状态；不自动清理旧审计或解除外层维护锁。

## 本地复现 Linux 回归

需要运行中的 Docker Desktop、已缓存且包含 Node 24.21.0 的 Linux 镜像、AgentsRuntime 工作区和固定原生测试夹具。在 ClawManager 仓库根目录执行：

```powershell
.\scripts\test-openclaw-receipts-linux.ps1 `
  -Image 'sha256:ad9dc0f6daf5ef911c5989db30d0ecb572070699f8b9dfed6bd4c64a23bdfa95' `
  -RuntimeRoot 'D:\AgentsRuntime' `
  -NativeFixtureDirectory 'D:\ClawManager\.codex-temp'
```

上述镜像 ID 是本次本机缓存镜像，并非远端可拉取标签；其他环境请显式提供自己的已验证缓存镜像。脚本不拉镜像、不构建、不启动 Docker、不操作 K8s。省略 `-NativeFixtureDirectory` 时只运行管理工具测试及 runtime writer 兼容测试；提供时增加全套运行时和原生契约测试，原生夹具内容由测试内固定哈希校验。

容器以非 root、断网、只读根文件系统运行；只读挂载 scripts、runtime patches 及指定的九个夹具文件，不挂载业务工作区、密钥或整个临时目录。`scripts/fixtures/receipt-test-machine-id` 仅用于隔离测试，不能复制到真实节点作为生产身份。测试数据写入 tmpfs，容器退出自动移除。该测试不验证实际 PVC/CSI、磁盘掉电语义或多 Pod 协调。

CI 的独立 Ubuntu 作业运行仓库内工具测试；跨仓库 writer/原生夹具兼容测试由上述本地联合入口执行，不会因缺少另一个仓库而假称完成跨仓库验收。

目前已补齐上述窄范围内的遗留保护处置代码；缺失回执/停止证明、无签名或损坏记录的处置、回执跨代次迁移、多 Pod 协调和真实容器验收仍未完成。Linux 目录同步路径仍需实际文件系统验收，新协议尚不能据此启用。
