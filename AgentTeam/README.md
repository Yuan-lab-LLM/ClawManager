# AgentTeam

这是一套可跨项目复用的多 Agent 协作模板。在 ClawManager 中，它是 `UnifiedFramework` 最小内核之上的可选 overlay，不是项目事实层、不是 feature 规格层，也不是验收层。

ClawManager 默认不扩编 agent team。只有在用户明确要求或当前 gate 确实需要时才使用。

## 目录
- `01-Commander-SOP.md`：总司令日常工作方式
- `02-Project-Onboarding-SOP.md`：首次接手一个新项目时必须执行的项目适配流程
- `03-Dispatch-Templates.md`：派单模板，直接复制后按项目改写
- `04-Quick-Reference.md`：极简速查表

## 适用方式
1. 当 Agent 第一次进入一个新项目时，先执行 `02-Project-Onboarding-SOP.md`
2. 项目接管完成后，由总司令按 `01-Commander-SOP.md` 运作
3. 具体派工时，直接套用 `03-Dispatch-Templates.md`
4. 需要快速回忆流程时，查看 `04-Quick-Reference.md`

## ClawManager 角色限制

只使用以下通用角色：

- `Worker`：执行一个明确、批准过的任务
- `Verifier`：只读或授权范围内验证
- `Reviewer`：审查代码、架构、风险或 evidence
- `Closer`：只在 fresh evidence + 用户批准后做 close/write-back/commit

不要为每个小 gate 创建长名字 agent。gate 名称写在 `任务类型` 中。

## 任务类型与防膨胀规则

必要时必须派任务类型，不要因为防膨胀而跳过真实工作。

允许的常见任务类型：

- `Design`：规格、方案、任务拆解、gate packet 设计
- `Research`：仓内/运行时/外部事实调查
- `Implementation`：明确范围内实现
- `Evidence Review`：复核 evidence、命令输出、状态
- `Architecture Review`：审查架构、安全、跨层风险
- `Closeout`：批准后的 Close / write-back / commit / handoff

防膨胀规则：

- `Designer` 和 `Researcher` 是任务类型，不是常驻 agent。
- 需要并行时可以使用 `WorkerA` / `WorkerB` / `WorkerC`，但必须有互不重叠的 scope。
- 不清楚 ownership、验证入口或集成方式时，必须串行。
- `Verifier` 和 `Reviewer` 不承担实现。
- `Closer` 平时 dormant，只有 fresh evidence + 用户批准后使用。

## 设计原则
- 这套模板与任何具体项目解耦
- 任何新项目都必须先做“项目适配”，不能直接机械套模板
- 总司令负责上下文、派工、审阅、集成、验收
- 子 Agent 负责具体执行与回报
- 项目适配只在首次接手时完整执行一次，后续由总司令持续维护上下文并按需更新
- 子 Agent 回传不是验收结论；Commander 必须复核证据
- `passes:true`、Close、longterm write-back、commit/push 必须由当前 gate 和用户显式批准
- GPT Pro escalation 由 Commander 决策并打包 prompt，用户决定是否发送；其输出是 advisory，不是验收。

## 后续演进
当前是方案 B（标准版）。
若后续需要更重的治理层，可以在此基础上补充：
- Context Update SOP
- Review & Handoff SOP
