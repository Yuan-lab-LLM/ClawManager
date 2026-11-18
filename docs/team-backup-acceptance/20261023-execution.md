# Team 备份连续实施记录

- 开始：2026-10-23 20:09:41（Asia/Shanghai）。用户后续延长时限至北京时间 2026-10-24 02:00；到时完成安全收尾并如实说明剩余工作。
- 用户授权：连续实现全部计划；分批测试、commit；仅测试服务器 10.130.14.23，租户 clawmanager-hxc-peer-system，入口 32543。部署使用 /home/hxc/clawmanager-apply.sh。正式环境只读。
- ClawManager 基线：48d00c09deabf3569fd044eaab837c6c94138111；Runtime 基线：770c469feafcc8169719075252bf9012c828bfcb，均 codex/team-personal-integration。
- 初始未跟踪文件：最终计划。未发现其他业务未提交改动。
- 测试环境原 App 镜像：10.130.14.23:5000/clawmanager-hxc-app:team-owner-placement-cards-20261023-v1。
- 初始只读检查：32543 对应指定租户；App 就绪。App 工作区通过 NFS service 挂载，底层受管 PV 为 HostPath；尚未证明符合 strong capture，不冒充 CSI 快照能力。

## 最新结果（2026-10-24 01:44）

完整功能未交付，按全部必需条件统计为 **0/5 完整阶段通过**。六源加密发布、隔离数据恢复、独立只读核对和验证 Job 协调已通过组件/联合测试，但控制台一键备份与自动演练尚未贯通。详细逐项缺口及继续开发顺序见 [remaining-work.md](remaining-work.md)，不能把部署成功视为整体验收。

- ClawManager 最终业务代码及实际部署版本为 `3a64956ffe1c935507016756009f45823a927dde`，镜像 `10.130.14.23:5000/clawmanager-hxc-app:team-backup-jobs-20261023-3a64956f`，digest `sha256:7ddadfa117aee3b2c9af3839714cb7a809323c1d9f05f3d236a0003ad1e498f9`。32543 version API 已返回此 commit，App/gateway 各 1 个副本 Ready，32556 auth challenge 复查返回 201。完整构建和部署证据见 `20261024-deployment.json`。后续收尾文档提交不重建镜像，须区分仓库 HEAD 与部署业务版本。
- `/home/hxc/clawmanager-apply.sh` 首次网关探测短暂不可达，命令退出 1；后续探测成功。保留此失败事实，不记作部署命令全程成功；共享部署脚本未修改。
- `995a364b` 实际 Job + SQL 控制器联合测试通过 98.58 秒，覆盖实际 Pod 身份、绝对期限、换主、报告保存与 Foreground 清理，见 `20261024-final-candidate.json`。随后清理 UID 修复再通过 96.84 秒联合回归；本地控制器使用修复代码，未改变的 artifact CLI/Job 二进制使用 995 镜像，见 `20261024-cleanup-identity.json` 及对应 evidence.txt。
- 容量验证通过：2 GiB 文件、10000 Skill 对象、100000 Team 事件、100000 Redis 记录；含基础样本的实际计数及资源测量边界见 `20261023-large-smoke.json`。该容量测试使用较早的 `a989ca13` 代码，不冒称测得后续优化的容量耗时。
- AgentsRuntime 本地 `9e9a64191b3232bc9c77b9c02da4e3c532710dd9`；未构建、推送或部署 Runtime 镜像。两个仓库均为 `codex/team-personal-integration`，未推送 Git，未操作正式环境。
- SQL/HTTP gate 与正式备份依赖尚未在租户开启；界面保持未就绪。没有伪造正式资格或手工绕过门禁。
- 浏览器最终实际显示 `3a64956f`、Console 下方的 Backups 入口、数据保留/不续跑说明、disabled Configuration/Create backup 与 not_ready。首次刷新工具超时，重新读取后成功；页面没有正式备份或演练任务，不将页面加载当作创建链路通过。

## 本轮资源收尾

- 已按 owner label、UID 和 resourceVersion 删除本次 fixture namespace `cm-backup-test-20261023-1209`，删除前无验证 Job。额外 ClusterRole/Binding 已单独删除并确认不存在，见 `20261024-fixture-cleanup.json`。
- 已核对绝对路径、owner marker 与活动进程后删除 `/home/hxc/cm-team-backup-20261023-1209`（29 个文件）。没有删除 registry 镜像、修改共享部署脚本或清理其他租户。见 `20261024-server-files-cleanup.json`。
- 已确认身份后关闭四条本次 SSH 转发；没有输出含密码的进程命令行，见 `20261024-tunnel-cleanup.json`。
- 本机递归删除临时目录被自动审批拒绝（仅返回 blocked by policy）。改用更窄的逐文件清理，已删除 7 个含临时口令/token 的文件。`C:/Users/hanxingchen/AppData/Local/Temp/cm-team-backup-20261023` 的无凭据构建缓存与测试日志保留；原已忽略的连接文档保留。见 `20261024-local-files-cleanup.json`。
- 另保留早前中断测试的两个非敏感假产物，目录 `C:/Users/hanxingchen/AppData/Local/Temp/TestSixRealSourcesPublishAndDataOnlyRestore2530569940`；它们不是实际 Team 数据，也不含凭据。本机清理不宣称完全无残留。
- 后续真实测试必须重新建立隔离 fixture 与短期身份。缺失环境变量时部分集成测试会 skip，不能把普通 go test 的绿色结果当成真实六源验收。

下面是各时点历史记录，较早的镜像和未完成项不得覆盖本节或后续收尾记录。

## 历史结果（2026-10-23 23:52）

| 项目 | 状态 | 证据/限制 |
| --- | --- | --- |
| T01 registry | 部分完成 | 75 张业务表六域归属、真实全量 migrations 与字段合同匹配通过；未知表/新增可执行字段拒绝。Runtime 文件/Redis/writer 完整归属尚未闭环 |
| 数据保留/归一化 | 组件验收通过 | 真实 MySQL 全 75 表恢复及独立 SELECT-only verifier；CM/多个 Owner/provider、半创建步骤、已完成子项、部分结果、原始状态证据通过；不等于完整演练流水线验收 |
| 加密与外部发布 | 组件验收通过 | AES-256-GCM 分帧、六 part 签名、真实 MinIO conditional-create/固定版本读取；删除本地 staging 后独立读取、旧版本固定、删除指定分片失败通过。仅 functional_test/best_effort，未生成正式 acceptance/catalog |
| schema ownership | 本地完成并已提交基础迁移 | 11 个 repository 构造 DDL 移到迁移 078；隔离 MySQL 实际迁移通过。目标 schema 仅可信 embedded DDL，业务进程拒绝连接 cm_restore_ 隔离恢复库 |
| Redis / 文件 / 对象 | 组件验收通过 | Redis DUMP 二进制/stream PEL/原 expiry 保留，只恢复到历史命名空间；文件隐藏/部分产物/hash/穿越负测；真实对象产物、metadata、空对象和目标不可覆盖验证通过。Linux 文件元数据和完整 strong checkpoint 待验 |
| K8s/Secret 采集 | 组件验收通过 | 独立真实 GET-only SA：普通采集不能读 Secret，Secret 采集不能读 Pod，两者不能跨命名空间/修改资源。固定 UID 的历史记录仅数据保留，不 Apply 旧 Pod/Secret |
| 控制面/前端 | 部分完成 | 配置 CAS、幂等创建、排队、双租约/fencing、未开始任务取消/过期收尾通过；左侧备份入口、总览/配置/任务详情接真实 API。artifact_verify Job/attempt 接管已完成真实验证；完整 capture/restore runner、外部动作和全部前端页面未完成，准入仍拒绝缺少依赖 |
| 六源联合恢复 | 功能联合验收通过 | TestSixRealSourcesPublishAndDataOnlyRestore：真实 MySQL/Redis/MinIO/本地文件/独立 K8s GET-only 账号，外部加密发布后删除 staging，再恢复验证六类数据；运行中任务变 failed，部分结果与产物保留。未证明活跃源的 strong checkpoint，也未完成断开整个源网络的灾备矩阵 |
| 镜像 push / 测试部署 | 已执行，分版本记录 | 7159197e 已部署到 32543 App/gateway 并 Ready；e63971f6 已推送并用于真实验证 Job，尚未替换 App。通过现有测试镜像叠加本次编译二进制/前端构建层，非完整 Dockerfile 重建 |
| 总体验收 T01～T13 / A01～A24 | 未通过 | 不以局部单测代替完整计划验收 |

原凭据仅从已忽略的连接材料读取，不写入本文、代码、日志或镜像。维护/备份/清理只作用于已登记的测试资源；不触碰其他租户。

## 已复现并修复的问题

- SQL 空字节切片经 JSON omitempty 变成 nil，驱动把非 NULL 空字符串写成 NULL：补真实 NOT NULL 空字符串回归，改用显式非 nil 空 bytes。
- 实际 schema 存在两个生成列：冻结允许的表达式，恢复 INSERT 跳过生成列，verifier 仍对比其值；未知生成列拒绝。
- JSON 重复 key / 未知字段会被静默接受：先失败回归，增加有界递归重复 key 检测与严格 DTO 解码。
- 当前升级状态 prepared/snapshotted/restart_ready/gateway_verified/restored 未登记：先失败回归，纳入中断态归一化；保留 before 状态，清除旧 operation token。
- 并发备份错误返回 conflict_active_job，与原文排队合同不符：改为 pending/global_capture_queue；幂等重放仍只创建一次。
- 失败分类允许任意字符串与取消码：改为 v12 闭集；失败状态拒绝 cancellation/API-only conflict 分类。
- 联合流水线漏传 Redis inventory budget；首次联合测试失败后补齐有界预算并回归。
- 直接行归一化与流式归一化采用不同时间精度，导致同一原状态证据发生冲突；联合测试复现后按列声明统一 finished_at 精度，重试与只读对账通过。
- 前端全局 401 拦截器会把备份强认证挑战当成登录过期，刷新普通 token 不能解决该挑战；先用实际拦截器回归复现，针对备份 strong_auth_* 返回独立处理，普通登录刷新仍通过。
- 测试 MySQL 初次启动遭遇节点 io_setup EAGAIN：只在独立 fixture Pod 禁用 native AIO，没有修改节点或其他租户。

## 隔离资源与完成边界

- 测试 namespace：`cm-backup-test-20261023-1209`，owner label `cm-20261023-1209`。内含临时 MySQL、MinIO、两个 Redis、按职责拆开的采集 SA/Role；无 NodePort。
- 临时连接经主机指纹固定的 SSH 转发；原服务器凭据和短期 SA token 均仅在仓库外临时文件中，结束时清理。
- P0 恢复目标完全不启动业务进程。`cm_restore_<32hex>` 是受保留的数据库名，官方 app/gateway 的统一连接入口拒绝该名字。此防线只声明 P0 数据演练，不能据此宣称 P1 生产启动恢复已完成。
- Runtime 停写、全部业务 writer 接入、正式 checkpoint、独立最终发布者、外部 catalog 完整扫描、strong-auth/nonce、prune/GC/promotion、四矩阵/large-smoke 和全部 UI 操作仍是未通过项。没有删除原计划这些要求，也没有把当前 functional_test 提升为正式恢复点。
- 已提交 ClawManager：`4ce7c273`、`507e69b4`、`7159197e`、`08071deb`、`e63971f6`。AgentsRuntime：`9e9a641`。未 push Git；App 测试部署状态见下。正式环境未操作。

## 已执行测试

- `go test ./... -count=1`：全后端通过；本次启用了 systembackup 的真实 MySQL、MinIO、Redis、Kubernetes fixture，systembackup 包耗时 218.711 秒。其他依赖专用环境变量的历史集成测试仍按原项目规则 skip，不算真实全功能验收。
- 完整数据库恢复用独立 SELECT-only MySQL 账号核对所有行，并真实确认 UPDATE/DELETE/CREATE 被拒绝。
- Linux 交叉编译测试程序在 10.130.14.23 的本次专用目录运行，文件元数据、隐藏文件、目录穿越、篡改与加密往返测试通过；没有在其他租户目录执行。
- 前端构建通过，保留现有 Node 22.11 / Vite 版本提示；备份 strong-auth 与普通 session refresh 两个行为回归通过。新增键盘焦点处理后会再构建。

## 23:52 实际增量与证据

- SQL 门禁 `08071deb`：实际 upper TeamRepository + MySQL 驱动边界接入，事务排空/维护拒写/继续读取/预编译语句/读接口写入拒绝均通过。默认未启用，不能据此宣布文件、对象、Runtime 已停写。
- Runtime `9e9a641`：先复现 Hermes 维护期间继续派发、OpenClaw 同批次维护仍派发后续消息，再最小修复；55 项 Hermes contract/真实 Redis 回归和 OpenClaw 真实 Redis 回归通过。Runtime 镜像尚未部署。
- 验证 Job `e63971f6`：固定 digest 镜像、无自动 SA token、非 root/只读根、挂载不可变 Secret，真实读取并解密六 part；UID 接管、有限结果报告、MinIO 只读身份拒绝 DELETE 验收通过。真实网络 probe 证实备份存储可达，fixture MySQL/Redis 连接均被策略阻断。
- 六源联合测试含实际 Kubernetes Job 首轮成功 82.26 秒；新增文件/对象/Redis 读回和 verifier 权限要求后再次通过 64.88 秒。两次均保持 acceptance_eligible=false。
- 新读回验证已检出同大小/同时间文件篡改、额外对象和 Redis 历史篡改。数据库 verifier 除身份不同还必须实际只有精确目标库 SELECT，无角色/写权限。
- 新发现并按回归修复：fixture 预建控制结构与应用全迁移重复执行 ALTER；fixture 共享源 Redis 记录造成只清一条恢复记录后残留；排队任务使用当前配置而不是创建时冻结配置；API/bootstrap JSON 大小写重复 key 被接受。修复和测试不放宽正式准入。

### 镜像与部署证据

- 实际 App/gateway：`10.130.14.23:5000/clawmanager-hxc-app:team-backup-data-20261023-7159197e`，digest `sha256:35af0ffb89b7b8cb736138e5e5b3cec6682a7e047ce9ec52a0e1e2034924fee8`；version API 返回完整 commit，两个 rollout Ready。用户已接受浏览器测试证书并登录，界面确认左侧备份入口和未就绪提示。
- 候选/Job 镜像：`10.130.14.23:5000/clawmanager-hxc-app:team-backup-jobs-20261023-e63971f6`，digest `sha256:9016d487cca64fc2cb14db1b1aa9b174fe13950c7e67f7b876877f6916764154`；仅验证 Job 使用该版本，不能把推送和 Job 运行等同 App 已更新。
- 新增仅本次 fixture 资源：archive TLS proxy、无 API 权限的 verifier SA、受限 job-controller SA、NetworkPolicy、namespace GET 的精确资源名 ClusterRole/Binding；均属于 `cm-backup-test-20261023-1209` / owner `cm-20261023-1209`。集群作用域的两个 RBAC 对象需要单独清理。

完整五阶段目前仍没有全部验收通过的阶段。主要缺口仍是全部 writer/Runtime 排空、strong checkpoint、capture/restore 编排、独立最终发布与 catalog、MFA/nonce/完整操作账本、生命周期/清理/基线、全 UI 和四矩阵验收。不会用已通过的组件测试冲抵这些要求。
