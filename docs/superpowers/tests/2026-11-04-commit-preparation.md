# 本轮代码提交整理

两个仓库均在 `feat/lite-idle-stop` 分支。只创建本地提交，未推送、合并或创建 PR；本次整理不更新测试集群，也不运行本地测试或构建。提交身份沿用当前 Git 默认值，未修改用户 Git 配置。

## 提交拆分

最初按下表整理 6 个功能提交。随后用户要求推送并核对部署，另补一个部署说明与[一致性核对记录](2026-11-04-deployment-consistency-audit.md)的文档提交；未改动已验证的功能代码或集群配置。

| 仓库 | 提交主题 |
| --- | --- |
| AgentsRuntime | `feat(runtime): add graceful idle stop for Lite instances` |
| AgentsRuntime | `fix(hermes): keep managed model providers in separate groups` |
| ClawManager | `fix(gateway): inject and route saved provider model catalogs` |
| ClawManager | `fix(redis): cap runtime event streams at 10000 entries` |
| ClawManager | `feat(runtime): add Lite idle stop and scale idle pools to zero` |
| ClawManager | `fix(frontend): recover failed embedded instance pages` |

公共文件中的混合改动按 diff 块暂存：Gateway 模型目录与空闲活动登记分开；多语言中的自动停止文案与嵌入页恢复文案分开。没有以整目录暂存方式混入其他文档。CM 自动停止依赖对应 Runtime 协议，发布应协调两个仓库。

## 提交前核对

- 130 个新增或修改的功能代码/测试文件逐文件 SHA-256 与测试节点源码一致，差异为 0。核对了节点已有的模型目录、Redis 保留、Hermes 分组、嵌入页回归与浏览器通过记录。
- 本次整理没有改动这些功能代码，没有重新运行此前已经通过的测试。前端已有 20 项访问恢复、20 项相关页面、5 个 Chromium 场景及构建通过记录；后端、Runtime 和原生验收范围见各专项报告，不能据此扩大为所有可选测试均通过。
- Go 格式检查与提交差异空白检查通过。提交范围未发现真实 API key 格式或私钥块；`.deploy`、集群访问 config、环境变量文件和私有快照未纳入。
- 修整了历史方案文档的尾部空白和当前状态指向；源代码与已验证版本保持一致。

## 保留与边界

未纳入的 UI 预演图片、UI/代码 review 整改计划、数据库改进方案及其他历史草案仍留在工作区，未删除。这些计划不会在本轮自动执行。

正式默认值保持开启、检查 1 天、空闲 1 天；分钟级逻辑仅存在于显式测试构建标签。当前测试集群部署状态与镜像见[发布准备记录](2026-11-04-lite-elastic-release-preparation.md)。

Hermes 模型分组的源码与离线原生检查已准备，候选镜像尚未部署，实际桌面列表验收仍未完成。四类完整模型目录都已注入，但分组显示名修复仅覆盖 Hermes，另外三类尚未统一为 ClawManager。CM 通知 WebSocket 的登录 token 刷新仍为[独立待修项](2026-11-04-openclaw-network-error.md)。外部模型直连被拒绝是正常安全策略，不列为缺陷。本地提交准备完成不等于这些发布待办已关闭。
