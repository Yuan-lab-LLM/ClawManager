# Team 模板目录、成员保护与四 Runtime 催促交付

## 范围与提交

仅修改团队项目 `codex/team-personal-integration`，个人项目未修改。

- App 工作目录：`D:/test/ClawManager-team-upstream-merge`。
- `fa939109`：公共成员删除入口拒绝操作；CM 和 owner Team 成员实例均受保护，整队删除使用现有受控入口。
- `52c0b555`：模板编辑改为侧边成员目录；团队信息与成员标题对齐；操作归属、草稿保留和窄屏布局。
- `74dda35a`：有界滚动区域的焦点留白；真实组件和共用对话框验收。
- Runtime 工作目录：`D:/test/AgentsRuntime`。
- `2acc00e`：在原执行内递送有身份约束的 Monitor 上下文，原业务消费者保持串行。
- `8b8ed42`：修正 DeepSeek Harness 原生上下文类型，并增加真实原生进程回归。

未增加数据库迁移。未修改 Leader/Worker 派发、阶段依赖、正式回执和 Leader 关闭根任务的基本机制。Worker 完成工具仍只要求 `status`、`summary`，完整的可选正文、产物和审核字段保留。普通实例 Pro 能力未收窄。

## 界面及权限结果

北向模板页采用“模板列表 → 团队信息 → 成员目录和单成员详情”。成员列表与详情各自内部滚动；窄屏使用内容切换和紧凑成员选择。六人场景已用实际生产组件验证。

- 去除编辑区顶部的冗长规则和自定义模板版本标记；规则放入说明对话框。
- 名称通过标题旁重命名操作保存，不覆盖未提交的目标和人数草稿。
- “按目标更新”“重新生成整套”在团队信息区；职责调整和单成员生成在成员详情区。
- 不再使用全局“更多”“仅保存名称”造成作用域歧义。
- 管理窗口仅展示成员；普通用户不能单独移除成员，只能删除 Team。
- CM 与 owner 成员 DELETE 均返回 409；权限和父子归属仍先验证。
- 直接删除或重置成员实例不能绕过 Team 保护。整队删除继续使用内部明确的 Team 身份。

## 执行中催促的实现边界

新增观察器只读取可信 Monitor 的上下文通知，并约束 team、member、root、work、revision 和可用的执行代数。它不创建第二个业务执行，不创建工作项，不改变工作版本，不强制中断模型。

- OpenClaw：使用当前 native run 的受保护 steering API；旧原生能力不支持时保留原串行路径。
- OpenCode：使用同一 native session 的 steering；主任务身份通过宿主签名继承，普通用户输入不会自动变成当前任务。
- DeepSeek Harness：使用当前 agent 的 next-step steering，身份及正式结果仍归原输入。
- Hermes：沿用已部署并验证的 v4 原执行上下文适配，本轮未重建 Hermes 镜像。

通知状态和消费证据单独记录，不参与业务成功裁决。提交结果不确定时不盲目重复外部操作；原消费者仍处理尚未接受的通知。

真实 DSH 回归发现：原生 `runtime-context` 是内部快照保留类型，会丢弃调用方提供的正文。因此使用明确的 `clawmanager-team-context` 来源，并仅对可信宿主来源保留原工具身份。真实原生进程已验证正文进入后续模型请求，没有启动第二个 agent。

## 已发布镜像

仓库均为 `10.130.14.23:5000`：

| 镜像 | 标签 | registry digest |
| --- | --- | --- |
| clawmanager-hxc-app | team-paper-ui-20261109-v1 | sha256:1e44089592212e46bc2e337304e9df2473e10bcfc33b7cf645798394c4a8445b |
| agentsruntime/openclaw-lite | team-paper-context-20261109-v1 | sha256:d1b2bd9292278d59aba521bc3eadd0a71e60ee92fa2005c44372ad9a23fc135b |
| agentsruntime/opencode-lite | team-paper-context-20261109-v1 | sha256:705dff6b1db2c599c4145cd09ea242142bbba897ba394db6b7d0bc591c2f91a8 |
| agentsruntime/deepseek-harness-lite | team-paper-context-20261109-v2 | sha256:169c137bb6deb7810af3a02b1ead77f67b4411d758b9701b42f99cabffd3baf7 |

Hermes 保持 `sha256:a09cf34517372a8d3fa244e8522deafb9975dd8dd38355dac0f05ca5fbc06bcd`。

OpenCode native 仍为固定的 2.0.27 源码，revision `1f4d07a4a519185955ae8ab2ea3607805509e386`；新二进制 SHA256 为 `c279bae536373caac7c6696f11eb9fb9a38fde636f93c18f349279d9d488b9e3`。编译复用同一已安装的 Linux 依赖及未变动的原生 UI，没有替换成未经治理的 stock binary。

App 使用当前提交的 Linux 二进制和前端构建；保留前版 hashed chunks，兼容已打开标签页。最终在 `/home/hxc` 使用 `clawmanager-apply.sh`、`TENANT_SUFFIX=-hxc-peer`、`NODE_PORT=32543` 更新，退出码 0。首次探测曾遇到短暂 NodePort 超时；后续真实鉴权及最终完整 apply 均通过。

Lite 镜像目录和 App 的 Runtime 默认值固定为对应 digest；全部 Pro 目录项逐项对比保持不变。OpenClaw 历史默认 placement 必须通过正式控制面切换，本轮使用空池 rollout #181，已 finished；未直接改数据库绕过旧 placement 约束。

## 必要验收及真实闭环

- 针对性 CM handler、整队删除及实例保护测试通过。
- 真实 disposable MySQL 覆盖 owner/CM 成员保护、旧成员删除操作处理、无残留新操作及整队内部身份。
- 后端 Linux 回归首轮有测试环境文件缺失；补齐仓库挂载后，相关包通过，services 仅剩容器缺少 Node 的脚本一致性用例。该单用例已在具备 Node 的宿主通过。没有把最初失败的整轮任务记成退出码 0。
- 前端构建及实际生产组件浏览器验收通过：六成员、对齐、焦点不裁切、窄屏、草稿保留、冲突和共用对话框。
- 15 个 native host/bridge/client 测试、真实 Redis 忙时上下文/串行去重、既有消费者和工作流上下文回归通过。
- DSH 真实 pinned native 进程验证 active context 进入模型请求、最小回执、产物、公开正文补交、重复输入及进程重启。
- 32543 真页面验证模板 #19：四成员目录、标题对齐、重命名对话框、管理窗口无移除成员按钮，无页面异常。
- 32543 owner Team #182 的成员 DELETE 为 409；CM Team #168 的成员 DELETE 为 409，成员数未变化。

真实模型验收使用账号 `portal-accept-20260923@example.com`，自定义模板 #19 创建 Team #182：

| 成员 | Runtime | 工作项 | 最终结果 |
| --- | --- | --- | --- |
| leader | OpenClaw Lite | leader-final-synthesis | succeeded，revision 1 |
| method-math-reader | Hermes Lite | A1-paperA-mechanism | succeeded，revision 1 |
| experiment-evidence-checker | OpenCode Lite | A2-paperB-evidence | succeeded，revision 1 |
| reproducibility-limitation-analyst | DeepSeek Harness Lite | A3-cross-paper-scope | succeeded，revision 1 |

任务 #395 只用两篇论文的已核对摘要材料：Attention Is All You Need 与 Deep Residual Learning for Image Recognition。重点验证流程，不做额外检索、全文下载、代码开发或第二批派发。任务从 09:28:19 UTC 到 09:34:45 UTC 正式完成，状态 `succeeded/completed`。三份 Worker 结果和 Leader 汇总均正式接受；共四个工作项，全部 revision 1，没有重复 Worker 工作项。

四成员各注入一次仅作用于当前工作的验收催促：

- Hermes、OpenCode：真实事件 `request_included`。
- DSH：真实事件 `native_context_consumed`；另外原生回归已确认进入实际请求。
- OpenClaw：native transcript 记录了唯一提醒 marker 的 user 事件（session `811ef537-01af-4b49-98e7-c905dc593988`、seq 6），之后有 14 个 assistant 事件。控制面通知保持 submitted，没有将其虚报为 request_included。

这些证据证明通知进入相应执行链，不保证模型逐字服从。没有增加模型必填参数或要求分批交付。

最终文件通过真实 owner HTTP 文件接口读回：`/team/results/team-182-task-395/final-synthesis.md`，1879 字符，包含两篇论文及对应数字。任务、聊天事件、成员执行证据、正式回执和文件一致。

## 经授权清理的历史测试资源

仅操作 `clawmanager-hxc-peer-system`。正常删除旧测试 Team #172、173、175、176、178、180、181 及独立测试实例 #558、562；owner 删除操作均已 succeeded。

清除 43 个旧 OpenClaw 升级/验证空池控制器，另清除旧个人版空 serving 测试池和废弃 DSH 空池；本轮正式空池切换产生的临时验证控制器完成后也已清除。保留新的四类验收池、普通在用绑定、基础设施及零副本基础 OpenClaw 配置。

清理先保存私有控制器/Pod/PVC 快照，再暂停调度，核对 SQL 绑定和 native Gateway 为空，以 UID/resourceVersion 约束删除。旧池的 SQL PodUID 与实际 UID 不一致，受保护单 Pod 删除拒绝；没有放宽程序校验，改在确认空池并缩为零后进行明确范围的控制器维护。

最终 14 个 Pod（开始时 44 个），15 个 Deployment，所有期望副本均 Ready；原有四个 PVC 的名称及 UID 集合完全一致。没有删除共享卷或其他租户资源。

## 证据和回退

本机私有证据目录：`D:/test/ClawManager-2/team-optimization-docs/team-ui-paper-20261109/`。服务器私有快照及 apply 日志：`/home/hxc/team-ui-paper-20261109/`。其中配置快照包含环境引用，不能放入 Git、工单公开附件或用户消息。

App 前版不可变 digest 为 `sha256:a7251a5c2820c97ab944e7551f2c02c3844bbd6a460fc13f54ee1fbc7353a273`。无 schema 变化，因此可恢复此前 App，但同时会恢复此前界面和权限行为；需要保留新成员保护时应选择性回退前端。

Runtime 回退须先确认目标池没有运行中的成员，使用保存的配置和正式升级/恢复控制面处理。不能只改镜像目录来绕过 OpenClaw placement，也不能把此前已删除的测试实例当成回退目标。不要重放已完成的 #395 或重新创建其工作项。

本轮仅按要求做 Git commit 和镜像推送，未执行 Git remote push。
