# Slot 重置与容量隔离 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本计划选择当前会话顺序执行，不主动派生子 agent。

**Goal:** 异常或未知 slot/Pod 不阻塞新增容量；管理员可精确重置旧 slot，保护其他运行，并由 CM 自动恢复仍期望运行且尚无其他有效分配的实例。

**Architecture:** 将物理 slot 回收、实例期望状态和扩容配置批准分开。复用持久 Runtime 管理任务、精确容器终止 helper、旧源退休凭据、条件绑定更新与现有 CM 调度恢复流程；不引入新的进程监督服务或独立调度器。

**Tech Stack:** Go、MySQL、现有 Redis 协调、Kubernetes/containerd CRI、AgentsRuntime 原生 slot 生命周期、React/TypeScript 与现有浏览器测试。

## Global Constraints

- 设计依据：`docs/superpowers/specs/2026-11-18-slot-reset-and-capacity-isolation-design.md`，包含用户 2026-11-18 对重置后自动调度的修正。
- 本地仅准备、校验和传输源码、文档与部署脚本；测试、二进制、生产前端打包和容器构建全部在 `10.130.81.54` 执行。
- 开发和故障注入使用隔离资源，不重置现场用户实例。保留工作区、Git、PVC、NFS 数据和现有资源请求。
- Runtime 调度、管理、扩缩容、升级和异常保护按 Runtime 类型限定冲突范围；短事务原子锁保留。
- 不以状态未知推断执行退出；不按 instance ID 或代次整体清理旧源，不停止实例当前 endpoint。
- 仍期望运行的手动重置实例进入待调度；用户明确的停止、休眠、删除优先，不自动复活。
- 不修改节点 kubelet 退避配置；slot 在退避期间不可分配，但不阻塞其他容量和实例恢复。
- UI 遵循 `DESIGN.md` 的 Fluent 2 规范，保留管理员权限、键盘操作、焦点返回、现有同步和多语言。
- AgentRuntime 若需协议兼容补丁，在隔离工作区完成；不直接改动其已有工作分支。镜像升级必须走 CM 原生升级流程并使用不可变 digest，不绕过保护。
- 本文件是实施安排；所有复选框初始未完成，不表示测试通过或功能已发布。

## 文件与责任

| 范围 | 主要文件 | 责任 |
| --- | --- | --- |
| 容量与创建 | `backend/internal/services/runtime_scheduler.go`、`runtime_scheduler_capacity.go`、`runtime_slot_audit.go` | 可分配容量、未知隔离、候选轮换、扩容需求 |
| 可信扩容配置 | `backend/internal/services/runtime_pod_scale.go`、`k8s/runtime_pod_scale.go`、`k8s/runtime_pod_scale_out.go` | 持久配置批准、无健康源 Pod 时扩容、只增副本 |
| 单 slot 重置 | 新建 `backend/internal/services/runtime_slot_reset.go` 与 `runtime_slot_reset_test.go` | 预检、固定物理目标、幂等管理任务和阶段推进 |
| 精确终止 | `backend/internal/runtimeeviction/cri.go`、`k8s/runtime_fault_eviction.go`、`runtime_fault_slot_release.go`、`runtime_slot_operator_fence.go` | helper、退出凭据、旧源退休和账本释放 |
| 解绑与恢复 | `backend/internal/repository/instance_runtime_recovery_binding.go`、`instance_runtime_recovery.go`、`backend/internal/services/runtime_instance_recovery.go` | 条件解绑与待调度意图同事务、一次性代次推进 |
| 普通回收 | `backend/internal/services/runtime_slot_termination.go`；AgentsRuntime 的 `slots/container_lifecycle.go`、`slots/fault_recovery.go`、`gateway/container_reset.go` | 正常清理失败后的受控容器终止、匹配原生账本 |
| API 与管理 UI | `backend/cmd/server/main.go`、新建 `backend/internal/handlers/runtime_slot_reset_handler.go`；`frontend/src/pages/admin/RuntimePodDetails.tsx`、`RuntimeActions.tsx`、`services/runtimePoolService.ts`、`types/runtimePool.ts`、`lib/runtimeManagement.ts` | 管理员入口、影响预检、提交与任务进度 |
| 验收 | 新建 `frontend/scripts/runtime-slot-reset.browser.mjs`、`deployments/validation/runtime-slot-reset-acceptance.py` | 隔离环境故障矩阵、证据与 HTML 报告 |

新增实现保持上述责任边界，不拆分重写整个 `runtime_scheduler.go`。持久管理任务复用已有表及 JSON journal；只有现有持久结构不能表达可信扩容配置或原子 slot 隔离时，才增加对应迁移，序号在执行时从当时实际最大序号后分配，不能覆盖已有 123 及之前迁移。

## Task 1: 固化回归与容量口径

**Files:** `backend/internal/services/runtime_scheduler_capacity.go`、`runtime_slot_audit.go`、`runtime_scheduler_capacity_proof_test.go`、`runtime_scheduler_occupied_slot_test.go`、`runtime_scheduler_liveness_test.go`。

**Interfaces:** 保留 `allocatableGatewaySlots(ctx, pod) (int, error)` 的严格分配语义；扩容调用方对运行观测失败按可分配零处理，配置或存储身份冲突仍单独报错。

- [ ] 在已有 fixture 中构造 8 个物理 slot，分别注入单 slot Unknown、quarantine、RPC 不可达、SQL 旧占用及 Pod 整体 Unknown；断言异常不能被分配、健康空闲仍按实际证明计算。
- [ ] 在测试集群运行定向用例，保存修改前的失败结果；不能把旧有拒绝不兼容协议的测试直接改成无条件成功。
- [ ] 扩容使用三项独立事实：已核实空闲、不可用占用、身份明确且未超时的启动中容量。需求计算采用下面的非负缺口，未知容量不提供抵扣：

```go
func runtimeCapacityDeficit(waiting, verifiedFree, verifiedStarting int) int {
	if waiting < 0 { waiting = 0 }
	if verifiedFree < 0 { verifiedFree = 0 }
	if verifiedStarting < 0 { verifiedStarting = 0 }
	remaining := waiting - verifiedFree - verifiedStarting
	if remaining < 0 { return 0 }
	return remaining
}
```

```go
func TestRuntimeCapacityDeficit(t *testing.T) {
	for _, tc := range []struct{ waiting, free, starting, want int }{
		{1, 0, 0, 1}, {1, 1, 0, 0}, {9, 0, 8, 1}, {1, 0, 8, 0},
	} {
		if got := runtimeCapacityDeficit(tc.waiting, tc.free, tc.starting); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
}
```

- [ ] 在测试集群执行 `go test ./internal/services -run 'Test(RuntimeCapacityDeficit|PhysicalExpansion|StrictScheduler|Pending)' -count=1`，核实分配与容量回归后提交本任务。

## Task 2: 可信扩容配置与异常池补容量

**Files:** `runtime_pod_scale.go`、`k8s/runtime_pod_scale.go`、`k8s/runtime_pod_scale_out.go`、`runtime_scheduler.go`；测试 `runtime_pod_scale_mysql_test.go`、`runtime_scheduler_scale_out_test.go`、`runtime_scheduler_capacity_proof_test.go`。

**Interfaces:** 保留 `ScaleOut(ctx, namespace, name, minReplicas) error` 的副本下限语义。普通创建和手动扩容共用有效配置证明；容量观测失败不能取消该证明。

- [ ] 增加“所有 Pod Unknown，但控制器与持久批准匹配时可以扩容”用例，以及 UID、镜像、挂载、资源、安全配置、批准来源变化时拒绝的负例。先在 54 运行确认失败。
- [ ] 保存已验证配置的 Runtime 类型、namespace、Deployment UID、模板指纹、不可变镜像、协议、资源、安全与存储身份及批准来源。只从现有已核实配置生成记录；不把当前未知 Pod 当成批准来源。
- [ ] 将 `defaultScaleSnapshot` 对新鲜健康 Pod 的必要依赖替换为“健康源现场核验或仍有效的持久配置证明”，二者均与当前 Deployment 校验一致。没有健康源 Pod 的场景必须实际通过，不能只修自动扩容。
- [ ] 两条自动扩容路径不再因某个运行观测失败跳过整个已批准池。按 Task 1 缺口计算需增加的 Pod，保留每轮并发、总量与资源边界；每次原子只增副本，不覆盖较新人工配置。
- [ ] 以 Pod UID、扩容任务及启动截止时间识别确实启动中的容量。Unknown、失败或已超时 Pod 不无限抵扣缺口；资源不足进入有界等待，不持续增副本。
- [ ] 在 54 执行 `go test ./internal/services ./internal/services/k8s -run 'Test.*(ScaleOut|PhysicalExpansion|RuntimePodScale|Capacity)' -count=1`，并运行需要 MySQL 的同类测试；确认日志没有环境跳过后提交。

## Task 3: 单 slot 手动重置与物理退出凭据

**Files:** 新建 `runtime_slot_reset.go`、`runtime_slot_reset_test.go`；修改 `runtime_management.go`、`runtime_slot_audit.go`、`runtime_slot_operator_fence.go`、`runtime_fault_slot_release.go` 及精确终止适配代码。

**Interfaces:** 增加管理员 API，路径均在现有 `/api/v1/admin` 下：

```text
POST /runtime-pods/:id/slots/:slot/reset/preflight
POST /runtime-pods/:id/slots/:slot/reset
GET  /runtime-scheduling
POST /runtime-scheduling/:id/retry
```

预检生成范围指纹；提交请求固定以下字段，不接收“忽略保护”开关：

```json
{
  "source_pod_uid": "inspected-pod-uid",
  "source_container_id": "containerd://inspected-container-id",
  "scope_fingerprint": "sha256-of-verified-source-scope",
  "request_id": "administrator-request-id"
}
```

服务端 journal 额外固定 Node UID/boot ID、容器名、旧 epoch、assignment 摘要及已知旧属主；未知属主使用独立物理范围，不造实例身份。

- [ ] 先增加固定身份、预检后容器变化、双击幂等、旧 worker 不响应、用户网站孤儿进程、属主未知及节点不可达的回归用例，并在 54 保存失败结果。
- [ ] 原子登记单 slot 隔离，并让所有普通 slot 分配读取这份隔离；避免只隐藏 UI 而后端仍能分配。与该 slot 现有管理任务串行，不长时间冻结整个 Pod 或类型。
- [ ] 复用节点固定的 helper 终止精确旧 ContainerID；旧辅助进程清理错误不能阻止该物理终止。不得重建整个 Pod、修改镜像或跟随当前实例 endpoint。
- [ ] 旧退出凭据持久保存并绑定操作；后续重试消费该凭据，不替换目标 ID 再次停止新容器。没有凭据就保留任务等待或错误，不能用 Ready/restartCount 充当退出证明。
- [ ] 未知属主的物理重置独立验证，不放宽现有 `runtimeeviction.Scope`。新 worker 的账本释放采用精确旧执行凭据；无法核实的本地账本继续隔离并给出原因，不删除共享 NFS 锁。
- [ ] 若现有 Agent 协议不足，在隔离 AgentsRuntime 工作区补齐原生验证与兼容测试；新接口仍要求 CM 认证、固定 Pod/slot/新 worker 身份和匹配旧退出凭据，不能接受单个 force 布尔值清账本。
- [ ] 在 54 执行 `go test ./internal/services ./internal/runtimeeviction ./internal/services/k8s -run 'Test.*(SlotReset|FaultSlot|SlotOperatorFence|FaultEviction)' -count=1`；有 Agent 补丁则执行其 `go test ./clawmanager-agent/slots ./clawmanager-agent/gateway -count=1`，通过后提交。

## Task 4: 条件解绑与恢复调度

**Files:** `backend/internal/repository/instance_runtime_recovery_binding.go`、`instance_runtime_recovery.go`；`backend/internal/services/runtime_instance_recovery.go`、`runtime_slot_reset.go`；新增对应 MySQL 回归。

**Interfaces:** 复用 `InstanceRuntimeRecoveryRepository`、`AdvanceRuntimeRecovery` 和现有 `source_stopped`/`placing` 检查点。待调度由 `creating` 加恢复阶段表示，不新增只为文案使用的实例状态枚举。

- [ ] 先增加真实 MySQL 用例：旧绑定匹配、已迁移到其他 Pod、同 Pod 其他 slot、同代次新运行、新分配仍在启动、解绑事务中断与用户停止竞争。记录修改前失败。
- [ ] 解绑匹配至少覆盖 binding ID、实例、源 Pod、Gateway、端口、代次及工作区；用条件更新与实例生命周期锁串行。不能调用按 instance ID 无条件删除的兜底。
- [ ] 同一事务读取当前分配和期望状态：仍为运行且无其他有效分配时，将旧源退出凭据和待调度意图一起提交；其他 slot 有运行或启动中分配时保留其状态与绑定，不创建第二份恢复任务。
- [ ] 恢复沿用现有调度流程，确认旧执行与必要 writer 隔离后只推进一次代次；优先其他可用 slot，容量不足交给 Task 2 扩容。原 slot 仍隔离或退避不影响恢复。
- [ ] 重新调度前检查停止、休眠、删除意图；它们优先于恢复。重复任务、迟到心跳与旧发布不得复活旧源或覆盖当前实例。
- [ ] 在 54 执行 `go test ./internal/services ./internal/repository -run 'Test.*(SlotReset|RuntimeRecovery|RecoveryBinding|ReportRecovery)' -count=1`，使用真实 MySQL/Redis fixture 验证原子性；通过后提交。

## Task 5: 普通停止、休眠、删除接入受控回收

**Files:** `backend/internal/services/runtime_slot_termination.go`、`runtime_slot_reconcile.go`；AgentsRuntime 的 `slots/container_lifecycle.go`、`slots/fault_recovery.go`、`gateway/container_reset.go`。

**Interfaces:** 保留各自 stop kind、原生休眠证明、旧 ContainerID/C1 或独立精确终止凭据、NFS writer 核验及保留账本释放语义。

- [ ] 在 54 复现辅助清理返回 `auxiliary writer stop unconfirmed` 导致到达不了容器重置的失败路径，覆盖停止、休眠、删除各自不同预期。
- [ ] 保留正常刷盘和优雅退出；正常清理无法完成时，经持久停止意图与精确源校验进入受控容器终止，不无限卡在终止前的清理门槛。
- [ ] 删除不重新调度；停止不重新调度；休眠仍要求原生安全证明，强停不伪造休眠完成。手动重置后的自动恢复只由 Task 4 的明确恢复意图触发。
- [ ] 核实容器终止能清除用户自行启动的业务进程，保留 NFS/PVC；外部 writer、EIO/ESTALE 独立处理，不清空全局账本。
- [ ] 在 54 执行 `go test ./internal/services -run 'Test.*(SlotTermination|ContainerStop|InstanceIdleStop|RestartFollowup|SlotReset)' -count=1`，加上修改后的 Agent 原生生命周期测试；通过后提交。

## Task 6: 管理 API、进度与前端入口

**Files:** `backend/cmd/server/main.go`、新建 `backend/internal/handlers/runtime_slot_reset_handler.go` 及测试；`frontend/src/pages/admin/RuntimePodDetails.tsx`、`RuntimeActions.tsx`、`services/runtimePoolService.ts`、`types/runtimePool.ts`、`lib/runtimeManagement.ts` 与现有语言资源。

**Interfaces:** Task 3 的两条 slot API 与既有任务历史/重试；服务端返回旧源目标、已知实例当前去向、是否需要恢复、当前阶段与明确错误。

- [ ] 增加管理员与普通用户权限、非法 slot、过期预检、重复 request ID 和范围变化的 handler 测试；失败先在 54 确认。
- [ ] 在单 slot 详情提供“强制重置 slot”，与整个 Pod 的故障驱逐分开。预检明确本次终止对象，以及“恢复实例”或“只清理旧残留”的结果。
- [ ] 区分物理阶段与实例阶段：旧容器终止、旧绑定处理、原 slot 等待新 worker；实例待调度、启动中、运行中。提交超时显示结果待核实并查询原任务，不自动创建新任务。
- [ ] Unknown 不禁用所有手动恢复入口；服务端重新校验物理范围。界面不增加泛化配置开关，不显示实现说明，沿用 Fluent 2 控件和自动同步。
- [ ] 在 54 执行 `node --experimental-strip-types --test scripts/runtime-management.test.mjs scripts/runtime-slot-grid.test.mjs`；新增行为测试覆盖预检失效与状态展示。生产 `npm run build` 仅在 54 执行。
- [ ] 浏览器检查 1440、1280、768、390 宽度，中英文与日/韩/德长文本，键盘、焦点返回、权限/错误/长内容、双击及刷新恢复；通过后提交。

## Task 7: 隔离环境完整功能与故障矩阵

**Files:** 新建 `deployments/validation/runtime-slot-reset-acceptance.py`、`frontend/scripts/runtime-slot-reset.browser.mjs`；扩展现有回归入口，不依赖现场实例 ID。

**Interfaces:** 隔离 namespace/资源前缀、精确构建版本、产品 API、SQL/CRI 只读观测和结构化 JSON 证据。故障注入只作用本次创建的测试资源。

- [ ] 冻结测试源码和镜像 digest，建立隔离 MySQL、Redis、Runtime 及 NFS 测试工作区；记录资源、原生能力与实际运行命令。
- [ ] 执行设计文档 1—15 全部场景；容量、停止、休眠、删除、迁移后旧残留、同代次新运行、未知属主、helper 中断、CM 重启与旧报告回放均有正反断言。
- [ ] 重置前在目标与邻居 slot 启动可识别网站；重置后断言旧网站 PID/端口消失、工作区文件保留、邻居与已迁移实例页面持续可访问。实例恢复后验证受管 Gateway/业务启动配置；不把任意手动网站自动重建算成已有能力。
- [ ] 持续检查单 slot 重启时的 Pod Ready、Service、EndpointSlice 和实际管理/网关/浏览器流量；邻居进程存活但请求中断仍判失败。
- [ ] 覆盖四类 Lite Runtime 各自声明支持的完整路径，原生不支持的能力验证明确拒绝并在报告单列，不能冒充正向通过。
- [ ] 执行与改动相关的完整后端、原生生命周期、SQL/Redis 集成和前端回归；测试失败定位根因后修复，只有新改动或未解问题才重复扩大测试。
- [ ] 若启用浏览器 origin/端口范围，检查 server54 对应 NodePort、目标 8444、就绪 EndpointSlice，并验证 OpenCode/DSH 实际页面；不能只检查 30443 管理首页。

## Task 8: HTML 报告、审查和交付

**Files:** `docs/reports/2026-11-18-slot-reset-functional-test-report.html`、同名 JSON、证据包；更新 `docs/runtime-reconciliation-recovery.md` 和 `deployments/k8s/lite-slots/nfs-rootless.md` 的实际已实现内容。

- [ ] 在 54 汇总 Task 7 的逐项结果，包含场景、步骤、预期、实际、耗时、版本、日志和截图；PASS/FAIL/SKIP/未执行分开统计。
- [ ] 生成可独立打开的 HTML；所有结果对应原始证据，保留失败和环境限制，不将测试数量作为功能覆盖的替代。
- [ ] 对精确旧源停止、同代次保护、解绑与待调度原子性、扩容并发、节点 helper 权限、原生休眠/NFS 安全及 UI 交互做完整代码审查，修复后补对应回归。
- [ ] 本地浏览器打开报告，检查链接、统计与证据一致；交付源码提交、测试命令、实际结果、HTML 与剩余限制。
- [ ] 发布时 CM 与 AgentRuntime 分开：构建在 54，Runtime 通过 `deployments/runtime-upgrade/upgrade-runtime.py` 的原生准备、预检、升级、验证与记录。若升级保护拒绝，保留并报告具体原因，不清除保护或直接修改 Deployment。

## 依赖与执行顺序

Task 1 → Task 2；Task 3 → Task 4 → Task 5；Task 2、4、5 完成后接 Task 6；之后 Task 7 → Task 8。实际采用同一会话顺序执行，各任务先红后绿并单独提交，不在计划阶段执行构建、故障注入或现场变更。

## 设计覆盖自查

容量与可信扩容对应 Task 1—2；单 slot 管理与未知属主对应 Task 3；新运行保护和自动恢复对应 Task 4；普通生命周期、NFS 和退避对应 Task 3—5；管理入口与状态对应 Task 6；设计全部 15 项验收对应 Task 7；用户要求的 HTML 报告和发布边界对应 Task 8。
