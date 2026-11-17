# Runtime 调度修复与 server54 验收计划

**目标：** 按工作区两份 2026-11-17 调查文档修复抢槽竞争、扩容错误分类及缩容源心跳计数，发布 clawmanager-system，生成四类生命周期 HTML 测试报告。

**方案：** 复用现有 pending/tick 重观察流程，不取消原子抢槽，不扩大升级通用重试分类。缩容源在同一条 SQL 按源 ID、UID 和未结束操作冻结心跳占用，保留正常释放。

**约束：** 本地仅源码和无产物校验；所有 Go 编译、测试及镜像构建在 10.130.81.54。Runtime 镜像升级只能走 CM 原生流程，不绕过身份、存储或未完成操作保护。此次目标命名空间由用户指定为 clawmanager-system，覆盖旧文档的 chatgroup 范围。

- [x] 先只读调查 system 已有异常实例，记录 SQL、绑定、物理槽位及错误证据。
- [x] 在 54 隔离源码目录先跑抢槽失败测试，再修复 assignInstance：尝试其他候选；真实错误优先；纯抢槽竞争返回 pending。覆盖四种 Runtime、下一 tick 成功及混合错误。
- [x] 为 ScaleOut 写失败增加结构化状态诊断；仅明确并发变化或写结果不确定返回局部类型化 pending；403、无效参数和 UID/运维保护继续拒绝，验证副本下限和 CAS。
- [x] 复用并审查历史心跳修复，先运行隔离 MySQL 失败测试，再改 UpdateHeartbeat 的原子 SQL。覆盖四个未结束状态、终态、源 ID/UID、其他 Pod 及正常释放。
- [x] 核对 system 物理 8 槽和启动并发配置。chatgroup 2 槽配置可修正源码，但不通过直接编辑现有 Runtime Deployment 迁移布局。
- [x] 修复既有停止围栏拒绝中断同 Pod 健康实例上报的问题；仅隔离已知停止拒绝，未知数据库错误仍中断。
- [x] 根据实测补充手动停止事务 MySQL 1213 有限重试，以及持久停止意图遇原生槽位短暂不可用时返回 202；保留绑定及终止证明保护。
- [x] 在 54 运行 services、services/k8s、repository、handlers 回归并审查变更；两组隔离 MySQL 用例均启用。补齐旧测试夹具缺失的团队表结构，保留原断言。
- [x] 备份 system 发布前 spec 与实例状态；54 构建 CM 镜像，保留现有前端及侧车，以不可变 digest 和 UID/resourceVersion CAS 发布 CM。Runtime 镜像不隐式更新。
- [x] 通过 CM 创建专属测试实例：OpenClaw、Hermes、OpenCode、DSH 各单个及批量创建，验证休眠、唤醒、停止、启动、重启和删除；记录 generation、binding、槽位、工作区文件一致性。不得删除既有实例。
- [x] 核对浏览器 NodePort、8444 targetPort、就绪 EndpointSlice，浏览器验证既有和新建 OpenCode/DSH 实际页面。
- [x] 生成脱敏 HTML 测试报告，包含异常调查、镜像/源码信息、每项结果与证据、未通过/受阻项及测试数据清理结果。

## 验收结果

- 最终 CM 镜像：`clawmanager.local/clawmanager@sha256:e92cf50aae1cc6533ee73f0b531d8a94fe295dadf554279cf3b31305fae52d17`。
- 部署版本：`fix-schedule-20261117-lifecycle`；源码清单 SHA256：`145988e3fae37c005bbbb01ff8ec5a2868f464c7bd33e8787db0f0dbaf9d77c9`，版本接口短值是清单摘要，不是 Git commit。
- 最终稳定发布后，以每类型单独 1 个、批量 2 个的新实例完整复测：56 项生命周期检查及 1 项清理检查全部通过。唤醒、启动、重启均验证 generation 推进及工作区文件 SHA256 保留。
- 首轮批量 8 个暴露上报隔离缺陷、CPU 资源不足及发布重叠的连接中断；第二轮暴露停止死锁及同步窗口不足。失败记录完整保留在报告中，最终结果只采用第三轮实际结果。
- 三轮共 60 个测试实例及绑定全部删除；原有 36 个实例状态、generation、错误信息未变，原有休眠策略及排除设置已恢复；两套临时 MySQL 测试命名空间已清理。
- 原有异常 233、242、264 未强制恢复，终止证明及身份保护保留。AgentsRuntime 仓库已纳入初始化与源码核对，本轮无需修改 Runtime 镜像或布局。
- [HTML 测试报告](../../reports/2026-11-17-fix-schedule-test-report.html) 包含完整结果和历史失败；已用 Chrome 验证 57 行通过、无脚本，并检查 1440px/390px 布局。
