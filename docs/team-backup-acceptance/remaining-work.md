# Team 备份完整交付：剩余工作与继续实施入口

更新：2026-10-24，北京时间。本页不缩减最终计划，也不把组件验收改成整阶段完成。

## 结论

当前已实现并真实验证“六类数据加密备份 → 外部固定版本发布 → 隔离数据恢复 → 独立只读核对”，以及独立完整性验证 Job 的持久化协调。**控制台的一键正式备份、自动隔离演练和全部生命周期操作尚未完成，不能作为完整功能交付。**

按原计划五个完整阶段的通过标准，目前没有一个阶段具备全部必需证据，即 **0/5 完整阶段通过**。这是严格的阶段统计，不表示没有完成组件开发。功能测试保持 `functional_test + best_effort`，`acceptance_eligible=false`；创建正式备份仍被门禁拒绝。

## 下一步应先打通的关键路径

1. **先补 writer inventory 与完整停写，不能先放开创建按钮。**
   - 已有 `internal/backupgate/driver.go` 覆盖官方 SQL 连接，`boundary.go` 与 middleware 覆盖同步请求。两者默认关闭。
   - 还要登记后台队列/异步 goroutine、Redis 消费、对象与文件写入、WebSocket/反向代理、升级/生命周期和其他裸连接。
   - `teamService.StartBackground`、Owner worker、runtime readiness/provision、materialize worker、runtime scheduler 是明确接入点；按一批可完成工作持有边界，不在无限循环外持锁，也不把取消 context 当成工作停止。
   - Runtime 只修了 maintenance 下的新派发。需完整覆盖 OpenClaw/Hermes 的模型、工具、子进程、回调、presence/local status，并提供绑定 generation/真实 writer 身份的 ACK。不能简单停止 presence 就宣称所有工作停止。
   - 真实长工具、网络中断、多副本、并行升级/新任务与失去 leader 下，证明旧写已结束且源业务可安全恢复。
2. **实现 capture/restore 持久化执行器。**
   - 可复用 `VerifyTaskRunner`、`PrepareAttempt`、固定请求、UID 接管、绝对截止时间与收尾，但不能直接把 verify 状态机改名。
   - capture 必须完成前置健康证据、gate、快照/暂存、源恢复、seal/publish；每个阶段冻结请求和外部动作，未知结果不能重复造资源。
   - restore 必须先记录目标资源 intent，再创建 namespace/数据库/Redis/卷/对象前缀，记录实际身份，建立隔离和最小权限。
   - 每次恢复使用独立写身份，完成后撤销写凭据，再在单独 Job 调用 `VerifyRestoredData`。它的 intent 应来自可信控制记录，不能从可编辑目标报告推导。
   - 失败/取消/换主也要完成有归属的清理并保留结果；bootstrap 共享配置与本次临时凭据要区分。
3. **完成正式发布与灾备发现。**
   - 当前 `_COMMITTED` 和固定版本读取只支持已知 origin/backup identity；没有 `_ACCEPTANCE` 和完整 catalog。
   - 按 v12 分离采集、发布、最终签名身份，冻结 provider 请求 bytes、版本、签名用途和外部 action；实现未知结果判等接管。
   - catalog scan/import 必须证明完整性；不能把普通 ListObjects 或 404 当成完整扫描/不存在的权威证明。
   - 在断开源数据库、源 API、源 Secret 和源网络后，仅用灾备 bootstrap、历史 key 与外部目录完成发现和演练。
4. **接上安全配置与全部前端操作。**
   - 需要近期强认证 proof/issuer/session/request-hash 绑定、nonce 消费与业务提交同事务、权限管理及完整 mutation/tombstone 账本。
   - 现有配置/取消幂等已提交且通过并发与回滚注入；不能据此宣称其他 mutation、nonce 与销毁操作已实现。
   - 前端已有入口、配置子集、列表/详情/取消与完整性报告。尚缺完整存储/密钥配置、创建演练、Team 明细报告、灾备目录、hold/prune/cleanup/promotion 页面和真实 API。
   - 不用 mock、手工填健康状态或给历史测试任务伪造 eligible 来演示完成。
5. **补 lifecycle、监控和完整矩阵。**
   - 实现 hold、最低恢复点、prune 预览/确认/候选漂移、部分失败重试、tombstone、staging/resource/evidence/control GC。
   - 增加真实告警、新鲜度、停止进展、容量与未恢复源业务检测，以及可执行应急手册。
   - Kubernetes/k3s × cluster/single-node 四个独立受支持安装，各自完成 eligible backup、eligible drill 和 promotion；再运行旧 Team/Owner、文件、Runtime 升级/生命周期回归。
   - 当前 32543 使用 NFS 共享及底层 HostPath，尚未完成 provider 分类/支持性证明，不能默认当成四矩阵的任一正式通过样本。

## A01～A24 的证据与缺口

下表的“部分”都不是整项通过。更早组件测试记录见 execution.md 和 implementation-status.md。

| 验收 | 已有证据 | 尚缺的整项要求 |
| --- | --- | --- |
| A01 | 75 表 registry、真实 migrations、未知表/状态拒绝、构造 DDL 移除、完整 SQL inventory | Runtime 键族/全部 writer 唯一归属与自动覆盖闭环 |
| A02 | 六源联合恢复、真实基础服务、large-smoke 固定计数与读回 | 全场景独立 fixture 清单、所有历史/软删/不同 Runtime 模式及正式一致性 |
| A03 | 多 provider/owner/CM 数据库归属恢复测试 | 实际用户可见性、共享/私有卷多 owner 无混用全链路 |
| A04 | 隐藏/部分文件、Linux 元数据、穿越/重复/篡改负测、2 GiB 读回 | 活跃保存中断、源缺失记录、全部卷/链接/provider 矩阵 |
| A05 | SQL 事务排空、HTTP 客户端取消后仍持有边界、过期 lease 不当停写 | 所有 worker/外部动作与升级竞争、真实全局 writer barrier |
| A06 | 两 Runtime maintenance 派发回归；控制器换主复用 Job | 模型/工具/子进程/presence 全部 ACK、源失败恢复与 forced-resume 资格 |
| A07 | Redis DUMP/type/streams/groups/PEL 原始保留；历史恢复不重放、只读 ACL 验证 | 正式 absolute expiry 策略、时间偏差/恢复到期差异与强 checkpoint |
| A08 | 加密帧/签名/固定版本/缺分片/篡改/conditional-create | 独立 finalizer 与四类外部提交未知结果的完整接管 |
| A09 | 删除 staging 后外部读取；验证 Job 无源配置且网络拒绝源 MySQL/Redis | 完整 catalog 发现、源安装整体断开、所有历史 key/provider 权限矩阵 |
| A10 | 部分结果保留、已完成子项保留、Team 状态摘要、SQL 独立读验 | 全 revision/人工审核/产物缺失组合、前端逐实体证据报告 |
| A11 | 闭集 normalizer、升级中间状态、重复原状态证据冲突拒绝 | 全执行阶段进程崩溃与恢复归一化的自动接管 |
| A12 | 保留原始 artifact、归一化旧操作、历史 Redis 命名空间；官方业务进程拒绝恢复库 | 所有 worker/Owner retry/ready/provision/升级生命周期在实际恢复上下文中的零执行证明 |
| A13 | 身份/调度/绑定归一化、加密 Secret 历史、不 Apply 旧资源 | 磁盘秘密与所有旧外联身份的完整失效和可证明撤销 |
| A14 | 独立 SELECT-only/Redis read-only/MinIO read-only；拒绝覆盖已拥有目标 | 全目标自动规划/资源重叠检测、只读挂载、写凭据销毁和 verifier 网络隔离 |
| A15 | 真实 controller+Job 换主、UID/固定请求、报告、绝对期限、实际 Pod 校验 | capture/restore/finalizer/provider 全故障注入与独立 cleanup 收敛 |
| A16 | 创建/配置/取消幂等、并发、旧版本/CAS、账本提交故障回滚、角色拒绝 | strong-auth nonce、所有 mutation 作用域/永久 tombstone、正式职责权限拆分 |
| A17 | 同步 SQL/请求边界测试、默认关闭不影响现有业务 | 活跃 Team/Owner runtime ready/文件/升级真实并发验收 |
| A18 | 无完整实现 | catalog receipt、漏页/交错/tombstone/provider 证明、接管 |
| A19 | 实际测试 UI 入口/版本/未就绪提示；基础 API 与报告 | 从配置到正式备份/演练/保留清理的全部页面链路 |
| A20 | 轮询退避/终态停止、稳定幂等 key、普通登录与强认证挑战区别、服务端权限 | 全后台工作与页面生命周期/关闭功能后的真实全链路收尾 |
| A21 | 无完整实现 | hold/prune/confirmation/部分删除重试/tombstone 全部 |
| A22 | 验证 Job 先持久报告、后 Foreground 删除并确认 Pod 消失 | 恢复失败/取消/未知归属、卷/凭据/证据/所有 resource GC |
| A23 | 部分 Go/TS DTO、闭集错误/状态、严格 JSON、报告不含 payload/凭据 | 全部机器合同 parity、证据 Range/下载审计、全配置字段与状态映射 |
| A24 | large-smoke 达最低数据量并恢复核对；相关后端/前端回归 | 四个 eligible 安装、完整资源测量、告警触发/恢复、全部旧功能与正式发布门禁 |

## 效率复盘与改进

本轮主要问题不是某一个测试没跑完，而是完整计划包含系统灾备平台的公共依赖，远超单独导出 Team 数据；早期没有把“控制台可用的纵向链路”与全平台依赖的工作量明确核算，导致组件完成很多，但整阶段不能验收。对此不能通过删掉原计划要求或降低成功标准解决。

执行中还出现了测试夹具缺少服务器迁移文件、短暂并行使用相同 Redis 库、重复全链路检查、部署脚本在 NodePort 尚未收敛时立即探测等时间损耗。后续已分离 Redis 逻辑库、固定测试预算、让长容量测试后台运行、针对变更选回归，并把 SQL 逐行往返改成每表流式读取。

继续开发应先冻结剩余机器合同与真实存储支持范围，再按上面的关键路径贯通一个正式支持矩阵。将可独立工作的模块拆成边界清晰的任务；如用户授权并行代理，再分工，避免共享文件/夹具冲突。每次只对新变化运行必要测试，里程碑统一跑完整矩阵。镜像在代码冻结后合并构建，发布探测使用有界重试并记录原失败，不反复部署只为刷新版本号。全过程继续保留“本地完成、镜像推送、实际部署、整体验收”四种不同事实。
