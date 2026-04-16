# 开发流程（Spec‑Kit / Codex）

本仓库采用 Spec‑Kit 的“文档驱动开发”骨架：每个功能都必须按 `spec → plan → tasks → implement` 推进。

边界提醒：
- `spec规范` 只负责当前 feature 的交付工件，不负责项目长期事实
- 项目 facts / progress / `passes: true` 仍由 `longterm` 主定义
- 运行时执行方法仍由 `superpower` 主定义；本文件不是运行时行为手册

## 1) 初始化（只需一次）

在项目根目录：

```bash
./scripts/codex
```

或手动：

```bash
export CODEX_HOME="$PWD/.codex"
codex
```

验证：在 Codex 里输入 `/`，应能看到 `/speckit.specify`、`/speckit.plan`、`/speckit.tasks` 等命令。

## 2) 开发一个新功能（每次都走）

0. 先读取项目本地规则层：`AGENTS.md` / constitution / repo rules。

1. 从项目事实层（例如 `longterm`）确认当前要做哪个 feature、当前项目约束是什么。

2. 在创建 `specs/<feature>/` 前，先建立 `longterm feature ↔ spec feature` 的唯一绑定；至少明确：
   - `spec.md` 在哪里
   - `plan.md` 在哪里
   - `tasks.md` 在哪里
   - acceptance evidence 预期写到哪里

3. 创建 feature 目录（生成 `specs/001-xxx/spec.md`）：

```bash
./.specify/scripts/bash/create-new-feature.sh "一句话需求" --short-name your-feature
```

4. 完善 `spec.md`（范围/验收/边界），生成并完善 `plan.md`：

```bash
./.specify/scripts/bash/setup-plan.sh
```

5. 在 Codex 里生成 `tasks.md`：

```text
/speckit.tasks
```

6. 将 `spec.md`、`plan.md`、`tasks.md` 交给执行层消费：
   - 默认由 `superpower` 选择本会话的执行方法
   - 若启用 `AgentTeam`，则由其按 tasks 做 ownership / 并行 / 收口
   - 但 `spec规范` 本身只负责 feature 工件，不承接 runtime behavior authority

7. 按 `tasks.md` 小步实现：一次只做一条任务；每做完就跑测试并勾选任务。

8. 提交 PR 前更新仓库入口文档：
   - `docs/PRD.md`：追加 1 条“变更记录”（必填）
   - `README.md` / `AGENTS.md`：按影响面同步更新

9. 只有当 acceptance evidence 已能回指当前绑定的 feature，且满足 `spec.md` 中定义的验收条件或项目允许的等价验证规则时，才允许把完成结果写回 `longterm`；长期 `passes: true` 不由 `spec规范` 自行裁决。

## 3) 最小主流程

`项目规则 -> longterm -> 建立 feature 绑定 -> spec.md -> plan.md -> tasks.md -> superpower 执行/验证 -> 写回 longterm`

## 4) `passes` / evidence 写回握手

- `spec规范` 负责把 acceptance criteria 定义清楚，并为 evidence 留出稳定锚点
- `acceptance evidence` 必须能回指当前绑定的 `spec feature`
- `passes: true` 是 `longterm` 中的长期事实，不由 `spec规范` 单独判定

## 5) 规范来源

- 工程/质量约束以 `.specify/memory/constitution.md` 为准。
