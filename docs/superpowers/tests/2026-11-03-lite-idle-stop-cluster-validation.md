# Lite 自动停止：测试集群验收记录

> 此文记录第一轮历史验收。随后用户明确四类 Lite 都按模型调用判定，已移除原生任务证明门槛；当前规则与四类验收见[统一模型活动补充报告](2026-11-03-lite-model-activity-unified-validation.md)。下文“仅 OpenClaw”的描述不再是当前代码限制。

日期：2026-11-03（Asia/Shanghai）。执行节点：`10.130.81.54`，通过仓库根目录 `config` 访问测试集群。

## 执行范围

所有测试、依赖安装、Go/前端构建、镜像构建及导入均在测试节点执行。本地仅编辑代码、计算源码校验值、传输源码及保存证据，未启动本地集群、容器或镜像构建。

使用独立命名空间 `cm-idle-test-20261103`，独立 MySQL、Redis、PVC 和宿主机测试目录；未使用现有业务实例模拟空闲，也未修改其活动时间。测试模型通过真实 CM AI Gateway 调用集群内的模拟上游，未使用外部收费模型。Redis/MySQL 改进项目不在此次范围内。

测试源码基于两个仓库的 `feat/lite-idle-stop` 工作区：ClawManager 基线 `66c5c4de9b58fa373b20925808cab494bcace0a7`，AgentsRuntime 基线 `8a0dddbf53f6199a2ff221c2a7d58b8b82ad9fe7`。本地与测试节点 2,487 个源码文件逐文件 SHA-256 一致；清单摘要 `51ed852789507d9006d34b737129d687e156180bd8b871b3435617c78e5a229a`。

## 自动化回归

| 项目 | 执行方式 | 结果 |
| --- | --- | --- |
| 后端常规全量回归 | 测试节点 `go test -race -count=1 -timeout=15m ./...`，启用独立 idle/security/Gateway MySQL 测试库 | 通过；不等于启用了每一个可选外部依赖测试 |
| 自动停止专项 | Repository、Services、Handlers、AI Gateway、Kubernetes、Models；启用独立 MySQL | 通过，包括管理员迁移兼容回归 |
| Runtime 回归 | clawmanager-agent、OpenClaw、Hermes、OpenCode、DeepSeek Harness、legacy-agent，非 root 用户执行 `go test -mod=readonly -race -count=1 ./...` | 六个模块通过 |
| 前端 | 仓库 scripts/hooks/services/lib 测试及 `npm run build` | 238 个测试通过，0 失败、0 跳过；构建通过 |
| 备份恢复 | 独立 MySQL 下的 systembackup、backupgate 全量回归及新迁移 schema contract | 通过 |
| Runtime 可选 MySQL 扩展回归 | Repository、Services；启用 runtime/security/idle MySQL，排除下述已有基线失败项 | 通过；包含管理员迁移及停止意图保护回归 |

已有基线失败项：`TestRuntimeMaintenanceMySQLDeleteFenceRejectsLastMomentAdmission` 的 `resume-and-claim`、`pending-register-and-claim` 子用例出现 `sql: no rows in result set`。同一测试节点、同一隔离数据库服务上，原始 HEAD 与当前代码均可复现。保留两份失败日志，不将其记为通过；扩展矩阵显式排除此父用例。`TestUpgradeFallbackMySQLStoppedAncestorAndPersistentIsolation` 的独立对照测试通过。

## 原生端到端验收

原生运行时使用保留现有 OpenClaw 镜像全部底层层及容器配置、仅替换 manager 的候选镜像。通过真实 CM 管理 API、AI Gateway、运行时上报和 Kubernetes 资源执行以下场景：

| 场景 | 观察结果 |
| --- | --- |
| 默认策略 | 自动停止默认开启；检查周期、空闲超时各默认 1 天 |
| 参数校验 | 两个天数字段合计 14 个非法输入返回 400；两字段可独立设置；旧版本更新返回 409；修改策略不重置活动窗口 |
| 保持运行 | 即便测试实例已满足空闲时间，管理员豁免仍阻止自动停止 |
| 模型活动 | 真实网关请求上游成功和 503 失败均登记活动；两次调用产生 4 次入场/完成版本变化 |
| 最近模型使用 | 日扫描保留最近经过网关调用的实例 |
| 未完成请求 | 20 秒真实在途模型请求与到期日扫描重叠，实例保持运行，不创建自动停止操作 |
| 自动停止 | 空闲后原生静默检查、优雅停止完成，实例为 `stopped`、原因为 `idle_timeout`，绑定释放，清理日志完成；首轮约 4.2 秒 |
| 访问与心跳 | 查看实例、周期心跳及携带合法 Runtime 凭据的旧运行状态上报，均不会唤醒已停止实例 |
| 槽位复用 | 第二实例复用第一实例释放的同一个 Runtime Pod、同一个槽位端口 |
| 人工启动及持久化 | 原启动接口恢复运行；原工作目录、home inode、UID/GID、文件 SHA-256、SQLite SHA-256 和会话行一致；重新建立活动窗口 |
| 人工停止 | 同样释放槽位，停止原因为 `manual`，与自动停止共用停止状态 |
| 重复启动 | 返回 409，运行代次和原启动截止时间不被重置 |
| 无容量启动 | 占满唯一真实槽位并用独立命名空间 ResourceQuota 阻止扩容；约 122.5 秒后启动超时，实例回到停止状态，无残留绑定 |
| 容量恢复后重试 | 释放槽位后人工重新启动成功，原 home 和持久数据保持一致 |

日级空闲使用**仅针对隔离测试实例**回拨 `started_at` 和活动时间、将 `next_check_at` 调整为到期来验证；未实际等待一天。因此已验证到期判断、完整日窗口保护和扫描执行，但不宣称完成连续 24 小时观察。

## 测试发现并修复的问题

1. **OpenClaw 静默检查认证失败。** 原 CLI 使用显式 Gateway URL 时不能仅依赖环境变量凭据。改为调用固定版本原生 SDK 的 Gateway RPC，并显式传入凭据；凭据不放入进程参数。真实原生检查及优雅停止已通过。
2. **平台虚报物理槽位容量。** 注册和心跳将真实 1 槽位覆盖成配置上限 8，导致错误分配不存在的端口。现在以 Runtime 报告的物理容量为边界，并在心跳修正历史错误值。新增数据库回归，重跑真实容量不足与重试场景通过。
3. **停止保护误拦管理员迁移。** 源端确认停止后，迁移需要持久化经过管理员授权的目标创建意图。现在校验操作所有者、代次、目标预留和实例锁后再创建；保留停止状态的一般保护，新增人工/自动停止意图不能被迁移覆盖的回归。

测试夹具也同步适配新增迁移、备份 schema、Node 原生插件依赖和非 root 文件权限。首次 Runtime 循环脚本曾掩盖中间模块失败，已改为严格传播退出码，并用非 root 用户重跑六个模块后取得通过结果。

## 实现边界与上线范围

自动停止只适用于 Lite。当前原生静默/停止证明适配 **OpenClaw 2026.10.2**；Hermes、OpenCode、DeepSeek Harness 及未适配版本会因缺少原生执行状态证明而保守跳过，不能据此宣称四类实例均已完成自动回收支持。

CM 已有的模型调用必须经过 AI Gateway 这一约束继续成立；本次没有引入“允许直连外部模型”的豁免。默认关闭 OpenClaw 新配置中的 heartbeat、dreaming 和 workshop autonomous；已有明确配置不会批量覆盖。低频、无模型调用的后台任务仍需要管理员使用“保持运行”。本次不宣称平台能够发现 Agent 内部所有未来计划任务。

更新现有 CM 服务时保留既有 Runtime Pod；候选 OpenClaw manager 在隔离池构建和验证，已有业务池需要后续按既有升级流程更新后才能使用原生自动停止能力。本次未对现有 58 个实例进行批量 Runtime 滚动升级。

测试开始前，现有 Team Redis 已处于 CrashLoopBackOff，未作为此次功能变更处理。

## 部署验收补充

最终镜像全部在 `10.130.81.54` 构建、导入：

- CM：`clawmanager.local/clawmanager@sha256:03b84412a76f3df137a494018785fe4a50f856b9cece5d8ee2aa1598e03ea209`。
- OpenClaw 候选 manager：`clawmanager.local/openclaw-lite@sha256:5ed31b5f809638597e8983c2af334dff528f390b896ceebd9559f7429423b8e6`；用于隔离池验收，未滚动替换现有业务 Runtime 池。
- 两个镜像均验证了原生基础层及容器配置保持一致，仅增加对应二进制和 CM 前端产物。

最终 CM 镜像上追加了真实管理员迁移：测试实例从代次 6 迁移为代次 7，再次自动停止并人工启动为代次 8。迁移、自动停止、人工恢复全过程完成，原 home 身份及文件、SQLite 数据一致，另一个已停止实例保持停止。

现有 `clawmanager-system/clawmanager-app` 已更新为上述 CM 镜像，公网新版 API 和前端可访问。58 个既有实例的状态及运行代次、13 个 Runtime Pod UID 均与更新前一致；58 个 Lite 实例监测窗口初始化完成，默认策略开启、检查周期 1 天、空闲超时 1 天。首次检查时间为 `2026-11-04 04:00 Asia/Shanghai`。新实例/新监测窗口需满足完整空闲窗口，不会因为此次更新立即回收。

第一次发布因 Deployment 就绪后公网入口短暂连接拒绝触发脚本自动回滚，58 个实例未改变。检查确认现有 `/healthz` 是 Nginx 静态 200，不能表示后端与 EndpointSlice/NodePort 已完成切换。发布脚本增加通过公网入口确认**新版后端版本**的最多 120 秒等待后重试成功；未为此改动业务代码或最终镜像。

需要回退 CM 应用时，在测试节点执行：

```sh
kubectl -n clawmanager-system set image deployment/clawmanager-app clawmanager-app=clawmanager.local/clawmanager@sha256:57e0dc9ef0b6167239246e0a3628ab614bd67fbcf574b4666930f7308b22bd2a
kubectl -n clawmanager-system rollout status deployment/clawmanager-app --timeout=180s
```

新增数据库迁移为兼容性扩展，应用回退不需要删除活动/操作记录；不得通过删除持久卷回退。

清理于 `2026-11-03 09:48:50 Asia/Shanghai` 完成：隔离命名空间、临时数据库/模型服务、ClusterRoleBinding、测试 PV/PVC 和专属宿主机目录均已删除，临时 MySQL 转发已停止，BuildKit 恢复到任务开始前的非活动状态；共享镜像缓存未清理。清理后再次确认 CM Ready 副本为 1，58 个实例状态/代次及 13 个 Runtime Pod UID 保持不变。本地约 61 MiB 的临时源码归档已删除，仅保留小体积脚本、日志与校验证据。

详细日志和来源校验保存在测试节点 `/root/cm-idle-stop-20261103`，本地脱敏证据在仓库根目录 `.deploy/idle-cluster-20261103`。私有部署快照及访问凭证不应加入版本控制。
