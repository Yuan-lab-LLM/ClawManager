# Team 备份实现状态与运行边界

本文件说明当前代码实际能力，不替代 `docs/team-backup-restore-team-integration-plan.md` 的全部交付要求。

最新部署/清理结论见 [实施记录](20261023-execution.md)，逐项未通过要求见 [剩余工作](remaining-work.md)。下方带时间的小节是历史增量，不能把其中旧镜像状态作为当前部署事实。

## 已有可执行能力

- 左侧“控制台”下方新增“备份”，路由 `/admin/backups`。总览、配置读取/部分保存、任务查询/详情/取消调用真实 API，权限和依赖不满足时拒绝，不返回模拟成功。
- `backend/internal/systembackup` 提供六类真实源的加密采集、固定版本外部发布、无源数据库的读取验证、独立数据恢复。联合测试覆盖真实 MySQL、Redis、MinIO、Kubernetes 只读账号及工作区文件。
- 原始数据在不可变加密 artifact 中保留。恢复库内中断任务归一化为失败，已完成工作保留；原/后状态与行 hash 写入外部控制库的加密证据。Redis 放入历史命名空间；部署和 Secret 只读作历史，不执行旧资源。
- 官方业务进程拒绝连接 `cm_restore_<32hex>` 命名的隔离数据恢复库。恢复验证账号只有 SELECT，并实际验证 DML/DDL 被拒绝。
- `system-backup-controller` 是独立的控制协调进程：领导租约、任务租约、数据库时间、排队原因和未开始任务的取消/超时收尾。独立 artifact_verify 现已接入 Kubernetes Job 和持久化 attempt 接管；capture/restore Job 编排仍未完成，不能将此进程称为完整备份执行器。
- `system-backup-artifact` 提供独立的 capture / verify / restore / verify-restored 命令。verify 仅使用外部对象存储和 bootstrap 密钥文件；restore 仅连接灾备侧预先创建的隔离目标，不连接源 DB、源 Kubernetes 或业务 Runtime。verify-restored 使用独立只读身份核对恢复数据。输出完整性结果不自动授予正式恢复资格。

## 独立验证工具

构建入口：`backend/cmd/system-backup-artifact`；容器路径 `/usr/local/bin/system-backup-artifact`。

```text
system-backup-artifact --bootstrap /run/backup-bootstrap/config.json --origin <installation-id> --backup sbk_<32hex> --max-bytes 17179869184 --timeout 30m
```

bootstrap JSON 包含 endpoint、bucket、root、secure、credentials_file、可选 ca_file、verification_key_file、verification_key_version、encryption_key_file、encryption_reference。凭据文件为 access_key/secret_key/可选 session_token；验证公钥是 32 字节原始 Ed25519 public key，KEK 是 32 字节原始数据；encryption_reference 使用与备份一致的 provider/name/version/purpose 四元组。所有文件通过外部 Secret 管理注入，禁止提交到 Git。非 TLS 仅允许本机回环地址，用于已经核验身份的本地测试转发。

工具固定读取签名 marker 指向的 manifest/index/object version，并完整解密校验六 part。错误或缺失历史 key 会失败；不会尝试回源、替换新 key 或宣布“全部备份已发现”。当前按已知 installation/backup identity 精确发现，**尚无正式 catalog 全量扫描**。

## 实际采集和数据恢复命令

以下入口已经接上真实数据适配器，均为显式执行的一次性操作；不是前端完整 Job 编排的替代品。

```text
system-backup-artifact --operation capture --bootstrap /run/backup/bootstrap.json --operation-config /run/backup/source.json --origin <installation-id> --backup sbk_<32hex> --max-bytes 17179869184 --max-items 1000000 --timeout 30m
system-backup-artifact --operation restore --bootstrap /run/backup/bootstrap.json --operation-config /run/backup/target.json --origin <installation-id> --backup sbk_<32hex> --max-bytes 17179869184 --max-items 1000000 --timeout 30m
```

- capture 的 bootstrap 额外指定 `signing_key_file`（64 字节原始 Ed25519 私钥，必须匹配验证公钥）。source.json 指定 `mysql_dsn_file`、`redis`、`objects`、`buckets`、`workspace`（id/path）、两个独立 Kubernetes 只读客户端、namespace/namespace_uid、部署/Secret 固定 UID 清单、空的 `stage_directory`。成功发布后仅删除本次生成的六个密文暂存文件；失败不递归删除任何目录。
- restore 的 target.json 指定 `run_id`（sdr_32hex）、`mysql_dsn_file`、`verifier_dsn_file`、`control_dsn_file`、`control_installation_id`、`redis`、`objects`、`bucket_mapping`、空的 `directory`。目标 MySQL 必须是已有可信 schema、无业务数据的 `cm_restore_<32hex>`；验证身份必须与恢复身份分离；外部控制库不能是恢复目标库。目标 Redis 必须为空且不能是源 Redis 服务器；对象桶必须预先创建、为空且使用 cm-restore-32hex 名称。
- 对象连接包含 endpoint/secure/credentials_file/可选 ca_file；Redis 连接包含 address/database/secure/credentials_file/可选 ca_file/server_name，凭据文件为 username/password。Kubernetes 连接包含 server/server_name/ca_file/token_file，不支持外部 exec 认证插件。非 TLS 对象/Redis 连接只允许回环地址转发。
- 恢复先完整认证源产物，然后归一化导入 SQL、使用独立只读连接流式逐行校验、恢复工作区/历史 Redis/对象，保存加密 Kubernetes 历史。它不应用旧部署/Secret。目标目录保存 `.restore-owner`、`restore-report.json`、workspace 和 encrypted-history；报告包含进度、失败阶段、表数量/hash、文件数量及加密历史元信息，不包含明文 Secret。
- 任何阶段失败都不启动业务服务、不自动覆盖重跑。保留本次隔离资源供核验，重新执行必须使用新目标。**尚未自动创建/销毁目标资源、验证完整网络隔离或销毁恢复写凭据，因此不能作为正式合格演练。**

## 控制协调进程

构建入口：`backend/cmd/system-backup-controller`；不调用 app 初始化或 schema migration。部署前由独立 migration 身份建立 078～082 所需结构。

- `SYSTEM_BACKUP_CONTROL_DSN_FILE`：只含控制数据库 DSN 的挂载文件。
- `SYSTEM_BACKUP_INSTALLATION_ID`：稳定安装标识，首次登记后不能静默更换。
- `SYSTEM_BACKUP_CONTROLLER_ID`：当前控制器实例标识。

API 的安装标识使用相同 `SYSTEM_BACKUP_INSTALLATION_ID`。`SYSTEM_BACKUP_ROLE_BINDINGS` 是离线引导映射 `用户ID:角色,...`；角色为 viewer/operator/config_admin/security_admin，非空配置要求至少两个 security_admin。仍须通过原应用管理员鉴权。此引导映射不代替原计划完整权限管理与近期强认证。

## 当前不能宣布完成的内容

完整业务 writer 接入、OpenClaw/Hermes 模型/工具/子进程停写、strong checkpoint、独立 finalizer/acceptance、外部 catalog、近期强认证/nonce、完整恢复执行器和报告、prune/GC/promotion、全部前端操作及四部署矩阵仍未完成。large-smoke 的最低数据量功能测试已经通过；完整资源测量和正式发布矩阵仍未通过。

因此当前功能测试产物固定为 `functional_test + best_effort`；API 创建正式备份仍受依赖门禁阻止，不能手动把健康状态勾绿来绕过。测试镜像即使部署，也只证明当前代码已上测试环境，不能等同完整 Team 备份功能已交付。

## 2026-10-23 23:15 增量：业务 SQL 门禁

- 可选 `SYSTEM_BACKUP_SQL_GATE_SCOPE` 将官方业务连接池接入数据库行锁门禁，涵盖 upper repository、后台 SQL、预编译语句和显式事务。现有写事务结束后才能进入停写；维护中读取继续，新的写入失败。Query 接口使用真实只读事务，不能通过换用 Query 绕过。
- 启用前需离线完成全部 migration、建立 scope，并通过 `SYSTEM_BACKUP_CONTROL_DSN_FILE` 提供独立控制连接。启用后应用拒绝自行迁移或执行 DDL/事务控制 SQL。该开关尚未在 32543 启用；它不证明 Redis、文件、对象或 Runtime 已停写，也不覆盖另行创建的裸连接池。
- 真实 MySQL 验证已覆盖写事务排空、维护拒写、只读访问、预编译语句、缺失 gate 拒绝，以及现有 TeamRepository / upper ORM 接入。恢复维护后可以继续正常写入。
- AgentsRuntime 本地提交 `9e9a641` 修复 Hermes 未检查 maintenance、OpenClaw 同批次中途进入 maintenance 仍派发后续消息的问题。真实 Redis 回归通过；尚未构建或部署 Runtime 镜像，也未完成所有工具/子进程/presence 排空。
- 32543 当前实际部署 ClawManager `7159197e` 镜像 `10.130.14.23:5000/clawmanager-hxc-app:team-backup-data-20261023-7159197e`。浏览器已验证入口和未就绪提示；新增 SQL 门禁代码仍属本地开发。

## 独立验证 Job 与控制器接管

- migration 082 为 attempt 保存完整冻结请求。Job 创建前先提交 attempt；恢复执行使用固定名称、请求 hash 和原 Job UID，创建响应丢失不产生第二个任务。旧 leader 不能提交结果。
- 可选 `SYSTEM_BACKUP_VERIFY_RUNNER_FILE` 为控制器配置 namespace、namespace_uid、固定 digest 镜像、service_account、bootstrap_secret。控制器使用独立 in-cluster 身份。此项只接管已经通过服务端准入的 artifact_verify 任务，不放宽正式备份门禁。
- 验证 Job 不自动挂载 Kubernetes token、无源连接配置、以非 root/只读根文件系统运行；读取挂载的灾备 bootstrap。Job 退出成功还必须存在绑定本次 backup/origin 的有限长度六 part 完整性报告；该报告永远不授予 acceptance 资格。
- 取消/截止时间会按原 UID 删除 Job，确认删除完成后才收尾。未知或内容不匹配的同名 Job 不接管、不删除。使用真实 MySQL 已验证 leader 切换复用同一 attempt、截止时间不延长、旧 leader 拒绝、取消等待实际停止。
- Job 的独立 namespace、只读存储权限、网络隔离和不可变 bootstrap Secret 必须由离线部署建立。正在测试服务器进行真实执行验收，不能将本节单元/数据库测试视为已完成全链路验收。

## 2026-10-24 00:00 增量：恢复读回与报告

- 数据恢复在写入后重新读取文件、对象和 Redis 历史，对照已认证的原始 part 核对内容、权限/归属/mtime、对象 metadata/额外版本、Redis DUMP/类型/历史无过期策略；报告区分写入数量与读回通过数量。
- 独立 SQL verifier 必须确实只有精确目标库 SELECT 和 USAGE，不接受角色、额外数据库或写授权；缺少该条件在导入数据前拒绝。
- artifact_verify 完成后提供受 backup.read 权限保护的安全报告 API，并在任务详情显示六 part 完整性结果。不会暴露冻结 Job 请求、Secret 引用或原始日志；报告不是 acceptance 证书。
- attempt phase 已按 v12 使用 verify，与任务状态 running 区分；排队任务的预算来自创建时冻结配置，当前功能开关只控制新启动。
- 本批相关后端包完整回归通过，systembackup 322.118 秒，启用真实 MySQL/MinIO/Redis/Kubernetes Job 和独立 capture/restore CLI。前端报告页面构建通过。

## 配置与取消的重试一致性

配置保存和任务取消现在要求 Idempotency-Key，前端在同一请求重试时保留该 key，修改请求后生成新 key。配置/状态变更、操作响应和审计事件在同一事务提交；同 key 不同内容拒绝，成功重试返回原响应。真实 MySQL 已验证八路并发配置请求只提交一次、取消只写一次事件，并在最终操作账本写入故障时回滚此前配置和审计写入。安全配置修改仍需尚未完成的近期强认证，不通过该改动绕过。

## 请求边界停写（2026-10-24 00:28）

启用 SQL 门禁时，App/gateway 同时在同步 HTTP handler 外持有同一物理数据库 gate 的共享锁，覆盖请求内的文件、Redis、对象操作；包括可能有写副作用的 GET。只有已注册的备份控制路由和纯 version 路由豁免。请求取消不会提前释放锁，必须等 handler 真正返回；维护中请求返回可重试 503。独立连接池缺失或数据库身份不一致时拒绝启动。真实 MySQL 已验证客户端取消后仍排空、维护拒绝和错误控制库识别；middleware 回归覆盖异常返回释放与未知路径不能借前缀绕过。

默认仍未启用；异步 goroutine、后台任务、反向代理和 Runtime 必须另外参与排空，不能把同步请求边界当作完整 strong checkpoint。

## Team 状态摘要与验证 Job 接管（00:42）

恢复报告新增 Team 表记录数、隔离记录数以及捕获/恢复状态计数；完成任务保留完成，中断任务改失败，完整 SQL 读回通过后才标记摘要已验证。摘要不包含任务正文、部分结果、owner 标识或凭据。真实六源恢复与 Kubernetes Job 联合测试通过（59.94 秒），状态摘要随恢复报告保存。

接管回归先复现网络隔离标签、并发数/完成数、资源预算及额外 lifecycle/probe 被修改后仍被接受的问题，再补冻结条件核对。新负测与真实 Job 接管均通过。

容量测试使用 2 GiB/2048 文件、10000 Skill 对象、100000 Team 事件、100000 Redis 记录，固定 4 GiB/200000 项预算。首轮外部发布与验证 Job 通过，但服务器测试包缺少控制迁移文件，尚未恢复；补齐运行材料后重跑，不把首轮算成功。

## 独立恢复读验、自动收尾和容量结果（01:05）

- 新增 `system-backup-artifact --operation verify-restored`：operation-config 只接受冻结 intent（run_id、normalized_at、history_parts）、verifier_dsn_file、只读 Redis/object 连接、bucket_mapping 和 directory；不接收恢复写 SQL、控制库或源 Kubernetes 凭据。对照认证源重新归一化并核对 SQL、工作区、对象、Redis 和两份加密资源历史，不写目标，不启动业务。intent 必须由可信控制证据提供，不能把目标上的可编辑 JSON 当作可信 intent；当前尚未自动绑定该意图或证明写凭据已销毁，结果始终不授予 acceptance。
- 真实测试使用 SELECT-only SQL、独立 Redis ACL、独立 MinIO 读取账号，实际确认对象 PUT/DELETE 和 Redis SET 被拒绝。独立 CLI 核对通过，修改恢复后的加密历史可被检出。
- artifact_verify 成功结果先写入 durable attempt，再按固定身份删除 Job并等待 Foreground 清理确认，才将任务置为成功；复用的外部 bootstrap Secret 不由该任务删除。真实控制库与 Kubernetes Job 联合通过换主、同 attempt、持久报告、Job/Pod 完全清理验收。
- 复现 SQL verifier 对 1000 行发出 1007 次查询；改为每表一个按主键排序的流式 cursor。真实测试验证查询次数不随行数增长，并继续检出字段损坏、额外行和超预算。全 75 表/多 owner/状态与结果保留回归通过（47.937 秒）。
- large-smoke 重跑通过：2 GiB/2048 文件、10000 Skill 对象、100000 Team 事件、100000 Redis 记录；加基础样本，恢复2052文件条目、10001对象、100001Redis记录。1237.17秒，测试进程峰值83944KiB；详细元数据见 `20261023-large-smoke.json`。该次运行在流式 SQL verifier 优化前启动，不声称已测得新版本容量耗时；指标不包括数据库/Redis/对象存储/Job Pod。

上述新增只补齐组件能力；自动创建/销毁恢复资源、全部 writer 与 Runtime 停写、正式 checkpoint/finalizer/catalog、强认证和完整 UI 等原有缺口仍存在。

- 独立校验和控制器联合链路在流式 SQL 优化后再次通过（100.40秒，包含真实 capture/restore/verify-restored 三个 CLI、真实 Job、控制器换主与清理）。
- 系统级恢复新增完整 SQL inventory 预检：在任何业务数据写入前验证 75 张登记表全部出现，即使空表也不能省略。合法的逐表 checksum 不再能掩盖缺少 Team 表；独立恢复读验同样要求完整 inventory。单表工具仍保留其显式局部处理能力。

## 执行身份与绝对截止时间（01:17）

先用回归复现后补齐：Job 完成结果还要核对实际 Pod 的冻结配置（含网络隔离标签、唯一容器、Secret 卷、权限、资源预算与无额外执行钩子），拒绝 Pod admission 后的替换配置。新 attempt 固定 task 的绝对 deadline_at，传给 CLI 的 --deadline-at；进程使用绝对期限与 --timeout 较早者，Job 被重建或延迟启动也不能重新获得原时长。旧冻结请求仍可读取/清理，但没有新请求的绝对期限保证；不能改写旧请求 bytes 来伪称已补齐。真实 MySQL/单元回归通过，`995a364b` 实际新镜像 Job 与真实控制器联合验收通过（98.58 秒）。

## 清理身份回归（01:35）

先复现成功结果已保存原 Job UID，但清理只比同名/同配置而误删替代 Job 的问题；修复为成功/失败 attempt 清理必须匹配持久结果中的 UID，缺失或冲突拒绝删除。首次成功收尾也重新读取已提交 attempt，避免使用 FinishAttempt 之前的内存值。对应替换对象负测、首次清理与真实 MySQL 控制器回归通过（7.557 秒）。这仅补齐已持久结果的身份检查；运行中 Job 的完整 incarnation/observation 账本仍待补充，不能声称已解决所有同名重建的接管问题。失败现场保持原保留策略。

清理 UID 修复后的真实六源与控制器联合回归通过（96.84 秒）；本地控制器/driver 使用本次修复，Job 及 capture/restore/verify-restored CLI 使用未改变的 995a364b 二进制。没有将此描述成尚未构建的新镜像已运行。
