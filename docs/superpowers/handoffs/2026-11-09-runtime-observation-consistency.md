# Runtime 数据一致性交接（2026-11-09）

> 2026-11-10 更新：隔离 MySQL 容器已清理，Hermes161 唤醒故障的独立热修复已上线；参见 [最新记录](2026-11-10-hermes161-idle-wake.md)。migration105 整体改动也已完成验证与发布，参见 [一致性修复验收记录](2026-11-10-runtime-observation-consistency.md)。下文保留当时交接状态供追溯。

## 当前交付范围

用户要求数据库与实际运行环境保持一致；本次代码已整理提交，尚未部署。用户随后要求换电脑继续操作，因此停止集群构建和部署，提交并同步 Git，不创建 PR。

- ClawManager：`preview` 分支。本次修改集中在 Runtime 观察数据、巡检、列表展示和自动回收校验。
- AgentsRuntime：`preview` 分支，当前基线 `d383bab`，本轮未修改 Runtime 源码。
- 不提交本机 `.deploy/` 下的调试记录、凭据、构建归档和临时脚本；已加入忽略规则。主工作区原有未跟踪文档保持原样，不包含在本次提交中。

## 实现内容

1. Migration105 为 `runtime_pods` 增加独立的 `observation_json` 和 `observation_at`，按当前 Pod UID 与递增观察时间保存经过筛选的现场事实。心跳保留同 UID 的观察结果；更换 UID 后清空旧结果。观察接口不修改分配计数或绑定。
2. 现有选主巡检器采集 Runtime、SQL 绑定、原生 assignment 预留及隔离状态，先保存最终观察结果，再执行有充分证据的清理。旧扫描保存被拒绝时不再修改隔离状态。不完整的旧协议仍显示实际报告的实例身份，但不能据此释放资源。
3. 页面、统计和自动回收共用同一数据解释逻辑。残留、冲突、未知分别显示；未知不当作空闲。保留运行时报告身份和数据库预期身份，未确认的绑定不能执行迁移或驱逐。
4. 自动回收在计划发布与最终修改副本数之前再次锁定源 Pod 数据库行，检查最新、完整、未过期的空闲观察结果。新占用、未知或过期结果阻止回收；失败后释放本次操作的保护和暂停状态。未绑定的 Creating 实例不再永久占住同类型所有空池，具体 claim、绑定和未确认停止仍保留保护。
5. 系统备份 schema 与恢复归一化已增加新字段。发布时需要同时重建 manager、`system-backup-artifact`、`system-backup-controller` 和前端；不能仅替换 manager 二进制。

## 已验证与限制

- 本地 models、repository、systembackup、handlers 的本次相关测试及 race 检查通过。
- 本地 services 的巡检、槽位分配、自动回收和缩容相关测试及 race 检查通过；命令使用 `go list` 选取当前平台文件，并排除了原有 Darwin 不兼容的 `browser_worker_nfs_test.go`。
- 前端 Runtime 相关测试通过（52 项），TypeScript/Vite 构建通过。
- 独立审查后补齐最终缩容前的 SQL 观察校验，并修复预期身份过滤、部分身份混合显示问题。
- MySQL 真实集成回归已经编写，但本轮未执行完成。特别需要运行 `TestRuntimeObservationMySQLCASAndCounters` 和 `TestRuntimeIdlePoolMySQLRechecksLatestObservation`，后者覆盖计划发布、执行、最终副本写入前变更观察数据的九种情况。
- Linux 后端完整回归、备份新 schema 验证、镜像构建、集群部署和现场验收未完成。测试机 `10.130.81.54` SSH22 与 HTTPS30443 在交接前连续超时。请勿将本次提交视为已经修复线上状态。
- 本机默认 `go` 的工具链/GOROOT 不匹配，本轮使用 `/usr/local/go/bin/go`。新电脑按项目 go.mod 准备匹配工具链即可。

可在 Linux、两个仓库并列检出后运行：

```sh
cd ClawManager/backend
go test -count=1 ./internal/models ./internal/handlers ./internal/services ./internal/services/k8s ./internal/repository ./internal/systembackup
go test -race -count=1 ./internal/models ./internal/handlers ./internal/services ./internal/services/k8s ./internal/repository ./internal/systembackup -run 'Test.*(RuntimeObservation|RuntimeInventory|RuntimeSlotAudit|SlotAllocation|RuntimeIdlePool|ScaleIn)'
# 先配置隔离测试 MySQL 的 TEST_RUNTIME_MYSQL_PORT / TEST_RUNTIME_MYSQL_PASSWORD。
go test -count=1 ./internal/services ./internal/repository -run 'TestRuntimeObservation|TestRuntimeIdlePool|TestScaleInMySQL|TestRuntimeManagementMySQL|TestDefaultRuntimeScaleMySQL'
cd ../frontend
npm ci
node --experimental-strip-types --test scripts/runtime-management.test.mjs scripts/runtime-slot-grid.test.mjs scripts/runtime-eviction-ui.test.mjs
npm run build
```

## 测试环境基线和待清理资源

最后部署的 manager 镜像仍为：

`clawmanager.local/clawmanager@sha256:5dbe2a45fe1153e0a0d3c700138d925c70fdcc64699cbf9a2133cb2bc56fb00d`

Migration105 尚未发布。只允许在 `clawmanager-system` 操作；保持现有 Deployment 环境变量，尤其四种 Runtime 引导镜像配置。受保护的用户 Runtime Pod 不得为了验证而重启；不直接修改业务 SQL、清除 writer fence、删除用户工作区或 PVC。不再创建测试 Kubernetes Pod。

上一轮已清理自建测试 Kubernetes 控制器/Pod。最后一次现场检查剩六个 Runtime Pod：Hermes132（残留实例25/G14）、OpenCode135（实例118/G9 异常绑定）、OpenClaw147/150/155/162。实例166/167 是用户实例，不能当成测试残留删除。Hermes161 的正式 Web 模板必须保留，副本数为0时仍供用户 Start 恢复。

本轮创建了一个隔离的 **containerd MySQL 容器，不是 Kubernetes Pod**；因 SSH 中断，交接时无法确认清理，须恢复连接后优先检查：

- containerd namespace：`k8s.io`
- 容器名：`cm-runtime-observation-mysql-20261109`
- 所有权 label：`clawmanager.io/change-owner=runtime-observation-20261109`
- 隔离 MySQL 端口：51369，仅监听127.0.0.1
- 连接元数据：`/root/cm-runtime-consistency-20261109/mysql-connection.private.json`
- 密码位于该元数据指向的本机私有文件，不能写入 Git、日志或聊天。

先用 `ctr -n k8s.io containers info cm-runtime-observation-mysql-20261109` 核对名称和 label，再只终止并移除此容器 task/container，删除其私有密码文件。不要操作生产 MySQL 容器。隔离数据库没有用户业务数据。

## 继续工作顺序

1. 检出两个仓库 `preview`，确认本次提交已同步；无需依赖旧电脑 worktree 或 `.deploy`。
2. 恢复测试机连接，完成隔离 MySQL 与 Linux 回归，修复实际失败后重新审查。
3. 构建包含 migration105、两个备份工具和前端的 manager 镜像，保持原镜像环境配置和 Hermes 原生静态页面。
4. 仅更新 manager。检查 SQL 的观察时间/UID/报告身份、列表统计、受保护 Pod UID/重启次数，以及确有证据的空池回收。
5. 清理上述隔离容器和密码，记录最终镜像与验收结果。当前交接不声明部署完成。
