# Runtime 故障驱逐设计（已确认）

用户已确认复用现有驱逐功能恢复实例与 Pod 的关系。原先“停止整个运行池”的草案不采用。

本设计描述有绑定的实例故障驱逐。无绑定旧 Pod 的精确恢复通过独立的 `orphan-recovery` 模式补充，见[无绑定残留清理设计](2026-11-10-runtime-orphan-cleanup-design.md)；不会扩大本模式的实例选择范围。

## 范围

- 以数据库业务意图为准，按实例／槽位处理；默认只选择 creating、error、stopped、deleting 的现有绑定。
- 健康 running 实例只有明确选择并确认影响后才允许驱逐。同 Pod 其他槽位及其他 Pod 不重启，不缩整个 Deployment。
- 项目、Git、PVC 保留；确认旧执行者停止后，才退休分配并释放绑定。
- 无目标容量也可清理旧源。需要运行的实例交给原调度器等待兼容目标，停止与删除继续原意图。

## 恢复流程

复用 runtime_management_operations 和 eviction，新增 recovery 模式。每个 item 保存固定源绑定、assignment/Secret UID、节点 UID/boot、容器 ID、存储及 helper 镜像，以及 planned → fencing → fenced → stopping → source_stopped → released 阶段。

优先走现有 Agent stop；失败时使用固定到源节点的 Kubernetes Job。平台 helper 经 containerd CRI 只停止指定旧容器，不能杀死 kubelet 创建的后续容器。在可信目录和稳定文件锁下，将精确旧 owner/generation 的 writer ledger 标记 stopped。未启动到创建 ledger 的场景必须先取得肯定的容器身份/退出证据，才允许创建旧代 stopped tombstone，不能覆盖新 owner。

helper 固定应用 digest 镜像，无业务凭据、无 service account token，有资源和时间上限。意图与恢复凭据保存在工作区管理目录；响应丢失后观察同一个操作，应用升级不改变已有任务的 helper 镜像。

assignment 按资源 UID 和源身份 CAS 退休；原生 stop 证据归档，不伪造 ACK。SQL 事务核验管理 owner、实例归属、精确 binding、源 Pod UID 和最新业务意图，释放旧绑定并重算占用。只对需要运行的实例增加一次 generation、更新启动监控状态；目标继续受现有驱逐的存储、镜像和资源兼容约束。

取消与进入 fencing 在同一个数据库行锁下竞争。开始 fencing 后不可取消；平台重启后续跑，结构性阻塞支持管理员对同一任务重试。旧运行时报告不能将恢复过程中的自身 stop 误当成用户停机意图。

## 边界

- Unknown 不作为已退出证明；没有可信数据库 owner/binding 的孤儿槽位不自动认领或销毁。
- Team 成员和升级中的实例保留已有协调保护，不能用单实例故障驱逐绕过。
- 历史模板、存储或节点归属不可信，节点 UID 改变或不可达时，明确报告阻塞。Pod 404、超时、强删不替代退出凭据。仅对 stopped/deleting 的退休清理，允许受保护的精确旧 ledger 和可信管理 PVC 替代缺失的模板来定位旧槽位；仍须独立确认节点退出，且不能据此重新调度。
- 不自动升级 Runtime 镜像，不恢复旧终端或调试服务进程，不保证临时依赖保留。
- 源 Pod 保持暂停新调度；实例绑定释放成功不等于旧 Pod 所有槽位已经可复用。

## 验收

真实 SQL 验证业务生命周期、pending 别名、启动 watchdog、最新停止意图、旧代绑定、意图精确退休、占用与健康邻居保留、事务失败及响应丢失。Kubernetes/CRI 测试覆盖错误 UID/CID/boot、API 默认值、Job/Pod 身份、失败重试、后续容器保护、ledger 冲突与 never-started tombstone。界面验证明确选择不扩大范围、影响确认和同一任务 Retry。

测试集群的部署、现场修复及剩余阻塞分别记录；本地测试不代替集群恢复验收。
