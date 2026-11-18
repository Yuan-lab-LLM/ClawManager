# Runtime 升级补齐实施计划

> **For agentic workers:** 使用 subagent-driven-development 按任务执行；独立的原生适配器、CM 协调器与现场双类型修复可分别负责，合并前逐项审查。

**Goal:** 完成四类当前 Runtime 的新 slot 能力上线，补齐 OpenCode 保留数据的原生 Agent 升级。

**Architecture:** OpenClaw/Hermes 复用已有 CM 原生路径和真实退出回执；先修旧管理任务造成的跨类型饥饿。OpenCode 新增有明确原生版本边界的专用升级协调器及原生 capsule 适配器，复用维护 sandbox 和现有持久升级模型，不开启旧的不安全 rolling 分支。

**Tech Stack:** Go、现有 MySQL/Kubernetes 仓库与客户端、Python 标准库 sqlite3、既有 Runtime CLI。

## Global Constraints

- 所有构建和运行测试在 10.130.81.54；本地仅源码、文档、格式检查与传输。
- 清数据许可仅覆盖 OpenClaw/Hermes 测试数据；OpenCode 保留、DSH 不扰动。
- 原 AgentsRuntime 工作区不改；使用现有 `.worktrees/AgentsRuntime-database`。
- 原生支持 2.0.27/revision 1f4d07a4a519185955ae8ab2ea3607805509e386、同 migration journal；未知来源拒绝。
- 不伪造 SQL/物理回执、不清未知保护、不把旧失败记录改为成功。

## Task 1：管理任务公平轮转和双类型更新

**Files:** `backend/internal/services/runtime_management.go`；新增 `runtime_management_queue_fairness_mysql_test.go`。

**Interfaces:** 保留 Process(context.Context) error 和各 operation 既有 journal；操作仍由选主及 owner_token CAS 串行处理。

- [ ] 实库测试创建旧 Hermes recovery 与新 OpenClaw 准备；持有旧实例锁，使旧任务需要重试，断言新类型可以推进。54 运行 RED。
- [ ] 对 resumable pending 按更新时间公平轮转，并验证并发认领和重启不扩大权限；54 运行 GREEN 与 management 回归。
- [ ] 独立提交精确改动；构建及发布对应 CM 修复，保留全部运行配置。
- [ ] 原 OpenClaw CLI 同状态继续准备和发布，验证实际镜像及 capability。
- [ ] Hermes 原恢复任务仅按正常 Retry 继续；消费已存在真实 helper 回执，正常完成实例 242 删除，再原生升级。若还有持锁路径，补定位和回归后修复。

## Task 2：OpenCode 原生 preserve adapter

**Files:** `opencode/lite/upgrade.go`、内嵌固定 Python helper、`upgrade_test.go`、真实 native acceptance；`profile.go`；必要的 overlay Dockerfile 元数据。

**Interfaces:** 实现 gateway.RuntimeUpgradeProvider、ScopedNativeUpgradeProvider；复用 WorkspaceUpgradeRequest 和既有 compatibility/migration/restore results。ProtocolVersion=opencode-preserve-v1，SessionStore=sqlite；能力 opencode.native-preserve-v1、opencode.workspace.writer-lease、opencode.session-continuity-v1、opencode.runtime-standby-v1。

- [ ] 在 54 以真实 SQLite WAL 和 v2 表建立失败用例；未知 journal、损坏 DB、路径覆盖、跨身份均拒绝。
- [ ] compatibility 只读源固定路径，写私有验证 receipt；migrate 保留 DB/WAL/SHM/固定状态，在 capsule 副本做完整 backup 与 catalog 验证。
- [ ] status 重验 capsule，恢复同身份幂等；restore 保留新增历史，故障/中断保留 displaced 文件和待核实状态。
- [ ] 54 运行 OpenCode 模块、共用 Agent 相关回归和实际 UID/NFS 维护容器验证；提交原生代码，记录精确文件 hash。

## Task 3：CM OpenCode 协调器、API、CLI

**Files:** 新增 `backend/internal/services/runtime_opencode_upgrade*.go` 与对应实库测试；`runtime_scheduler.go`、`runtime_rollout_sleep.go`、`runtime_pool_handler.go`、`deployments/runtime-upgrade/upgrade-runtime.py` 及测试；按需复用/限定 repository 与 k8s 接口。

**Interfaces:** 新 strategy 为 opencode_native_preserve_v1；保留 runtime_rollouts、runtime_upgrade_items、gateway_runtime_upgrades 及持久 checkpoint。既有六个原生维护动作；EnsureUpgradePool/SetUpgradePoolActive 创建隔离 standby，旧 rolling 继续拒绝。

- [ ] 添加 RED：缺失原生契约/错误 digest/未知来源不得修改共享池；支持同原生版本的请求进入专用协调器。
- [ ] 冻结 source UID/template/storage 和原生身份，持久队列与 per-instance fence；控制面错误不当成空池。
- [ ] 逐实例源 C1 结束、外部 writer fence、目标兼容/备份/连续性验证、原期望状态恢复；每阶段提交后可续跑原操作。
- [ ] 新运行失败时使用已验证 capsule 和原镜像保留恢复；不覆盖升级后历史，未知结果保持保护。
- [ ] API 与 CLI 开放专用策略，保留旧调用兼容及明确拒绝；真实 MySQL、HTTP 契约和中断恢复 GREEN。

## Task 4：冻结、集群发布和功能交付

**Files:** 新版本 HTML/JSON/证据包与升级文档；54 专属构建与验收目录。

- [ ] 冻结 CM/Agent 提交，全部编译与 OCI 构建在 54；不可变 digest 和所有产物 hash 对应源码。
- [ ] 发布 CM；核对持久配置、数据库迁移、就绪和真实版本。
- [ ] OpenCode 仅 CM 原生升级；先空池 legacy bootstrap，再以有真实原生会话与项目文件的专用实例验证 preserve 升级、失败及回滚保护。
- [ ] 验证 OpenCode slot 重置、网站进程结束、NFS 标记保留及邻居实际访问；核对 OpenCode/DSH 页面与全部 128 NodePort→8444/Ready EndpointSlice。
- [ ] 全量后端/原生/前端相关回归；修复失败后再冻结必要变更，独立代码与证据审查。
- [ ] 四类型实际容器镜像/capability/CM 操作记录核对；生成可独立打开的 HTML，清理专属测试资源，交付剩余限制。
