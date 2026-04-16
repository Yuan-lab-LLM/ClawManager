# ClawManager Constitution（Spec‑Kit / Company Baseline）

> 本文件是项目“开发宪法”：约束 AI 与团队的开发方式与质量门槛。
> 本项目采用 Spec‑Kit（Codex）文档驱动工作流：需求 → 方案 → 任务 → 实现。
>
> 使用方式：
> - 本文件定义的是稳定规则，不定义项目动态状态；
> - 项目当前状态、backlog、当前阶段与近期进度应落在项目记忆层（如 `longterm`），不要写在本文件中；
> - 当前 feature 的需求、方案、任务与验收应落在 `specs/<feature>/spec.md`、`plan.md`、`tasks.md`；不要把 feature 内容主定义写进本文件；
> - 运行时执行方法与会话行为属于 `superpower` 等 execution layer，不由本文件主定义；
> - 本文件可作为公司级“基础宪法”复用到不同项目；
> - 第 2 节为“项目补充约束”（Project Addendum），每个项目必须按自身情况填写/替换。

## 1) 核心原则（非协商）

1. **简单至上（保留关键信息）**
   - 减少不必要的代码量/文件数/文档噪音；优先复用与抽象，但不牺牲可读性。
   - 文档优先用表格/图/示例替代长段落。

2. **模块化与清晰边界**
   - 避免超大文件/函数/类；按职责拆分并用目录组织。
   - 新增内容优先归类到 `docs/`、`tests/`、`scripts/`、`examples/` 等（缺目录再建）。

3. **面向接口编程（契约优先）**
   - 先定义模块/服务之间的接口与数据契约（必要时提供 schema/示例），再实现细节。
   - 接口变更必须同步更新契约文档与相关测试。

4. **文档驱动：先改文档，再改代码**
   - 每个功能必须先落仓库文档：`specs/<feature>/spec.md`（需求与验收）→ `plan.md`（技术方案）→ `tasks.md`（任务拆解）。
   - 实现过程中按 `tasks.md` 小步推进，完成一项勾选一项，并跑对应测试。
   - 每个 PR 必须同步入口文档：至少更新一个“对外可读的变更记录”（例如 `docs/PRD.md` 的“变更记录”），并按影响面更新 `README.md`/`AGENTS.md`。

5. **测试驱动 & 可复现**
   - 代码变更必须伴随测试变更（单测/集成/端到端按需）。
   - 测试脚本/命令/参数应落盘（脚本、Makefile、配置文件），避免“只存在于对话里”。
   - CLI 测试必须设置超时，避免卡死。

6. **易用性与健壮性**
   - 尽量把复杂流程做成一键（脚本/Makefile）；减少人工配置与手工步骤。
   - 配置与环境变量统一管理（例如 `.env` + `.env.example` + YAML/JSON 配置）。

7. **风格与一致性**
   - 统一命名、目录结构、配置方式；必要的 docstring/type hints。
   - 避免引入多套重复工具链；尽量统一依赖管理方式。

8. **做确定的动作**
   - 不确定就提问或标注 TODO，不靠臆测补细节。
   - 只做有依据、可验证的修改；每一步都能解释“为什么”和“如何验证”。

9. **规则稳定、事实外置**
   - 本宪法只定义稳定规则、质量门槛与流程边界，不承载 backlog、progress、当前状态、阶段推进或长期 `passes` 真值。
   - 项目动态事实应写入项目记忆层（如 `longterm`）；当前 feature 的交付工件应写入 `specs/<feature>/`。
   - 宪法不能替代项目事实层，也不能替代 feature delivery 或 execution layer。

## 2) 项目补充约束（Project Addendum / 必填）

> 目的：把“所有项目通用的工程规范”与“单一项目特有约束”拆开，便于复用与迁移。
>
> 要求：
> - 新项目落地 Spec‑Kit 时，必须先填写本节；
> - 本节内容优先级与第 1 节一致（对项目而言同为“宪法”约束）。
> - 本节只补稳定项目约束，不填写 backlog、当前阶段、近期 progress、当前 feature 状态等动态事实。

1. **形态与边界**
   - 项目形态：全栈 Web 应用 + K8S/K3S 控制平面
   - 前端：React 19 + TypeScript（Admin Portal + User Portal）
   - 后端：Go 1.21+（REST API + K8S client + AI Gateway）
   - 部署：K3S（优先，适合本机/边缘/低资源）或 K8S（完整集群）
   - 不做清单：不实现自己的容器运行时；不替代 K8S/K3S 本身；不做多集群联邦（V1）
   - 外部接口边界：对外提供 HTTPS REST API（端口 8443）；前端通过同一端口访问

2. **质量门槛**
   - 测试策略：
     - 后端：`go test ./...`（单元测试为主，关键路径含集成测试）
     - 前端：`npm test`（组件测试）
     - 部署：`kubectl apply --dry-run=client` 验证 YAML 合法性
   - 验证方式落盘：所有测试命令写入 `longterm/workspace/init.sh`
   - 版本与兼容：数据库变更必须通过 SQL 迁移文件（`deployments/k3s/clawmanager.yaml` 中 ConfigMap），向后兼容

3. **安全与合规**
   - 密钥管理：所有密钥（MySQL、JWT、MinIO）通过 Kubernetes Secret 管理，禁止硬编码
   - 禁止提交：`.codex/auth.json`、`.codex/config.toml`、任何包含真实密码的文件
   - 访问控制：JWT 认证；ServiceAccount `clawmanager-app` 持有 cluster-admin，最小化权限为后续迭代目标
   - 审计：所有操作写入 `audit_logs` 表

4. **工程约定**
   - 后端代码风格：`gofmt` + `go vet`；包名小写；错误必须处理
   - 前端代码风格：ESLint + Prettier；组件用 TypeScript 严格模式
   - 目录结构：`backend/`（Go 模块）+ `frontend/`（Node 模块）+ `deployments/`（K8S YAML）+ `docs/`（文档）+ `specs/`（feature 工件）
   - 子项目约定：`backend/` 和 `frontend/` 各自必须有 `AGENTS.md`，说明本子项目的运行/测试/结构/约束
   - 数据库变更：必须作为 ConfigMap SQL 迁移文件，按序号命名（`00N_description.sql`）

## 3) 工作流（Spec‑Kit）

1. 生成 feature 文档：`./.specify/scripts/bash/create-new-feature.sh "一句话需求" --short-name xxx`
2. 完善 `spec.md`（范围/验收/边界）→ 生成并完善 `plan.md`：`./.specify/scripts/bash/setup-plan.sh`
3. 在 Codex 内生成 `tasks.md`：`/speckit.tasks`
4. 按 `tasks.md` 小步实现；每步跑测试并更新文档/勾选任务

## Governance

- 本宪法优先级高于口头约定与临时习惯；任何变更需以 PR 方式提交并说明影响与迁移策略。

**Version**: 1.0.0 | **Ratified**: 2026-04-14 | **Last Amended**: 2026-04-14
