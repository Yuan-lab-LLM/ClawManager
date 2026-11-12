# Team 整队启停、容器恢复与上游合并交接

更新时间：2026-11-11。此文替代之前过时的阶段结论；以本文件的现场证据及最终验收表为准。

**当前状态：计划内代码、上游合并、必要回归与32543发布完成。最终镜像上的四引擎任务402正式成功，原Team182整队停止及再次启动4/4通过，历史文件摘要一致。交接提交仅更新文档，不改变已验收业务二进制。**

## 仓库和合并方式

- App：`D:/test/ClawManager-team-upstream-merge`；Runtime：`D:/test/AgentsRuntime`。
- 两仓库均为团队项目 `codex/team-personal-integration`。个人项目业务代码未修改。
- App origin：`https://github.com/Stark-Han/ClawManager.git`；Runtime origin：`https://github.com/Stark-Han/AgentsRuntime.git`。
- App 最终合并基线：`upstream/main@646833710b3fed1a1dbc3b89436a744db3445b1c`，合并提交 `4eee25fa`。
- Runtime 最终合并基线：`upstream/main@3f1cd721cee1880849cd9abc26771c0cc476ac29`，合并提交 `5fbe896`。
- 逐项解决冲突，保留双方功能，没有整体使用 ours/theirs 覆盖。App 的槽位审计冲突保留上游逻辑，同时保留当前 worker 空闲证明要求，历史 epoch 不单独授权槽位复用。
- 既有迁移按完整文件名记账；未删除、重命名或修改已发布 SQL。本轮没有新增 schema。

## 已实现能力与边界

1. CM 和北向共用整队启动/停止接口与状态；原 Team、owner、模板、四种 Lite Runtime、普通实例 Pro 保留。
2. Team 成员不进入普通实例单独自动停机。整队自动休眠要求最新用户任务正式成功、无未结束工作/审核/通知/模型请求/命令/写入，且达到上游空闲期限。心跳、在线回报、页面查询不延长业务空闲。
3. 停止/启动使用原实例和成员，保留工作区、会话、文件和关联。Worker 先启动、Leader 后启动；启停本身不派发任务。
4. 故障恢复替换运行载体，不调用恢复出厂、不创建同角色新实例、不重复占用用户配额。
5. 槽位容器停止按实际容器退出证明完成；404、Pod Ready、租约到期、SQL 显示 stopped 均不能单独代替退出证明。
6. 运行中成员明确出错或停止时，接口显示需恢复并提供启动动作；不因心跳暂时 unknown 改写团队状态，也不修改业务任务回执。
7. 保留 Leader/Worker 原调度、工作项、依赖、正式回执及根任务最终完成规则，没有增加 Worker 必填参数。
8. 沿用上游 NFS、沙箱和权限配置，没有批量改目录权限或放宽隔离。

## 本次修复点与代码入口

| 提交 | 内容 |
| --- | --- |
| `5a585600` App | 过期模型请求租约独立于每日停机扫描清理；短周期控制循环也处理，不撤销有效续租或无退出证明的 legacy 请求。 |
| `a689bf9` Runtime | 区分纯容器重建要求和真实停止失败；与真实错误组合时继续保守失败，不释放旧 writer。 |
| `ffd8e431` App | 明确的 `container_reset_required` 接入既有故障恢复；通用 `gateway_stop_failed` 不整体开放自动重试。 |
| `27a91196` App | 团队就绪状态读取结合实际故障成员，提供恢复入口，历史启动操作记录不被读取操作改写。 |
| `f1b2bdd5` App | 已确认的容器停止来源接入既有冷启动校验，补上错误清理未经过 idle-stop SQL 账本的衔接。 |
| `6def1685` App | 非破坏性续启验证当前停止来源及后续未确认发布；删除仍走完整历史证明，不因旧凭据放松删除保护。 |
| `4daa919e` App | 手动整队停止先阻止新推理，再等现有请求结束；无实际执行空闲证明时不发停止指令。自动休眠继续要求零请求。 |
| `e1a4d89` Runtime | 更新主/恢复管理器时重新生成并验证 Hermes release inventory，原生版本保持0.22.5。 |

主要入口：

- `backend/internal/repository/team_lifecycle.go`：事务准入、整队操作、进度与状态。
- `backend/internal/services/team_lifecycle.go`：复用普通实例启停、原生空闲预检、成员恢复。
- `backend/internal/services/instance_auto_stop_service.go`：独立请求租约清理与已有停止续做。
- `backend/internal/services/runtime_team_stopped_source.go`：读取真实停止来源，保持存储和原生版本约束。
- `backend/internal/services/k8s/runtime_assignment_cleanup.go`：续启与删除的不同证明范围。
- `clawmanager-agent/gateway/container_reset.go`、`isolated_lifecycle.go`：容器重建语义及原始故障保留。

## 请求与 fallback

- 自动停机：先验证业务与实际执行都空闲，再冻结整队状态、停止并验证全部成员。
- 手动停止：业务任务、工具命令和写入必须已结束；可以先记录停止意图阻止新推理，等待当前模型请求自然结束，之后才发原生停止。超限显示需恢复并保留原操作，不无限 running。
- 当前容器需重建：继续已有容器终止协议；必须按原 Pod、容器、generation 和操作确认退出。
- 真正无法正常停止：现有受控管理员容器隔离/终止能力可以作为 fallback；作用于准确的故障执行，不删工作区。不能以通用错误直接无限重试，不能只删 Pod 记录当作旧执行已死。
- desired=stopped 时，完成清理后保持停止；desired=running 时，确认旧执行退出后才让原实例接管存储。
- 新容器可以提前准备，禁止与未确认退出的旧执行同时写原存储。
- 已完成的工作项和正式回执保留；外部工具副作用不确定时先核实，不直接重放。

## 当前发布

环境：`10.130.14.23:32543`，namespace `clawmanager-hxc-peer-system`。

按用户命令执行：

```sh
APP_IMAGE=10.130.14.23:5000/clawmanager-hxc-app:team-lifecycle-20261111-resume-v13
TENANT_SUFFIX=-hxc-peer
NODE_PORT=32543
bash ./clawmanager-apply.sh
```

App digest：`sha256:d36d210ea6555ff6b0c93be6e1232c6433c05a35f5e401df7b0e5902bb819dd7`，业务源码 `4daa919e`。

四种 Lite Runtime 使用 `team-lifecycle-20261111-resume-v8`，管理器源码 `e1a4d89`：

| Runtime | 镜像仓库 | digest |
| --- | --- | --- |
| OpenClaw | `10.130.14.23:5000/agentsruntime/openclaw-lite` | `sha256:4fa6efccc7987d2d63074c62240a27f232950e0217caf1534fdb9a57b10bf7cd` |
| Hermes | `10.130.14.23:5000/agentsruntime/hermes-lite` | `sha256:f3c280bfcaebafb2a5daff6d26043b5d5bdcaa1e8a89b32c8c252c275eaf7694` |
| OpenCode | `10.130.14.23:5000/agentsruntime/opencode-lite` | `sha256:3351b922756bcad78085f112b1963572c59256d36fcdce6d6221f4a0cc798e99` |
| DeepSeek Harness | `10.130.14.23:5000/agentsruntime/deepseek-harness-lite` | `sha256:6881eca847fcbfd03f89d985fd20e792b3aaa9268a33758b1c12418c52b88e41` |

Runtime 主二进制及恢复助手均与本地构建SHA256匹配。原生程序版本分别保持 OpenClaw2026.10.2、Hermes0.22.5、OpenCode2.0.27、DSH0.2.1rc3。池、Core镜像配置、持久化overlay、镜像目录同步到相同digest；普通Pro目录保留。

## 已有现场证据

- Team182 原实例603/604/605/606，成员574/575/576/577，全部保留。
- 原故障Leader603第11代真实容器终止于 `2026-11-11T08:24:10Z`；没有写SQL伪造退出或任务完成。
- 原团队恢复操作 `6736a005-28df-471e-80ca-7ce005edaae1` 于 `2026-11-11T09:01:02.521511Z` 完成4/4；Leader保留原实例进入第17代。
- 原任务401于 `2026-11-11T09:03:32Z` 正式 succeeded/completed，回执 `completion:182:team-182-task-401:leader:root:1`。三个Worker正式结果仍为revision1，没有新增重复工作项。
- 整队停止操作 `c7bd25d6-f3de-400d-8834-1cc741a77717` 于 `2026-11-11T09:21:43.171524Z` 完成4/4，四成员实际stopped；绑定归零后才更新Runtime。
- 普通Hermes609在0.22.5上已实际启动、重启并停止，第5代无绑定；这与更早旧版本608的验收区分记录。
- 395最终文件、397两份Worker产物的字节数及SHA256再次读回保持一致。旧399正式完成及文件证据仍保留，不把旧验收冒充最终镜像验收。

## 最终镜像真实验收

- 发布后原团队启动：操作 `38b88386-d129-430a-bf69-d215ee14135e`，`2026-11-11T09:34:10.41022Z` 达到4/4；原实例603/604/605/606运行代数为18/23/12/10。
- 四引擎短任务402：`2026-11-11T09:44:18Z` 正式 succeeded/completed，根回执 `completion:182:team-182-task-402:leader:root:1`。Hermes、OpenCode、DSH三个工作项 E1/E2/E3 均 succeeded、revision1，分别交付2/4/6；Leader正式汇总，无重复派发或代交完成。
- 任务完成后的整队停止：操作 `d84fc5c4-11b3-4a31-b1f5-a3365e6c84c2`，`2026-11-11T09:45:20.358804Z` 达到 stopped4/4；四成员均 offline/stopped，接口 can_start=true。
- 最终再次启动：操作 `d3eba716-9eeb-4a27-b390-1c857f660b09`，`2026-11-11T09:48:18.600233Z` 达到 running4/4；原四实例全部 idle，can_stop=true。Team及成员ID、owner、存储关联均保留。
- 停启后文件复核：任务395最终文件3541字节、SHA256 `3309f56d5edf73193021c62ce9e2685ca51d55c8f3f844497cd16984ad72cd17`；任务397两份产物894/1087字节、SHA256分别为 `f658c2c4a32bc9df26f207bebd5d150a073dcd75ea754118a8ac8980c07efb87` / `bf4b342f4dcaa86bdb89f25b48c5536c9c3f6cb02025141749e43a0edce07485`，均与停启前一致。

自动空闲准入已用真实MySQL及关键并发/失败用例验证；未缩短发布镜像的一天空闲期限，也不声称现场等待一天的自动触发已完成。现场完整停止/启动验证使用正式手动入口及相同成员执行链。

## 必要验证

- 真实MySQL：关闭/未到期的空闲扫描不阻断过期请求清理；有效租约、legacy请求保持保护。
- 真实MySQL：整队故障显示不重写操作；手动停止冻结新推理、等待旧请求结束；活动业务任务、原生任务、写入仍拒绝；自动停止仍拒绝活跃推理。
- 真实MySQL：容器停止来源续启保留owner、工作区、原生版本及升级围栏。
- K8s控制边界：当前停止来源与删除历史分开；新代数未确认发布、错误owner/工作区、未完成容器释放均拒绝；旧凭据不授权删除。
- Runtime：纯重建要求可接续，组合真实错误继续失败；保留旧writer/端口，不能提前启动后继；既有槽位、空闲停止及隔离回归通过。
- 本轮未改前端源码，不重复构建前端；沿用已通过的原前端构建和页面验证。

## 数据、证据与回退

- 本机私有证据：`D:/test/ClawManager-2/team-optimization-docs/team-lifecycle-build/`；服务器：`/home/hxc/team-lifecycle-20261110/`。不提交凭据、token、配置正文或数据库dump。
- 最新上线前部署及配置快照：服务器 `resume-before-*.json`，保留原PVC、NFS设置和旧镜像。回退前先核实并收尾已接受的启停操作，不能让旧执行与新执行同时写存储。
- App兼容回退候选v12 digest `sha256:7515e00a8e36adb0144b9a284d4284059ad58f9760d6fb994f8280766a407fe9`；不回退schema或覆盖业务数据库。
- Hermes604旧0.21工作区已在停稳、备份和原生迁移验证后升级0.22.5；3个会话、37条消息迁移前后关键字段哈希一致，之后产生的新业务数据继续保留。禁止直接换回0.21。
- 原Hermes完整备份：服务器 `team-182-before-hermes-0225-20261111.tar.gz`，SHA256 `09b9b85400fb415ff31d85d218a56dae793b1a07f311ff10245f7bd26ee6372c`。
- 已存在的显式管理员kernel容器隔离证明不等于graceful退出，不用于自动休眠，不放宽给owner或Worker。
- 旧历史任务失败、旧工作项仍显示running的历史保留，不被改成成功；只有生命周期准入识别已终结失败父任务，不改业务账本。

## 复盘

- 必须区分代码完成、正常路径通过和最终镜像现场闭环。进度按具体证据报告，不再使用无统一口径的百分比。
- 之前把模块构建、版本迁移、上游更新及业务内容验收混在一起，导致交付链路过晚暴露问题。
- 本轮恢复来源最初误用了删除用的全历史校验，增加了无效阻塞；应一开始区分续启和删除的权限范围。缺少诊断又增加了一轮部署。
- 有效措施：固定upstream基线；复用原生层和测试环境；只补当前边界测试；保留原团队做真实恢复；不新增复杂业务任务、不重写调度；在安全停止及绑定释放后才更新池。
- C盘Docker清理及压缩已完成：数据盘100.44GiB降至40.89GiB，释放约59.55GiB。保留现用/回退镜像及测试数据，Docker和三个测试容器已恢复。
