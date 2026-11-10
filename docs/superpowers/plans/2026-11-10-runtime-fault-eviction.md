# Runtime 故障驱逐实施计划

> **For agentic workers:** Use subagent-driven-development for the independent helper and UI tasks; root integrates lifecycle and performs final verification.

**Goal:** 复用现有驱逐任务，以数据库业务意图为准恢复实例与槽位绑定；不重启健康邻居、不清除项目数据。

**Architecture:** 新增 recovery 驱逐模式及持久 item checkpoint。正常 Agent stop 优先；失败后由精确 UID/容器范围的节点侧 helper 确认停止并修复旧 writer ledger，控制面 CAS 退休旧 assignment，再事务性释放绑定并继续原生命周期。无可用目标容量不阻止旧源清理。

**Tech Stack:** Go / MySQL / Kubernetes Jobs + CRI / React TypeScript，复用 runtime_management_operations，不另起调度器。

## Global Constraints

- 默认只选择异常实例/槽位；用户明确选择的健康实例必须展示重启影响。
- 不缩整个 Deployment 到 0，不重启其他 Pod 或 slot。
- 不将 Unknown 当作已退出，不凭 API 404 直接解绑。
- 项目、Git、PVC 保留；旧写入停止证据、writer ledger 和精确绑定一致后才允许接续。
- stopped/deleting 保持其意图；只对需要运行的实例排队，generation 在重试中只增加一次。
- 每次外部操作前持久化范围和阶段，响应丢失后观察同一任务。
- Team/外部写入保护不能绕过；无法证明隔离时保留阻塞并给出具体原因。

### 任务 1：独立于旧 Agent 的精确容器停止与 ledger 修复

Files: backend/internal/runtimeeviction/*, backend/cmd/runtime-eviction-helper/*, backend/internal/services/k8s/runtime_fault_eviction.go, Dockerfile.

- [x] 先写失败测试：错误 PodUID/容器身份/Node boot/ledger owner 拒绝；仅停止选定 slot；停止后精确修复 ledger；重复调用返回可验证结果。
- [x] 实现 Scope/Receipt、CRI 客户端与节点 Job。Job 输入固定、无用户命令、无用户可读凭据。
- [x] 用 fake CRI 和 fake Kubernetes 验证 stop/reply loss/Job identity conflict；真实 CRI 测试只用自建隔离容器。

### 任务 2：故障驱逐的持久编排与事务性解绑

Files: backend/internal/services/runtime_management_recovery.go, runtime_management.go, runtime_management_force.go, backend/internal/services/k8s/runtime_assignment_recovery.go, backend/internal/handlers/runtime_management_handler.go.

- [x] 先写失败测试复现 generation 提前 fencing 后无法继续、deleting/stopped 不可驱逐、源失联无法取得原 Agent 证明。
- [x] recovery preflight 固定源身份、容器、节点、存储与 binding；默认过滤健康实例。
- [x] 在现有 operation 中保存 scope/phase；优先原 stop，失败才调用 helper；校验 receipt 后 CAS 退休 assignment。
- [x] 事务内精确释放旧 binding、重算实际占用；保持 stopped/deleting 或排队 creating。任务恢复不重复 generation。
- [x] 重启自动续跑 checkpoint；管理员 Retry 继续同一操作。无目标容量显示等待，旧源继续隔离。

### 任务 3：现有驱逐界面中的故障恢复

Files: frontend/src/pages/admin/RuntimeActions.tsx, RuntimePodDetails.tsx, frontend/src/services/runtimePoolService.ts, frontend/src/types/runtimePool.ts, frontend/src/locales/*.

- [x] 先写服务请求与范围选择失败测试。
- [x] 增加 recovery 模式、选择异常实例、完整影响提示；无可验证目标不宣称恢复成功。
- [x] 操作记录呈现阶段和 Retry，错误分组显示；中英文保持一致。

### 任务 4：集成、审查与测试环境验证

- [x] 定向后端/SQL/前端测试，独立审查身份范围、停止证据、并发与崩溃恢复。
- [x] 构建包含 helper 的应用镜像，更新测试环境；先读现场与 desired state，再经产品路径逐个恢复。
- [x] 验证健康邻居不重启、工作区保留、残留删除完成、占用与现场对齐、实例可重新调度。
- [x] 清理仅本次自建测试资源；提交并推送 preview，不创建 PR。准确记录仍需基础设施隔离的情况。

验收记录：[测试环境结果](../handoffs/2026-11-10-runtime-fault-eviction.md)。现场完成实例 156 删除和 118 停止意图收敛；无绑定的历史 Unknown 槽位不自动强清。
