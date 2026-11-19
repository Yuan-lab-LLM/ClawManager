# Durable observability rollout

The four installation manifests under `deployments/` include the audit PVC
and both credential references. Prepare the immutable Secrets for a fresh
installation and keep their private files outside the repository.

Run `python3 tools/k8s/observability/prepare-observability-secret.py --initialize --key-file /secure/cm-observability-v1.key --output /secure/cm-observability-secret.json`
once, on protected persistent storage. Subsequent preparations omit
`--initialize` and reuse the same key file. The script refuses replacement and
never prints the key. Preserve this file in the encrypted secret backup.

The PVC must provide shared POSIX flock and durable fsync across app replicas.
Mount it only in ClawManager. It requests 128 GiB, with admission determined by
the filesystem's available space. Each request reserves 64 MiB by default
before model dispatch. Space is released after the audit facts and terminal
status commit, including the unknown outcome for an expired admission.
For deployments maintained outside the four profiles, the
[`durable-observability.yaml`](./durable-observability.yaml) patch supplies the
claim and app references.

Full backup capture must include this PVC as a separate `source_volumes` entry
with ID `observability` and a `workspace` FileRoot at
`/run/backup-source/observability`. Include the immutable key Secret in the
`secret_bundle` selection. Restore its files to the independent observability
PVC before starting the app with the same key. Include `observability_objects`,
`observability_body_refs`, `observability_admissions`, and
`observability_maintenance_progress` SQL tables and the encrypted object bucket.
Do not treat a backup missing these dependencies as complete.

Native full capture freezes the `observability` PVC UID and both immutable
Secrets (`clawmanager-observability-key-v1` and
`clawmanager-history-restore-v1`) in its paired ordinary/secret plan. Its ordinary
`source.object_buckets` must contain the serving `OBJECT_STORAGE_BUCKET`,
including the configured `OBJECT_STORAGE_BASE_PATH`. The collector now verifies
archive/body references against the captured bytes and immutable key, follows
nested body dependencies, and rejects missing buckets, keys, changed owners or
corrupt objects. Independent restore verification repeats this against the
actual isolated object bucket. Historical Secrets remain encrypted evidence;
verification does not apply them to Kubernetes or activate application access.

The admin-only Team data flow automatically freezes the immutable observability
key when Team/Redis history depends on it; its Kubernetes identity needs GET on
that exact Secret. It preserves existing non-observability source objects and
copies only the referenced Team/Redis archive objects and their nested bodies
under `observability/v1/`. Global AI SQL rows, summaries, admissions and receipts,
and unrelated AI monthly tables and encrypted body objects are excluded. A
missing dependency fails the backup rather than producing a broken pointer.

Prepare the isolated history proof account. Set the MySQL host and TLS mode to
match the installation.

```sh
python3 tools/k8s/observability/prepare-history-restore.py \
  --output /secure/history-restore-v1 --host MYSQL_HOST --port 3306 --tls true
```

Review the two immutable Secret manifests and apply them to
`clawmanager-system`. For bundled MySQL, execute the private `bootstrap.sql`
as a database administrator after MySQL is ready. The account can only
SELECT/INSERT/UPDATE/DELETE/CREATE in its `cm_history_restore_<32hex>` schema.
On an existing installation, reuse both Secrets and the isolated account.
For a separately maintained app manifest, merge the generated credential mount.

Build `./cmd/history-maintenance` on the test cluster. The CLI uses the serving
DB/object/key/WAL configuration plus `SYSTEM_BACKUP_SQL_GATE_SCOPE` and the
independent `SYSTEM_BACKUP_CONTROL_DSN_FILE`. Every invocation is limited to
30 seconds and at most 500 source rows. Example backfill:

```sh
history-maintenance -mode backfill -kind team_events -since 2026-01-01T00:00:00Z -until 2026-02-01T00:00:00Z -after 0 -limit 500
history-maintenance -mode prove -limit 500
history-maintenance -mode verify -month team_events_202601 -after 0
```

Continue with the returned original-ID cursor; the CLI never deletes sources.
`-mode rollback -month ...` refuses after any source deletion. Automatic monthly
creation uses the existing serving DB credential on a separate one-connection
pool, validates the same database/server, and holds the independent backup work
lease across only a registered UTC table's fixed `CREATE TABLE ... LIKE
history_month_template`. The control credential needs no CREATE privilege;
the serving credential needs CREATE on the application schema. Ordinary gated
application SQL continues to reject DDL and explicit transaction controls.

Encryption version 1 and the immutable key must remain available for all live
journals and body/archive references. Key rotation requires a verified migration
and a new versioned key; replacing the existing key fails journal authentication
and is intentionally unsupported. Never delete ambiguous admission journals to
free capacity. Inspect the authenticated recovery list and resolve the external
execution outcome first.

Body maintenance migrates up to 500 rows per 30-second batch, verifies encrypted
object readback, and conditionally clears original fields. Body/orphan deletion
requires 30-day age, independent restored-object proof, closed request recovery,
no fresh references, and no live archive dependency on that object. Failure retains
source data and reports a deferred maintenance error.

### 报表摘要回填与发布前检查

管理员审计列表读取 `observation_audit_trace_reports`，不再在每次列表/计数中聚合全部热表历史。默认查询 30 天，最多 90 天；更早历史使用页面上的归档导出。游标固定筛选条件、窗口和新 Trace 的序列上界。排序时间在首次形成报表行后固定，迟到事件仍更新完整 Token、最早请求和最新状态，但不会把已翻阅的 Trace 移到另一页。

发布前先在测试/构建集群编译 `backend/cmd/observation-backfill`，通过现有凭据配置目标库，使用独立的 `SYSTEM_BACKUP_CONTROL_DSN_FILE`（0600）和 `SYSTEM_BACKUP_SQL_GATE_SCOPE`。不得在用户本地构建，也不得将测试 fixture 的 root 凭据用于生产。该命令只连接已迁移的数据库，不运行迁移、不清除备份门禁。

```sh
# 在测试/构建集群、backend 目录执行；将二进制交付给受控维护环境。
go build -o observation-backfill ./cmd/observation-backfill

# 完成已应用 116/118/119 迁移后的预回填，每事务最多 500 条。
./observation-backfill --timeout=30m

# 发布前的必需检查。退出非零时不得将新报表 API 切入正常流量。
./observation-backfill --check
```

命令以 JSON 输出就绪状态与剩余审计来源。退出/中断后可重新执行，已提交水位和去重事实保持有效；不会重置保护或重算费用。缺失调用依赖的费用会明确报出费用与调用 ID，必须先恢复或重放原调用再重试。备份门禁关闭时拒绝维护。启动后的后台任务也以最多 30 秒一轮、每批最多 500 条继续回填。

回填未完成时，查询明确返回 HTTP 503、`Retry-After` 和 `data.state=rebuilding` / `processed` / `target`，不返回部分总数。前端保留错误说明及重新应用筛选入口。回填覆盖热调用、已有调用摘要、热审计事件以及此前已标记验证完成的归档调用/审计元数据，因此升级前归档的 event-only Trace 不会遗漏。`--check` 成功才表示允许报表切换；实际容量验收结果应使用对应发布的测试报告，不能以本节计划代替测量。

### 从原生恢复演练生成正文回收证明

在源侧受控维护环境完成一次全量备份的原生隔离恢复后，保留由恢复流程冻结的验证配置、恢复目录与只读目标凭据。在该维护环境运行以下命令。归档 bootstrap 使用源安装信任的签名公钥、加密引用和只读归档凭据。恢复验证进程继续使用原有的 `verify-restored` 配置，恢复环境无需源库写凭据。

```sh
SYSTEM_BACKUP_SQL_GATE_SCOPE="$SERVING_GATE_SCOPE" system-backup-artifact \
  --operation record-observation-restore-proof \
  --bootstrap /run/cm-archive/bootstrap.json \
  --operation-config /run/cm-maintenance/body-proof.json \
  --origin "$ORIGIN_INSTALLATION_ID" --backup "$BACKUP_ID" \
  --max-items 1000000 --max-bytes 17179869184 --timeout 30m
```

`body-proof.json` 仅保存在源侧，指向这三个文件。

```json
{
  "verification_config_file": "/run/cm-readonly/verify-restored.json",
  "source_dsn_file": "/run/cm-maintenance/observation-writer.dsn",
  "control_dsn_file": "/run/cm-control/gate.dsn"
}
```

源库身份需要读取安装身份、对象登记和字段释放记录，更新对象证明时间，并读写归档依赖索引。独立的 control 身份连接同一物理数据库并持有正在使用的维护门禁。操作重新读取签名备份、核对六个部分、验证独立恢复目录与目标数据，并认证每个对象的实际密文和明文摘要。全部校验通过后才按对象键、所有者、长度、加密版本及摘要写入源库。命令输出本次新增证明数，门禁释放后可以重试。

旧归档第一次成功验证后建立逐对象依赖索引，新写入的归档在发布引用前锁定依赖。仍被归档引用的正文随归档保留。其他正文达到 30 天、请求恢复已关闭且证明齐备时可回收。已上传的孤儿也可通过演练取得证明。仅有上传意图而没有密文的记录继续保留。热表和归档详情保留已释放字段的身份，重新备份和恢复可据此显示正文已过期。

#### 版本化源桶的物理空间

应用默认源桶为 `clawmanager-skills`，基路径为 `skills`。服务端创建源桶时使用普通 `MakeBucket`，部署清单没有为该桶开启版本控制或设置生命周期。原生全量备份要求管理员事先开启源桶版本控制。Team Data 可为独立备份目标桶开启版本控制。使用文件存储回退时，回收直接删除文件。

在版本化源桶里，CM 提交字段释放后删除当前键。S3 的删除标记会隐藏该键，原密文字节成为非当前版本。CM 没有给这些字节设置默认物理过期天数。检查容量时，分别记录活跃键数、非当前版本字节数、删除标记数，以及独立备份目标所占空间。生命周期删除由存储提供方异步执行。[S3 版本和过期规则](https://docs.aws.amazon.com/AmazonS3/latest/userguide/intro-lifecycle-rules.html)

以下为部署方只读核对命令，凭据使用源桶的只读 profile。将端点设为含协议的实际地址，按当前安装独占的基路径填写前缀。若多个安装共用同一前缀，先完成归属核对。返回 `AccessDenied` 表示缺少查询权限。

```sh
cm_s3() { aws --profile "$CM_READONLY_PROFILE" --endpoint-url "$CM_S3_ENDPOINT" s3api "$@"; }
bucket=clawmanager-skills
prefix=skills/observability/v1/
cm_s3 get-bucket-versioning --bucket "$bucket"
cm_s3 get-bucket-lifecycle-configuration --bucket "$bucket"
cm_s3 get-object-lock-configuration --bucket "$bucket"
cm_s3 list-object-versions --bucket "$bucket" --prefix "$prefix" --max-items 100
# 从上述结果选取一个已完成证明和逻辑释放的键、一个实际非当前版本进行核对。
cm_s3 get-object-retention --bucket "$bucket" --key "$CM_PROVEN_KEY" --version-id "$CM_VERSION_ID"
cm_s3 get-object-legal-hold --bucket "$bucket" --key "$CM_PROVEN_KEY" --version-id "$CM_VERSION_ID"
```

完成独立恢复演练并核对正文证明、独立备份留存策略和源桶版本依赖后，再由桶管理员审查非当前版本策略。先确认该前缀的全部写入来自本安装，且既有非当前版本均允许按日龄过期，包括上线前产生的版本。保留天数至少满足所引用原生不可变备份的保留期限和站点要求。存在无限期保留、法律保全或未核实的恢复依赖时，继续保留对应版本。Object Lock 默认规则与具体版本的保留日期、法律保全状态需要分别核对。[S3 生命周期与受保护版本](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-expire-general-considerations.html)

下面的命令仅输出一个规则对象作为合并到现有策略的模板。指定 `CM_NONCURRENT_RETENTION_DAYS` 为经核定的正整数天数。模板保持禁用，匹配范围为当前安装的正文物理前缀；不包含当前版本过期动作。应保留源桶已有规则和独立备份桶的策略。

```sh
jq -en --arg p "$prefix" --argjson d "$CM_NONCURRENT_RETENTION_DAYS" '
  if ($d | type) == "number" and $d >= 1 and ($d | floor) == $d and
     ($p | endswith("observability/v1/"))
  then {ID:"cm-observability-noncurrent", Status:"Disabled",
        Filter:{Prefix:$p}, NoncurrentVersionExpiration:{NoncurrentDays:$d}}
  else error("invalid reviewed prefix or retention") end'
```
