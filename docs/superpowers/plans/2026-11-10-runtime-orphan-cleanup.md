# Runtime orphan cleanup implementation plan

**Goal:** 修复删除时“无 binding 即成功”的漏洞，闭合迁移/删除的现场清理，并为已无 SQL 实例的旧 Pod 提供可重试的手动恢复。

**Architecture:** 复用现有生命周期锁、管理任务 journal、独立节点 helper 和 PVC 身份校验。删除必须证明从未发布或旧执行者已退出；数据库缺失本身不是停止证明。孤儿 Pod 恢复只适用于没有有效绑定、没有当前运行实例发布意图的精确 Pod UID，保留项目/PVC，恢复任务持久化且重试不会扩大作用范围。

**Global constraints:** 不清空数据库、不批量停止池、不删 PVC、不打断健康实例；不使用时间过期当退出证明；不改写假的 Agent ACK；不让旧报告复活已经释放的绑定。节点/身份/存储无法确认时保留任务和具体阻塞。测试环境通过产品 API 验证；提交推送 preview，不建 PR。

- [x] Task 1: 删除无绑定缺口。先写回归：没有 binding 但存在旧 assignment/迁移源时不能删除实例；从未发布和有可靠停止凭据的实例正常删除。共用 cleanupV2GatewayBinding 路径覆盖普通删除及 reset 清理。检查实例删除是否级联移除源凭据，保证清理未完成前保留实例/deleting 或独立 journal。
- [x] Task 2: 孤儿 Pod 恢复。预检固定源 UID、controller、Node UID/boot、全部旧容器 ID、原始 PVC，核对 SQL/发布意图没有有效所有者。先暂停调度；独立确认旧 Pod 全部执行者退出，修复仅属于旧 UID 的管理账本，退休旧发布记录；旧 Pod 重建或回收使用现有维护流程。任务支持 checkpoint/retry/cancel-before-fencing；迟到注册/报告不能覆盖被退休旧源。真实停止证明前不标空闲。
- [x] Task 3: 管理入口与统计。Unknown 不禁用“检查/故障恢复”入口，正常驱逐仍保留严格校验；展示有效绑定、残留归属、未知状态，避免把旧上报当业务实例数。复用现有操作记录和 retry UI。
- [x] Task 4: 验证与部署。源身份/代次替换、新绑定竞争、helper 失败/重启、PVC/健康邻居保留、普通停止/删除回归；独立审查后构建并更新测试环境，逐个处理已核对的孤儿 Pod，清理所有自建辅助资源并保存验收。

测试现场结果与仍独立存在的容量阻塞见[验收交接](../handoffs/2026-11-10-runtime-orphan-cleanup.md)。
