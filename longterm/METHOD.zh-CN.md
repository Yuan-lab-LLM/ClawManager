# 长程开发方法论（本地化）

## 来源
- 文章：`Effective harnesses for long-running agents`（发布日期：2025-11-26）
- 参考仓库：`anthropics/claude-quickstarts` 的 `autonomous-coding`
- MCP 采集文本：`research/` 目录

## 核心原则
1. 首次会话和后续会话分离（Initializer / Coding）。
2. 依赖文件与 git 持久化状态，而不是上下文记忆。
3. 每个会话只做一个高优先级未完成特性。
4. 必须做端到端验证后才能标记通过。
5. 会话结束时保持代码库可运行、可交接。

## 边界说明
- `longterm` 是项目事实层与 workspace contract，不是 feature 交付主路径。
- feature 交付仍应走 `spec规范` 的 `spec -> plan -> tasks`。
- 运行时执行方法仍应由 `superpower skill` 主导。
- `longterm` 中的 checklist / prompt templates 应理解为项目接手与兼容性 scaffold，不应扩张为第二套执行框架。

## 本仓库落地约定
每个项目工作区放在：`longrun/workspaces/<project>/`

必须存在四个工件：
- `app_spec.md`
- `feature_list.json`
- `init.sh`
- `claude-progress.txt`

## 迁移到已有项目（推荐流程）
1. 将 `longrun/` 整体拷贝到目标仓库根目录。
2. 执行 `./longrun/scripts/bootstrap.sh <project-key>` 创建工作区。
3. 在 `app_spec.md` 中优先补全“迁移约束”（不可破坏模块、兼容接口、禁改目录）。
4. 调整工作区 `init.sh`：
- 必要时设置 `PROJECT_ROOT` 指向真实项目根目录。
- 可选设置 `SMOKE_TEST_CMD` 与 `APP_START_CMD`。
5. 先跑一次 `./init.sh`，确保环境可重复启动，再进入 Session 1。

## feature_list 约定（标准化）
必填字段：
- `id`, `priority`, `category`, `description`, `steps`, `passes`

建议字段：
- `component`, `depends_on`, `acceptance_criteria`, `evidence_hint`, `risk`

说明：
- `depends_on` 用于描述前置依赖，`next_feature.py` 会优先选择未阻塞的特性。
- `progress_report.py` 会显示阻塞项与依赖异常，便于会话交接。

## 会话流程
### 初始化会话（Session 1）
- 读取 `app_spec.md`
- 生成完整 `feature_list.json`（初始全部 `passes: false`）
- 生成可重复执行的 `init.sh`
- 建立基础目录和首条 `claude-progress.txt`

### 持续开发会话（Session N）
- 先做定向（spec / feature_list / progress / git log）
- 运行 `init.sh`
- 回归验证 1-2 个已通过核心特性
- 若发现回归，先修回归
- 选一个未通过且未被依赖阻塞的最高优先级特性完成
- 端到端验证通过后再改为 `passes: true`
- 更新进度并提交

## 现成工具
- 初始化工作区：`./longrun/scripts/bootstrap.sh <name>`
- 进度统计：`python3 longrun/scripts/progress_report.py <feature_list.json>`
- 下一特性建议：`python3 longrun/scripts/next_feature.py <feature_list.json>`
- 会话起步检查：`./longrun/scripts/session_start.sh <workspace_dir>`
