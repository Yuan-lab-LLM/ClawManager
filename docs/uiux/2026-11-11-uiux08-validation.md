# 方案 08 实施验证记录

当前状态：用户后续明确授权后，已更新测试环境 [https://10.130.81.54:30443](https://10.130.81.54:30443)。前半部分的“未部署”描述属于授权更新之前的开发验收阶段；最终部署记录见文末。

Task 1–7 已完成源代码与隔离环境验证；Task 8 已在测试集群完成精确提交的生产构建、四项生产 dist 行为回归及完整视觉矩阵。本文仅记录实际运行的证据。

## 基线与环境

- 仓库：`ClawManager`，分支 `uiux`，起始提交 `6468337`。保留工作区原有 `.gitignore` 修改、设计规范、设计元数据及其他未跟踪文档，不将整个脏工作区传入集群。
- 本地：macOS，Node `22.17.0`，Vite 开发服务 `http://127.0.0.1:9002`，已安装 Chrome。只运行源码校验、无产物类型检查与浏览器检查；未在本机进行生产打包、Go 或容器构建。
- 测试集群：`10.130.81.54`。新建隔离 namespace `cm-uiux08-e2e`，独立临时 MySQL 数据库 `clawmanager_e2e` 和后端 Pod，使用固定测试凭据，不读取业务数据库与业务 Secret。后端使用集群现存镜像 `sha256:30f98851d7b3f061718ad0ee5a5e94a5d4af580adfbd954c236373733185085e`，本轮不修改后端。
- Playwright 原配置所需的 Chromium 构建未在本机缓存；验证采用临时配置指定已安装 Chrome，沿用仓库 auth/DB fixtures。原始配置保留。命令：`E2E_HEADLESS=true ./node_modules/.bin/playwright test -c /tmp/cm-uiux08-20261111/playwright.config.mts ... --workers=1`。

## 改造前截图与真实数据差异

桌面截图来自真实路由和 React 页面，API 用明确的隔离 fixture，不能视为真实集群业务容量或账户数据。改造前实例图中的生命周期错误提示来自旧 fixture 把批量生命周期列表错误返回为对象；实际契约是数组。最终改为 `[]` 后提示消失属于夹具纠正，不是本轮修复了后端或产品错误。

- [实例改造前，1440](assets/2026-11-11-uiux08-implementation/before-instances-1440.png)
- [Runtime 改造前，1440](assets/2026-11-11-uiux08-implementation/before-runtimes-1440.png)
- [团队改造前，1440](assets/2026-11-11-uiux08-implementation/before-teams-1440.png)

| 方案预览 | 正式接口/既有行为 | 实施约束 |
| --- | --- | --- |
| 固定示例实例名、ID、工作区大小与时间 | `Instance` 的 `name`、`id`、`workspace_usage_bytes`、`last_online_at` 为实际数据，后两项可缺失 | 缺失在线记录显示未知/无记录；不以创建或更新时间冒充最近在线 |
| 16 个槽位、固定在线和可接纳数 | Runtime 提供真实 `capacity`、可选 `slots`、`bound_instances`、`agent_reported`、`bindings_reported` 和 inventory summary | 动态渲染 8/16/64 等容量；绑定、就绪和可接纳分别取真实语义，缺失不上报不得假造零 |
| 示例团队对话与常驻任务面板 | 正式团队有流式消息、任务历史、文件预览与 owner 嵌入；`workspaceMode` 使用容器宽度 | 保留既有双面板/单面板/抽屉与历史阅读位置；预览面板排列不覆盖真实工作流 |
| 简化操作和短状态文案 | 实例区分 Lite/Pro，跨页选择、批量创建、生命周期任务与真实错误 | 不用预览省略能力边界与风险确认；短状态同行，完整信息保留可访问入口 |

## 已运行的基线检查

- 两项 `tsc --noEmit --incremental false`：通过。
- `frontend` 的 `test:runtime-management`、`test:instance-access`、`test:workbench`：通过；Node22 的 strip-types 警告为现有运行环境提示。
- 原团队工作台浏览器回归经临时脚本将静态 dist 服务改为 Vite，保留原行为断言：通过。覆盖面板、预览、阅读锚点、草稿、窄屏及暂时失败。
- 隔离真实后端 Playwright：auth、DB fixture 与四项 smoke 导航通过；两项 Runtime 测试基线失败（旧 Page Object 的筛选按钮定位歧义及过时空态字符串），由 Task 5 更新。测试未连接生产。

- 实例自动同步脚本经临时改为 Vite 服务：所有业务断言通过，进程退出 0。Runtime 开发态脚本运行至 WebSocket 重连计数断言失败；开发模式 StrictMode 增加连接次数，此项必须在集群生产 dist 上执行后再定结论。
- 本地 Chrome157 的更新辅助进程曾占用测试退出管道。通过 `lsof` 定位后只终止本次测试产生的孤立 crashpad/GoogleUpdater，导航最终正常退出；未操作用户原有浏览器。

## 分步记录

- Task 1，`abaa209`：语义 token、公共类映射和管理表格/工具栏/弹层表面。两项无产物类型检查、`git diff --check` 通过。Chrome 检查控件颜色、焦点及 1440/1280/768/390 宽度下的局部滚动；独立代码复核通过。

- Task 2，`989913c`、`fbff01c`、`8e86a36`：共享紧凑顶部与侧栏、路由位置、折叠持久化、移动抽屉、全高布局。隔离 auth/DB 导航回归 8 项通过；针对 300/400px 高度的弹层末项点击、滚动和 resize 关闭回归 4 项通过；真实团队 390px 对象信息/操作/输入区/草稿回归通过；四种布局模式的 iframe/草稿连续性通过。复核修复顶栏配件溢出、Logo 焦点返回、短侧栏及分类弹层高度/遮挡后，规格和代码质量复核通过。

- Task 3，`b753f3c`：受控搜索、统一筛选列表/操作菜单、原生确认框和账户语言选择。公共控件 7 组浏览器断言通过；隔离真实登录环境账户语言回归 3 项通过；app/node 无产物类型检查及新文件/头部定向 lint 通过。独立复核通过，并追加验证 Tab/Shift+Tab 不困住焦点。实例页搜索集成用例已写入，等待 Task 4 接入后验证。当前浏览器证据为 Chromium；组件展开时卸载的焦点恢复未单独运行回归，监听器与 observer 清理由静态复核确认。

- Task 4，`49e9bb4`、`3750bcd`：八列实例表、紧凑筛选和独立创建区、底部分页、行内状态与菜单、原生批量创建框。保留跨页选择、后台同步和生命周期任务轮询；活动与策略信息改由行菜单访问，复用当前实例对象；长错误弹层可键盘滚动。页面集成回归 6 项通过，补充活动/策略与错误滚动回归 3 项通过，相关 Node 回归 17 项通过，Vite 自动同步断言全部通过，app 无产物类型检查通过。1440/390 常规、长内容和详情已目视；规格和代码质量复核通过。定向 lint 只有页面原有 4 项 set-state-in-effect 问题。生产 dist 同步脚本等待 Task 8。

- Task 5，`aee78e0`：Runtime 汇总、动态容量表格和详情统一，运维对话框复用原有回调并接入统一选择器；统计分页位于底部。保留在线与调度区分、未知上报、绑定和占用的独立含义；物理状态依据实际观测，合成未知槽位不再误显未就绪。浏览器 23 项、容量专项 6 项、Runtime 状态单测 21 项通过；既有弹窗请求/草稿/提交回归通过；app/node 类型检查和修改范围 8 文件 lint 0 错误 0 警告。已查看 8/16/64/999999 容量的 1440/390 渲染及真实测试后端 100 槽位详情。独立规格/质量复核通过；生产 dist 的轮询/WebSocket 回归等待 Task 8。

- Task 6，aae8107：团队清单、对象栏、消息/输入/历史/文件面板统一；保留面板模式和阅读/草稿逻辑。完整工作台、owner 嵌入及刷新、文件确认/错误反馈/焦点回归通过，app/node 类型检查通过。目标文件 lint 34 错误 11 警告均在扩展历史基线中，无新增诊断。已查看 1440/1280/768/390 实际渲染，规格和代码质量复核通过。真机键盘、实际模型流式请求、生产 owner 鉴权未运行。

## Task 7：最终集成与视觉验收

本轮源代码从 `aae8107` 接续。没有修改 API、权限、实例生命周期、任务状态、容量模型或工作区模式阈值。

本轮必要兼容修正：北向接口共享导航补齐五语言；选中/悬停实例 ID 和悬停团队 ID 使用现有较深文字色；Runtime 详情辅助文字使用现有文字 token；未加载/失败时的 `Unknown / Unknown` 数量可在自己的汇总格内换行，不与相邻格重叠；用户账户菜单恢复 Portal 入口，管理员菜单不增加该入口。AdminLayout 的弹层锚点由事件中更新的 ref 改为同批更新的 state，消除新增 React refs lint 诊断，保留滚动关闭/resize/焦点行为。

### 环境与命令

本地运行 macOS / Node `22.17.0`，Vite `http://127.0.0.1:9002`。真实集成使用隔离 `cm-uiux08-e2e` 后端，隧道 `9001`，临时 MySQL `13307`；没有连接生产数据库。浏览器改用已缓存 Chromium 151：

```sh
export PLAYWRIGHT_EXECUTABLE_PATH=/Users/mowen/Library/Caches/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-mac-x64/chrome-headless-shell
```

从 `frontend` 执行：

```sh
./node_modules/.bin/tsc -p tsconfig.app.json --noEmit --incremental false
./node_modules/.bin/tsc -p tsconfig.node.json --noEmit --incremental false
npm run test:runtime-management
npm run test:instance-access
npm run test:workbench
node scripts/user-layout.test.mjs
node scripts/app-version-display.test.mjs
node scripts/user-portal-entry.test.mjs
node scripts/team-workspace-contract.test.mjs
node scripts/team-chat-presentation.test.mjs
```

两项类型检查退出 0。三组 Node 测试分别 **21 / 20 / 7 通过，0 失败**。额外五个脚本通过：真实 SSR 布局单 child/iframe/accessory/main；共享用户/管理员版本标识；Portal 入口与路由；Team 模式/工作项/表格契约及聊天表现。版本/Portal 旧的文件位置和 CSS 源码匹配已迁移为实际布局树断言，没有删除原有的可访问性/入口约束。

定向 lint 命令（`frontend`）：

```sh
./node_modules/.bin/eslint src/components/UserLayout.tsx src/components/AdminLayout.tsx src/components/ManagementHeader.tsx src/components/ExpandableSearch.tsx src/components/FilterSelect.tsx src/components/ActionMenu.tsx src/components/ConfirmDialog.tsx src/components/InstanceLifecycleStatus.tsx src/components/useManagementPopover.ts src/pages/instances/InstanceListPage.tsx src/pages/admin/RuntimePodsPage.tsx src/pages/admin/RuntimeManagementUI.tsx src/pages/admin/RuntimePodDetails.tsx src/pages/teams/TeamListPage.tsx src/pages/teams/TeamDetailPage.tsx src/components/teams/TeamChatWorkspace.tsx src/components/teams/TeamTaskComposer.tsx src/lib/i18n.ts --format json
```

ESLint 退出 1：**历史 30 错误、9 警告，新增 0**。以原始 `lint-baseline.json` 和扩展 `task6-extra-baseline.json` 为依据，按文件、规则、严重级别计数并比较 multiplicity，没有用简单集合隐藏同规则新增诊断，也没有清理历史问题。新浏览器 runner 及三项修改的 `.mjs` 验证脚本另经 `node --check`。`git diff --check` 通过。

从 `e2e` 执行真实 auth/DB 回归：

```sh
env -u NO_COLOR -u FORCE_COLOR E2E_HEADLESS=true ./node_modules/.bin/playwright test -c /tmp/cm-uiux08-20261111/task2-review-playwright.config.mts tests/uiux/management-ui.spec.ts tests/smoke/navigation.spec.ts tests/admin/runtime-pods.spec.ts --workers=1
```

**32 项通过（含 2 项 DB/auth setup），0 失败**。涵盖搜索展开、账户嵌套语言列表、跨页 Lite 选择与轮询、活动/策略更新与长错误滚动、真实容量/未知上报、运维弹层、导航伸缩/抽屉/短视口、真实隔离后端 Runtime 记录。新增兼容检查覆盖设置、资源管理、技能中心、管理员入口以及实例对象详情的 1440/1280/768/390 几何与可见性。实例详情采用真实接口 shape 的独立停止实例夹具，非启动集群实例；未模拟的可选接口明确 404，不以错误的 `{}` 冒充成功数据。Portal 从实例账户菜单实际点击到原路由，并验证管理员菜单无该入口；两种 shell 版本标识可见。

从仓库根执行：

```sh
node e2e/management-team-list.browser.mjs
node e2e/management-team-shell.browser.mjs
FRONTEND_URL=http://127.0.0.1:9002 node frontend/scripts/runtime-grid.browser.mjs
node e2e/management-ui-visual.browser.mjs
```

前两项通过，包含 owner 自有 API/Layout/返回查询、删除失败/取消/焦点及草稿、390px 输入与对象信息、列表搜索/空/错误/更新。`runtime-grid.browser.mjs` 因卡片迁移改为表格几何检查：中英文均保留分页 16、全部 16 槽位图形及可访问分布、菜单键盘与关闭焦点、实际 Events/Pause 调用和 `{enabled:false}` 请求、行操作不进入详情、窄屏局部滚动、跨页搜索和键盘进入详情。其本地检查使用 `FRONTEND_URL` 直接访问 Vite；静态 dist 模式留给 Task 8 集群验证，未在本地打包。

### 完整截图与人工核对

[截图索引](assets/2026-11-11-uiux08-implementation/README.md) 与 [机器清单](assets/2026-11-11-uiux08-implementation/matrix.json) 保存 **191 张 final 截图**：

| 范围 | 数量 | 覆盖 |
| --- | ---: | --- |
| `/instances`、`/admin/runtime-pods`、`/teams/144` | 120 | 中英文 × 常规/空/加载/错误/长内容 × 1440/1280/768/390 |
| 日/韩/德文导航、工具栏、语言列表 | 45 | 3 页面 × 4 宽度弹层 + 各页 390 抽屉导航 |
| Runtime 边界数据 | 6 | 0 容量、离线、未上报 × 1440/390；常规同时含真实 8/16/64 容量 |
| 中英文交互与高度模拟 | 20 | 展开搜索、筛选、行菜单、确认框、选中焦点、折叠侧栏、Runtime 1440/390 详情、390×560 输入区、团队列表悬停 |

图像来自实际路由和组件，用清楚隔离的 API fixture。实例生命周期批量列表返回**数组 `[]`**；常规 Runtime 摘要与真实 slots/bound/ready/available 一致（8+16+64，绑定 6、就绪 88、可入 82），不复用容量压力测试的零摘要。未上报不冒充空闲。常规 Team 采用三个短业务请求和结果；长内容包括长名字、段落、表格；空 Team 是已加载的 Team，members/tasks/events/work_items 均为空，并保留输入区，绝非 404。加载只挂起本场景数据请求并在关闭上下文时释放；错误仅令本场景 API 503，auth/version 仍成功。runner 断言当前路由、shell 与场景状态，防止把登录页误记为错误截图。

已人工查看三类页面完整的中英文状态/宽度拼图及日/韩/德文弹层/导航图，额外原尺寸查看移动团队、Runtime 加载 390/768、详情 390、确认框及长内容；与批准的三张 `08-instances/runtimes/teams.png` 并排核对。保留白灰外壳、彩色 Logo、紧凑控件、边缘侧栏按钮和单行列表单元格。正式业务功能使实例存在选择生命周期操作，Team 保留真实成员栏与任务面板模式，这些与静态预览数据/固定排布的差异有意保留。仅修复本节记录的明显尺寸/对比度兼容问题，并重跑相关脚本和截图。消息正文/既有业务事件的原始语言未自动翻译；日/韩/德文此次检查是溢出检查，既有部分 Runtime 文案仍使用英文回退。

复用命令可指定 `FRONTEND_URL`；集群对**精确提交**的静态产物可改用 `FRONTEND_DIST=/absolute/frontend/dist MANAGEMENT_VISUAL_ARTIFACTS=/path/to/output node e2e/management-ui-visual.browser.mjs`。本地本轮仅验证 Vite 模式，静态模式未宣称执行。

### 实际对比度、焦点与检测边界

[渲染对比度样本](assets/2026-11-11-uiux08-implementation/rendered-contrast.json) 记录中英文共 **70 个实际 computed-style 样本**。原选中 ID 在 `--cm-selected` 上为 4.4098:1，修复后 7.0665:1；实际 Runtime 返回文本在 canvas 上原为 4.4009:1，现使用较深 token。最终样本组最低：选中行/共享导航 7.07、菜单 5.35、Runtime 悬停节点/数量 4.69、详情辅助文字 7.28、Team 对象/辅助标签 4.91、团队悬停 ID/日期 7.07。未用纯图形状态点、禁用控件作为正文对比度断言。键盘行焦点有实际描边，确认框 Escape 返回触发器；菜单/选择器键盘与弹层焦点另有真实回归。

Impeccable 在 Task 6 的 9 个 Team 目标之外，对本轮剩余及新修正的 22 个 UI 文件运行一次：0 项 regex anti-pattern；1 项旧 `index.css` 网格背景 advisory。工具明确跳过 TSX/CSS 的 parsed element/contrast 检查。这不是全应用无障碍认证；色板证明也不能替代这里的实际选中/悬停样本。

### 剩余限制与 Task 8

- 本轮没有本地生产前端/Go/容器构建，没有合并、部署或发布。精确提交源码的集群构建、生产 dist 自动同步/WebSocket/团队完整工作台结果见 Task 8 记录。
- 浏览器证据为桌面 Chromium 151；其他浏览器引擎、真机移动键盘未测。390×560 只是视口缩小模拟，验证草稿与发送按钮可见，不能代表真实键盘和系统安全区行为。
- owner 是既有隔离 fixture 的嵌入/权限命名空间回归，不是生产 owner 鉴权；实际模型流式回复和生产业务数据未运行。此前完整 Team 工作台业务断言由 Task 6 已验证，本轮未改其流式/阅读/面板状态逻辑。
- 保留用户原有未跟踪设计文档及 `.gitignore` 修改；没有将脏工作区或测试凭据打包进提交。


## Task 8：精确提交的集群构建与交付

构建源码为分支 `uiux` 的 `e737ca5040e637f271ea5f394ca35aeac985ab98`（最终源码复核通过，动态选项键盘焦点的重要问题已关闭）。本节后续交付记录提交仅包含文档和少量验证证据，不属于生产构建输入。执行主机 `root@10.130.81.54`，Node `24.21.0`、Chrome `157.0.8103.50`。没有本地生产打包、Go/容器构建，没有创建第二套部署系统，也没有合并、部署或发布。

只用 `git archive` 导出该 SHA 的 `frontend`、`backend/internal/teampresentation/catalog.json`、`e2e/package.json`、`e2e/package-lock.json`、`e2e/management-ui-visual.browser.mjs`、`e2e/team-chat-workspace.test.mjs`、`e2e/management-controls.browser.mjs`。没有打包工作区、`.auth`、`.env`、`.deploy`、Mac node_modules、测试凭据或用户未跟踪文档。归档 SHA256 在传输两端核对一致：`1d8324a566fa745be6d762822bdfc23d5a4d10a92e8d027d41a401e52d61d16f`。

集群源码目录为 `/root/cm-uiux08-20261111/source-e737ca5040e637f271ea5f394ca35aeac985ab98`；日志与截图位于 `/root/cm-uiux08-20261111/verification-e737ca5`。在集群设置：

```sh
export PATH=/root/cm-slot-release-20261111/tools/bin:$PATH
export npm_config_cache=/root/cm-slot-release-20261111/npm-cache
export PLAYWRIGHT_EXECUTABLE_PATH=/opt/google/chrome/chrome
```

依次执行（相对路径从上述源码目录计算）：

```sh
(cd frontend && npm ci)
(cd frontend && npm run build)
(cd e2e && npm ci)
(cd frontend && npm run test:instance-refresh)
(cd frontend && npm run test:runtime-refresh)
node frontend/scripts/runtime-grid.browser.mjs
TEAM_WORKSPACE_DIST=$PWD/frontend/dist node e2e/team-chat-workspace.test.mjs
FRONTEND_DIST=$PWD/frontend/dist MANAGEMENT_VISUAL_ARTIFACTS=/root/cm-uiux08-20261111/verification-e737ca5/visual node e2e/management-ui-visual.browser.mjs
```

前三条安装/构建和四项行为回归均实际退出 **0**。依赖安装出现已有 audit 与 npm install-scripts 提示，未运行 audit fix 或变更锁文件；生产构建有大分包提示，构建正常完成。实例脚本保留后台/空筛选列表、启动休眠同步、焦点、选择保留、不轮询团队、晚到响应隔离、请求去重、失败保留已有行与重连恢复断言。Runtime 脚本保留概览/详情后台轮询、生命周期事件与 WebSocket 断线重连断言；生产模式下未出现开发 StrictMode 的双连接夹具差异。Runtime 表格脚本保留十六行分页、槽位分布、双语、菜单键盘/焦点、实际 fixture Events/Pause 请求、行操作隔离、跨页搜索与窄屏局部滚动。完整 Team 脚本保留浮动工作台、表格/链接、原生文件预览层、移动控件、阅读锚点、面板状态、传输/终端、刷新及草稿等业务断言。上述 API/model 均为显式隔离 fixture，没有访问生产业务或调用真实模型。

生产 dist 交付物只保留在集群：

- `/root/cm-uiux08-20261111/artifacts-e737ca5/frontend-dist-e737ca5040e637f271ea5f394ca35aeac985ab98.tar.gz`，SHA256 `6e416f49cec1c7cb262cea8375eaf11b975a96bda268fa66f9b9cd854f5b9584`。
- 逐文件校验和 `artifacts-e737ca5/frontend-dist-e737ca5.files.sha256`，其文件 SHA256 `b9cd059c29ec539592b7743df260e7f0660c4a23e2923eca8e60b4fa99192a52`；同一 `artifacts-e737ca5` 目录的 `SHA256SUMS` 和 `build-provenance.txt` 记录来源及工具版本。

此交付是可供后续预发布验证的静态产物，未生成/推送镜像，未修改任何 Deployment。若之后另行授权部署，回退采用先前可用镜像；源码回退可逐一逆序 revert 本轮独立提交。原 API、路由、权限与状态存储键保留，无数据库迁移，回退不需要数据修复。其他浏览器引擎、真机键盘和生产模型调用仍不在本轮验证范围。


生产构建的代表截图已从集群取回并实际查看：[实例 1440](assets/2026-11-11-uiux08-implementation/production-dist/final-instances-en-regular-1440.png)、[Runtime 1440](assets/2026-11-11-uiux08-implementation/production-dist/final-runtimes-en-regular-1440.png)、[Team 390](assets/2026-11-11-uiux08-implementation/production-dist/final-teams-en-regular-390.png)。这些图与 Task 7 开发服务截图分目录保存，不覆盖已有基线。


完整生产 dist 视觉 runner 实际退出 **0**，生成 **191 张截图**（与 Task 7 同一完整场景/状态/语言/宽度矩阵，另含弹层、零容量/离线/未知上报及高度模拟）。[生产矩阵清单](assets/2026-11-11-uiux08-implementation/production-dist/matrix.json)、[实际渲染对比度样本](assets/2026-11-11-uiux08-implementation/production-dist/rendered-contrast.json)、[逐项退出码](assets/2026-11-11-uiux08-implementation/production-dist/exits.txt) 与小型日志同目录保存。完整截图留在集群 `verification-e737ca5/visual`，只取回以上三张代表图；191 张均经 runner 的实际路由/状态/横向溢出与契约断言，人工查看范围为前述代表图，不把它称为全部逐图人工认证。


### 最终复核修复与历史交付

最终复核发现共享弹层按旧数字索引处理动态选项：Alpha/Bravo/Charlie 中焦点在 Bravo，移除 Alpha 后 Enter 错选 Charlie。修复 `e737ca5` 让键盘激活、移动和 typeahead 依据实际聚焦节点及当前可用选项；移除或禁用当前项时聚焦第一个可用项，全禁用/空列表停在弹层并取消选项 tab stop，重新加入选项后恢复首个可用项。没有更改公共组件 API、业务 API 或依赖。

修复者记录了两项 **RED（exit 1）**：错误选择 `c` 而非 `b`，以及全禁用残留 tab stop；最终完整控件 fixture **9 组 GREEN（exit 0）**，覆盖两种控件的删除/重排/禁用/空列表/重建/方向键/typeahead/选择身份，原有嵌套弹层与焦点返回仍通过。Runtime native popup 回归 **3 项通过（含 auth/DB setup）**，app/node 无产物类型检查、修改源码 ESLint、脚本语法及 diff 检查均通过；最终独立源码复核确认无剩余 Critical/Important。Task 7 的历史 scoped lint **30 错误、9 警告，新增 0** 未被声称为零错误，最终修复的修改源码 lint 也无新增问题。

旧交付记录提交 `ab1b0d3` 的构建源码为 `1f41222b93dea443ebc9de71c963ed9e4315ce59`，虽当时五项生产检查通过，因上述最终复核键盘问题，已由本节 `e737ca5` 产物取代。旧集群 `source-1f41222…`、`verification-1f41222` 与 `/root/cm-uiux08-20261111/artifacts/frontend-dist-1f41222b93dea443ebc9de71c963ed9e4315ce59.tar.gz` 仅作历史保留；旧压缩包 SHA256 `e40c6eb7fda90671bd636e998c45646640ef6ae78f2d85219e3385462cc07de2`。当前 `production-dist` 小型记录和三张图均已替换为本次修复后的执行，历史版本仍可从 `ab1b0d3` 查看。

交付依据：[批准的 08 实施计划](../superpowers/plans/2026-11-11-cm-uiux08-implementation.md)、[系统规范 DESIGN.md](../../DESIGN.md)、[批准方案 08 的设计对比/渲染规范](2026-11-11-design-comparison.html) 与 [08 参数和设计来源](2026-11-11-design-sources.json)。这些已有用户设计输入仍按原状保留，未将其未跟踪源文件纳入本交付提交。


### 最终验收与环境收尾

最终独立复核通过源码 `e737ca5` 与交付记录 `81a6182`，无剩余 Critical/Important；构建输入仍为上述精确源码，不因后续文档收尾提交而改变。保留 `uiux` 分支与工作区，无推送、合并或部署。

隔离 namespace `cm-uiux08-e2e` 已删除并确认不存在；本轮 Vite、SSH 隧道与两项集群端口转发已停止，相关本地和集群监听端口已确认关闭。集群源码、验证日志和最终产物继续保留供后续预发布使用。用户原有 `.gitignore` 修改与未跟踪设计输入均保留。


## 后续授权：更新 81.54 现有环境

用户明确要求“不在本地跑，直接更新到 81.54 环境”后，本轮将前端源码 `e737ca5040e637f271ea5f394ca35aeac985ab98` 对应的已验证产物部署到 `clawmanager-system` 的 `Deployment/clawmanager-app`。访问入口保持为 **https://10.130.81.54:30443**，沿用现有账号、TLS 和服务配置。此次没有在本地启动前端、后端或浏览器测试，没有本地镜像/二进制构建。

复用现有 BuildKit、containerd 本地镜像导入与 Kubernetes 镜像滚动更新流程。新镜像完整继承当前运行镜像，仅复制前端静态产物。

- 最终镜像：`clawmanager.local/clawmanager@sha256:e8252ca5c28f37bd30a4adfffe59b01babb6d72da3a39050f1e90487b9c03592`。
- 原镜像/回退镜像：`clawmanager.local/clawmanager@sha256:30f98851d7b3f061718ad0ee5a5e94a5d4af580adfbd954c236373733185085e`。
- 前端产物 SHA256 仍为 `6e416f49cec1c7cb262cea8375eaf11b975a96bda268fa66f9b9cd854f5b9584`，文件内容未改变。镜像内公共静态目录/文件显式使用 `COPY --chmod=0755`，保证非 root Nginx worker 可读取。
- 所有原基础镜像层和运行配置均保留；上线前后 `clawreef-server`、`start.sh` 和 `nginx.conf` 的 SHA256 完全一致。其他 Deployment 的镜像引用均未变化，无数据库迁移或业务数据修复。页面显示的后端版本仍是原 `v2026.11.11-fix4`；前端更新以本节源码/镜像记录为准。

首次上线检查发现私有构建目录权限被复制进镜像，静态资源返回 404。已先恢复原镜像，再显式指定 COPY 权限并检查最终 OCI 层的静态资源可读/目录可遍历；修正后的镜像重新滚动更新成功，最终 Ready/Updated 均为 1。首次镜像 `sha256:3d302d7f…` 已弃用，不是当前部署。

最终验证全部在 81.54 执行：

- 正式 HTTPS 入口的 HTML、入口 JS/CSS 与已验证 dist 逐字节一致；`/healthz` 和 `/api/v1/version` 均为 HTTP 200。
- Chrome 实际使用当前环境账号登录；实例、Runtime、团队列表的新顶部/表格和 1440/390 视口检查通过，实例/Runtime 筛选器键盘与焦点返回通过，页面脚本错误及 HTTP 5xx 均为 0。只做页面读取和筛选器开关，没有创建/删除实例或团队、发送模型请求或触发运维操作。
- 实际团队列表为空，现场验证覆盖该空态；团队对话工作台的其他行为证据仍为前述隔离回归，不扩大声称生产模型/会话验证通过。实际查看了部署后的实例、Runtime 桌面截图和团队 390px 截图。

[镜像来源](assets/2026-11-11-uiux08-implementation/deployment/candidate-image.json)、[滚动更新结果](assets/2026-11-11-uiux08-implementation/deployment/rollout-result.json)、[HTTPS/静态资源校验](assets/2026-11-11-uiux08-implementation/deployment/http-check.json)、[线上只读浏览器检查](assets/2026-11-11-uiux08-implementation/deployment/browser-check.json)。完整操作脚本、日志、私有部署备份与六张现场截图保留在集群 `/root/cm-uiux08-20261111/deployment-e737ca5` 及其父目录；凭据与业务截图未加入 Git。

需要回退时，在 81.54 执行：

```sh
kubectl -n clawmanager-system set image deployment/clawmanager-app clawmanager-app=clawmanager.local/clawmanager@sha256:30f98851d7b3f061718ad0ee5a5e94a5d4af580adfbd954c236373733185085e
kubectl -n clawmanager-system rollout status deployment/clawmanager-app --timeout=180s
```

本次授权仅更新测试环境；没有推送 Git、合并分支或更改其他环境。
