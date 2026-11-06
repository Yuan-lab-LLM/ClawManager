# 四类 Lite 统一按模型活动自动停止：修正与验收

## 修正原因与当前规则

此前额外要求 Runtime 提供“原生任务静默证明”，只有 OpenClaw 固定版本具备适配，导致 Hermes、OpenCode、DeepSeek Harness 被跳过。这是实现自行增加的门槛，不符合用户要求，已移除。

现在 OpenClaw、Hermes、OpenCode、DeepSeek Harness 四类 Lite 使用同一规则：有效 AI Gateway 模型请求更新活动窗口，连续达到空闲超时后进入自动停止；默认超时 1 天、最小 1 天，检查周期也默认 1 天、最小 1 天。成功和失败请求均计活动，不区分调用用途，不使用判别模型，不依据 Agent 内部有没有计划任务进行分类。

CM 已登记的在途模型请求、平台任务/命令、写入以及迁移/升级冲突保护仍保留。需长期执行低频或无模型任务的实例由管理员设置“保持运行”。停止后只允许人工启动，不因页面访问、请求或心跳恢复。

## 代码变更

- CM 不再将 `execution_proof_supported` 或 `execution_state` 作为自动停止资格条件。预检仅核对公共停止协议可达与源实例身份，避免旧镜像根本没有停止接口时先提交停止目标；默认配置来源字段仍只是展示元数据。
- 四类 Runtime 共用 supervisor 的命令入场保护和 `GracefulStop`，不再调用类型专用的任务/cron 静默钩子。删除了 OpenClaw 固定版本的 SDK 原生任务适配及相关接口、状态字段。
- 仍然持久化停止意图、固定代次/Pod/工作区身份，并在实际进程退出、卸载和 writer 清理确认后释放槽位。停止超时保留清理记录和占用，不升级为强杀。
- 保留文件及 SQLite 原数据、同一工作目录、人工启动窗口，以及旧心跳不能复活停止实例的保护。
- 最后只读复核发现删除原生钩子时误删了公共 `FenceIdleStop` 调用，网络故障可能绕过 graceful_only。已恢复公共策略原子提交，并新增有/无持久化回调的顺序回归：旧实现失败、修复后通过。复核确认遗漏已解决，六模块重新回归并重建四类镜像。该公共保护不涉及 Agent 类型或任务证明。

## 测试与源码

全部测试、格式化、Go/前端依赖和镜像构建均在 `10.130.81.54` 执行，本地没有运行测试、容器或镜像构建。使用独立命名空间 `cm-idle-test-20261103`、独立数据库、缓存和持久卷，模型请求通过真实 AI Gateway 调用集群内模拟上游；没有调用外部收费模型。

本地与测试节点的 2,485 个源码文件逐文件 SHA-256 一致，清单摘要：`9af018f3bf21353f20581cdf7cd4263259befd50ed32d91580fb961be441f099`。

| 检查 | 结果 |
| --- | --- |
| 新回归先失败 | 旧实现四类均因原生证明缺失无法停止；另验证无公共停止接口时不能改为停止目标 |
| 自动停止专项与真实 MySQL | 通过；四类均验证原生标记为 unsupported 不阻止停止、协议失败不提交停止、超时保留占用、重试确认后释放及持久数据保留 |
| 后端常规全量 | 测试节点 `go test -race -count=1 -timeout=15m ./...` 通过，启用隔离 idle/security/Gateway 数据库 |
| Runtime 六模块 | 非 root 执行 `go test -mod=readonly -race -count=1 ./...`，clawmanager-agent、OpenClaw、Hermes、OpenCode、DSH、legacy-agent 通过 |
| Runtime 扩展数据库矩阵 | Repository、Services 的 runtime/security/idle MySQL 回归通过；显式排除上一轮已在原始 HEAD 复现的 `TestRuntimeMaintenanceMySQLDeleteFenceRejectsLastMomentAdmission` |
| 镜像 | CM 与四类 Runtime 均在测试节点构建、导入，验证保留原生基础层及容器配置，仅覆盖对应 manager 或 CM 二进制/前端产物 |

前端和备份模块本次未改动；上一轮前端 238 项及生产构建、真实 MySQL 备份恢复的通过结果保留为历史证据，本轮未重复宣称重新执行。

## 四类真实链路验收

四类候选 Runtime 在隔离池完成 17 项检查：

| 场景 | OpenClaw | Hermes | OpenCode | DSH |
| --- | --- | --- | --- | --- |
| 模型请求成功与上游 503 均计活动 | 通过 | 通过 | 通过 | 通过 |
| 近期模型调用后日扫描保持运行 | 通过 | 通过 | 通过 | 通过 |
| 达到空闲窗口自动停止并确认释放绑定 | 通过 | 通过 | 通过 | 通过 |
| 第二实例复用同 Pod、同端口槽位 | 通过 | 通过 | 通过 | 通过 |
| 人工启动恢复原 home、文件和 SQLite 数据 | 通过 | 通过 | 通过 | 通过 |

计数为四类各 4 项独立记录，加 1 项共同的近期活动保护检查。持久化校验包括 home inode、UID/GID、文件 SHA-256、SQLite SHA-256 和数据库中的会话内容；停止的替代实例不会被恢复。

日级窗口通过仅回拨隔离测试实例的启动时间和活动时间、令扫描到期验证，未实际等待 24 小时，未修改既有业务实例的活动时间。

## 构建产物与部署范围

| 组件 | 不可变镜像 |
| --- | --- |
| CM | `clawmanager.local/clawmanager@sha256:366f0906e27edc3a18d5c9a714b502eab165911a40c5dd4ea804224fd3008b18` |
| OpenClaw | `clawmanager.local/openclaw-lite@sha256:2e617606c437732d56a5a53d00947eb4e1bab3a385003de30dcc2aea07b0764b` |
| Hermes | `clawmanager.local/hermes-lite@sha256:b752d632c1bc1eacf924ff8f13e6cee04e34c44c9bb937f96dcbf3d488a7ade0` |
| OpenCode | `clawmanager.local/opencode-lite@sha256:8272d3be32d26821658ac3220aa3119758fcf7b8a39b836320d2d333d827ae7a` |
| DSH | `clawmanager.local/deepseek-harness-lite@sha256:c4c5e406bf9e381e75585ba3da6fd9f9362b246b8ba2671834d222b6bbb2a41c` |

最终公共策略修复后，使用表中 CM 和四类最终 Runtime 镜像重新完成全部 17 项真实链路检查，全部通过，包括近期活动保护、自动停止、槽位复用、人工启动与原数据一致。完整回执为 `final-unified-native-verification.json`，其中记录实际验收的四类镜像摘要。

现有 `clawmanager-system/clawmanager-app` 已更新为最终 CM 镜像，公网新版 API 和前端访问通过；58 个既有实例的状态/代次、13 个 Runtime Pod UID 保持不变，默认开启/检查周期 1 天/空闲超时 1 天，监测窗口继续保留。

四类候选 Runtime 镜像已在隔离池更新并验证。**现有业务 Runtime 池没有批量替换，旧镜像需要升级到此次公共 Runtime 后才具备统一停止能力。** 不将隔离池验证等同于现有 58 个实例已全部升级。旧镜像缺少公共停止接口时，会在提交停止意图前保留原运行状态，而不是仅写成 stopped 或误释放资源。

应用回退镜像为上一轮已验证的 `clawmanager.local/clawmanager@sha256:03b84412a76f3df137a494018785fe4a50f856b9cece5d8ee2aa1598e03ea209`。回退只替换 CM 应用镜像，不删除数据库记录或持久卷。

清理于 `2026-11-03 11:25:21 Asia/Shanghai` 完成：隔离命名空间、临时数据库/模型服务、PV/PVC、ClusterRoleBinding 和专属宿主机目录已删除，临时转发已停止，BuildKit 恢复为非活动。清理后再次核对现有 CM Ready=1、58 个实例状态/代次和 13 个 Runtime Pod UID 不变。本地临时归档已删除，没有启动本地测试环境。

详细脱敏证据位于测试节点 `/root/cm-idle-stop-20261103`，本地 `.deploy/idle-cluster-20261103`。源码未提交或推送；访问凭证与私有部署快照不得加入版本控制。
