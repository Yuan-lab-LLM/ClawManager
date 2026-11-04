# Lite 弹性调度：正式默认值与代码准备

日期：2026-11-04。代码位于 `ClawManager`、`AgentsRuntime` 两个仓库的 `feat/lite-idle-stop` 工作分支。本轮按功能整理为本地提交，未推送；不将无关 UI 预演、数据库改进方案或其他旧计划混入功能提交。提交整理状态见[提交准备记录](2026-11-04-commit-preparation.md)。

## 正式默认值已恢复

测试集群 `10.130.81.54` / `clawmanager-system` 已改用不带 `cm_idle_stop_test_timing` 的普通 CM 构建，并通过正常管理 API 重新设置调度。

| 配置 | 生效值 |
| --- | --- |
| Lite 自动停止 | 默认开启，当前开启 |
| 实例检查周期 | 1 天，最小 1 天 |
| 实例空闲超时 | 1 天，最小 1 天 |
| 下一次实例检查 | 2026-11-05 04:00，北京时间 |
| 启动方式 | 用户点击现有启动按钮；访问不自动唤醒 |
| Runtime 事件流上限 | 严格 10,000 条 |

实例空闲扫描与空 Pod 回收是两层逻辑：恢复日规则不把空 Pod 的连续空闲确认窗口改为一天。无绑定、无预占、无未确认停止且满足实际空闲校验的 Pod，仍按既有 60 秒确认窗口回收。

已部署 CM 镜像：

```text
clawmanager.local/clawmanager@sha256:03765053a04d0b2163f9cf854eba8cc16019d58ce8dafadb699750d57dd35fa7
```

API 版本为 `lite-pod-elastic-20261104`；设置版本 7，`enabled=true`、两项 days 均为 1。此操作没有改变 Runtime 镜像或工作空间。恢复过程中 CM Pod 在 19:41:09 更换，现有连接会中断；后续更新应结合当前使用情况安排，不能声称零中断。

源码正式默认值本来就是日规则：`instance_auto_stop_timing.go` 受 `!cm_idle_stop_test_timing` 约束；独立测试构建文件保留，便于今后测试。正式构建不传该标签，不能只查看管理页面上的天数推断二进制计时模式。

## 本轮代码范围

- CM：管理设置与界面、四类 Lite 实例统一 AI Gateway 活动记录、持久化停止及回执、人工启动恢复、空池缩至 0 和真实休眠证明、存储身份校验、备份恢复登记。
- AgentsRuntime：受管自治任务默认配置、槽位停止及工作空间释放协议，与 CM 停止确认协作。
- 修复：Hermes 冷启动重复拉池及桌面制品校验、Runtime 事件流严格上限、已保存供应商模型目录展开及明确选模的权限/撤销边界。
- Hermes 分组：保留独立的 ClawManager → auto；icompify → 10 个已保存模型。源码和节点离线回归已准备，新的 Runtime 候选镜像尚未部署，不能计为已完成线上验收。

数据库增量为 `096_add_instance_auto_stop.sql`、`097_add_instance_auto_stop_scan.sql`、`098_add_instance_auto_stop_policy_status.sql`。使用现有迁移机制；发布前备份，回退时不删除工作空间或直接回滚删表。

## 验证与发布前待办

已有测试集群验证记录：

- [四类实例休眠、Pod 归零、人工启动和存储保持](2026-11-04-lite-pod-elastic.md)。
- [Hermes 冷启动与桌面](2026-11-04-hermes-cold-start-and-desktop.md)。
- [Redis 事件保留上限](2026-11-04-runtime-events-retention.md)。
- [供应商模型目录注入与权限边界](2026-11-04-provider-model-catalog-injection.md)。

以上文档中的分钟测试镜像和实例数量是对应验收时的快照；正式计时与当前 CM 镜像以本文为准。所有测试和构建均在测试节点执行，本地未启动集群或构建镜像。

本次恢复后另在节点执行普通构建的 `go test -race -count=1 ./internal/models -run '^Test(AutoStop|AdvanceAutoStop)'`，3 项通过，覆盖日单位校验、连续空闲窗口与工作保护、错过周期后的检查时间推进。两个仓库 `git diff --check` 均通过。这些检查不代替上文实机验证，也不证明新发现的网络问题已修复。

还不能将这一工作区标记为正式发布验收完成：

1. [最新 OpenClaw 网络错误排查](2026-11-04-openclaw-network-error.md) 确认通知 WebSocket 用过期 access token 重连，需要修复和专项验收。外部模型连接被拒绝属于正常安全策略，不列为缺陷。
2. Hermes 新分组候选镜像仍待协调更新和实际桌面模型列表验收；保留已有镜像制品真实性检查，不能伪造 release acceptance。
3. 整理两个仓库的提交时，逐项核对功能依赖及新增文件；不要直接 `git add .`，不要把 `.deploy`、集群 config、环境文件或诊断凭据打包。

Redis 现有持久化文件仍可能在重启加载时占较多资源，当前内存上限不应仅根据裁剪后的实时内存立即下调。历史 AOF 整理和数据库架构改进仍为独立计划，不在本轮执行。

节点证据：`/root/cm-pod-elastic-20261104/formal-defaults-restored.json`。只有指定测试命名空间发生变更，其他业务命名空间未修改。

## 后续前端修复

用户确认嵌入 OpenClaw 页面空白后刷新可恢复，并授权修复。[嵌入页恢复逻辑已修复、测试并部署](2026-11-04-openclaw-frame-recovery.md)。当前 CM 镜像更新为 `clawmanager.local/clawmanager@sha256:f52d375a8ee742e4c864c21c93a9dfa1d6560120a4369da61f72dd1a0dd2cc1a`，仅叠加前端文件；普通后端、日规则设置及 Runtime 均保持原值。前述镜像是恢复默认值时的快照。通知 token 待办仍未关闭；外部模型直连被拒绝不列为问题。
