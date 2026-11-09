# 团队版 Team 完整备份功能最终开发与验收计划

> 日期：2026-10-23；修订：final-plan.1；状态：部分组件已实现并验证，完整功能尚未验收。
> 主分支：`codex/team-personal-integration`。  
> 目标：保留 Team 数据、产物及恢复点状态；恢复后终止历史未完成任务，不自动续跑。  
> 本文是完整交付计划，不以菜单、占位页面、数据库 dump 或演示成功代替功能验收。正文 P0 范围内所有任务、测试及运维能力均为必交付项。  
> 执行更新（2026-10-23 20:09）：用户已授权按本计划连续实现与验收、分批 commit，并在 10.130.14.23 的 hxc-peer/32543 测试环境构建、推送和更新验证；原最长五小时，用户后续将截止时间延长到北京时间 2026-10-24 02:00。开发顺序仍用于依赖管理，不作为逐阶段停工确认点。正式环境始终只读。进度与未通过项目见 `team-backup-acceptance/20261023-execution.md`。

## 1. 本次基线与原方案评价

实施结果以 [实施记录](team-backup-acceptance/20261023-execution.md) 和 [剩余工作及逐项验收缺口](team-backup-acceptance/remaining-work.md) 为准。下文的原始基线描述保留用于追溯；原 T01～T13 / A01～A24 要求不因组件通过而缩减。

| 项目 | 目录 | 分支 | 读取时 HEAD | 工作区 |
| --- | --- | --- | --- | --- |
| 团队 ClawManager | `D:/test/ClawManager-2` | `codex/team-personal-integration` | `48d00c09deabf3569fd044eaab837c6c94138111` | 本轮开始仅本文未跟踪，其余工作区干净 |
| 团队 AgentsRuntime | `D:/test/AgentsRuntime` | `codex/team-personal-integration` | `770c469feafcc8169719075252bf9012c828bfcb` | 读取时干净 |

此前评估基于 ClawManager `da0b2cce` 加 Owner Team 未提交改动；现在这些能力已进入 `48d00c09`。本轮重新核对了导航、路由、migration 位置、Owner 创建完成与 runtime readiness、worker 延期接管和 retry 入口。特别要覆盖“成员已创建但等待运行就绪”的操作：ready 检查成功后可能派发首个 leader 任务，不能仅隔离失败重试。AgentsRuntime 基线未变。Git 提交存在不代表镜像已推送或实际已部署，本轮没有核验后二者。

此前两份 `system-backup-restore-division-plan.md` / `system-backup-restore-execution-readable-plan.md` 的 v12 方案没有要求恢复后重跑任务，其隔离演练、归一化和防重放方向正确。个人版专项方案的数据保留原则继续有效，但八表清单、维护能力判断和防重放边界必须更新。

系统主计划应补：团队新表及 owner 归属、Owner 操作账本及创建残留、文件编辑审计、升级/生命周期接管、恢复后旧操作不可重试规则。易读版仅补范围和导航，不复制公共状态合同。

原文明确已有 `/admin/backups`、配置、备份、演练、preflight/diff、prune；没有指定菜单排序。本次由用户明确要求在截图所示管理后台左侧增加“备份”，具体落位为“控制台”下一项、与“用户/实例/运行时”等同级；这项菜单位置是本次产品决定。保留中断前状态证据、将未完成任务收尾为失败，以及当前 Owner Team 的数据依赖是本专项的补充，不冒称原文已逐项规定。

权威输入固定为两份原文件的 v12（2026-09-09）：主计划 `system-backup-restore-division-plan.md` SHA-256=`A0FF7273A158A936B51F53B9552B659573D53BD4EE1AE1B86D39C863FD58912D`；易读版 `system-backup-restore-execution-readable-plan.md` SHA-256=`37FFEE77EFAA7E39007DA5C26C479A250774EBAAC6BC388538FC3D046A69E1B6`。读取位置均为 `C:/Users/hanxingchen/Documents/WeChat Files/a970301135/FileStorage/File/2026-09/`。实施 T02 将原文快照、来源及 hash 纳入版本管理，便于其他开发者复核；不在源文件上改写历史规范。

本文是 Team 专项实现输入，复用公共 artifact、加密、catalog、任务、权限和恢复隔离合同；不另外创造第二套系统备份。v12 的 P0 仍是隔离演练及只读 preflight；生产覆盖恢复仍需 P1 的独立控制日志和另行授权。

## 2. 团队版已有能力与新增缺口

| 当前代码证据 | 可以复用 | 不能据此宣称 |
| --- | --- | --- |
| `team_service.go` / `team_repository.go` | 任务、完成事务、账本、outbox、单成员/协作执行 | 跨 MySQL/Redis/文件的原子备份已存在 |
| `owner_team_api.go`、`owner_team_repository.go`、migration 077 | CM/northbound 隔离、provider/owner、幂等、异步创建与步骤记录 | 关闭 Owner 入口就会停止既有后台 |
| `workspace_upgrade_mutation.go` | 文件写入保护与升级互斥的接入位置 | 已覆盖所有备份写路径和所有副本 |
| `team_workspace_text.go`、migration 072 | 文本版本校验、Team 文件审计 | 只读请求绝无文件或审计副作用 |
| `SetRuntimeUpgradeMaintenance`、OpenClaw `maintenance_drain_v1` | 阻止部分派发、维护期间不释放延期任务 | 全部 Runtime 和共享卷已经停写 |
| `runtime_upgrade_service.go` 的本地 snapshot/recovery | 文件检查、停止观测、恢复错误处理的参考 | 升级快照可替代外部加密灾备 |
| 九节点 MySQL full-backup / restore-verification YAML | dump 及校验实践 | Team Redis、产物和凭据已同步备份 |

在所检查的应用源码中未发现完整的 system-backup controller、Team collector/normalizer/verifier。不能把 models/backup.go 或生产备份 YAML 当成已交付系统备份。

OpenClaw 维护期间仍调用 writeLocalStatus 和 HSET presence；在途工具也不因维护键自动停止。Hermes 当前正式 adapter 未发现对应 maintenance/quiesce 接口。备份必须补完整停写证明，不能只设置 Redis maintenance=true。

## 3. 产品范围与实施选择

第一版做安装级系统备份中的 Team 数据域，覆盖 CM Team 与 Owner Team。暂不做“把单个 Team 导入已有安装并重新分配 ID”，避免引入 owner 合并、ID 重写、Redis 前缀和产物路径迁移问题。

恢复目标为隔离数据副本，保留 ID 和原归属。已完成任务保持完成；未完成任务可变为现有 failed；原状态、部分结果、工作项和产物可追溯。只承诺恢复点已持久化的数据，不承诺备份之后或内存中的最新内容。

第一版优先评估有界维护窗口。维护暂停需要独立协调器，源环境停写期间不删除或重建业务实例、不旋转原凭据、不取消用户任务来获得空闲。无法在允许时间内安全排空时退出备份并恢复源服务，不强制杀进程后宣称强一致。

OpenClaw 和 Hermes 的 writer 排空/停止确认、generation ACK、超时恢复均为本版必交付。可以复用经验证的进程生命周期能力，不必重写任务执行引擎，但不能把 Runtime 停写适配留作后续可选项。长任务不能在窗口内安全排空时，本次备份失败并恢复源服务，不取消业务任务来制造成功。

正式 P0 支持矩阵固定为 Kubernetes/k3s × cluster/single-node、embedded 数据源。OpenClaw/Hermes 及 Lite/Pro 在受支持存储组合中均须有覆盖；实际卷能力不足则给出明确拒绝，不默认为支持。Docker Compose、bare-metal、legacy-nfs、external datasource、定时备份、自动 retention prune、单 Team 合并导入及生产覆盖恢复是明确的 P1/本版外范围，不列入本版完成项。实现拒绝/提示这些范围的行为本身属于 P0 验收。

本版“完整”指：管理员从入口完成配置和手动备份，生成离开源集群仍可恢复的加密数据，隔离恢复并核验 Team 历史/产物/状态，查看证据，并安全保留或清理备份。它不意味着把恢复副本投入生产继续服务。

## 4. 数据范围与所有权清单

### 4.1 Team 主体与历史

原八表全部保留：teams、team_members、team_tasks、team_events、team_work_items、team_event_outbox、team_workflow_phases、custom_team_templates。

新增 `team_workspace_file_audits`，保留成员 icon、Team display_name、单成员执行上下文等当前 schema 字段，不手工只导出旧字段 DTO。备份应依据数据库 schema 和冻结 registry，并使用一致事务快照。

业务查询通常过滤 deleted 或按用户分页；collector 不能直接复用列表接口来枚举全部历史。正常 API 隐藏的控制事件也应加密保留，但不能作为普通 owner 聊天内容泄露。

### 4.2 Owner Team 依赖

| 内容 | 捕获与恢复政策 |
| --- | --- |
| teams/templates 的 resource_scope、owner、user_id | 原样保存；provider user_id 不是最终 owner，禁止通过首个实例推断 |
| display_name 与内部 name | 分别保留；改显示名不等于改目录或实例身份 |
| northbound_operations | registry 归 resources 业务域，由 Team verifier 通过 depends_on 引用相关 create/delete/member/template 操作；保留 request hash、幂等键、冻结请求、结果与原状态；不新增第七个业务域 |
| owner_team_provision_steps | 纳入 Team 依赖，保存原步骤和 operation 关联；恢复后不得自动续建 |
| owner_team_admission_locks | 表结构保留，恢复不复制“仍持锁”的语义；按当前代码可重建空锁行，不用它代替系统备份 gate |
| northbound_owner_records、配置/策略 | registry 归 resources 域保存，敏感认证字段与 users/Secret 策略关联；验证 owner 归属，旧外联及自动创建能力恢复为禁用 |
| iei_session_revocations / 会话与认证状态 | 归认证域明确策略；演练不能恢复有效旧会话，不能仅清空撤销记录却继续接受旧签名会话 |
| 部分创建实例及 PVC/Secret | 依据稳定 provisioning operation ID、步骤及实际 inventory 纳入；不能只从已有 team_members 反查 |

同一 provider 下不同 owner、不同 provider 下同名 owner、CM Team 与 northbound Team 均必须隔离。077 已进入当前提交；开发时仍重新核对最终 schema，不重写已应用迁移，不抢占其他开发者编号。Registry 为每张表指定唯一 owner/category，不能同一表重复捕获，也不能因为本文只讨论 Team 而漏掉其他五类必选业务 verifier。

### 4.3 可自动接管的状态

关联的 runtime_rollouts、runtime_upgrade_items/audits/checkpoints、实例生命周期 batches/items/attempts、instance_commands、desired state、runtime bindings 和 browser workers 交由实例域捕获。

这些是升级/生命周期历史，不是 system-backup 控制表，不能统一丢掉；恢复副本必须阻断旧接管和删除动作。单独把 Team 根任务设为 failed 不足以阻止其他 worker。

### 4.4 Redis、文件和凭据

- Redis：inbox/events/presence/DLQ、processed、completion/attempt/ack/state、root state、dispatch/deferred/activity/turn-facts；加上团队版 maintenance 等新增键。枚举实际连接身份、DB、显式 key override；按代码生成/校验键族清单。
- 文件：Team 共享根、各成员私有 workspace、OpenClaw 私有 active-assignment、Hermes 隐藏状态、任务、结果、用户编辑文件、产物与配置。不因 tmp 或隐藏目录命名而自动排除。
- 凭据：K8s Secret、实例环境 JSON、磁盘配置、旧 agent/Team/Gateway token 等，全部按敏感 registry 处理；清单只含受控身份引用。
- 存储：以真实 PV/卷/相对路径去重；保存受管链接映射与权限。NFS/HostPath/CSI 逐一探测，不能仅看 cluster profile；v12 不支持的路径不得冒充正式资格。

## 5. 捕获与维护协调的实现

新增通用 maintenance coordinator，数据库持久化 ownership、generation、阶段、截止时间和参与者结果；不要把备份任务 ID 假装成 rolloutId 直接调用升级维护方法。

第一版备份与升级、恢复、Team 创建/删除、实例重置、批量生命周期互斥。先在数据库准入处实现双向竞争保护，再驱动现有组件暂停。只在备份预检读取一次“没有升级”存在竞态，不能作为实现。

接入点：

1. CM Team handler/service、Owner Team API 与 IEI/北向入口都经过 gate；service 层也保护，避免漏掉后台调用。
2. Owner `submit/execute`、`ClaimNextOperation`、显式 retry 与 provision steps 阻止新动作，等待已派出的外部动作结果。过期 lease 不等于旧写入停止。
3. Team 消费投影、outbox、监督/完成恢复提醒暂停；保留原运行状态以便在源端正常恢复。
4. workspace 通用 mutation guard 组合 backup guard 和 upgrade guard，保留原版本冲突保护；检查 Team 文本编辑、上传、审计写入以及读取时目录准备。
5. RuntimeScheduler、升级协调器、生命周期 worker 与 runtime writers 全部列入参与者。所有 App 副本确认，不只暂停 leader。
6. OpenClaw drain 先阻止新消费，再等待工具/子进程/在途回调收尾，停止 presence 等写入。Hermes 需补等价能力或经过验证的完整进程生命周期适配器。

写入完全停止后建立 MySQL transaction、Redis 不可变视图和卷/目录捕获。能建立独立 checkpoint 的路径按 v12 提前恢复源服务；无隔离视图必须在停写预算内完成 staging。

维护原始运行状态和用于运行态展示的字段可能因停写发生变化。保存“进入维护前状态”与“实际 capture 状态”两个有时间的记录：前者用于说明进度，后者用于证明一致性，不混为同一瞬间。若期间有任务合法完成，以捕获时完成事实为准。

超时、取消、换主及故障都必须恢复源服务；恢复失败保留事实并告警。任何 forced resume 使相关捕获失去强一致资格。释放维护时核对 owner/generation，不误删升级方原有维护键。

## 6. 备份产物与完整流程

1. 固化代码版本、工作区构建标识、迁移 hash、schema hash、owner 范围、源资源清单和实际镜像身份。
2. 预检容量、停写时长、存储/Redis 能力、加密密钥与外部目标；失败不进入停写。
3. 准备 Job/只读源访问及加密 staging，再取得维护 gate 并排空所有 writer。
4. 捕获全部必选系统 part；Team 提供范围和验证结果，避免重复导出 users/instances 等共享数据。
5. 验证 checkpoint、完成允许的 resume、seal 加密数据；保留原始数据库字段与文件校验。
6. Team verifier 验证跨域关系、scope、产物和捕获事实；缺原有产物如实记录，不能在源端修复后掩盖。
7. 使用公共 manifest/index/marker、签名和 catalog 发布流程；清理按系统 inventory 完成。

不能原样包装九节点旧 MySQL dump 为 v12 artifact：其 all-databases 与磁盘 gzip 路径需要按新方案调整成业务 allowlist、控制数据分离和流式加密。不要修改现有生产 CronJob 来实现首版；在独立捕获路径验证后再评估替换。

## 7. 恢复、归一化与防重放

### 7.1 隔离恢复

校验 artifact、兼容性、密钥和目标身份，创建专用 namespace/数据库/Redis/卷，导入数据前禁止 worker 和模型外联。恢复后不自动 apply 旧 Deployment/Secret 或连接旧 Runtime。

原 artifact 永不修改。归一化仅修改恢复副本，先持久化 before/after 记录，再按幂等策略提交；记录必须长期可查询，不能依赖短期日志。敏感字段采用加密证据或受控摘要。

### 7.2 业务数据处理

| 对象 | 目标行为 |
| --- | --- |
| succeeded/failed 任务 | 保留已确定终态、原原因、结果及完成时间 |
| pending/dispatched/running/waiting | 原状态留证后转 failed，workflow 同步收尾，注明“恢复终止，未自动重试” |
| stale/其他状态 | 在规则冻结时逐个映射，未知状态失败，不猜测 |
| 子任务/阶段 | 已完成、审核通过和 superseded 等历史保留；未完成项收尾，部分成果和产物保留 |
| plan/ledger/revision/accepted completion | 不清零，不伪造成功，不调用普通完成投影来收尾 |
| 成员/绑定 | 身份和历史关联保留，当前执行指针和源运行绑定禁用 |
| 文件/active state | 产物恢复原逻辑位置；旧活动信封隔离归档，避免再次被 Runtime 加载 |

finished_at 若因恢复填入，明确它是恢复收尾时间。provider、owner 和 scope 不做自动映射；不允许将 Owner Team 退化为 CM Team 绕过访问控制。

### 7.3 旧操作不能因服务启动或 retry 复活

旧 Redis 消息只用于隔离验证/历史读取，不连接新业务消费者。outbox 原 payload、状态留证后变为不可调度；不能把未发送记录标成 delivered。

使用恢复控制模块的持久隔离登记，不给每张 Team 表增加恢复枚举：登记恢复 run、实体类型/ID、源恢复点、原因与原状态引用。实体包括 Team、outbox、northbound operation 和关联升级/生命周期记录。登记与 before/after 证据属于本次演练控制面，以目标身份绑定；不从源库复制旧控制记录，不在恢复目标的 system_backup 控制表中造数据，保持 v12 的目标控制表为空约束。

恢复上下文中的后台 claim、显式 retry 和恢复入口都要检查隔离登记。尤其 `OwnerTeamRepository.RetryOperation` 当前允许 failed→queued；只把旧操作改 failed 无法满足零重跑。`finishOwnerTeamProvision` 和 `DeferOwnerTeamReadiness` 的后续接管也必须覆盖。登记必须在恢复副本仍隔离时建立，查登记失败必须拒绝执行；正常源安装没有恢复上下文时不能因演练登记影响源业务。P0 不启动完整业务服务，用隔离的 worker/repository 测试夹具证明拒绝路径；未来生产恢复必须由 P1 独立 control journal 持久提供此上下文，不能移除保护直接启动副本。

保留 operation ID、幂等键和冻结请求，重复原请求返回原记录及恢复说明，不能因丢失幂等账本重新创建或再次产生模型费用。完成结果不明确的外部操作保留 unknown 的事实，不推断成功、失败或再次执行。

P0 的 isolated drill 只验证该策略，不要求启动完整 Team。未来若要恢复生产并允许新任务，必须单独完成全入口防重放及新身份重建验收；不得通过一键“解除全部隔离”复活旧工作。

## 8. 代码组织与最小业务接入

以下为待新增路径，实施前与公共系统备份代码约定统一目录，避免平行建设：

| 模块/文件落位 | 职责 |
| --- | --- |
| `backend/internal/systembackup/team/inventory.go` | 全量 Team/scope/依赖/部分创建资源清单 |
| `backend/internal/systembackup/team/registry.go` | 字段、Redis 键、目录、敏感配置与版本策略 |
| `backend/internal/systembackup/team/collector.go` | 参与公共捕获，输出 Team 统计和证据引用 |
| `backend/internal/systembackup/team/normalizer.go` | 状态收尾、凭据禁用和活动文件归档 |
| `backend/internal/systembackup/team/verifier.go` | 独立只读验证，不依赖 TeamService 构造/启动 |
| `backend/internal/systembackup/maintenance/` | gate、ownership、参与者、持久恢复与超时 |
| `backend/internal/systembackup/restore/` | before/after、隔离登记、幂等归一化 |
| `backend/cmd/system-backup-controller/` | 公共独立控制器，若公共项目已有实现则复用 |
| `frontend/src/components/AdminLayout.tsx` | “控制台”下一项增加“备份”，复用展开/折叠及高亮规则 |
| `frontend/src/router/index.tsx` | 注册 `/admin/backups` 及详情路由，使用 AdminRoute |
| `frontend/src/pages/admin/backups/` | 配置、总览、备份、演练、差异、目录、清理与证据页面 |
| `frontend/src/services/systemBackupService.ts` | 从冻结合同生成/校验 DTO，封装真实 Admin API |
| `frontend/src/lib/i18n.ts` | 现有五种语言的菜单、状态、错误、操作和可访问性文案 |
| `backend/internal/db/migrations/` | 公共控制面及必要 registry migration，使用实施时可用编号 |

模型和 migration 仅添加公共备份控制记录、恢复证据及隔离登记等必需结构；名称/枚举随公共合同冻结。迁移号从实施时目录分配，不能假定 078 可用。原八表原则上不加恢复专用字段。

Runtime 适配放在 `plugins/openclaw-redis-team`、`plugins/hermes-redis-team` 与 `clawmanager-agent` 生命周期/控制模块。先完成能力 probe，再决定复用完整停止还是补 cooperative pause；不得仅修改提示词声称已停写。

## 9. 可执行开发任务与完成门槛

| 编号 | 工作内容与主要文件 | 依赖 | 完成标准 |
| --- | --- | --- | --- |
| T01 | 核对 072/077、模型、Owner repository/readiness、Redis/存储清单；落盘六域 registry 与 Team fixture | 无 | CM、两个 owner、不同 provider、半创建及等待就绪 Team 均覆盖；未知字段/对象产生门禁失败 |
| T02 | 冻结 v12 公共机器合同及 Team capture/normalization/verify、scope 与敏感字段策略 | T01 | OpenAPI、Schema、Go/前端/报告 parity；各对象 preserve/reset/disable 明确；原文规则有测试映射 |
| T03 | 公共任务/独立 controller/gate/证据和隔离登记 migration/repository；配置/权限/审计基础 | T02 | CAS、重启接管、截止时间、幂等、强认证、权限边界及数据库集成测试通过 |
| T04 | 接入 CM/Owner/IEI/API、claim、retry、workspace/upgrade/lifecycle 准入 | T03 | 多副本新写入与 gate 竞争只有一方获准；锁顺序统一，无跨 scope 绕过 |
| T05 | 复用 OpenClaw drain，完成 Hermes/工具/子进程及 presence 停止与 resume 适配 | T03,T04 | 两类 Runtime 的写入确实收尾；旧 generation ACK 不可用；超时/崩溃可恢复源服务 |
| T06 | 存储/Redis/MySQL capability probe 与加密 capture adapter | T01,T03 | 支持形态清楚；实际不可变 checkpoint 可证明；无明文 staging；不支持则明确拒绝 |
| T07 | 六类 collector 与 Team 适配接入 artifact/index/certificate/catalog；外部发现与导入 | T05,T06及公共发布能力 | DB/Redis/共享/私有文件统一恢复点；校验、发布、清理失败不会误标正式成功；脱离源安装可发现和读取 |
| T08 | normalizer 与 before/after、旧执行隔离、防 retry；fixture 可先开发 | T02,T03 | 终止未完成任务但保存成果；多次运行不覆盖首次原值；旧 operation 不能复活 |
| T09 | SELECT-only verifier、只读 Redis/文件与 owner 关联验证 | T02；集成依赖T07,T08 | 权限实际拒绝写入；坏文件、越权、漏记录、旧执行入口均能检出 |
| T10 | 左侧“备份”入口及完整管理界面/API：第13节全部操作、权限、报告与异常处理 | T02,T03；真实联调依赖T07～T09,T12,T13 | 实际从页面完成全流程，无 mock 成功、空按钮或占位功能；普通 CM/Owner 无权访问全安装数据 |
| T11 | 完整隔离演练、故障注入、四部署矩阵与证据归档 | T07～T10,T12,T13 | 第10、16、17节全部通过；无跳过或待补证据；运行结果与发布状态分开记录 |
| T12 | artifact health/full verify、手动保留与 prune、catalog tombstone、资源 cleanup 和控制/证据 GC | T03,T07；演练引用保护依赖T08,T09 | 活动/保底恢复点不误删；失败重试可接管；未知 ownership 不删；历史资格不漂移 |
| T13 | preflight/diff、当前矩阵 promotion、指标告警及灾备 bootstrap/runbook | T03,T07～T09,T12 | 只读目标零写入；基线资格条件完整；可离开源库发现、验签、解密；监控故障可触发和恢复 |

公共系统备份能力若尚未实现，T03/T06/T07 的通用部分是实质工程依赖，不能按“只新增三个 Team 文件”估算工作量。T08/T09 可提前用 fixture 开发，但不能跳过公共门禁发布。

实施批次固定为：T01/T02 合同；T03 控制基础；T04/T05 写入协调；T06/T07 捕获发布；T08/T09 恢复验证；T12/T13 生命周期与可运维能力；T10 全功能联调；T11 总验收。T10 可在 T02 后开发界面，但以真实联调通过为完成。每批独立审查，功能默认关闭；部署/推送不因计划存在而自动获授权。T01～T13 没有“可选实现”项。

## 10. 具体测试清单

| 测试组 | 关键用例与断言 |
| --- | --- |
| 归属与枚举 | CM + provider A/owner X/Y + provider B/owner X；deleted 历史、空页、模板无 Team、创建残留不漏；恢复后无跨 owner 数据访问 |
| 任务历史 | 根运行中但子任务完成、单成员任务、待审核/不同 revision、部分产物、已失败任务；完成事实不被根收尾抹掉 |
| 消息窗口 | MySQL 已完成但 ACK 未写；XADD 成功但派发 DB 更新未完成；outbox 重复投递风险；恢复不靠重放修补 |
| Owner 操作 | queued/processing/过期 lease、创建 Secret/PVC 后未记步骤、模型付费结果未知；启动/retry/原幂等请求均不重复执行 |
| 升级并发 | 同时备份与升级/回滚/实例重置/批量删除；准入事务互斥，不只做一次预检；不释放他人 maintenance |
| Runtime | OpenClaw 维护时仍有 presence、Hermes 长工具、共享 Lite Pod、已阻塞 XREADGROUP、子进程写共享卷；全停写后才 capture |
| 文件 | 编辑版本冲突、文件审计、合法 Team 链接、恶意链接、外部 NFS/无 CSI、保存中断和权限；正确归档或明确失败 |
| 恢复安全 | 旧 session、源 Redis URL、环境 JSON token、active-assignment、provision/upgrade claim 全部不可激活 |
| 恢复幂等 | 归一化前/中/后崩溃；原值只记一次，恢复收尾不新增普通业务 outbox；重复执行结果一致 |
| 权限与完整性 | verifier 无 DML/DDL；无模型/源网络外联；checksum 错、缺文件、失效密钥明确失败；保留范围内无静默丢失 |

优先复用 `owner_team_mysql_test.go`、`owner_team_engine_mysql_test.go`、`owner_team_auth_test.go`、`team_recovery_mysql_test.go`、workspace 和 runtime upgrade 的现有测试夹具；测试补在新模块，避免同时大改正在开发的现有用例。真实 MySQL/Redis 集成只使用明确隔离的测试目标，不能默认连接部署实例。

新增 Runtime pause 测试覆盖 OpenClaw/Hermes，而非仅跑静态字符串检查。最后运行适用后端测试、前端构建/相关 E2E；本轮只写计划，不将历史日志当作本方案已通过的证据。所有 bug 修复先复现并记录根因，补能失败的回归测试，再做最小修改并执行回归验收。

## 11. 与当前并行开发的协作方式

- 保留所有已有改动及未跟踪文件；本轮只更新本文，不创建占位功能充当交付。
- 实施前重新记录 HEAD/status、关键源文件 hash 和最终 schema；出现新未提交变更时，HEAD 不能单独代表实际构建内容。
- 优先新增独立备份包，现有 main.go/team_service/owner API 只做小型依赖注入与 guard 接口接入，并协调修改时机。
- 不在开发中的 checkout 自动 checkout/reset/stash/clean，不把未完成改动复制成一个未经核验的发布基线。
- Runtime 与 App 定义能力协商和兼容矩阵；旧 Runtime 缺少停写证明时拒绝正式捕获，不强制升级镜像来掩盖缺口。
- 首版上线默认禁用；服务端按 v12 拒绝新的执行任务，不能仅隐藏按钮；查询、配置、取消、清理及规定的灾备发现/安全收尾例外继续可用，活动备份不得被开关直接打断。

## 12. 实施决策

本版本适合做“公共系统备份能力 + Team/Owner Team 数据适配 + 恢复隔离”。最重要的新增代码是统一维护协调、全量数据清单、恢复原值记录和防旧操作接管。

无需增加任务续跑协议或重写正常 Team 工作流；但不能以“不续跑”为理由忽略旧 outbox、Owner retry、升级恢复及生命周期 worker。完成标准是数据/产物完整、原状态可追溯、scope 不变、旧执行入口全部关闭，并满足公共系统备份资格门禁。

## 13. 前端入口与最终交互合同（T10 必须全部实现）

### 13.1 导航位置与权限

管理后台左侧菜单最终顺序为：控制台 → **备份** → 用户 → 实例 → 运行时 → 北向接口 → 安全防护 → AI 网关 → 设置。备份使用独立归档/数据库图标，点击进入 `/admin/backups`；不是控制台首页中的临时链接，也不是 Team 用户页面中的按钮。

复用现有 AdminLayout 的样式、折叠、悬浮提示、键盘焦点和选中逻辑。访问备份详情/演练等子路由时仍高亮“备份”；控制台 `/admin` 继续精确匹配。前端复用 AdminRoute，但服务端必须独立执行 v12 的 read/operate/configure/security_configure/destructive 权限检查，不能仅凭 role=admin 放行全部动作。

页面标题为“系统备份”，说明 Team 数据属于安装级备份。本版创建流程不提供“只勾选 Team 即得到正式灾备资格”的选项，不在普通用户或 Owner 门户暴露全安装数据。现有五种语言、窄屏/折叠侧栏、刷新直达、浏览器返回均验收。

### 13.2 必须完成的页面与操作

以下是页面职责划分，子页面可用标签页或详情路由实现；不新增另一套后台状态枚举。所有写操作使用真实 API，错误来自服务端结构化结果。

| 页面 | 必须提供的内容和操作 | 通过标准 |
| --- | --- | --- |
| 总览 `/admin/backups` | 功能启用状态、依赖就绪、最近备份、最近合格恢复点、恢复验证、当前矩阵基线、容量/告警；进入配置、创建备份、目录发现 | 无备份显示“尚无恢复点”，不能显示绿色健康；关闭功能说明原因并允许查询/配置/安全收尾 |
| 配置 | 外部存储目标及凭据引用、KEK/签名/catalog/evidence 引用、演练目标、停写/容量/期限、保留数量和时间、权限及告警配置；历史版本和回滚 | ref 不回显密钥正文；版本冲突可处理；安全变更强认证；配置保存不伪称完成恢复验证；引用版本不得静默替换 |
| 创建备份 | 展示六域范围、Team/Owner Team 依赖、源部署形态、预期停写上限、目的与配置版本，提交后进入详情 | 双击/网络重试只创建一个任务；不支持形态明确拒绝；默认正式验收目的，功能测试显著区分 |
| 备份列表/详情 | 状态、恢复点时间、一致性等级、六 part/六域结果、Team 数量/范围、数据大小、manifest、校验摘要、事件/脱敏日志、失败原因和证据；取消、符合条件的新任务重试 | 页面刷新继续原任务；不能把 queued/上传结束显示为备份成功；取消由 can_cancel 决定；备份任务重试与 Team 业务任务重跑明确区分 |
| 备份后续操作 | 完整校验、创建隔离恢复演练、手动保留/解除保留、暂存清理、必要的归属/外部提交结果处置 | 动作由服务端状态和权限决定；未知结果不能“强制成功”；proof 走已登记证据，禁止自由路径覆盖 |
| 演练列表/详情 | 选择合法源及隔离目标、创建演练、恢复步骤、资源清单、六域验证、取消、清理；查看 Team 状态变化与产物校验 | 明确“隔离数据恢复”，没有“恢复生产/继续原任务”按钮；完成与 cleanup 状态分别显示；失败也能查看证据 |
| Team 恢复报告 | Team/owner/scope、原任务状态、捕获状态、恢复后状态、收尾原因、子任务/阶段保留情况、文件数量/字节/hash及缺失差异；受控证据查看/下载 | 完成子任务仍为完成；未完成任务原状态可追溯；敏感事件、token 和原始 URI 不泄露；不是伪造原 Team 的聊天事件 |
| 灾备目录 | 发起完整扫描、进度/receipt、registration 列表、导入、签名/密钥/读取健康、导入后完整验证与演练 | 不依赖源 backup 数据库 ID；扫描不完整不得显示“发现全部”；不可用原因可定位 |
| 只读预检查与差异 | 选择源和既有只读目标，展示兼容性、目标差异、风险及报告 | API/页面不提供执行覆盖；只读权限可验证；报告过期/源健康变化明确标识 |
| 保留与清理 | dry-run 候选、受保护原因、数量/字节/hash/过期时间；强认证确认、执行、逐项结果及失败重试 | 预览不删除；变更候选后旧确认无效；只重试未成功项目；剩余恢复点不被误删 |
| 恢复基线与异常处置 | 创建/撤销当前矩阵 promotion；呈现演练、清理、health 不满足的条件；提供有权限的证据/ownership 处置入口 | 只有全部资格满足才可建立基线；不能手动勾选“已验证”；无权限时服务端也拒绝 |

轮询按 v12：默认 5 秒，连续 5 次无变化退避为 15 秒，尊重 Retry-After 并加入抖动；普通任务终态停止，演练须等 cleanup settled，日志过期停止日志轮询。组件卸载停止页面请求但不取消后台任务。错误、空数据、403、409、依赖不可用、长时间无进展和重新进入页面都必须有处理。

### 13.3 页面到后台的真实操作链

管理员配置并启用 → 创建备份 → API 鉴权/校验/幂等落库 → controller 准备与预检 → gate 排空 writer → 固定一致视图/加密暂存 → 按规则恢复源服务 → 封存/校验/独立发布 → 清理 staging → 签名验收及外部目录登记 → 页面展示执行及资格。

随后管理员另行选择备份 → 创建隔离演练 → 校验源与隔离边界 → 创建临时资源 → 恢复 MySQL/object/workspace/Redis → Secret、运行态和控制面归一化 → 销毁恢复写凭据 → 独立只读 verifier → 持久化报告 → 按清理政策安全清理 → 满足条件时创建当前安装恢复基线。

source backup 是保留原始状态的不可变数据；normalize 只作用于恢复副本。源服务在采集后的 resume 与恢复副本禁止历史任务重跑是两件事，页面文案不得混淆。

## 14. 完整功能不可省略的公共依赖

以下能力全部归入 T01～T13，不允许因“只做 Team”而省略。复用已有实现必须提交覆盖证据；找不到可用实现就必须开发，不允许写成假定存在的外部前提。

| 公共交付 | 实现要求 | 责任任务与原文对应 |
| --- | --- | --- |
| 机器合同与 schema registry | 同版本 OpenAPI/JSON Schema/enums、Go/TS parity、全部表/view/trigger/routine/event 归属；禁止构造器隐式 DDL；新增可执行字段未登记则失败 | T01/T02/T03；v12 §3、§5、§6 |
| 六域六 part | users/instances/ai_gateway/teams/resources/skill_hub 全覆盖；mysql/redis/object_storage/workspace/deployment_resources/secret_bundle 六必选 part；不把“Team 专项”变成漏依赖的局部 dump | T01/T06/T07/T09；§2、§10、§11 |
| 配置与身份 | append-only 配置版本、expected_version、权限分工、近期强认证、nonce 防重放、幂等账本、Secret typed ref 和历史版本保留 | T02/T03/T10；§6、§14 |
| 独立控制器 | leader lease + DB claim、任务/attempt CAS、截止时间、fencing、持久化 relay/日志/证据、kill switch、安全接管和收尾 | T03；§6～§8 |
| 真实权限隔离 | 移除官方 app cluster-admin 绑定；API 不能创建备份资源；capture/secret-capture/publish/restore/verifier 各自最小 SA/DB/网络权限；无运行时工具下载 | T03/T06/T08；§7、§12、§18 |
| 加密和不可变发布 | 内存中压缩/加密、无持久化明文；DEK/KEK 分离；用途独立签名；external-action 发送前冻结完整 bytes；conditional-create、结果未知接管 | T06/T07；§7、§9 |
| 存储 provider 能力 | 精确对象读写删除授权、权威存在/不存在证明、完整 catalog snapshot/inventory；不以普通 ListObjects/404 替代证明 | T06/T07/T12；§9、§13 |
| 外部灾备可恢复性 | 外部 artifact 与签名 catalog、灾后 bootstrap（读取身份/CA/历史 KEK/验证 key）、scan/import、完整解密验证；源库/源 API/源网络断开仍能验证 | T07/T13；§9、§12、§17 |
| 健康与清理 | 独立 artifact health/full verify、manual/promotion hold、最少恢复点、prune dry-run/confirmation/retry/tombstone；staging/resource/evidence/control GC | T12；§6、§12、§13 |
| 只读检查与恢复基线 | preflight/diff、目标只读凭据、当前 matrix promotion；四矩阵证据只用于发布汇总，不要求单安装伪造四套基线 | T09/T13；§4、§12、§18 |
| 观测与应急 | redacted audit/events/logs、引用证据完整性、容量余量、监控新鲜度、RPO/未知结果/停写/未恢复/清理失败告警、runbook | T03/T12/T13；§6、§15、§17 |

配置 min/default/max、所有公共状态/失败码、API 全清单、签名规范及各阶段资格以 v12 机器合同为准。T02 必须为原文 §17.1～§17.5、§18 建立逐条 requirement→代码→测试→证据映射，不能只用本文较短的 Team 用例表替代原文全部验收。

原文开发门禁保持顺序：P0-0a（capability spike/合同）→ P0-0b（迁移/权限/控制器）→ P0a（捕获发布）→ P0b（恢复验证）→ P0c（健康/清理/基线/完整验收）。后置阶段不得补交前置安全门禁。T06 的能力探测在 T01/T02 期间先做 spike，正式适配器按依赖完成。

## 15. 逐任务实施拆分与交付检查

本节进一步明确第 9 节每项如何实施。每个任务都必须交付代码、回归测试、接口/运行说明和本次执行证据。计划中的路径是落位约定，不代表文件已存在。

### T01：数据盘点与可验证样本

1. 从 embedded migrations 和实际隔离测试库 information_schema 比对所有对象；为北向、认证、升级、生命周期等新增表补唯一归属。
2. 从 Team 服务/Runtime 生成 Redis 键族、存储卷/共享与私有根、隐藏活动文件、敏感字段清单；保存代码版本及 registry hash。
3. 构造 CM、多个 provider/owner、不同 Runtime/mode、半创建/等待就绪、已删除历史、不同任务阶段的确定性 fixture；每个产物预先记录 hash。
4. 输出 inventory fixture 和基准计数；未知对象、越界路径、重复归档或漏 owner 关联必须被测试检出。对应 A01～A04。

### T02：冻结实现合同

1. 导入带 hash 的原文快照；落盘 v12 API、配置、任务、artifact、catalog、证据、状态及错误合同，并补 Team 策略 registry。
2. 明确原始保留字段、规范化字段、敏感字段、源控制面排除字段；已有每种 task/workflow/operation 状态必须有映射，不以默认分支吞掉未知状态。
3. 冻结维护参与者、generation/ACK、RestoreContext/隔离查询接口及 before/after evidence 格式，保持公共状态不分叉。
4. 自动化 Go/TS/parity 和覆盖门禁，生成原文 requirement 映射报告。对应 A01、A07、A12、A23。

### T03：控制面、配置与安全基础

1. 在实施时可用迁移编号建立任务、attempt、operation、资源 inventory、external-action、配置版本、catalog、evidence、依赖健康等 v12 全部控制结构；禁止 repository 构造建表。
2. 建立独立 controller、两级 claim、CAS/fencing、relay、取消/截止时间、重启接管、配置开关和按配置的容量准入。
3. 实现服务端权限、strong-auth/nonce、幂等、脱敏审计、只返回安全引用的配置 API；拆分 SA/DB/网络权限。
4. 验证 API/controller 重启不丢任务、同请求只产生一次动作、旧 leader 无权写入、服务端独立拒绝越权。对应 A05、A06、A15、A16、A23。

### T04：业务写路径接入

1. Team/Owner/IEI/service、workspace mutation/audit、outbox/消费者、北向 claim/provision/readiness、升级/生命周期/调度和 migration 都登记 writer。
2. 复用业务入口接入统一 gate/lease，不在 handler 外围包一层后漏掉 worker；锁顺序统一，提交业务写前复核 lease/generation。
3. 按相同准入机制处理升级与备份的双向竞争；对触发目录创建的“读取”路径拆出纯读或纳入保护。
4. 多 API/worker 副本、事务超时及异步外部动作全部排空；关闭 Owner 功能开关不能被当成后台已停止。对应 A05、A06、A17。

### T05：Runtime 完整排空与恢复

1. OpenClaw 暂停新消费及延迟派发，等待在途模型/工具/子进程与回调完成，冻结 presence/local status 等持久写入；补 Hermes 等价适配。
2. 共享 Lite Pod 按真实 writer/卷确定参与范围；Pro 卷和成员进程同样纳入。不能仅暂停 leader 或只看 HTTP 无请求。
3. ACK 绑定 generation、writer 身份和最后完成位置；只读状态报告通过控制通道，不能继续改业务 Redis/文件；实现 watchdog 和有主恢复。
4. 真实长工具、子进程、阻塞消费、断网/进程退出验证暂停和恢复，不能只用字符串存在测试。对应 A05、A06、A17。

### T06：源采集与加密适配器

1. 实测各矩阵的 MySQL 一致事务、Redis RDB/AOF或受限 shared 方式、CSI snapshot/HostPath staging、对象 version 能力、secret capture 与内存安全能力。
2. MySQL 使用业务 allowlist，排除源控制表 DDL/数据；源 schema、engine、GTID/DEFINER 等按 v12 固化。Redis 保留 type/stream/TTL 证据，正式验收采用 absolute expiry 语义；过期差异显式列出。
3. 文件包括共享/私有目录、权限与受管链接；拒绝穿越/非法链接/恶意 tar，按物理资源去重，文件和对象均校验真实 hash。
4. 分离普通与 Secret capture，流式压缩加密为不可变 staging；按 profile 的 checkpoint/seal 条件恢复源写入；任何必选 part 失败都不能正式成功。对应 A02～A08、A15。

### T07：collector、发布与灾备发现

1. 接入六域 collector，Team 统计通过 shared registry 引用依赖；不重复导出同一实例或目录。
2. 独立 publish Job 无源数据权限；controller 校验 candidate，冻结 external-action，再提交 manifest/marker；清理后签名 acceptance 并登记 catalog。
3. 实现提交响应丢失、未知结果、接管、重复写判等及冲突拒绝；数据库投影不能覆盖已确认外部登记事实。
4. 实现 catalog 完整扫描、可持久接管的 receipt、导入及历史 key/credential 精确解析；源库丢失后仍能找到备份。对应 A08、A09、A15、A18。

### T08：隔离恢复、归一化与旧执行阻断

1. 先校验源，再校验 planned/observed target identity；先记录资源 intent 后创建隔离 namespace/DB/Redis/卷/prefix，禁止生产外联与旧 runtime 启动。
2. 按 v12 顺序导入数据，逐实体保存原状态/原因/时间/结果证据后幂等收尾。旧 outbox、ready/provision、active-assignment、绑定和外联秘密不可执行。
3. 完成结果和已完成子任务原样保留；未完成任务使用现有失败状态并区分恢复收尾时间，保留原始错误/部分结果，不调用普通完成服务制造新 outbox。
4. 恢复隔离记录放在本次控制面；使用明确恢复上下文接入 claim/retry/readiness 等拒绝逻辑。测试目标与源身份不匹配必须拒绝，绝不能误阻断源 Team。
5. before/after 写入失败、normalizer 中途崩溃、再次执行都不能丢第一次原值；恢复写凭据销毁后才进入 verifier。对应 A10～A14、A17。

### T09：独立只读校验与报告

1. 新 Job/SA 与新只读连接执行六域及控制面 verifier，禁止引用会建表或启动 worker 的业务构造器。
2. 验证记录数量、主外键关联、owner/scope、ledger/revision、任务与阶段、文件内容/权限/链接、Redis 类型及时间点允许差异。
3. 验证未完成项已收尾且原状态证据存在；源控制表行为空、敏感凭据禁用、隔离账本完整；恢复修改仅允许 registry 白名单内的变化。
4. 输出结构化差异和证据摘要，前端按权限读取；超过界限不静默截断关键验证证据，日志不能充当权威报告。对应 A10～A14、A23。

### T10：完整管理前端与 API 联调

1. 在 AdminLayout 增加确定位置的“备份”，实现第 13 节所有页面和状态；读取合同生成 DTO，统一权限/错误显示。
2. 对接 v12 §14.1 全部 P0 API，包含目录、清理、处置及基线入口；不新增生产恢复、定时或 agent 占位 API。
3. 无配置、依赖未就绪、关闭开关、成功、失败、取消、未知结果、清理未完成均使用真实状态；不展示测试假数据冒充备份记录。
4. Playwright 从管理员入口完成配置→备份→演练→报告→清理，验证普通用户/Owner/API越权、刷新/关闭页面、深链接、折叠及多语言。对应 A19、A20、A23。

### T11：总集成与发布验收

1. 执行下节完整用例、v12 §17 全部门禁，以及 §18 发布标准；四套独立安装分别形成 eligible backup、eligible drill 与当前矩阵 promotion。
2. 执行固定大数据集和声明容量上限代表性测试，记录时间、停写、CPU/内存、bytes/items、资源与清理，不以空库代替。
3. 回归原 Team 正常派发、完成判定、Owner 创建/重试/隔离、文件编辑、升级与生命周期，证明备份关闭及开启但空闲时业务行为正常。
4. 验收报告绑定源代码、Runtime 镜像、合同和 fixture 版本；未执行或 skip 均不算通过。对应全部 A01～A24。

### T12：保留、健康与安全清理

1. 完成只读轻量 health 与 full verify，临时依赖错误不误判永久损坏；相同 immutable version 通过完整校验后按规则恢复可用状态，不改历史资格。
2. 完成保底恢复点、manual/promotion hold、lease 保护，dry-run→confirmation→execute→item retry→catalog tombstone 全链路。
3. 完成 staging、演练资源、orphan、控制元数据和 evidence 的独立 inventory/GC；未知 ownership 停止自动删除，凭证销毁和含明文卷回收可验证。
4. 删除部分成功、catalog 不可用、控制器退出、记录与资源不一致均可接管且告警，不留下假健康记录。对应 A15、A18、A21、A22。

### T13：预检查、基线与运维

1. 实现只读 production preflight/diff，验证 source acceptance-ready、目标权限和预算；报告不执行覆盖、不重建实例。
2. 实现本安装当前矩阵 promotion、source/drill/evidence/cleanup 健康联动及撤销，保持历史资格不可变。
3. 实现 v12 指标/告警、启用历史、RPO/恢复点不足、停写/强制恢复、目录/签名/密钥、容量/清理/证据新鲜度监控，包含触发和恢复测试。
4. 交付部署配置、bootstrap/key rotation、灾备目录导入、失败处置、安全清理及关闭功能的 runbook，明确离线保管与访问身份。对应 A09、A18、A21～A24。

## 16. 必须实际执行的验收矩阵

状态初始全部为“未执行”。以下列出本专项核心验收；v12 §17 的完整映射同样必须通过。API mock 和 fake provider 可用于单元测试，不能替代真实存储/权限/四矩阵集成证据。

| 编号 | 场景与实际执行方式 | 必须观察到的通过结果 |
| --- | --- | --- |
| A01 | 对最终 migrations/隔离数据库/Runtime 键族运行 registry coverage；加入未知对象/状态负例 | 正常零漏项；未知对象、可执行字段或状态阻断资格；唯一 owner 与依赖可追溯 |
| A02 | 完整 fixture 备份并逐对象核对 | 六域/六 part 完整，历史/软删/模板/半创建均不漏；统计与独立基准相符 |
| A03 | 同 provider 多 owner、跨 provider 同名 owner、CM 与 Owner 混合恢复 | ID/scope/归属不变，无越权可见或文件混用 |
| A04 | 共享/私有/隐藏文件、链接/权限、保存中断、恶意路径 | 合法产物 bytes/hash 一致；明确记录源已有缺失；无静默排除、重复数据或目录逃逸 |
| A05 | 多 API/worker 与 backup/upgrade/lifecycle 同时准入 | gate 和新写只有一方成功；旧写明确完成/停止后才捕获；不可只等待 lease 超时 |
| A06 | OpenClaw/Hermes 工具/子进程/阻塞消费，controller 换主、断网与超时 | 所有 writer ACK，正常/失败均恢复源服务；forced resume 不获 strong；不取消源业务任务制造空闲 |
| A07 | Redis streams/groups/pending/DLQ/TTL/时钟偏差；恢复时部分 key 已到期 | 保留原始快照证据，按 absolute expiry 策略核对差异；不自动重新消费或伪造全量一致 |
| A08 | 加密数据篡改、缺 part、截断、坏 manifest/marker/签名、丢上传响应 | 校验失败或进入明确未知状态；不会误报正式成功；相同外部动作接管不重造数据 |
| A09 | 阻断源 DB/K8s API/Secret/网络，在独立灾备测试安装只用 bootstrap+catalog+artifact | 能完整发现、验签、读取、解密并演练；缺任一历史 key/credential 明确失败，无回源依赖 |
| A10 | 根任务运行中但子任务成功、待审核/不同 revision、部分产物/失败历史 | 根收尾不抹掉已完成子项；ledger/结果/产物保留；原/捕获/恢复后状态和时间可查 |
| A11 | pending/dispatched/running/waiting/stale 等全部当前状态，重复 normalizer 与中途崩溃 | 按已冻结映射幂等收尾，原值只记录一次，未知状态拒绝；不新增业务完成事件/outbox |
| A12 | 恢复上下文下尝试 Team claim/outbox/Owner retry/readiness/provision/升级/生命周期 | 旧任务零执行、旧模型调用零触发、旧实例资源零续建；重复原幂等请求不重复执行；源上下文无误拦截 |
| A13 | 旧 session/token、环境 JSON、磁盘配置、active-assignment、旧 runtime binding | 原 artifact 加密保留；副本按策略禁用/隔离；无有效旧外联身份，无秘密泄露到报告 |
| A14 | 尝试恢复目标重叠/恶意 namespace/root，verifier 试 DML/DDL/取得写凭据 | 写数据前拒绝重叠；独立 verifier 的权限真实拒绝写入；不启动完整旧业务栈 |
| A15 | Job/leader/provider/DB 故障注入，四类 final PUT/append 响应丢失和超时 | 按已冻结 bytes 判等接管；不跨截止时间补发非法动作；commit/eligibility/cleanup 各自真实收敛 |
| A16 | 同幂等键重放、异请求复用键、strong-auth nonce 重放、旧 version/CAS、越权 | 重放不重复动作、冲突明确拒绝；API 进程不能创建备份资源，credential 不能跨职责访问 |
| A17 | 备份并发 Team 新任务、文件编辑、Owner 等待 runtime ready、升级/回滚 | 源备份暂停/恢复有界；正常任务/升级逻辑不被改写；旧 ready 不能绕过恢复隔离派发 leader |
| A18 | catalog scan 中断、漏页、记录/tombstone 交错、provider 普通404 | 接管保留进度；无完整证据不得宣称发现全部/删除不存在；已删除恢复点不再可用 |
| A19 | 前端从左侧“备份”完成配置、创建、详情、演练、报告、保留/清理 | 每个动作调用真实 API；状态与数据库/外部事实一致；没有空按钮/mock成功；深链接/折叠/五语言正常 |
| A20 | 页面关闭/刷新、双击、过期登录、普通用户/Owner直接请求，功能关闭 | 后台不随页面关闭中止；请求不重复；服务端拒绝越权；关闭后新工作拒绝、安全收尾可用 |
| A21 | 受保底/hold/lease保护的数据 prune；候选漂移；删除部分成功；tombstone失败 | 受保护数据不删；旧确认无效；只重试失败项；目录失败不伪装全成功；原有资源不误删 |
| A22 | 演练失败/成功/取消及显式 cleanup，残留明文卷、未知 owner、GC 中断 | 有清单、有结果、可接管；未知不删，残留不算清理成功；活动证据不被 GC |
| A23 | schema/API/DTO parity；所有状态到 UI 的映射；审计 redaction/证据 Range/权限；配置边界 | 无合同分叉，错误与终态组合合法；报告可验证且无明文凭据；敏感下载被审计 |
| A24 | 四部署矩阵、大数据集、停写/任务期限与告警触发恢复、旧功能回归 | 各矩阵有真实 eligible backup+drill+promotion；资源预算满足；告警可触发/恢复；功能未完成不发布 |

大数据集至少采用 v12 `large-smoke.v1`：2 GiB 分片 workspace、10,000 Skill 对象、100,000 Team/审计事件、100,000 Redis key。声明支持规模超过此量时增加代表性容量测试；仅通过该样本不等于支持无限规模。通过上限取正式配置 snapshot 中的容量、停写和总 deadline，不在测试后临时放宽阈值制造通过。

端到端验收 fixture 必须有能独立核对的清单：每个 Team 的原始 ID/owner/scope、每个任务状态及结果 hash、工作项/阶段/事件计数、每个文件相对路径/bytes/hash、Redis 类型/过期时刻。验证前后的差异只能来自冻结 normalization/TTL 政策；任何未解释差异都失败。

## 17. 交付完成定义与验收证据

### 17.1 完成判定

只有同时满足以下条件，才能标记“Team 备份功能完整交付”：

1. T01～T13 全部完成，P0-0a 到 P0c 按顺序通过；第 13 节每个页面和操作均真实接通。
2. A01～A24 及 v12 §17、§18 的适用 P0 要求全部有通过证据；未运行、skip、blocked、仅 mock 或“后续再补”不得计为通过。
3. Team 数据、历史、产物、原状态与 Owner 关联恢复后可核验；未完成任务可读但不可自动重跑；源业务正常恢复。
4. 断开源安装后可从外部目录恢复验证；artifact、目录、密钥与证据具备真实可恢复性，不依赖源磁盘上的单份文件。
5. 四个独立受支持部署矩阵及真实存储/权限负测完成；清理无未说明残留，runbook 与告警可执行。
6. 原 Team/Owner、文件、Runtime 升级和生命周期回归通过；无未经授权的数据/部署操作，无秘密进入仓库或报告。

### 17.2 每批必须提交的证据

在 `docs/team-backup-acceptance/` 保存脱敏摘要与 evidence 引用，不将大数据、数据库 dump 或 Secret 放入 Git。每份记录必须包含：

- requirement/T/A 编号、实际测试命令/入口、执行时间、结果和失败修复记录。
- 两个仓库 commit、相关未提交 diff/hash、构建身份、实际运行镜像 digest、schema/contract/fixture hash。
- 测试矩阵、隔离资源标识、配置 snapshot、采集/停写/恢复/验证耗时、大小与资源消耗。
- backup/artifact/catalog registration/drill/promotion/evidence 身份及校验摘要；原值/归一化/产物核对报告。
- 清理前后 inventory、凭据撤销与卷回收结果；异常保留资源及处置结论。

验收表每项使用“未开始/开发中/待验收/通过/失败/阻塞”；只有本次真实执行及证据完整才进入“通过”。源码中的 TODO、占位页面或截图不能作为数据功能完成证据。

### 17.3 发布状态分别记录

| 结论 | 必须具备的证据 | 本轮状态 |
| --- | --- | --- |
| 最终计划完成 | 本文、原文引用/hash、当前代码接入点及完整任务/验收映射 | 已完成文档修订，未代表功能完成 |
| 本地实现与测试完成 | 两仓实现代码、构建/测试及隔离集成结果 | 未实施、未验收 |
| 镜像已推送 | registry 中镜像 digest、对应源代码和构建记录 | 未执行、未核验 |
| 测试环境已部署并验收 | 实际工作负载镜像、配置与本次四矩阵验收记录 | 未执行 |
| 生产已部署 | 经授权的发布记录、实际运行版本和现场校验 | 未执行；不得从本地或推送状态推断 |

本计划不授权自动提交、推送、生产部署、创建业务实例或恢复生产数据。进入实施后按每批完成实现和验收，不能为了赶进度删除必交付项或把未验证能力改称“已支持”；确有环境阻塞时保留未通过状态与具体缺口。
