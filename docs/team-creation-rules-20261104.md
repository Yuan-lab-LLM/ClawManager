# Team 创建规则对齐与发布记录

开发仓库为团队项目 `ClawManager-team-upstream-merge`，分支
`codex/team-personal-integration`。未修改个人项目、备份项目或 AgentsRuntime。

## 最终规则

| 范围 | 新建规则 |
| --- | --- |
| CM、北向公开 API、北向门户 | 使用同一公共 Team 准入校验 |
| Leader | 恰好一个，固定 OpenClaw Lite |
| Worker | OpenClaw、Hermes、OpenCode，均为 Lite |
| 协作模式 | 新建为 leader_mediated；历史模式保留 |
| 自定义模板 | 2～6 人；职责由模型生成，Runtime/模式由系统控制 |
| 内置模板 | 保留各自人数规格，包括已有八成员模板 |
| 普通实例 | 保留原有 Lite/Pro、配额、镜像和授权逻辑 |
| 存量 Team | 保留 OpenCode Leader、Pro 的历史读取和生命周期兼容，不自动转换 |

`services.ValidateNewTeamRequest` 在操作预约、同名失败资源清理和实际资源分配前
检查最终配置。显式 Pro、未知模式、冲突字段和非 OpenClaw Leader 被拒绝，
不会静默转换成 Lite。历史表示层 `planTeamMembers` 保留兼容能力，不能把新建
规则无差别放进它，破坏已有资源恢复。Owner 创建全部步骤已提交时可完成原有
就绪检查；步骤未齐全的违规旧配置不能继续新增成员。失败重试也会在入队前拒绝，
原有数据保留，并返回明确原因。

自定义模板的生成、整体修订和成员调整校验唯一 Leader。已知 Worker 调整时，
模型不能通过 isLeader/role 改变原身份；原成员 ID、用户图标和 Leader 基础职责
保留。整体生成有身份歧义时不保存，不自动反复调用付费模型。旧模板读取返回
creation_error，不重写原 ID、内容或版本；创建时再次验证。CM 提交已确认的成员
快照，北向检查模板版本并冻结合成结果，后续修订不影响已创建 Team。

两个创建页面共同使用 `teamCreationPolicy.ts`，移除 Pro 和 Leader Runtime 选择。
自定义编辑页说明运行规则，Leader 只允许调整职责。实际成员入口 `MemberEntry`
以及门户成员列表使用 `target="_blank" rel="noopener noreferrer"`，分别保持 CM
或北向实例详情地址。打开成员不会替换原团队对话页，也不会派发任务。

未修改任务派发、成员串行执行、依赖审核、正式回执、Leader 汇总和 Worker 参数。

## 提交与镜像

- `152787d9`：公共新建准入、自定义身份校验、旧模板提示、旧操作保护和关键测试。
- `bb142c00`：CM/门户/自定义页面对齐，成员入口新标签页，前端测试及发布装配。
- App 镜像：`10.130.14.23:5000/clawmanager-hxc-app:team-lite-policy-20261104-v1`
- Registry digest：`sha256:e458fa0732260efeef04c8cc6b32c62ac35a059d2c9aa4cab1736aab2a6777d8`
- 镜像代码源码：`bb142c00f47150830c25f9aef5dd67aded0dca0f`。

使用 `/home/hxc/clawmanager-apply.sh`，参数为上述 APP_IMAGE、
`TENANT_SUFFIX=-hxc-peer`、`NODE_PORT=32543`。实际 App/Gateway Ready、restart0，
imageID 与 Registry digest 一致；认证版本接口返回 bb142c00。正式入口执行过
nginx 配置检查。保留原 nginx/独立 Runtime origin 配置、备份工具与旧前端哈希
资源；重新构建服务端、Gateway 和前端，不更新或重启 Runtime。

原 App 镜像为 `10.130.14.23:5000/clawmanager-hxc-app:team-recovery-20261104-v3`，
摘要 `sha256:62c037bb5ad3ff04981c9a581500a6e9b231f8190411e0bfd987be04b1fefd0b`。
原 Deployment 配置保存在服务器私有 `/home/hxc/team-lite-policy-20261104/`。
无数据库迁移、格式迁移或用户数据删除；App 回退无需回滚数据，但回退旧版也会
恢复旧的新建准入行为，需要控制新建风险，不能认为旧镜像已包含本次限制。

## 必要验收结果

- 先复现两个失败：Worker role 为 leader 但 isLeader=false 被错误保存；调整 Worker
  时模型把 role 改成 team-leader。修复后的测试通过。
- 后端全量 `go test -p 2 ./...` 通过。真实一次性 MySQL 验证 CM/Owner 的拒绝、
  无操作/Team/实例残留、旧模板和重试保护；普通 Pro 全局解析保留。
- 前端规则测试、TypeScript/Vite 正式构建通过。使用现有 Node22.11 构建时 Vite
  给出版本建议，构建退出码为0；额外 Node24 Linux 依赖安装停滞后取消，未冒称
  该额外构建成功，也未修改全局网络/包安全设置。
- 线上 SPA 入口无缓存，JS 入口文件 SHA256 为
  `cee1ed9c2cc15137404e9be840dc3579d7679e94530d4a20505a549be62d7d1d`，
  与本地已验收构建逐字节一致。
- 实际公开北向 API 和 CM 请求拒绝违规 Leader/Pro；同名测试残留 Team 为0。
- API 凭据缺少模板生成 scope 时返回403，未放宽权限。北向自定义模板正向验收
  使用该 owner 的正式门户 SSO/HttpOnly session 链路，凭据仅在内存。

| 实际链路 | 结果 |
| --- | --- |
| CM 自定义模板15 → Team176（585/586）→ 任务378 | OpenClaw Lite Leader＋OpenCode Lite Worker；Worker 文件读回779，Leader正式关闭根任务 |
| 北向自定义模板16 → Team177（587/588）→ 任务380 | 同一成员规则；Worker 文件读回391，Leader正式关闭根任务 |

两个任务各两个工作项、revision1；Worker 回执不关闭根任务，Leader 完成后成员
idle/currentTask=null。四个 Redis 收件组 pending0/lag0，八条通知 outbox 已送达、
无错误。原 Pro570/573仍 running；历史372/373/374仍完成。新成员使用已有
Claw251717 / Codee71280 镜像与池367/368，不涉及本轮 Runtime 升级。

浏览器自动验收被工具安全检查停止：无法可靠识别当前 Chrome URL。未绕过、
未改用其他浏览器自动化。因此本轮不声称真实页面视觉或实际点击新标签页验收
通过；已完成组件链接契约、正式构建、线上文件一致性及真实接口/模型/产物验收。

本机忽略目录 `team-optimization-docs/team-policy-*20261104*` 保存测试、构建、推送、
部署、真实任务和队列证据，禁止混入提交。自有 MySQL 容器及一次性卷已清理，
Registry 临时桥和 SSH 隧道已关闭，保留镜像、构建缓存及证据。未操作32443、
生产或其他租户；没有创建定时监控。Git 分支推送未在本轮授权范围，镜像已推送。
