# OpenClaw 嵌入页空白恢复：修复与验证

2026-11-04。用户确认嵌入的 OpenClaw 页空白，刷新后恢复，并授权修复。所有测试、浏览器验证、前端构建、镜像构建和部署均在测试节点 `10.130.81.54` 执行；本地只编辑、检查差异和查看测试截图。

## 修改

复用 `useInstanceDesktopAccess` 公共访问逻辑及原有短期凭据，不延长有效期、不绕过鉴权。

- 新增独立的 iframe 重载代次。即使同源访问 URL 不变，人工刷新或故障恢复也会重新挂载 iframe。两个公共调用方均使用该代次。
- 已经打开过的页面遇到明确加载/代理鉴权错误，也能重新取得访问凭据并恢复。
- 仅 OpenClaw 启用每 5 秒一次的同源 DOM 检查，不额外请求后端，也不计为业务活动。连续空白至少 10 秒后进入恢复；不能读取跨域页面不是失败。
- 自动恢复采用 5/10/20 秒退避，最多 3 次。凭据请求进行中合并焦点事件，避免同一个慢请求重复耗用预算；连续确认健康 30 秒后重置预算，人工重试也可重置。
- 对空白恢复继续检查 DOM：页面若自行恢复，取消等待中的重载；异步取得凭据后也再检查一次，避免覆盖刚恢复的输入。正常聊天内容中引用鉴权错误字样不会被当成代理错误。
- 恢复期间显示状态，耗尽后显示失败提示和重试按钮。停止、切换或卸载取消排队恢复，过期异步结果不能恢复旧页面。合法跨域 load 会结束遮罩，避免挡住可用界面。

正常页面不会按时间强制重载。上述检查判断的是页面是否呈现内容，不能据此推断应用内部每个模型/工具请求都正常；跨域内部空白也无法通过 DOM 直接判断。

## 回归验证

- 公共访问 hook 20 项通过：包括原有跨小时续期、休眠标签恢复、同源相同地址强制重载、已建立后错误、空白及失败预算、代理拒绝、长退避自行恢复、保护草稿、跨域、并发、停止和卸载。
- 相关页面 20 项通过：Hermes 页面能力边界、Pro/Lite 页面、门户、共享页、重启提示和自动停止管理界面。
- `npm run build` 的 TypeScript 检查及 Vite 构建通过。已有 Browserslist 数据过期和包体体积提示不属于此次功能失败，未借此更新依赖。
- Chromium 使用真实 React `InstanceServiceFrame` 和访问 hook，配合独立 HTTP fixture 验证 5 个场景：持续空白重新申请凭据并实际导航；已建立页面 401 后恢复；退避期间自行恢复保留草稿；持续空白恰好 3 次自动重试后停止并显示重试；人工重试恢复。浏览器没有 pageerror，没有访问业务对话或发送模型请求。
- 受控浏览器时钟分段推进，并等待实际网络响应和 React 提交，避免虚拟时间超过 iframe 导航。审查发现的卸载、并发、跨域遮罩和退避输入保护均有回归；长退避问题先观察到失败，再修正。失败日志保留。
- `git diff --check` 通过；UI 静态检查无报告项。浏览器分别验证了中文按钮行为和英文截图布局；测试 Runtime 缺少中文字库，截图用英文。

## 部署与实机传输检查

仅替换 CM 镜像的前端静态文件，底层镜像、后端二进制与 OCI 运行配置保持一致；Runtime、工作空间、Redis 和数据库配置均未变更。

新 CM 镜像：

```text
clawmanager.local/clawmanager@sha256:f52d375a8ee742e4c864c21c93a9dfa1d6560120a4369da61f72dd1a0dd2cc1a
```

基于正式日规则镜像 `sha256:03765053a04d0b2163f9cf854eba8cc16019d58ce8dafadb699750d57dd35fa7`，未加入分钟测试标签。更新限定在 `clawmanager-system` 的 CM Deployment，临时使用 `maxSurge=1`、`maxUnavailable=0`，就绪后恢复原更新策略。

部署后通过验证 TLS 的请求确认新首页哈希与构建产物一致。实际实例 154（`lite-openclaw-129`）页面及所检查的 JavaScript/CSS 返回 200，WebSocket 握手 101；未发送业务消息。Runtime Pod 身份与重启次数未变化，实例仍 Running、代次 2。

自动停止设置保持版本 7、开启、检查 1 天、空闲 1 天；下次检查仍为北京时间 2026-11-05 04:00。用户需刷新一次 CM 主页面，已有标签页才能加载新前端代码。

节点证据目录 `/root/cm-pod-elastic-20261104`：

- `frame-boundaries-red.log`、`frame-long-backoff-red.log`。
- `frame-green-results/frame-recovery.log`、`related-frames.log`、`frontend-build.log`。
- `frame-browser-results/frame-browser.log` 及 `screenshots/`。
- `app-frame-recovery-image.json`、`frame-ui-deployed.json`、`openclaw-ui-transport-verified.json`。

部署前完整快照仅保存在节点的 0600 私有文件。未提交或推送代码。本轮只处理嵌入页恢复；CM 通知 WebSocket 的登录 token 刷新仍为[独立待修项](2026-11-04-openclaw-network-error.md)。记录中的外部模型连接被拒绝属于正常安全策略，不是待修缺陷。
