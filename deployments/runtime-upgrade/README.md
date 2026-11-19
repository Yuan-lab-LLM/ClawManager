# Runtime 镜像发布

`upgrade-runtime.py` 只使用 CM 管理员 API：受控唤醒准备、OpenClaw 预检/确认、OpenCode 原生保留升级、升级提交、历史与状态查询。它不访问 Kubernetes 或数据库，也不提前修改默认镜像、slot worker 配置或副本数。需要 Python 3.9+，仅使用标准库（Linux/macOS）。

源码准备、静态检查和传输可在本地执行；测试、Runtime 二进制和镜像构建必须在 `10.130.81.54` 执行。先按现有构建流程在 54 构建并推送镜像，取得 registry 返回的不可变 manifest digest，再提交 CM。不得用 `kubectl set image`、`scale`、Deployment/设置补丁或 SQL 写入代替发布流程。

```sh
# 在 54 执行；凭据文件只含管理员 Bearer token，权限应为 0600。
export CM_TOKEN_FILE=/secure/cm-admin.token
python3 deployments/runtime-upgrade/upgrade-runtime.py \
  --api-url https://cm.example/api/v1 \
  --runtime-type hermes \
  --target-image registry.example/hermes-runtime@sha256:<64位小写十六进制digest> \
  --state-file /secure/releases/hermes-release.json
```

也可由环境变量 `CM_TOKEN` 提供 token，或使用 `--token-file` 指定文件；CLI 不接受命令行明文 token。私有 CA 使用 `--ca-file`，没有跳过 TLS 验证选项；仅隔离 HTTP fixture 可使用 loopback HTTP。`--timeout` 默认 1800 秒，`--poll-interval` 默认 3 秒，`--request-timeout` 默认 30 秒，`--batch-size` 默认 1。输出为逐行 JSON，仅含操作 ID、受控状态和分类信息；不回显 token、API 错误体、迁移详情或预检凭据。

每个发布使用独立的状态文件，父目录须已存在。状态文件权限为 0600，更新会原子替换并 fsync，进程锁阻止相同状态文件并发提交。保留该文件及其 `.lock` 文件；正常重试使用相同参数和状态文件，自动继续观察已知操作，不能通过删除文件重提。需要调整批次或目标时，应在核对历史并确认前一操作终态后创建新的发布文件。

## API 顺序与边界

Runtime 控制操作按类型隔离：其他类型的升级、缩容、恢复或准备预约不会阻止本类型。普通休眠恢复接受当前配置的镜像及镜像身份环境变量，不要求与休眠快照中的镜像相同；资源 UID、存储、操作标记和非镜像启动配置仍须验证。新的升级准备会冻结当前配置，后续预检与发布仍核对该范围以及最终目标 digest。

1. `POST /api/v1/admin/runtime-rollouts/prepare`，请求 `{runtime_type}`；保存返回的 `data.operation.id`，只查询 `GET /api/v1/admin/runtime-scheduling` 的同 ID `rollout_wake` 操作，等待 `pending/processing/starting` 进入 `completed`。准备只能唤醒原生证明通过的自动休眠池；无证明、漂移或业务冲突由 CM 阻塞。`completed/ready` 保留 15 分钟准备预约；CLI 在预检和升级 POST 均携带相同 `preparation_id`，由 CM 在升级入队事务中校验并消费。CLI 退出不会取消或清除预约；过期或不匹配时停止，由管理员经 CM 重新准备/预检。缺少准备接口的旧 CM 会停止，需要先更新 CM。
2. OpenClaw 调用 `POST /api/v1/admin/runtime-rollouts/preflight`。这是现有分类入口，没有额外的 classify API。`openclaw_8plus_data_safe` 必须 `passed=true`，并携带持久 `rollout.id/preflight_id` 向升级入口确认；固定 `max_unavailable=0`、`auto_rollback=true`。`legacy_rolling` 使用现有旧版滚动路径，`empty_pool_reset=true` 仅表示服务端确认的空池镜像重置，不代表进行了用户数据迁移。CLI 不排除不可用实例、不绕过 blockers、不改变迁移或回滚规则。
3. `POST /api/v1/admin/runtime-rollouts`，保存 `data.rollout.id`；持续 `GET /api/v1/admin/runtime-rollouts/:id`。只有 `finished` 退出 0；冲突、blocked、error、failed、cancelled、needs_attention、未知状态、网络错误及超时均非零退出，不再提交或自动恢复业务池。

支持的提交类型为 `openclaw`、`hermes`、`deepseek-harness`、`opencode`。OpenCode 提交固定携带 `strategy=opencode_native_preserve_v1`；缺少该策略的旧调用仍拒绝共享 Deployment 滚动替换。休眠升级及原生注册验证仍由 CM 执行，脚本不会直接启动用户实例、清除保护或篡改旧休眠 journal。

## OpenCode 原生保留边界

首版针对现场已核验的 OpenCode **1.18.13**，官方源码引用 `a105350812f05f914c768e468559dbd6bd508d8e`，业务二进制 SHA256 `bcebc898d22419417b8dbec0f5e3f00f5b67b5a38c1de37c77c699d0e31fe0dd`、38 项精确 journal SHA256 `2ef42ffb09410c0f91535d25fce5b4107c46ee3fd7a4725e521c197ad4f5949c`。这更新 Agent 管理能力，保留同一原生版本；不支持跨原生版本、未知 schema/journal、`OPENCODE_DB`/channel 或原生路径覆盖。仓库旧默认 2.0.27 不能作为现场数据格式证明。当前协调器一次处理一个有单副本、八槽位的已证明来源池。 这是整池计划维护：池中所有原来 running 的实例会暂停，待迁移验证后按原期望状态恢复；不承诺 `max_unavailable=1` 在线升级。提交响应返回 `availability_mode=pool_maintenance` 与冻结的 `planned_unavailable_instances`，rollout 的 `max_unavailable` 记录该计划暂停数量。此专用策略不接受通用非零 `max_unavailable` 或 `batch_size>1` 请求；CLI 固定 batch1 并发送不适用在线上限的 `max_unavailable=0`。

目标 OCI 必须发布 `io.clawmanager.runtime.type=opencode`、`io.clawmanager.upgrade.strategy=opencode-native-preserve-v1`、`io.clawmanager.upgrade.protocol=opencode-preserve-v1`，以及 `io.clawmanager.opencode.version/revision/binary-sha256/journal-sha256/base-image`。审计来源为 `clawmanager.local/opencode-lite@sha256:7ba6888e43480496704a10d9a496b1398863515166acab5c544ccc9c9d91ed31`。CM 按不可变 digest 校验有限大小的 manifest/config 实际内容及来源层链，使用既有 registry TLS 信任；不会把 `OpenClawVersion` 字段用于 OpenCode 身份。 目标还必须发布 `opencode.native-payload-poduid-v1`，表示已通过实际原生 payload 测量并支持 PodUID 绑定；目标还必须具备第六项 `opencode.scoped-payload-grants-v1`，证明其支持父进程固定 payload 身份及隔离 namespace 中的管理 grant-root 验证；旧四项或五项能力的镜像均不能作为目标。CM 在停止来源和设置 workspace fence 之前准入，并在后续阶段按当前目标 Pod 注册重新核对；旧来源 Agent 不要求这两项新能力。

旧 Agent 仅需提供现有 C1 容器停止证明。CM 冻结来源 UID、Pod、模板和持久存储，创建不可调度的目标 standby，逐实例关闭平台写入并确认旧容器及外部 writer 退出，再由目标维护 sandbox 创建、复核 UID 私有原生 capsule。备份范围只含固定原生 DB/WAL/SHM、配置和状态；项目目录不遍历、不复制。`compatible`、`validated`、完整状态摘要、session catalog、`DataPreserved` 和可回滚证明缺一不可。未知维护结果保留同操作 lease、写入 fence 和 checkpoint；按原 ID 续接，不能重提新操作或清除保护。


查询 `GET /admin/runtime-rollouts/:id` 时，原生进度位于独立的 `native_operation`，初始尚无目标 Pod 的阶段同样返回 200。该视图只展示记录的阶段、控制器/Pod 身份、逐实例 `state` 及 `has_stop_proof`/`has_capsule`，不返回原始维护请求、凭据、workspace 路径或证明内容。`protection_held` 的范围明确为 SQL 中本操作仍为 pending/running 时的 Runtime 类型保护，不代表重新核验了写入 fence 或原生 lease；`error_message` 是 CM 最后记录的错误，并带有未重新验证的 `error_scope`。SQL `running` 不能作为业务进程仍在运行的证明，应查看逐实例维护记录。

对记录在 preserve 阶段、实例均已 quiesced 且无 capsule 的确定 payload 信任拒绝，视图返回 `blocked=true`，同时保留 SQL running 及所有保护。记录的目标缺少 `opencode.scoped-payload-grants-v1` 时也明确展示受保护的能力拒绝，不会把该错误改成解除保护的 SQL 终态。CLI 输出同一操作 ID、受控阶段和保护标志后非零停止，不回显原生错误体，不修改该 ID 的观察状态或重新提交。

管理员明确决定强制重试这类尚未创建 capsule 的失败时，可使用修复后的不可变镜像和**新的状态文件**：

```sh
python3 deployments/runtime-upgrade/upgrade-runtime.py \
  --api-url https://cm.example/api/v1 --runtime-type opencode \
  --target-image registry.example/opencode-runtime@sha256:<64位小写十六进制digest> \
  --force-retry-id 14 --state-file /secure/releases/opencode-14-force.json
```

此选项明确授权 CM 对原操作进行强制重试，先只读预检，再以 UUID 和预检指纹确认。CM 保留原失败审计及源数据保护，只对已证明未启用的目标执行缩零、旧 Pod 物理退出确认、更换镜像及重新就绪；之后继续同一操作的原生保留升级。已有租约、未知写入结果、源停止证明缺失、身份或存储漂移均不能借此绕过。它不提供一般性的 abort、取消保护或任意阶段 retarget。

状态文件独立保存 force 请求 UUID；不要复用、修改或删除原发布状态。确认响应丢失后，用相同命令及新的 force 状态文件恢复，CLI 只查询原操作，并核对服务端已记录的 UUID 和目标；未找到或不匹配则停止，不重发确认。预检拒绝或预检响应未知也停止，先检查原操作。`--operation-id` 仍是只读观察入口，与 `--force-retry-id` 互斥。

目标激活后仅恢复被冻结的原期望状态和预期 generation；原来停止的实例继续停止。完成验证后，CM 在确认来源无 binding、无物理 writer 后按 UID/模板/存储 CAS 将来源副本降到零，保留其模板、持久卷和 capsule；等待旧 Pod 实际退出，再提交 `finished` 及后续默认放置。再次升级已停止实例时，必须保留其原始 C1 身份，并通过已完成升级的 capsule、当前控制器 UID 和相同存储续接来源证明。

目标执行明确失败后的恢复仍属于同一操作：先确认目标 writer C1 退出，再用 `preserve_session_sqlite=true` 验证当前历史保留；目标执行为 N+1 时，恢复请求携带当前 generation N+1 和原 capsule 的 `resume_generation=N`。只有 `restored` 与独立 status 都确认配置、状态和当前 session 保留，才允许在原镜像以新 generation 恢复。恢复成功记录 `error` 与 `rollback_status=restored`，目标不成为默认镜像；缺少证明时保持保护。

## 中断恢复

CLI 在每次 POST 前落盘提交意图；收到 ID 后先保存再轮询。同一状态文件不会自动重复任何已经发出的 POST。超时、终端断开后，使用原命令重新执行即可观察已知 ID。

若提交响应丢失且尚无 ID，CLI 无法证明服务端是否已创建操作，因现有 API 没有通用提交幂等键而停止。只读查看历史：

```sh
python3 deployments/runtime-upgrade/upgrade-runtime.py \
  --api-url https://cm.example/api/v1 --runtime-type hermes --history
```

在 CM 核对时间、操作者、类型和目标 digest 后，给原命令增加 `--operation-id <rollout-id>` 可只读接续该升级；CLI 会先校验返回的类型、目标和 ID。若丢失的是准备响应，用 `--preparation-id <rollout_wake-id>`，校验 kind/类型后继续等待该准备并提交后续升级。历史列表有保留范围，查不到时保持停止，通过 CM 审核，禁止猜测 ID 或直接修改数据库。

已知数据安全预检 rollout ID 的确认响应丢失时，后续只观察该 ID；如果仍为 `preflight_passed`，CLI 非零停止，由管理员核对后通过 CM 处理。预检响应完全丢失时，`--operation-id` 也只观察，不自动重新预检/确认。该保守行为避免生成第二个操作，不承诺网络中断下的“恰好一次”投递。

## 隔离验收

只在 54 执行，fixture 使用随机 loopback 端口、临时状态文件和测试 token，不请求真实 CM，不读写业务池、实例、Secret 或 journal：

```sh
python3 -m unittest discover -s deployments/runtime-upgrade -p 'test_*.py' -v
```

测试覆盖提交一次/恢复同 ID、成功和失败终态、超时、409、丢失响应、受控准备阻塞/恢复、OpenClaw 数据安全确认和旧版空池重置、OpenCode 专用策略及丢失响应不重提、可变 tag 拒绝、身份不符、重定向拒绝及凭据不回显。fixture 验证 HTTP 契约，不替代 CM Go 回归、真实镜像分类、原生注册或发布后验收；这些 fixture 不执行真实 Runtime 发布或故障池恢复。
