# DeepSeek Harness 版本同步与 Team Worker 接入说明

原始核对日期：2026-11-05。原分析基线保留在下文；实施结果已更新到第 0 节。

## 0. 第一次开发交付：M0、M1 已完成并在 32543 验收

本轮严格限定团队仓库 `codex/team-personal-integration`。个人两个仓库仅只读已提交
来源，没有修改其工作区。App 本轮只更新交接文档，Runtime 按功能适配新版 DSH，
没有整体移植个人单体 Agent，也没有修改稳定 Team 调度或增加 Worker 必填参数。

| 阶段 | 结果 | 提交与证据 |
| --- | --- | --- |
| M0 | 固定 0.2.1-rc.3 完整依赖/原件与原生宿主契约；工具上下文、非人工输入、真实取消/idle、同会话恢复可用 | Runtime `396af99`，后续 `73cbfe6` 加强实际取消及子进程限制断言 |
| M1 | Lite/Pro 同版本；模型窗口/vision、组合 preset、Cookie 续期、nofile、受限模块加载、Pro 模式选择已适配 | Runtime `1523f11`、构建输入 `21e5d05`/`cd6f79f`、修复 `01b8077` |
| 发布与现场验收 | 两个镜像已 push；32543 新增独立 DSH 池及原生访问域名；普通 Lite #589、Pro #590 页面与真实模型回复通过 | Runtime `docs/dsh-m0-m1-release-20261105.md` 记录完整 digest、来源、验收和回退 |

现场与预想存在一个重要差异：32543 仍使用兼容进程池，未启用全局 native slots。
本轮保留其实际部署契约，只新增独立 DSH 池，不为此迁移已有产品。镜像保留团队
isolated slots 支持；只有可信 supervisor 的 IsolatedLifecycle 才允许内层使用外层
实例私有持久化 `/tmp`，兼容池保留内层 tmpfs，read-only 未修改，Pro 保持原始 sandbox。
不能把本次发布写成“全局沙箱迁移已完成”。

最终镜像业务源码为 Runtime `01b8077`；后续只有测试和交接更新。

- Lite：`10.130.14.23:5000/agentsruntime/deepseek-harness-lite@sha256:a356a00e15da7e9e8807cdcdf913acfa617ed5d9c17ac3f4828a9ed22b28f431`。
- Pro：`10.130.14.23:5000/agentsruntime/deepseek-harness@sha256:6dd7f1a01a4b7644fec6fe6f6621b36b7f9b5495555cd15fa0500e07fdebe371`。

App 沿用已验收的 `clawmanager-hxc-app:team-portal-sync-20261105-v2`，无需重打业务镜像。
在 `/home/hxc` 用该 APP_IMAGE、`TENANT_SUFFIX=-hxc-peer`、`NODE_PORT=32543`
执行 `clawmanager-apply.sh` 成功。Core 只追加 DSH 独立域名配置，镜像目录只更新
DSH id7/id8；服务端原配置保存在 `/home/hxc/team-dsh-m1-20261105/`，旧目录值保存于
本地忽略的验收证据目录。实例和旧数据没有删除。

必要验收包括：固定原生包契约；真实低权限镜像启动；Cookie 续期及不重放；模型配置；
旧 0.1.2-alpha.3 会话迁移及用户 profile/技能/文件保留；现有外层私有 tmp 隔离/恢复；
最终 Lite/Pro 现场依赖与二进制匹配；#589/#590 的公开模型回复和 native 完成事实。
北向原规则仍为 Team Lite、Claw Leader、自定义 2–6 人，DSH 不在 Team 候选中；
原有 29 个 Runtime Pod UID/镜像未变，Team175/任务381报告 SHA 与 3166 bytes 均未变。
App 业务代码/schema 未变，未重复无关的全量构建和大规模验收。

**2026-11-06 第二次开发：M2–M4 已实现，真实模型与发行验收通过。**
Runtime `dbac27e4` 接入原生 DSH Team 宿主；`ea2b4c07` 修正实际受管工作目录，
`494debd7` 保留心跳的兼容容量字段，`a4aaad23` 修正原生 AGENTS.md 上下文识别。
App `677f9f1a` 接通 CM/北向/自定义模板、
能力过滤及受保护预览；`2699a226` 修正文案，`630d2b37` 保留旧 Hermes 错误契约。
`b3586758` 修正已终结旧根任务的 running 历史卡覆盖成员状态，仅过滤状态投影，
保留历史工作项、未知事实及新任务归属，不修改调度和任务结果。
DSH 只开放 Lite Worker，Leader 仍仅 OpenClaw Lite，普通 Pro、稳定调度及 Worker
最小参数均保留。M1 镜像仍不支持 Team；新的 Team 镜像才具备 `dsh-team-v1`。
App 初次镜像 `team-dsh-20261106-v1`、最终状态修正镜像 `team-dsh-20261106-v2`
按授权更新 32543，DSH 最终镜像 `team-dsh-021-20261106-v4` 使用独立 Team 池。
真实验收通过 CM Team178/任务386、北向自定义 Team179/任务385，包含 DSH 正式交付、
OpenCode 核验、Leader 正式关闭及产物读回。首轮 CM383 因适配错误正式失败并保留历史，
未改写其状态；后续386才是最终镜像的成功验收。未用 fixture 回复代替模型验收。
最终来源、镜像 digest、证据、现场发现和回退见团队 Runtime 的
`docs/dsh-m2-m4-release-20261106.md`，实时完成状态以该文档最终验收段为准。
新版会话已写入后不能直接假定旧包能读；回退先停止新增分配、保留绑定及数据，
在隔离副本验证，不能删除 NFS 目录或整份覆盖后来变化的部署配置。

## 1. 当前代码与提交状态

| 用途 | 仓库 | 核对的已提交基线 |
| --- | --- | --- |
| 唯一 App 开发目标 | `D:/test/ClawManager-team-upstream-merge` | `codex/team-personal-integration@31d6122e06dd1ca06155cc33bc7e3dff1e7c7a7f` |
| 唯一 Runtime 开发目标 | `D:/test/AgentsRuntime` | `codex/team-personal-integration@831bba3194608b51da0c47ec8589da4a11490b08` |
| 个人 App，只读 | `D:/test/gitlab/clawmanager` | `codex/openclaw-terminal-writer-reconcile@963dc8734fde0edf88a2d8b8a3ab664616c7d715` |
| 个人 Runtime，只读 | `D:/test/gitlab/agentsruntime` | `codex/openclaw-102-team-integration@ce30810d7eb7f1082c95aa604a4aa1ae408ed4c1` |

开工检查时，团队两个工作区均干净，上一轮门户同步已完整提交，最新 App 提交
为 `31d6122e`。不制造空提交。本文作为新的分析成果单独提交。
个人两个仓库均有其他工作正在进行的未提交内容；本次只读 `git show HEAD:...`，
没有切分支、暂存、清理或修改个人目录。这里的版本是已提交构建声明，不冒充现场镜像版本。
本轮未 fetch 或合并远端；实施时重新固定这些来源 SHA，按下述功能补丁适配。

32543 只读检查：`clawmanager-hxc-peer-system` 中没有 DSH Runtime Pod，数据库
也没有 `type=deepseek-harness` 的实例。接入需要准备新的 DSH Lite 运行池，
不能因为普通实例类型已存在，就假设现场已有支持 Team 的镜像。

## 2. 版本与架构差异

| 项目 | 团队版当前代码 | 个人版已提交代码 | 同步判断 |
| --- | --- | --- | --- |
| Lite DSH 版本 | `0.1.2-alpha.3` | `0.2.1-rc.3` | 有明确版本差异，应先适配新版本 |
| Pro DSH 版本 | `0.1.0-rc.6` | `0.2.1-rc.3` | 另有版本差异；普通 Pro 兼容保留，Team 不开放 Pro |
| Lite Node 基础镜像 | Node 22 Bookworm，固定 digest | Node `24.21.0` Bookworm，固定 digest | 按新版要求同步并记录新基础镜像 digest |
| Agent 组织 | 公共包与按 Runtime 组合的独立模块；DSH 有自己的 `lite`、`dshproxy` | 旧的 `clawmanager-agent/internal/...` 单体结构 | 保留团队架构，不整体覆盖 Agent |
| Lite 执行边界 | isolated slots、网络隔离、维护写入权、实例私有持久化 `/tmp` | 另一套旧目录与 OpenClaw 恢复改动 | 保留团队实现，不借升级带回旧运行方式 |
| Cordis | 锁定依赖族和归档完整性，处理旧版 HMR/Loader 兼容 | 新版本构建与额外 preset patch | 新版重新核对依赖；不能盲留旧 pin，也不能直接移除保护 |
| DSH 内层 `/tmp` | 对旧固定原件做 SHA 校验后，将 workspace-write 临时目录接到实例私有持久化目录 | 未提供团队同样的补丁链 | 对新原件重新审核；不能只改 SHA 让旧字符串替换通过 |
| 模型配置 | 已有统一模型解析，但 DSH patch 的 model 条目较旧 | 增加 contextWindow、逐模型窗口、vision 输入映射 | 移植 DSH 配置语义，复用团队已有统一解析器 |
| preset 管理 | 顶层禁用直连 `tool-web` | 从实际组合配置中修订嵌套 preset，保留用户配置 | 同步新版管理脚本与测试，不能只禁用顶层后宣称完整 |
| 浏览认证 | 启动时换取一次 DSH Cookie，后续固定使用 | `browserSession` 依据 Cookie 生命周期提前刷新、合并并发刷新 | 移植到团队 `deepseek-harness/dshproxy`，不恢复旧包路径 |
| 资源限制脚本 | 启动脚本以子进程执行准备脚本 | 改为 `source` 后再 exec DSH | `ulimit` 在子进程中不能提高父进程限制；同步调用方式并验证实际 DSH 进程值 |
| 原生会话兼容证据 | 启动/HMR、持久化 tmp 的现有测试 | 新增版本迁移、模型及配置测试 | 复用双方用例，不把 fixture 结果冒充真实模型验收 |

排除换行差异，`deepseek-harness/` 范围有 **37 个不同路径**：团队独有 23 个、
个人独有 5 个、双方同路径内容不同 9 个。独有不等于缺失：很多是团队模块化、
凭据迁移、依赖锁定及测试文件。必须按功能映射，不能按文件数批量覆盖。

个人 Runtime 的相关最近提交包含 `e8874b8`（统一视觉及图片工具适配），
团队 DSH Dockerfile 的相关来源包含 `c5b52b7`（共享 Lite Agent 与隔离 slots）。
这些提交都跨越多个产品；不能整条 cherry-pick 来代替 DSH 范围适配。

## 3. App 差异不能等同于“DSH 功能落后”

两版均已支持 DSH 普通 Lite/Pro 实例、资源类型、独立访问域名、代理 Host/Origin、
插件批量查询参数、文件区与技能路径。已有枚举迁移不需要重复复制或执行。
按 DSH 名称定位的专用 App 文件未发现需要单独移植的差异。

含 DSH 引用的共享 App 路径共 129 个，其中 35 个内容相同、94 个有其他差异。
这些差异大量来自 Team、沙箱和个人 OpenClaw 恢复链路，并非 94 项 DSH 缺陷。
对 `core_service`、代理、实例 Runtime、技能、文件服务等关键路径进一步核对，
没有发现应整体替换的个人 DSH 实现；普通 Runtime 类型规则中的 WorkBuddy 等差异不属于本次。

因此不覆盖 `main.go`、`core_service.go`、`instance_service.go`、`runtime_scheduler.go`
或整个前端页面。只有接入实际需要的 Team 规则、能力查询和调度候选过滤进行定点修改。

## 4. 当前 Team 的真实缺口

| 链路 | 当前事实 | 必须补什么 | 邻近影响及保护 |
| --- | --- | --- | --- |
| 创建准入 | 后端仅接受 Claw/Hermes/OpenCode；前端同样列三类 | 增加 DSH Lite Worker；Leader 仍仅 Claw Lite | CM、北向、内置/自定义模板和异步冻结请求一起校验；普通 Pro 不受影响 |
| Runtime 执行 | DSH 仅启动原生 Web 与认证代理 | 新增 DSH 原生宿主/Team 适配器，接现有 Team 协议 | 注入环境变量不是已经接通；普通实例不启动 Team 消费器 |
| 模型工具 | 未安装 DSH 的 `team_*` 原生工具 | 注册同名工具及完整现有 schema | 完成仍只要求 status、summary；保留正文和产物等可选字段的说明 |
| 身份关联 | 通用 Team 配置已有成员/任务上下文 | 原生会话、输入、工具调用与当前派发绑定 | 不要求模型提交工作项、版本、令牌、哈希；冲突不猜身份 |
| 结果回传 | 没有 DSH 正式回执与最终正文的接线 | 接正式完成工具、公开正文补充及原生失败事件 | DSH turn 结束不等于业务完成；Worker 不关闭根任务 |
| 独立观察 | 原生 DSH 进程健康可读，协作心跳未接 | 启动后主动 presence、执行事实和有限退出探测 | 不能等第一次任务才变在线；未知停止不触发并发重派 |
| 镜像兼容 | OpenCode 已按 TeamPluginVersion 过滤运行池；DSH 无对应证明 | DSH 独立 Team 能力标记和候选过滤 | 仅 Team 实例要求能力，旧镜像仍可服务普通实例 |
| 文件/预览 | 通用产物协议已有；受保护预览迁移仍明确限定 Claw/Hermes | 核对 DSH 共享目录、用户组、scope 和原生 sandbox 写入权限；补受保护预览能力 | 不能无条件删掉白名单；不支持浏览工具时允许静态检查并诚实说明 |
| 正文补充 | 后端 `projectNativeDeliverySupplement` 当前只接受 OpenCode | 精确增加经过认证的 DSH 来源及原生输入证据 | 不取消来源限制，不让任意 Runtime 的旧正文修改已审核结果 |
| 门户统计 | 已有 DSH JSONL/zstd、source.kind、turn 完成的读取 | 用新版实际格式验证，并标注 Team 自动输入来源 | 不把自动派发、恢复提醒和子代理计为人工提问；保留旧事实键和 XP |
| UI 展示 | Team 类型、选择器和名称分支只含三类；部分兜底会当作 Claw | 两端统一加入 DSH 类型、标签、图标及可用镜像状态 | 不改 Dashboard 布局、成员新窗口、模板人数和现有 Team 样式 |

## 5. 接入方案：复用协议，只增加 DSH 宿主适配

范围固定为 **DSH Lite Worker**。继续使用现有 Leader 中介模式、串行成员消费、
工作项/版本、阶段依赖、正式回执与 Leader 汇总。保留现有 2～6 人自定义模板规则、
owner 隔离、Team 数量限制和资源账号；不把个人版的限制倒灌到团队版。

新增代码建议位于 Runtime 的 `deepseek-harness/team/`，包括原生宿主插件、
执行/观察适配和测试。复用现有 `plugins/openclaw-redis-team/dist/index.js` 的
协议导出方式及 `createRuntime`、`withActiveEnvelope`、消费与持久回执能力；
不复制出 DSH 专用业务状态机，不为此重构稳定的 OpenCode/Claw 消费方式。

DSH 宿主插件处理其自己的会话/Agent、工具注册和公开事件；Team 协议层负责
原有 Redis 收发与业务身份。进程内适配优先；如果实际固定包要求本地 IPC，
只允许带随机凭据的实例内本地通道，不新增公开控制 API，不向模型暴露管理凭据。
只保留一个 Team 消费者；HMR/插件卸载必须先停止准入，不能加载出第二个消费者。

官方 Agent/Session/Tools 文档支持采用原生扩展点，而不是操纵浏览器页面或
再起一个无关 CLI Agent。但本次查看的是官方 master 文档，不能当成
`0.2.1-rc.3` 的精确调用签名；M0 必须核对固定 npm 原件和实际依赖，再冻结契约：
[Agent](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/agent/README.md)、
[Session](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/session/README.md)、
[Tools](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/tools/README.md)。

具体容错要求：

1. 系统派发先固定原生会话/输入与工作身份；响应丢失先按原身份查回，不能再建一个执行。
   原生 ID 使用 DSH 该版本允许的格式，不照抄 OpenCode 的 `ses_`/`msg_` 格式。
2. 工具执行从认证的原生上下文继承身份。最小完成调用保持现状；可兼容明确的
   `completed/done/success` 别名，但仅限显式完成工具，不把原生 `turn/end` 自动当作完成。
3. 参数错误返回安全、可修正的原因；写入结果不确定时保持待核实，不能说“未执行”诱发重放。
   原生 call ID 与请求摘要保护重复调用；未知字段/错误 ID 不能覆盖可信派发身份。
4. 正文、文件和 idle 不构成完成回执。正式回执后，属于同一原生输入的最终公开正文
   可走补充链路；不抓思考、中间文本或其他会话，不改旧审核和完成判定。
5. 原生异常与正常结束但漏回执分开报告。复用现有 Monitor 的全根任务检查、
   有限恢复和旧事件保护；不新增自动强停 Leader、清工作项或盲目再次派发。
6. 存活/工具进展观察独立于模型调用。超时只触发已有受控退出流程，确认原执行停止
   才能接下一条；停止不明就保留占用并显示原因，不以 lease 过期当作停止证明。
7. Team 工具只属于受管 Team 原生输入。普通手工会话不能借用 Team 凭据或工作身份。
   关闭 Team 配置的 DSH 实例保持原生体验，不加载消费者和 Team 工具。
8. 自动输入使用该固定版本支持的非人工 source/origin 证据并持久化；不要伪装成
   `source.kind=user`。若原生契约不能表达，使用可信关联证据在统计读适配中排除，
   不能按正文关键词猜测，不能通过改成成功零值掩盖格式不支持。

## 6. 详细执行阶段与提交门槛

### M0：固定新版契约，直接验证关键假设

- 固定个人 `e8874b8` 中 DSH 相关最终语义与团队现有模块映射，不整提交移植。
- 取得 `0.2.1-rc.3` 及实际依赖的固定原件/完整性清单，核对原生输入准入、
  会话恢复、事件持久化、工具原生上下文、取消与 idle 语义。
- 核对新版 Cordis 是否仍需要旧锁定族、内层 sandbox 与 `/tmp` 的实际实现，
  保留约束同时去除已被原生解决的旧兼容补丁。不得靠替换 SHA 绕过变更检查。
- 用任务专用临时目录验证一次最小“送入原生输入→调用测试工具→读最终公开文本→确认 idle”。
  使用受控模型 fixture 只证明契约；不是生产模型验收。

通过条件：上述关键扩展点与身份恢复可用、依赖可固定、沙箱约束可保留。
若该版本无法安全接入，先输出具体不兼容点和最小原生修补范围，不开放前后端选项。
不要先花时间建大批管理页面、抽象层或重复测试矩阵。

### M1：同步 DSH 新版本，保留团队沙箱架构

- 在团队 DSH Lite Dockerfile 更新版本/Node 和实际依赖；继续从团队模块编译 Agent，
  保留 `bubblewrap`、网络隔离、固定 image-owned binary 及完整性检查。
- 适配团队 `dshproxy` 的提前 Cookie 续期。并发刷新合并；请求派发前刷新，
  不在 401 后自动重放 `/api/respond` 等可能已经产生副作用的请求。
- 移植模型 contextWindow/vision 和组合 preset 的管理补丁，保留用户自己的 profile、
  技能、模型路由；保持仅经 CM 治理的模型出口，不恢复公网直连工具。
- 修正资源准备脚本的 shell 作用域，验证实际 DSH 进程继承的 nofile，保留节点限制提示。
- 普通 Pro 使用同一版本语义，保留其既有桌面监督器和目录；单独验证 Pro 启动、
  浏览会话和旧文件读取，Team 准入仍拒绝 Pro。Pro 基础镜像解析后固定 digest，
  不为了构建 DSH 修改或升级现有 OpenClaw 运行池。

必要验收：一个 Lite 实际镜像完成启动、认证、插件加载/卸载、Cookie 短生命周期续期；
旧会话+用户 profile/技能/普通文件在隔离副本中恢复；实例私有 `/tmp` 保留且不能读
其他实例目录。再做一个普通 Pro 冒烟。复用双方已有测试，针对新增缺口补失败用例。
按 Runtime 可运行成果提交，不宣称已支持 Team。

### M2：贯通 DSH Worker 原生执行与交付

- 注册现有 `team_send/status/update_progress/complete_task/artifact_*` 工具，
  保留正式工具 schema 全量可选字段及其说明。
- 在现有 active envelope 边界接 DSH Agent/session；复用串行消费、工作项身份、
  去重、durable receipt、断线重放和终态规则。
- 主动 presence 与独立原生执行观察接通；补公开最终正文、异常、漏回执的正确分类。
- 检查 shared dir、`/team` 产物路径映射、UID/GID、共享组与 DSH 内层 sandbox 的读写许可，
  不把根目录、整个 `/workspaces` 或其他 Team 作为 writable grant。
- 原生插件重载/进程重启时保留原会话与输入映射；先停止旧消费者及确认旧执行退出，
  重启不得复制模型工作或重复发布回执。

必要验收：一个 Worker 完成最小参数交付；包含正文/文件的交付；重复派发/丢响应
只执行一次；原生失败不报漏回执；旧代数工具与迟到结果被拒收；未确认停止不并发执行。
这些竞争场景采用受控 adapter/Redis 测试即可，不逐项消耗真实模型。
提交 Runtime 适配及对应构建/能力声明。

### M3：CM、北向及自定义 Team 一起接通

- App 后端：`team_creation_policy.go`、`team_service.go` 的 Runtime 归一化/文案、
  模板与最终冻结请求校验，增加 DSH Lite Worker；Leader 规则不放宽。
- Runtime 能力：沿用已有 `TeamPluginVersion` 字段，定义 DSH 独立版本标记
  （建议 `dsh-team-v1`，实施时冻结）；只在实际桥接产物和原生版本通过验证后上报。
  候选选择与已有绑定复核都检查能力；普通 DSH 实例不因此被禁止使用旧镜像。
- 不全局放宽 preview/source 白名单；以 Runtime 版本、身份、代数及实际镜像为依据
  适配 DSH 预览与公开正文补充。补充事件沿原业务证据链去重，不能改变审核依赖。
- 北向 `team-capabilities` 复用同一规则；创建、模板生成/修订/再生成、成员调整、
  操作重试全部执行同样校验。旧冻结请求不会因新增 Runtime 自动变成另一份计划。
- 前端 `teamCreationPolicy.ts`、`types/team.ts`、CM CreateTeam、OwnerTeamDialog、
  模板页面与成员显示一起补 DSH；删除三元判断的错误 Claw 兜底，保留现有布局和新窗口进入。
- 用新版实际日志核对 `ownerrecords/dsh_stream.go`，验证自动 Team 输入/子代理不重复计数；
  保留已有 generation fence、source keys、XP、历史 snapshot，不重置 epoch。

必要验收：CM/北向/自定义均接受 DSH Lite Worker、拒绝 DSH Leader/任意 Team Pro；
无兼容运行池时明确不可创建且不留下资源；普通 DSH Pro 仍可用；跨 owner 文件和任务拒绝。
必要 schema 验证只确认已有 VARCHAR/能力字段足够，不新造表、不修改历史迁移。
按 App 后端和两端 UI 可运行成果提交。

### M4：少量真实闭环与发行准备

1. 固定代码后跑受影响 Runtime/DSH/Team 测试、一次必要后端全量及前端构建，
   同一代码不重复跑无关全量矩阵。
2. 实际最终 Lite 镜像核对 npm/Node/Agent/桥接协议版本、安装路径、能力上报与 digest。
   不是给旧镜像贴一个标记就当成通过。
3. 32543 若仍无 DSH，准备一个兼容 DSH Lite 池；先用新建测试成员，不重建既有
   Claw/Hermes/OpenCode 实例，不迁移忙碌的旧任务。
4. 一条 CM Team（Claw Leader＋DSH Worker）和一条北向自定义 Team
   （Claw Leader＋DSH Worker＋OpenCode Worker）完成真实模型任务：
   对话→派发→原生内部公开消息/工具证据→正式回执→Leader 汇总→产物读回。
   检查工作项没有重复、Worker 不关根、最后成员空闲、owner 归属和 Dashboard 计数正确。
5. 一次断线/页面重新进入验证普通 DSH 入口与 Team 成员新窗口访问；不靠刷新触发重新派发。
   模型竞争故障复用 M2 测试，不在服务器反复重演所有失败。
6. 分别记录 App、Runtime 源 SHA、镜像 digest、配置、真实验收任务及保留数据。
   发布须有该阶段明确授权；本轮分析不包含现场部署。

## 7. 回退与完成判定

版本升级可能改变 DSH 会话存储或配置。发布前保留原成品、原配置与非空测试副本的
迁移证据。新版本写入后不能直接假设旧 DSH 可读，也不能通过覆盖旧目录回退。
暂停新增 DSH Team，保留已接受任务与成果；确认执行退出后才调整绑定或切换镜像。
如果旧 App 不认识 DSH Team，保留认识该类型的兼容 App 并关闭新建入口，
不能退到会把它当 Claw 或丢成员的旧版本。

只有“新版本实际镜像可用＋普通实例兼容＋两端 Team 创建与执行＋正式交付＋
身份/去重/恢复保护＋统计与权限”均通过，才称 DSH Team 已接通。
单纯升级 npm、设置环境、增加选项或让页面 HTTP 200 都不算完成。

原始分析时未做业务开发或 DSH 验收；分阶段实施、发布和验收见第 0 节与对应发行记录。
不能把普通实例成功或受控模型测试等同于 DSH Team 的真实生产链路验收。
