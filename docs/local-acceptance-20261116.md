# 合并后本地验收记录（2026-11-16）

## 结论

部分通过，尚未达到全量发布验收。未提交、推送或更新 K8s；Hermes 运行时版本升级不在本轮范围内。没有修改 Team 业务代码。

## 已通过

- 前端 `npm run build`：TypeScript 与生产打包通过。存在大资源包、Browserslist 数据过期等非阻断警告。
- 前端、鉴权、实例代理入口、OpenCode 原生入口、桌面凭据续期和恢复、治理只读预检：58 项通过。Windows Node 22 需 `--experimental-strip-types`；CI 配置使用 Node 24。
- 工作区状态与运行时管理：28 项通过。
- OpenCode Playwright 存储隔离：通过。验证 legacy 缓存隐藏、local/session storage 与 IndexedDB 隔离、刷新保留状态、不破坏其他实例数据。
- OpenCode Playwright 新向导：通过。验证显式创建项目、焦点保留、重新打开向导、状态及权限隔离。
- Gateway、repository、services、handlers 当前代码的 Linux 测试程序均成功编译。
- Gateway 全量测试输出 PASS；repository 非数据库全量测试输出 PASS；handlers 全量测试输出 PASS。部分 Go 日志写入 stderr，被 Windows PowerShell 包装为 NativeCommandError，不能将包装后的管道退出码当成业务断言失败。
- Linux 回执/恢复工具：188 项中 185 通过、3 跳过、0 失败。剩余三项需要配置实际 AgentsRuntime writer 夹具，不能计为通过。Windows 对应运行结果为 154 通过、34 跳过。
- 独立 MySQL 8.4.8 集成测试输出 PASS：实际执行并通过浏览器源身份分配、任务检查、撤权生命周期、模型稳定身份、能力登记/续期、停机审计。使用独立测试容器和测试 schema，无业务数据库凭据。

## 失败与限制

### 首轮服务层全量测试：2 项失败（后续修复记录见文末）

1. `TestManagedRedisNativeScriptsMatch`：实际读取本地 AgentsRuntime 的 CRLF 文件，但提取器要求 LF 函数边界。只读诊断证明：原始输入提取失败；仅在内存中将 CRLF 转 LF 后可成功捕获两个 Lua 脚本。这证明当前失败受换行格式影响，不代表已完整验证 Lua 内容等价。未修改 Team 或 AgentsRuntime 文件。
2. `TestNFSTeamStoppedMutationStillTracksIntent`：200ms context deadline 在 durable intent 开始前耗尽，子进程被终止。尚不能排除实际时序问题，也不能仅凭超时认定生产逻辑有错；需要在原生 Linux 验收环境进一步定位。

### Hermes 管理端渲染器

安装锁定依赖后，测试 35 项：25 通过、10 失败。失败涉及构建前缺失的 `.upstream/source` 快照文件；未执行获取该快照的完整渲染器构建。不是完整 Hermes UI 验收通过，亦没有将管理端 v0.22.5 约束与现有 0.21.0 运行时匹配问题标记为已解决。用户要求暂不处理 Hermes 升级。

### MySQL 跳过项

部分上游测试要求额外的 `TEST_IDLE_MYSQL_PORT` / `TEST_RUNTIME_SECURITY_MYSQL_PORT` 等 localhost 夹具变量，本轮未配置，因此跳过。部分父测试显示 PASS 但其子测试全部跳过，不能据此算作功能通过。本轮也不是全套迁移在生产数据副本上的升级验证。

## 尚未覆盖

- 其余 Go 包完整 `go test ./...`、全部 vet/race 检查。
- 浏览器工作进程/CDP relay 的 race 测试和相关镜像构建。
- 全量数据库迁移、生产数据副本升级、所有额外 MySQL fixture。
- 各运行时在 K8s 中的真实新旧会话切换、旧模型引用、撤权中止、重启保留与 slot 生命周期验证。
- 整体应用镜像构建与上线冒烟；本轮未替换现有 K8s 镜像。

## 可复查记录

临时日志位于 `.codex-temp/acceptance-{aigateway,repository,services,handlers,mysql,receipts-linux,hermes-web}.log`。
CRLF 只读诊断脚本为 `.codex-temp/check-native-contract.cjs`。
本轮安装的浏览器缓存位于 `.codex-temp/playwright-acceptance`，可供后续复用。

## 核心代码收尾复测

### 后续新增：上下文配置链路补齐（尚未整体验收）

- 确认管理端已有保存和 `CLAWMANAGER_LLM_CONTEXT_WINDOW(S)` 下发，但 AgentsRuntime 统一解析器原先丢弃该字段；OpenClaw 原先生成固定上下文值。
- 已在 AgentsRuntime 补齐上下文解析、配置对象往返、canonical 输出，以及 OpenCode 的 `limit.context`、OpenClaw 的 `contextWindow`、Hermes 的模型 `context_length` 写入。Hermes Desktop 配置分支也补充模型字段。
- 按限定 provider/model、裸模型名、默认值依次取值；显式零表示未知，不套用其他模型的限制。非法负值、非整数及溢出输入拒绝。
- Harness 修复零上下文被误判为配置错误的问题，零保留运行时默认值。
- 验证：AgentsRuntime 的 llmconfig、runtime/opencode、runtime/openclaw、runtime/hermes 四个包测试通过；Harness 配置测试 2 项通过。包含新增的实际模型配置字段断言。
- 未完成：Pro 独立启动脚本的全部配置路径、已运行实例的上下文更新/自动压缩阈值刷新、镜像重建和真实实例生效检查。因此不能将以上代码变更视为所有现有实例已经生效。

#### Pro 配置路径补充

- OpenClaw Pro 独立配置管理器已增加上下文环境变量解析、按模型写入 `contextWindow`，并约束输出额度不超过已配置上下文。新增回归覆盖重新应用较小上下文和保留无关设置；配置管理包测试通过。
- Hermes Pro 独立启动脚本已增加按模型写入 `context_length`，限定为同 Gateway URL 且使用托管凭据引用的 provider，保留其他 provider 和无关模型元数据。新增 Python 测试 2 项通过，原有目录合并测试 6 项通过。
- 尚未验证 Hermes 原生压缩器是否采用该模型元数据；不能只凭 YAML 字段存在宣称压缩阈值已更新。现有进程刷新、旧镜像生效及真实 K8s 联动仍未完成。

#### 每轮刷新与原生压缩器接入补充

- 已核对锁定 Hermes 源码的原生 `context_length` setter：它会使派生压缩阈值、尾部和摘要预算缓存失效。托管 `monitor_turn` 开始前现读取用户授权的 Gateway 目录，对唯一匹配的当前模型调用该 setter，并同步 Agent 的配置上下文值。未改写历史会话；Team 和用户独立 provider 排除。
- 非法、歧义或不可用目录不用于猜测模型；零上下文保留原生值。失败仍按托管授权目录不可用处理，不回退到其他模型。刷新不会中途改变已执行中的一轮。
- 新增 Hermes 刷新测试 4 项通过；既有治理测试 36 项中 34 项通过、2 项需要原生源夹具而跳过。补丁哈希已同步并校验一致。
- OpenCode 的现有实时目录机制已补充上下文变化回归：64K 更新为 32K，输出预算保留；治理测试 11 项全部通过。
- 确认管理端 start/restart 路径会重新生成 Gateway 环境。OpenClaw/Harness 等未接热刷新路径仍以用户主动重启为生效边界，不后台自动重启。
- 管理端中英文上下文说明已补充：保存不自动重启、旧镜像需更新、不支持热刷新的路径需重启。
- 尚未完成的是部署级验收：重建相关运行时镜像、更新测试实例，实测新旧会话与压缩阈值。源码与模拟/配置测试通过不等同于现有运行实例已升级生效。

#### 镜像级预验收与实际版本差异

- 已编译 Linux Agent，并构建独立诊断镜像 `clawmanager-context-canary:20261116`（不是可发布运行时）。其中 Linux `llm-config` 上下文输出检查通过。
- 核实本机现有 Hermes 基础镜像 `6b4524af6dee` 的 `hermes_cli/__init__.py` 实际版本为 **0.16.0**，源码位于 `/usr/local/lib/hermes-agent`，并非当前代码锁定的 0.21.0。它的压缩器没有锁定源码的原生 context setter，所以组件验收明确失败；没有以旧镜像替代目标版本或把失败计为通过。
- 完整构建 `hermes-lite-local:context-0.21-20261116` 尚未成功：第一次在 Docker Hub 解析器镜像令牌请求处超时；改用内置解析器、保持全部源码和摘要锁定后，基础镜像 `ghcr.io/astral-sh/uv:0.12.10` 的 OAuth 令牌获取返回 `denied`。
- 当前阻塞为镜像仓库访问/鉴权。没有修改 Docker 登录凭据、代理、镜像摘要或版本锁来绕过校验。
- 没有部署验收 Job，也没有替换任何现有 K8s deployment；现有 Hermes 部署镜像保持不变。恢复仓库访问后应先完成真正 0.21.0 镜像构建，再执行原生组件和独立 K8s 实例验收。

#### 仓库访问复查

- Windows 匿名令牌请求及 uv 0.12.10 manifest 请求成功（HTTP 200），返回摘要与 Dockerfile 锁定摘要一致，因此不是 uv 标签或摘要不存在。
- 已试临时空 Docker 凭据配置、仅构建进程使用现有代理，以及 Docker 引擎直接拉取同一摘要，Docker 路径仍返回 denied。原有 Docker 登录配置未修改。
- 系统代理为 127.0.0.1:7890，Docker Desktop 为 system 模式，引擎经 http.docker.internal:3128。后续可尝试显式配置 Docker Desktop 代理并重启复测，但可能短暂中断本机 K8s，需用户确认后再操作；目前没有执行重启或修改全局设置。

#### 用户授权后的显式代理试验与回滚

- 用户确认后，先备份并核验 Docker Desktop 设置，再改为显式 127.0.0.1:7890 代理并正常重启。固定摘要的 uv 镜像仍返回 denied，未解决仓库访问问题。
- 已恢复原来的 system 模式，HTTP/HTTPS override 均为空，并再次正常启动 Docker。未强杀后端、未清理镜像/卷、未重置 Kubernetes。
- 回滚后已确认 Docker 引擎恢复，docker-desktop 节点 Ready；clawmanager-system 下原有 9 个 Pod 均 Running 且容器 Ready=true，包括 ClawManager、MySQL、MinIO 和四种运行时。
- 配置备份位于忽略目录 `.codex-temp/docker-desktop-backup-before-proxy-20261116-01.json`。镜像仓库访问仍是未解决阻塞；没有替换运行时镜像，没有把此次代理试验计为功能验收通过。

## 后续本地测试部署（已完成）

本节是用户随后要求本地部署后的新状态，取代上文“没有替换镜像”的历史状态描述。

- 本地入口：`https://localhost:30443/`，首页和 `/healthz` 返回 HTTP 200。
- 管理端/Gateway：`clawmanager-local:context-20261116`，版本接口返回 `local-20261116-context`。
- OpenCode Lite：`opencode-lite-local:context-20261116`。
- OpenClaw Lite：`openclaw-lite-local:context-20261116-r2`。
- Harness Lite：`deepseek-harness-lite-local:context-20261116`。
- 所有 9 个现有 Deployment 均 1/1 available；上述三个新 Lite 镜像的注册记录与持续心跳均为 ready。三个运行时的进程级上下文解析探针通过。
- 三条对应 Lite 默认镜像已同步。Hermes 和 Pro 镜像没有升级；Hermes 专用前端资源保留基础镜像版本，不计入本次新功能验收。
- 使用本机编译缓存和缓存基础镜像构建本地增量镜像，不是官方完整发布构建。管理端 frontend/backend 及配套工具为当前工作区代码；未提交或推送。
- 数据库已备份后执行待补迁移，关键模型身份、provider 引用、停机审计及活动租约迁移记录均存在。数据库备份约 13 MB、Deployment 快照位于 `.codex-temp/local-deploy-20261116/`，已确认被 Git 忽略。
- 保留数据库、实例记录及原工作区 PVC；没有启用容器槽位架构迁移。三个 Lite 池采用 Recreate 先停后启，避免新旧进程并行写工作区，因此运行中的任务可能被中断。
- 发现原来的 OpenClaw 本地 Lite 标签实际使用 Webtop `/init` 入口，导致 Pod Ready 但没有新注册。r2 已改用本地缓存的真正 Lite 基础镜像（tini + openclaw-lite-entrypoint），并核验注册与心跳，不仅依赖健康探针。
- 推荐先新建 Lite 测试实例，明确选择一个模型，配置 64000 上下文后保存并重启目标实例，再验证配置/对话；Auto 仍采用候选部署的保守窗口。没有在本轮真实调用上游模型，因此不宣称端到端对话、压缩触发或所有旧会话已验收。

- 修复 Lua 原生契约测试的 CRLF/LF 兼容问题。读取的源码先统一行尾，变异夹具也使用统一行尾；实际执行和比较 Lua 脚本的检查不变。新增 CRLF checkout 子测试，原有插入 Lua、替换脚本和变更分隔符的拒绝检查保留。
- 修复 NFS 不确定写入用例的取消时机：在上传内容被读取时主动取消，而不是从整个调用开始计时 200ms。10 秒超时仅为测试故障上限。仍检查取消错误、不确定状态、恰好一次意图登记、意图未释放且完成次数为零。未修改 Team 业务逻辑、NFS 权限或生产超时策略。
- 两项定向测试在 Linux 容器中连续运行 5 次全部通过。
- 修复后服务层全量 Linux 测试输出 `PASS`，没有失败断言。日志为 `.codex-temp/acceptance-services-closeout.log`；需要外部 NFS/MySQL 等夹具的跳过项仍不计入通过。
- 本轮没有提交、推送或替换 K8s 镜像。真实实例联调和上述未覆盖项目仍保留为独立验收项。
