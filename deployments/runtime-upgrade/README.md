# Runtime 镜像发布

`upgrade-runtime.py` 只使用 CM 管理员 API：受控唤醒准备、OpenClaw 预检/确认、升级提交、历史与状态查询。它不访问 Kubernetes 或数据库，也不提前修改默认镜像、slot worker 配置或副本数。需要 Python 3.9+，仅使用标准库（Linux/macOS）。

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

支持的提交类型为 `openclaw`、`hermes`、`deepseek-harness`。OpenCode 当前 CM handler 明确拒绝不安全滚动升级；CLI 在任何准备或升级写入前停止。休眠升级及原生注册验证仍由 CM 执行，脚本不会直接启动用户实例、清除保护或篡改旧休眠 journal。

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

测试覆盖提交一次/恢复同 ID、成功和失败终态、超时、409、丢失响应、受控准备阻塞/恢复、OpenClaw 数据安全确认和旧版空池重置、OpenCode 拒绝、可变 tag 拒绝、身份不符、重定向拒绝及凭据不回显。fixture 验证 HTTP 契约，不替代 CM Go 回归、真实镜像分类、原生注册或发布后验收；本轮不执行真实 Runtime 发布或故障池恢复。
