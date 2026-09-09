# Hermes Desktop Renderer Web 适配

目标是交付锁定上游的完整 Desktop renderer，而不是精选组件加 ClawManager 自绘聊天页。此前 Desktop Core 未达到这个目标，已经退出生产构建入口；旧 controller 的单元测试不代表当前完整 renderer 的界面验收。

## 源码与构建

固定基线：Hermes `v2026.8.31` / commit `29112bef099274229cadff79cdff7bf7b99c4b77`；Agent `0.21.0`，Desktop package `0.17.0`。

`upstream.lock.json` 锁定完整 `apps/desktop/src`、public 静态资源、shared 源码与相关包/构建元数据的 Git blob。源文件以原始字节导入；不能修改缓存、改用 main/latest，或在依赖失败时替换为另一套 App。构建拒绝未锁定模块、内容漂移、Electron/Node 宿主依赖和遗漏真实主入口。

```sh
npm ci --prefix hermes-desktop-web --ignore-scripts
node hermes-desktop-web/scripts/build.mjs --source /path/to/hermes-agent
npm run typecheck --prefix hermes-desktop-web
npm test --prefix hermes-desktop-web
```

要求 Node 22.22+（CI 使用 24）。省略 `--source` 时使用已验证缓存或从官方固定 commit 下载。CI/Docker 可将本地 Git 对象库作为构建上下文；不依赖工作区脏修改。

生成物位于 `frontend/public/hermes-desktop-web/`，含 hash 静态资源、许可证/归属说明及 `build-info.json`。只有构建成功后才替换该精确生成目录。元数据明确 `scope=desktop-renderer`、真实 renderer 入口和 `acceptance=not-asserted-by-build`：**编译成功不是 Runtime 或浏览器验收通过。**

### 许可证与字体边界

Vite `build.license` 在 `dist/` 根生成 `THIRD-PARTY-LICENSES.md`，不是默认隐藏的 `.vite/` 路径。`THIRD-PARTY-NOTICES.md` 补充模块图中各包提供的全部根级 LICENSE/NOTICE、单独复制的 emoji/font 资源归属及 `license-coverage.json`；如发行包未提供许可全文，清单明确记录，不能把标识字符串当成完整版权审查。Hermes 原始 MIT、Codicons 完整 CC BY 4.0（包含 Jason Long Git Logo CC BY 3.0 归属尾注）、Codicons 代码的独立 MIT 均保持原文。

这些文件由构建发射到 `dist/`，随后原样复制到 `frontend/public/hermes-desktop-web/`，随现有 Docker 静态目录复制进入镜像；生产可访问 `/hermes-desktop-web/THIRD-PARTY-NOTICES.md` 等同级路径，HTML 带 `rel="license"` 索引。构建失败或缺少真实 React/assistant-ui 依赖许可报告时不能发布。

JetBrains Mono 和 KaTeX 字体保留独立的 SIL OFL 1.1 条款；`FONT-LICENSES.md` 包含字体自身版权/保留名称、完整许可正文，不能用 JavaScript 包的 MIT 替代。`font-coverage.json` 记录所用原始字体 SHA-256；构建拒绝未经审核或内容改变的字体文件。许可正文固定在 `scripts/notices/`，与构建补丁一起进入 `build_input_sha256`，不在构建时从外部下载许可证。

主题 `fontUrl` 只允许无用户名/密码的同源 HTTP(S) stylesheet；Google Fonts 等跨源地址在创建 DOM 资源之前拒绝，不放宽 CSP，保留原 bundled/system 字体栈。Collapse 字体自身元数据声明 Keussel/Blaze Type 的独立条款，不能因为 `@nous-research/ui` 的 MIT 元数据就推定有字体再分发权；本构建精确移除其 `@font-face`，并拒绝该字体重新进入产物。标题等文字因此使用现有 sans-serif 回退，存在字体外观差异；不修改原 App、布局或路由。

## 真实入口

```text
CM src/main.tsx：校验实例、隔离浏览器存储、校验 BFF 会话、安装 window.hermesDesktop
  → 上游 apps/desktop/src/main.tsx
    → 原始 Providers / HashRouter
      → app/index.tsx → ContribController
        → 原始布局、路由、会话侧栏、聊天、输入器、主题及交互
```

浏览器桥必须先安装，再执行上游带副作用的模块。构建检查入口静态分包的传递依赖，不允许 chunk 分组把上游初始化提前到桥安装之前。父页面的 `ready` 只来自上游 `$desktopBoot` 的真实 `renderer.ready`，不来自资源下载成功或伪造的 Runtime capability。

## 浏览器 bridge 与 BFF

父页面以正常 ClawManager 登录和实例权限完成 bootstrap，再打开：

```text
/hermes-desktop-web/?instance_id=<positive-integer>
```

只允许这一个查询参数，不接受 Electron 的 `win/profile/connection` 入口覆盖。桥完整对应上游 `Window['hermesDesktop']` 类型，显式实现必要方法；不存在万能 Proxy 或未知方法的成功占位。

- 连接注册表只有当前实例，固定默认 profile；其他实例、profile、外部 URL、上传和任意 API 被拒绝。
- HTTP 走同源 `/api/v1/instances/:id/hermes-desktop/api/...`。已配置模型、状态、会话/历史和安全 UI 配置由 BFF 白名单投影，不能透传完整配置与凭据。
- 每次 WS 连接通过 CM `POST .../ws-ticket` 获取新的 CM 一次性票据。上游要求的结果格式是 `{ok:true, wsUrl}`；不缓存连接票据，不把 Hermes Cookie/密码/内部票据放进 renderer。
- CM → Runtime 登录 Cookie 和 WS subprotocol 私有票据仍由服务端持有。CSRF、实例归属、代次、Redis 防重放、续期和 WS 生命周期边界不能为了适配而关闭。
- 正常聊天创建使用 `source=web` 和受管默认工作区；BFF 也约束 source、cwd 和 profile，不能启用 Electron 的 `desktop_ui` 工具。
- 模型选择只作用于已授权的当前会话，不写全局模型或 Provider 凭据。未开放的修改会明确失败，不自动重放写请求。

浏览器中的 `localStorage/sessionStorage` 为每次文档独立的有界内存存储，既不读取 Manager 登录存储，也不在磁盘遗留上游草稿、历史尾部或连接偏好；跨窗口 Storage/BroadcastChannel 同步禁用。刷新会丢失未提交草稿和界面偏好，已保存历史仍由 Runtime 提供。

## Web 能力适配与禁用

`scripts/web-adaptations.mjs` 对原始已锁定源码施加少量构建期补丁。每个补丁有具体文件、唯一锚点和原因，源码漂移即构建失败；不替换 App、主布局或路由，也不通过隐藏 Runtime 登录表单解决认证。

| 能力 | Web 行为 |
| --- | --- |
| 上游布局、路由、主题、侧栏、会话与聊天组件 | 使用真实 renderer；需要 BFF 和浏览器联调验证 |
| 主题远程字体 / Collapse 字体 | 仅同源主题资源；不打包授权未确认的 Collapse，保留原系统/已授权 bundled 回退 |
| 浏览器剪贴板、页面可见性 | 使用浏览器原生权限与实际状态；失败不伪装成功 |
| 文件、Git、终端、Electron webview | 保留对应 pane/入口并显示明确不可用说明；无宿主透传 |
| 本机安装、更新、MCP 管理轮询 | 不启动；Runtime 生命周期仍由 CM 管理 |
| 原生窗口、全局快捷输入、桌宠弹窗、系统通知 | 禁用原生调用；保留应用内提示与页面显示 |
| 语音采集、唤醒词、桌宠后台资源/本机项目扫描 | 不启动，相关设置/控件明确说明能力未开放 |
| Runtime 动态插件源码加载 | 禁止下载执行/热加载；不放开 eval/blob 脚本 |
| Hermes 模型密钥引导 | 不在 iframe 收集密钥，提示回 CM 配置；不伪造 Runtime 模型已配置 |
| 实例级配置、跨连接/Profile、Shell、上传及未审核接口 | 明确拒绝；后续需求须单独增加精确 BFF 契约与测试 |

这不是集群内运行 Electron，也不承诺 Electron 全部宿主功能在浏览器可用。UI 是上游真实 Desktop renderer；功能开放范围与 Runtime 验收状态必须分别说明。

## 验收与发布

`test/browser-desktop-bridge.test.ts` 验证真实桥协议、单实例作用域、新票据、无凭据、非伪造 ready 和原生能力拒绝；storage 与 adaptation 测试验证隔离、边界补丁和锁定源码语法。构建、typecheck、单元测试都不能代替交互验收。

`e2e/fixtures/hermes-desktop/main.go` 是 loopback-only 的合成协议服务：用于验证真实编译页面、原始侧栏/路由、聊天/历史和禁用提示，不调用真实模型，不操作实例工作区。其通过不能替代真实 172 的 BFF Cookie、WS、三副本重连和权限验收。

上线门禁为 CM 开关和真实 Runtime capability。不能直接修改验收字段，也不能把 `dashboard` 改成 `serve` 来绕过能力检查。当前 UI 只在能力确认后进入 Desktop renderer；能力不可用时展示明确错误并允许重试，不再回退到旧界面。

发布前运行 renderer build、typecheck、全部单元测试、ClawManager 前后端测试和 `e2e/hermes-desktop-smoke.mjs`。集群部署记录和截图保存在发布系统或本地忽略目录，不提交到源码仓库。
