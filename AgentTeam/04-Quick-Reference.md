# Quick Reference

## 一句话原则
- 先接管项目，再派工
- 先划边界，再并行
- 先拿证据，再宣布完成
- 总司令负责上下文和裁决，子 Agent 负责执行和回报
- AgentTeam 是 optional overlay；主路径仍是 `Project Rules -> longterm -> specs -> execution -> evidence -> write-back`

## ClawManager 角色词汇
- `Worker`
- `Verifier`
- `Reviewer`
- `Closer`

不要给每个小 gate 新建专名 agent；把 gate 名称放进 `任务类型`。

## 任务类型
- `Design`
- `Research`
- `Implementation`
- `Evidence Review`
- `Architecture Review`
- `Closeout`

必要时一定要派对应任务类型处理；防膨胀只限制常驻 agent 名称，不允许跳过真实设计、研究、验证或审查工作。

## 标准顺序
1. 首次进项目 → 跑 `Project Onboarding SOP`
2. 明确当前目标 → 判断任务规模
3. 先拆解 → 再派工
4. 声明文件所有权和验证要求
5. 回收结果 → 审核证据
6. 验收后更新项目上下文

## 编队建议
### 小任务
- `Worker`
- `Verifier`

### 中任务
- `Worker`
- `Reviewer`
- `Verifier`

### 多线战役
- 总司令
- 少量 `Worker`
- `Reviewer`
- `Verifier`
- `Closer`（仅 fresh evidence + 用户批准后）

## 什么时候不能并行
- 不清楚谁拥有哪个文件
- 两个 Agent 会改同一块
- 验证入口还没搞清楚
- 项目接管还没完成

## GPT Pro 决策
推荐打包 GPT Pro prompt 的场景：
- 架构 / 安全 / K8S / runtime image / browser E2E 风险较高
- root cause 经过本地调查仍不确定
- 两轮 Worker/Reviewer 后仍未收敛
- 错误决策会导致高成本 rebuild/deploy/E2E
- 用户明确要求 GPT Pro review / research / patch proposal

GPT Pro 输出只作为 advisory；仍需 Commander 复核并走 Worker / Reviewer / Verifier gate。

## 总司令每天最少要看什么
- 当前目标
- 当前阻塞
- 当前验证证据
- 当前上下文是否过期
- 下一轮该怎么派工
- 当前 gate 是否允许 mutation/write-back/Close

## 反模式速查
- 没接手项目就开干
- 让多个 Agent 自由发挥改同一模块
- 把规划、实现、验证混成一坨
- 没有证据就说完成
- 让历史经验压过当前项目事实
- 子 Agent 说完成就直接 Close
- 没有用户批准就写 `passes:true`、longterm、commit 或 push
