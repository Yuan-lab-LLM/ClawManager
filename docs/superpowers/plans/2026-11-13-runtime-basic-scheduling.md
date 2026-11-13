# Runtime 基础调度 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 日常调度只保留创建时扩容、实例休眠/唤醒、状态同步和空 Pod 回收到 1。

**Architecture:** 复用现有槽位分配和持久化管理流程。日常循环与显式维护/升级循环分开，删除日常自动恢复及冷启动分支。

**Tech Stack:** Go、现有 MySQL 仓储、Kubernetes Deployment。

## Global Constraints

- 构建及测试在 `10.130.81.54`，本地只准备、校验、传输源码。
- 不绕开 CM 原生升级、安全证明、生命周期锁及备份工作单元。
- 不清空既有异常、恢复或升级记录；不变更用户实例。
- 不新增依赖或数据库结构。

### Task 1: 自动缩容保留一个 Pod

Files: `backend/internal/services/runtime_idle_pool.go`、`runtime_pod_scale_in.go`、`runtime_pod_scale_in_worker.go`、`runtime_idle_pool_mysql_test.go`。

- [x] 修改原“缩容至 0”集成用例，断言两 Pod 只选一个、目标为 1，重复扫描不缩最后一个。
- [x] 在 54 执行 `go test ./internal/services -run 'TestRuntimeIdlePoolMySQL' -count=1`，确认新断言失败。
- [x] 选取来源最多 `len(pods)-1`；计划、提交及执行拒绝 `AutomaticIdle && Plan.Replicas < 1`；拒绝旧请求时沿用已有取消/解除保护流程。
- [x] 在 54 重跑上述用例及自动缩容 admission/recovery 用例，确认通过。

### Task 2: 普通调度只处理明确启动意图

Files: `backend/internal/services/runtime_scheduler.go`、`runtime_upgrade_guard.go`、`runtime_scheduler_test.go`。

- [x] 将旧自动恢复/故障迁移测试改为断言保留原绑定、代际及异常状态，不调用创建。
- [x] 在 54 运行 `go test ./internal/services -run 'TestRuntimeScheduler' -count=1`，确认新策略断言失败。
- [x] 删除日常自动 recovery/failover 和历史冷池恢复调用；只让 `creating` 进入分配；`running` 无绑定记录 `RUNTIME_BINDING_MISSING`。
- [x] 扩容 backlog 只计 `creating`，按类型收集错误，某类型失败继续其他类型。
- [x] 在 54 重跑调度、身份、槽位保留、休眠停止及类型隔离用例。

### Task 3: 显式操作与调度分开

Files: `backend/internal/services/runtime_scheduler.go`、`runtime_scheduler_operations.go`、对应测试。

- [x] 增加长升级阻塞时日常循环仍能完成的通道同步用例，确认旧串行实现失败。
- [x] 从日常 `reconcile` 移出 `management.Process`、`reconcileRollouts`；独立 ticker 调用原流程，保留 leader 和完整 backup work-unit，不缩短 native 操作超时。
- [x] 删除自动历史故障回收触发点；显式管理操作仍保留原工具。
- [x] 在 54 运行相关服务及仓储用例，完成代码审查；记录尚未发布和旧环境阻塞事实。


## 验证结果

2026-11-13 在 10.130.81.54 的独立源码目录 `/root/cm-fault-slot-release-20261113/basic-scheduling` 完成：

- 新策略用例先失败后通过：自动缩容保留 1、拒绝旧自动缩零请求、维护与日常调度分离、扩容错误按类型隔离、错误实例需要明确启动、同进程并发续租、Pod 启动等待、代际同步校验、已停止源恢复的代际保护。
- 相关 services 回归通过，耗时 40.232s；原生升级准备、休眠历史兼容、缩容证明和手动恢复用例均在此范围内。
- repository 查询及 K8s 缩容/睡眠/唤醒用例通过，分别耗时 0.029s / 0.057s。
- `go build -o clawreef-basic-check ./cmd/server` 在测试集群完成。
- 本地只执行源码准备、gofmt、diff 校验和传输，没有构建二进制或镜像。
- 初次 services 全量执行未通过：除随后已更新的新策略断言外，旧版 Pro 数据库测试 fixture 缺少 `desired_config_revision_id`，独立源码包亦缺少跨仓库 Redis 插件资源。不宣称全量测试通过。
- 未发布 CM，未更新 AgentRuntime 镜像、未改变 Runtime Deployment，未清除保护或修复既有零容量池。

最终服务回归命令（由远程 `go-test.py` 注入独立测试库配置）：

```sh
go test ./internal/services -run 'TestBasicScheduling|TestRuntimeLeader|TestRuntimeScheduler|TestRuntimeOperations|TestRuntimeIdlePool|TestRuntimeRolloutPrepare|TestRuntimeRolloutPreparation|TestRuntimePrepareIgnoresOtherType|TestRuntimeHeldScaleIn|TestProtectedScheduler|TestRuntimeManagementMySQLRefresh|TestRuntime.*Recovery|TestRuntime.*ScaleIn|TestRuntime.*Admission|TestRuntime.*Idle|TestInstance.*Idle|TestInstance.*Start' -count=1 -timeout=180s
go test ./internal/repository ./internal/services/k8s -run 'TestV2Desired|TestBuildV2Scheduler|TestRuntime.*ScaleIn|TestRuntimeIdle|TestRuntime.*Sleep|TestRuntime.*Wake' -count=1 -timeout=90s
```

旧自动冷启动及自动重试实现已删除。保留原生升级的历史 sleep journal、K8s 证明接口和 PreviousIdleStorage；历史 fixture 直接在 fake Kubernetes 构造旧完成记录，不放宽生产准入。无绑定的启动错误会结束相关维护条目，避免等待已被移除的自动重试。
