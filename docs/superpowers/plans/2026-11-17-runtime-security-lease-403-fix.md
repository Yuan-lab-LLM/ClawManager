# Runtime security lease 403 修复计划

> **For agentic workers:** Use `subagent-driven-development` for implementation and independent review. Preserve all earlier `fix-schedule` changes.

**Goal:** 防止受保护实例首次并发创建时，把 assignment 缓存可见性窗口或签发阶段短暂库存失效变成不可恢复的启动错误。

**Architecture:** CM 发布 assignment 后，用本次发布对象的名称、UID、resourceVersion 和不可变请求确认共享安全库存。未追上时沿用持久 assignment 的 pending/reconcile，保留绑定和槽位。租约领取仅对明确的库存暂态错误，在原授权期限内有限次重新验证整组凭证；身份冲突仍拒绝。复用现有库存证明和生命周期围栏，不增加缓存或直接读取回退。

**Tech Stack:** Go、client-go、共享 runtimeinventory、现有 runtimeauth 和 fake Kubernetes 回归；构建与测试在 server54。

## Global Constraints

- 本地不编译 Go、前端生产包、二进制或镜像；均在 `10.130.81.54` 执行。
- Runtime 镜像、身份、槽位布局、副本不由外部脚本修改。本方案 CM-only，不需要更换 Agent 镜像。
- 不把所有 403、assignment 缺失或身份冲突标成暂态。不放宽缓存健康证明，不新增 Kubernetes 直接读取回退。
- 不清零 SQL 认领、不删除业务绑定、不清除停止/删除/升级保护。当前已有实例保留，验收资源通过 CM API 创建、删除。
- 安全日志仅包含稳定原因、关联 ID、实例/generation、Pod UID、assignment UID/resourceVersion、阶段；不记录 token、环境、请求正文或模型密钥。
- 用户明确本次发布与验收目标为 `clawmanager-system` / `30443`，覆盖文档的 chatgroup 历史范围；不得更新 chatgroup。

### Task 1: CM 缓存确认、受限签发与诊断

**Files:** `backend/internal/services/k8s/runtime_assignment_service.go`、`backend/internal/runtimeinventory/{inventory,proof}.go`、`backend/internal/services/runtime_slot_agent_client.go`、`backend/internal/services/runtime_slot_security.go`、`backend/internal/services/runtime_security.go`、`backend/internal/services/runtime_security_lease.go`、`backend/internal/runtimeauth/capability.go`、`backend/internal/handlers/runtime_security_lease.go` 及相应测试。

- [x] 先补失败回归：未观察/旧版本 assignment 不触发 Agent POST；追上后同绑定创建；UID 替换、追上但请求不同、停止/删除意图、Pod 身份变化拒绝。
- [x] 复用发布函数获得本次持久对象；保留原 `EnsureDesired` 调用兼容。共享缓存确认名称、UID、同类型版本、完整请求和身份，pending 进入已有 reconciliation；有绑定/assignment 不重新选 Pod。
- [x] 先补失败回归：签发中库存失效后整组重试成功；持续失效有界失败；取消立即结束；generation/来源冲突不重试；失败不返回部分凭证。
- [x] 保留可识别的内部库存错误。租约最多八次整组尝试、总上限 1.5 秒且服从父期限；退避 25/50/100/200/250/250/250ms，每次完整重验。旧身份、来源不符、状态冲突不重试。
- [x] 为拒绝分类及阶段增加脱敏关联日志，外部响应继续简短拒绝；验证任意错误文本及秘密不会泄露。
- [x] 审查补充：请求与 SQL 已知 user/generation 冲突时，在同一次 SQL 观测后、库存读取前立即拒绝；重试后身份变更也立即拒绝。
- [x] 审查补充：直接新建 G1 的已发布等待以不可变 `CreatedAt` 为起点，最多五分钟；到期显示 `RUNTIME_PUBLICATION_TIMEOUT` 并保留身份、槽位和绑定。调度与持久 assignment 重放均围栏迟到启动，不依赖 Northbound 操作或手动 Start 活动记录。
- [x] 在 54 记录 red/green，运行相关完整包回归。独立审查本次差异，修复 Critical/Important 后再发布。

### Task 2: 测试环境发布与真实并发验收

- [x] 只读重新记录目标环境部署、策略、实例/binding、assignment、物理槽位和资源余量。确认目标与运行实例，创建新测试资源不操作旧实例。
- [x] 54 构建 CM 镜像，记录源码清单和不可变 digest；以 UID/resourceVersion/image CAS 发布 CM，并核对滚动完成。
- [x] 通过 CM 创建 OpenClaw 同池并发实例，覆盖第二物理槽位及仅剩一槽时三实例竞争/扩容；核对首次 running、generation、binding、槽位唯一性和租约日志。
- [x] 验收脚本在 POST 前持久记录精确名称及用户；响应丢失或记录中断后只读发现并确认所有本轮资源，不重发创建。结果未决时不得把清理标为通过；以假 API 重现提交后丢失响应。
- [x] 对 Hermes、OpenCode、DSH 共享路径做新建并发冒烟，完成后删除专属资源；保留任何失败或容量受阻证据。
- [x] 核对实际浏览器 NodePort 数量/范围、8444 targetPort、就绪 EndpointSlice；验证 OpenCode/DSH 实际页面。
- [x] 保存结果报告，复核既有实例、策略及 Runtime 镜像/布局未改变；只在真实证据满足后声明完成。已有实例恢复仅走正常生命周期并以当前现场为准。

## 最终证据

- `clawmanager-system` 已发布 `lease403-20261117`，CM digest `sha256:72a85b999d155febc151224f295a03a86777786767b1fc8e09959546a56a5d0e`；源码清单 `a6a6aafac6f6b76a244225e1636154429408ebc07cf2cf4f131b56ed6c8b034a`。
- 最终六包及两组隔离 SQL 回归、六项脚本内存回归与独立复审通过。
- 最终 15 个新实例 G1 并发、双槽及单空槽扩容等八项检查通过；26 次租约全部 200。峰值槽位无重复或超额，既有/新建 OpenCode/DSH 页面及 128 个 NodePort/8444/就绪 EndpointSlice 通过。
- 两轮脚本中止记录保留，三轮共 19 个实例及绑定已清理；原有 36 个实例与 7 份绑定未变，全局策略未改，临时 SQL 命名空间已删除。
- [完整 HTML 报告](../../reports/2026-11-17-runtime-security-lease-403-test-report.html) 已以 Chrome 验证 1440/1280/768/390 宽度，无横向溢出或外部脚本。
