# 四类 Runtime 更新补齐

用户于本轮明确授权：OpenClaw、Hermes 在测试集群可强制更新，并可清除各自测试数据；OpenCode 完成升级能力开发并发布。清数据授权不扩大到 OpenCode、DSH、其他类型或整库。源码与运行证据分别保存，历史报告不覆盖。

## 现状与目标

CM 已发布 slot 管理功能；四类候选均构建，但仅 DSH 安装。本轮结束须核实四类所有当前容器的实际 imageID、管理能力和原生发布记录。OpenCode 保留已有原生数据，不能仅删除原来的拒绝判断后使用共享 Deployment rolling replacement。

OpenClaw 当前没有实例或绑定，可直接走已有原生空池发布。Hermes 有删除中的实例 242；既有恢复任务已有真实旧容器退出回执，应继续同一任务。新的调查发现全局 pending 队列只对 slot_reset 按更新时间轮转，旧 recovery 可以一直抢占队首，阻碍其他类型准备。先修公平性，保留串行执行、选主、CAS、原始操作及所有物理回执。若生命周期锁仍阻止恢复，继续定位持锁路径，不伪造回执、删除保护或直接修改任务结果。

## OpenCode 原生保留升级

首版支持测试集群实际运行的 OpenCode 1.18.13、原生 revision `a105350812f05f914c768e468559dbd6bd508d8e`、相同精确的 38 项 migration journal 的 Agent 镜像升级。原生二进制 SHA256 固定为 `bcebc898d22419417b8dbec0f5e3f00f5b67b5a38c1de37c77c699d0e31fe0dd`，journal SHA256 为 `2ef42ffb09410c0f91535d25fce5b4107c46ee3fd7a4725e521c197ad4f5949c`。版本与 schema 已通过原镜像二进制在私有空目录初始化数据库及对应官方 tag 双向校验。未知 journal、旧 schema、未支持的 OPENCODE_DB/channel 路径覆盖、跨原生版本不进入修改阶段。该边界要写入 API、文档和报告，不能声称支持任意 OpenCode 版本迁移。

协调器首版一次处理一个单副本、八槽位的来源池。这是当前测试池的升级边界；多副本和更大来源范围明确拒绝。本轮的实例创建和默认池扩容继续使用原有容量调度路径。目标只有经过实际原生 payload 验证才宣称升级能力；维护、租约及激活操作均核对冻结的 Pod UID。旧请求省略可选身份字段时保持原有协议。

复用现有 RuntimeUpgradeProvider、ScopedNativeUpgradeProvider、六个维护动作、UID 隔离 sandbox、跨 Pod 升级锁、writer CAS/lease、capsule 挂载、维护容器退出回执和 CM 持久 intent/result。旧镜像不需要新增 provider：源负责既有的 C1 停止证明，目标 standby 执行检查和备份。

原生适配器只访问固定的原生配置、数据和状态目录。保留完整 SQLite DB/WAL/SHM 和配置状态到私有 capsule；在 capsule 副本上生成独立 SQLite backup，核对 integrity_check、foreign_key_check、精确 journal、表与索引 schema、所有用户表 typed catalog，以及 session、message、part、session_message、session_input、session_context_epoch 六张原生历史表的独立连续性 catalog。项目目录不遍历、不复制，不运行可能重置历史的未知自动迁移。

结果沿用现有结构：compatibility 返回 compatible 和三项验证；migrate 返回 validated、manifest 摘要、全状态保留摘要、session catalog、DataPreserved、RollbackAvailable；status 重新校验实际 capsule。部分写入不算成功，同一身份重试返回同一结果。恢复优先保留升级后新增历史，必须恢复备份时先保留 displaced 数据，不能用旧备份覆盖后丢弃新记录。

CM 使用独立 OpenCode strategy/controller，保留旧 rolling 路径拒绝。冻结来源 Deployment/Pod/存储和目标 digest，创建不可调度的目标 standby，确认原生版本及维护能力；逐实例持久冻结、停止旧执行、取得 C1 和外部 writer 结束证明，目标维护并校验 capsule，激活后按原期望状态恢复并核对新执行。无实例时仍校验真实空闲、UID、存储与目标原生契约。只有 CM 提交已验证完成，才能影响后续默认放置；中断和不确定结果续接原任务。

## 验证与发布

所有 Go/Python/前端测试、二进制和镜像构建都在 10.130.81.54。分别保存 RED/GREEN、源码 hash、不可变镜像、API 操作 ID、截图和实际结果。覆盖真实未 checkpoint WAL、六张历史表与其他原生表、项目目录隔离、错误 journal/schema/索引、损坏数据库、备份与恢复中断、幂等及新增历史与事件计数的回滚保护。

CM 发布与 Runtime 发布分开。三类更新均优先通过 CM 自带流程；两个获准清数据的类型只处理各自测试范围。OpenCode 做真实受管实例、工作区及原生会话的更新前后对比，再验证手动 slot 重置和浏览器页面。核对 server54 的 128 个 NodePort、8444 和就绪 EndpointSlice。DSH 保持可用；临时实例正常清理。最终交付新的 HTML 报告，逐项列出真正通过、失败、拒绝或未运行。
