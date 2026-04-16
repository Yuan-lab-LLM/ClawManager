# AgentTeam

这是一套可跨项目复用的多 Agent 协作模板，目标是让一个“总司令（Commander）”能够快速接管任意新项目，并把规划、实现、验证、文档与收口任务稳定地下发给子 Agent。

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

## 设计原则
- 这套模板与任何具体项目解耦
- 任何新项目都必须先做“项目适配”，不能直接机械套模板
- 总司令负责上下文、派工、审阅、集成、验收
- 子 Agent 负责具体执行与回报
- 项目适配只在首次接手时完整执行一次，后续由总司令持续维护上下文并按需更新

## 后续演进
当前是方案 B（标准版）。
若后续需要更重的治理层，可以在此基础上补充：
- Context Update SOP
- Review & Handoff SOP
