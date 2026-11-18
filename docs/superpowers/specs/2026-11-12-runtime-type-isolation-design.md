# Runtime 类型隔离与镜像可变休眠恢复

用户已确认：调度、管理、扩缩容、升级和恢复按 Runtime 类型隔离；创建/唤醒不要求镜像与休眠前相同。保留资源身份、工作区存储、绑定、冲突操作检查；滚动升级仍验证实际目标 digest。

- 长期操作冲突限定到 runtime_type；同类型内保留现有资源冲突与逐实例保护规则；安全的不同实例操作可并行，恢复所需的扩容仍可进行。历史 migration/balance/eviction 从 request、items 的 source_placement/runtime_pod/binding 推导涉及类型，不能把已知 DSH needs_attention 解释成所有类型冲突。无法推导的历史操作保守阻塞并返回原因。
- 保留短 SQL admission 事务锁，使冲突检查与发布原子；运行整个操作期间不得持有全局阻塞。
- 普通唤醒、升级准备接受镜像更新以及对应的 image identity 环境变量更新、generation 增长；不得改写旧休眠日志。保留 Deployment UID、角色、类型、操作标记、原始 PVC/PV/挂载和无残留 Pod 检查。非镜像启动/权限/存储变化继续拒绝。
- 准备冻结当前镜像/模板，而不是旧日志中的镜像。观察唤醒结果时使用本次准备固定的当前配置，避免滚动预检期间偷偷改变目标。
- 升级中的原生版本兼容、迁移和最终 digest 验证不放宽。历史 DSH 的失配操作不清除，不直接 patch Runtime 副本/镜像或 SQL。
- 所有测试、二进制、镜像构建在 10.130.81.54；本地只改源码/文档及传输。不修改用户现有 .gitignore。

验收：DSH held 操作下 OpenClaw 创建/唤醒/升级准备可以进行；同类型保持阻塞；替换镜像后合法自动休眠池可唤醒；替换 UID/PVC/安全权限或残留 Pod 仍阻塞；原始 journal 保留。发布 CM 后验证创建卡住的实例状态；真实 Runtime 升级只走 CM API。
