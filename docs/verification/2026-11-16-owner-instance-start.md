# 北向停止实例启动迁移与验收（32543）

## 改动边界

- 从个人项目 `6be76000` 迁移北向启动入口和 `connectOwnerInstance` 的观察/等待规则，没有合并个人项目的底层调度实现。
- 代码提交：`5ab90c440ffb363907112054e455622279611e8b`。
- 已停止的普通实例在卡片、菜单和详情页提供“启动实例”。只有显式点击才发起启动；打开停止实例的页面不会自动启动。
- 新入口为 `POST /api/v1/ieisystem/instances/:id/start`。使用专用 owner 会话、原有类型/模式范围和 `X-Owner-Action: 1`，保留 owner 隔离及待执行生命周期操作检查。
- `StartStopped` 复用团队版的 Team 变更保护、跨副本生命周期锁、Runtime 升级保护及既有 `startInstance` 沙箱启动链；在锁内核实状态。没有调用重置、重建角色或修改停止证明。
- 启动结果不确定时先读取原实例状态，不自动重复 Start。刷新启动中的详情页只继续观察；运行界面就绪后自动连接。
- 未修改 Team 调度、Worker 参数、Runtime 镜像、公共 CM 主题或当前北向白底/彩色标识。

## 代码验收

- 无产物 TypeScript 检查通过；5 项 `owner-instance-start.test.mjs` 通过。
- 测试服务器前端构建通过；App 和网关二进制构建通过。
- 后端全包检查中的启动接口权限、状态、重复请求、Team 保护、生命周期/升级互斥和原待发布槽位续做测试通过。
- 测试镜像起初缺少仓库外测试资料及 Python/Git/Node。补齐资料与 Python/Git 后，全包检查仅余 `TestManagedRedisNativeScriptsMatch` 因缺 Node 失败；使用服务器缓存的 Node 单独执行该用例通过。其余已通过用例未重复执行。完整日志和组合校验记录见服务器发布目录，未移除、跳过或放宽业务断言。

## 真实 32543 验收

- 账号：`portal-accept-20260923@example.com`；普通 Hermes Lite 实例 #609。
- 受控浏览器故障场景：启动中刷新页面、启动响应丢失，均只发送一次 Start 并恢复连接。受控请求由浏览器拦截，不修改真实实例状态。
- 实际从停止实例卡片点击“启动实例”，请求返回 202；原实例由 stopped 经 creating 到 running，随后进入 Hermes 界面。
- 实际页面仅发起一次 Start，没有 Restart/Reset。缺少显式操作头的请求返回 403；运行后重复 Start 返回现状，未再次创建 Runtime 代数。
- 原实例 ID、owner、类型、Lite 模式及工作区路径保持不变；Runtime generation 只由 5 增至 6。
- 原 `SOUL.md` 摘要不变，原历史数据库仍在原路径。该实例验收前历史为空（0 sessions、0 messages），本次没有声称验证了非空会话恢复，也没有发送模型任务。
- Hermes 原生界面输入框可用，观察窗口内没有失败的 Desktop 请求或页面脚本错误。
- Team184 的四个成员身份、实例绑定和职责保持不变，验收后 4/4 idle。

## 发布与回退

- 只更新测试环境 32543 的 App/网关；32443 和正式环境未修改。
- 镜像：`10.130.14.23:5000/clawmanager-hxc-app:owner-start-20261116-5ab90c44`。
- 实际 App digest：`sha256:3e380586f10ffd05e70f96c2f1a71cf774faa62add75caf5415f42411b52748d`。
- 发布前后 Runtime Deployment spec 一致；没有新增数据库迁移。
- 服务器证据目录：`/home/hxc/owner-instance-start-20261116/`；包含测试日志、组合验收结果、数据检查、原实例工作区备份和 `rollback.sh`。
- 回退脚本恢复上一版 App/网关镜像；不会删除数据或停止本次已启动的实例。工作区备份包含敏感配置，不放入 Git 或镜像。
