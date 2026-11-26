# ClawManager 备份试行版使用手册

## 开始前

源端和目标端均从管理员导航进入 **备份**（`/admin/backups/records`）。演练使用两个独立的 Kubernetes namespace。若源和目标位于不同集群，页面交付步骤相同。

源端保存要备份的账号、实例和内容。目标端需要一个可登录的管理入口和一个已准备的空恢复业务库。恢复完成后，通过目标运行入口使用恢复的账号和实例。

### 平台前置配置

**「查看源和目标配置」是只读面板。** 面板中的「集群」和「命名空间」来自平台预先登记的源、目标配置。同一集群演练可填写相同的集群标识，并分别填写源和目标的 namespace。跨集群演练填写各端对应的集群标识和 namespace。

| 位置 | 平台准备内容 |
| --- | --- |
| 两端 | 固定 CM 镜像摘要、Kubernetes 服务账号、准备任务、归档 PVC 和试行密钥 Secret 引用。升级应用时，同时核对该镜像要求的观测日志持久卷和独立观测加密密钥。 |
| 源端 | 现有业务库和专用只读账号、可写归档卷及独立读回挂载。需要采集 Hermes 聊天或工作区时，登记允许采集的实例和目录。 |
| 目标端 | 可登录的管理入口、独立的空恢复业务库、恢复写账号、独立复核只读账号、与源包匹配的解密密钥，以及所需工作区卷。启用页面上传时，配置专用导入接收器，恢复服务从只读挂载读取原件。 |

下文先说明如何在现有 namespace 完成源端备份，然后说明如何把交付包恢复到目标端。密钥通过独立渠道交付，交接单和交付包携带恢复所需的产物及校验信息。

## 1. 更新现有 namespace 的 ClawManager 镜像

使用可从集群拉取的试行版内网镜像，完整填写 `仓库/镜像@sha256:摘要`。升级前核对业务库迁移权限、`CLAWMANAGER_OBSERVABILITY_DIR` 对应的持久卷，以及 `CLAWMANAGER_OBSERVABILITY_KEY`。观测日志和备份使用各自的密钥。

由 Helm 或 GitOps 管理的环境走原发布流程。直接管理 Deployment 时，可用 Windows PowerShell 执行以下命令。先把前五个变量替换为现场值，后续命令继续使用这些变量。

```powershell
$trialContext = '<源集群的 kubectl context>'
$trialNs = '<现有的源 namespace>'
$trialDeploy = '<ClawManager Deployment 名>'
$trialContainer = '<ClawManager 容器名>'
$trialImage = '<内网镜像仓库/镜像@sha256:摘要>'

kubectl --context $trialContext -n $trialNs set image "deployment/$trialDeploy" "$trialContainer=$trialImage"
kubectl --context $trialContext -n $trialNs rollout status "deployment/$trialDeploy"
```

滚动更新成功后，确认源端页面能正常登录，原有用户和实例能正常查看。

## 2. 首次登记备份环境

平台管理员在试行版代码仓库的根目录执行本节命令。脚本需要 Python 3。Windows 提供 `py` 启动器时按示例执行，只有 `python` 命令时，将 `py -3 -B` 换为 `python -B`。

### 2.1 读取现有部署和迁移目录摘要，填写登记文件

复制[登记配置模板](../deployments/k8s/system-backup-trial-registration.example.json)。先查看现有 Deployment 的服务账号、容器名和镜像，再从已经更新的容器读取迁移目录摘要。

```powershell
$trialConfig = '.\trial-registration.json'
$trialScript = '.\scripts\system-backup-trial-register.py'

kubectl --context $trialContext -n $trialNs get deployment $trialDeploy `
  -o 'custom-columns=SA:.spec.template.spec.serviceAccountName,CONTAINERS:.spec.template.spec.containers[*].name,IMAGES:.spec.template.spec.containers[*].image'

kubectl --context $trialContext -n $trialNs exec "deployment/$trialDeploy" -c $trialContainer -- `
  /usr/local/bin/system-backup-migrate -catalog-only

if (-not (Test-Path -LiteralPath $trialConfig)) {
    Copy-Item .\deployments\k8s\system-backup-trial-registration.example.json $trialConfig
}
```

打开 `trial-registration.json`，按实际环境填写。源、目标分别在各自的 MySQL 执行 `SELECT @@server_uuid;` 获取服务器 UUID。

| 字段 | 填写内容 |
| --- | --- |
| `migration_catalog_sha256` | `-catalog-only` 命令输出的同名字段。 |
| `source.cluster`、`source.namespace` | 当前集群标识和现有的源 namespace。 |
| `source.image`、`source.service_account` | CM 容器的完整 digest 镜像引用和现有 CM Pod 的服务账号。 |
| `source.artifact_claim`、`source.artifact_storage_class` | 源端专用归档 PVC 名和实际 StorageClass。 |
| `source.key_secret`、`source.source_read_secret` | 准备 Job 将写入的备份密钥和备份专用只读账号所用的两个独立 Secret 名。 |
| `source.prepare` | 实际业务库名、MySQL UUID、MySQL 账号允许的连接来源 `account_host`、准备 Job 名和版本、Job 专用服务账号、环境 ConfigMap 名、DSN Secret、CA ConfigMap，以及卷权限所用的 UID 和 GID。 |
| `target`、`target_isolation_confirmed` | 按模板填预先选定的隔离目标，核实隔离后将 `target_isolation_confirmed` 设为 `true`。同一集群使用相同的集群标识和不同的 namespace。目标 namespace 以 `cm-drill-` 开头，恢复业务库以 `cm_drill_trial_` 开头，目标使用独立的 MySQL 实例，`server_uuid` 与源端不同。 |

只做备份时仍需填好 `target`。本节命令只部署源端的准备任务和应用配置。

### 2.2 准备源端归档卷和数据库连接

在源 namespace 创建或选定专用归档 PVC，配置实际的 StorageClass 和容量。CM Pod 和准备 Job 都会挂载该卷，存储配置需支持两者访问。确认准备 Job 和 CM 的运行身份能够写入归档，并通过只读挂载独立读回。

准备 Job 使用专用的数据库管理凭据，用于创建备份专用的只读账号并授予指定业务表的 `SELECT` 权限。用受保护的文件向 Secret 提供 DSN，连接远程 MySQL 时使用 `用户:密码@tcp(MySQL地址:3306)/业务库名?tls=true` 形式。CA 文件使用 PEM 格式的信任链。

```powershell
$trial = Get-Content -Raw -LiteralPath $trialConfig | ConvertFrom-Json
$trialJob = $trial.source.prepare.job_name
$trialDsnFile = '<受保护的 DSN 文件路径>'
$trialCaFile = '<CA bundle PEM 文件路径>'

kubectl --context $trialContext -n $trialNs get pvc $trial.source.artifact_claim

# 首次创建时执行，已有资源可核对并复用
kubectl --context $trialContext -n $trialNs create secret generic `
  $trial.source.prepare.owner_dsn_secret "--from-file=dsn=$trialDsnFile"

kubectl --context $trialContext -n $trialNs create configmap `
  $trial.source.prepare.ca_configmap "--from-file=ca-bundle.crt=$trialCaFile"
```

需要备份 Hermes 聊天或 workspace 文件时，在 `source` 中登记专用的 `workspace_claim`，以及对应的 `hermes_history` 或 `workspace_files`。源端以只读方式挂载该工作区卷，并按登记的实例和目录提供内容选择。字段约束见[登记脚本](../scripts/system-backup-trial-register.py)。

### 2.3 创建准备 Job 和环境 ConfigMap

```powershell
# 首次创建备份密钥和只读账号的空 Secret，由准备 Job 填充
py -3 -B $trialScript --config $trialConfig --side source placeholders |
  kubectl --context $trialContext -n $trialNs create -f -

# 创建挂起的准备 Job、Job 专用服务账号和权限
py -3 -B $trialScript --config $trialConfig --side source render |
  kubectl --context $trialContext -n $trialNs apply -f -

# 使用已创建的 Job 生成环境 ConfigMap，绑定实际 Job UID
kubectl --context $trialContext -n $trialNs get job $trialJob -o json |
  py -3 -B $trialScript --config $trialConfig --side source profile --job - |
  kubectl --context $trialContext -n $trialNs apply -f -
```

Job 此时保持挂起，后面由管理员在页面启动。内网镜像仓库需要认证时，给准备 Job 的服务账号配置相应的镜像拉取凭据。已填充的备份密钥和只读账号 Secret 要保留。 [准备 Job 模板](../deployments/k8s/system-backup-trial-prepare.yaml.template)

登记了 `hermes_history` 时，应用脚本生成的采集脚本 ConfigMap。

```powershell
py -3 -B $trialScript --config $trialConfig --side source hermes-source-script |
  kubectl --context $trialContext -n $trialNs apply -f -
```

### 2.4 给源端 ClawManager 部署挂载试行配置

从实际 Deployment 生成 `app-patch`。直接管理 Deployment 时应用 JSON 补丁。由 Helm 或 GitOps 管理时，把补丁描述的挂载、环境变量和采集容器配置合入原发布配置，再走原发布流程。

```powershell
$trialPatchFile = '.\trial-source-app-patch.json'

$trialPatch = kubectl --context $trialContext -n $trialNs get deployment $trialDeploy -o json |
  py -3 -B $trialScript --config $trialConfig --side source app-patch `
    --deployment - --container $trialContainer

if ($LASTEXITCODE -ne 0 -or -not $trialPatch) {
    throw '生成源端 Deployment 补丁失败'
}
$trialPatch | Set-Content -LiteralPath $trialPatchFile -Encoding ascii

# 以下两条用于直接管理 Deployment 的环境
kubectl --context $trialContext -n $trialNs patch deployment $trialDeploy `
  --type=json --patch-file $trialPatchFile

kubectl --context $trialContext -n $trialNs rollout status "deployment/$trialDeploy"
```

补丁绑定生成时的 Deployment 版本。如果应用时提示版本已变动，重新读取 Deployment 生成补丁。滚动更新成功后，用管理员账号到源端页面启动准备任务。

## 3. 在源端页面检查并准备

用管理员账号打开源端 **备份**（`/admin/backups/records`），点 **检查试行就绪状态**，展开 **查看源和目标配置**，核对源 namespace、目标 namespace 和归档卷。首次看到 **准备试行环境** 时，点击它，等任务完成后再次检查。源库只读权限、备份密钥、归档写入和独立读回都通过后，就可以备份。

如需留档或定位缺项，点 **下载环境检查报告**。

## 4. 创建并下载备份

- **备份全部账号和实例**，点 **创建试行备份**。
- **要带上已登记的聊天或 workspace 文件**，点 **选择备份内容**，选账号和实例，再勾选会话或目录，最后点 **创建所选备份**。按范围选择时，至少包含一个启用的管理员。管理员名下的实例也要选中，才会纳入这次按范围备份。勾选实例会自动包含它的归属账号和配额。选择 Hermes 续会话时会保留恢复所需的父会话。
- 等任务显示 **独立读回校验通过**，点 **下载备份交付包**。保存原始 `.tar`，记录备份 ID、密文 SHA-256 和内容计数，并通过独立渠道保管对应的解密密钥。`.tar` 内已经包含 `.trial` 密文、`.receipt` 回执和交接单。

本次只做备份时，到这一步完成。

## 5. 在目标端导入并恢复

平台完成 5.1 至 5.4 的目标端配置后，ClawManager 管理员按 5.5 导入和恢复。只需要验证恢复内容时，到 5.5 即可。需要用恢复的账号登录并启动实例时，继续执行 5.6。

### 5.1 核对登记，建立目标管理入口

沿用源端使用的 `trial-registration.json`。同集群跨 namespace 时，两端使用相同的集群标识和不同的 namespace。跨集群时，分别填写实际集群标识。以下 PowerShell 命令在试行版代码仓库根目录执行，使用目标集群的 `kubectl` context。

```powershell
$targetContext = '<目标集群的 kubectl context>'
$targetDeploy = '<目标 CM Deployment 名>'
$targetContainer = '<目标 CM 容器名>'
$targetConfig = '.\trial-registration.json'
$targetRegister = '.\scripts\system-backup-trial-register.py'
$target = (Get-Content -Raw -LiteralPath $targetConfig | ConvertFrom-Json).target

$targetNs = $target.namespace
$targetJob = $target.prepare.job_name
$targetRestoreDb = $target.prepare.database

# 首次安装，且目标 namespace 尚未创建时执行
kubectl --context $targetContext create namespace $targetNs
# 已有 namespace 时，核对实际目标
kubectl --context $targetContext get namespace $targetNs
```

目标端涉及两个库。

| 用途 | 配置位置 |
| --- | --- |
| 管理入口库 | 目标 CM 正常安装时使用的 `DB_HOST`、`DB_NAME` 等配置。先用该库启动 CM，管理员在这个入口登录、上传并执行恢复。库名使用普通安装库名，例如 `clawmanager_trial_admin`。 |
| 恢复业务库 | `target.prepare.database` 指定的独立空库，名称以 `cm_drill_trial_` 开头。由准备 Job 安装试行恢复结构，恢复时写入源端用户和实例。 |

先在登记文件中核对 `target.artifact_claim` 和它的 StorageClass、`target.instance_storage_class`、恢复读写 Secret 名、准备 Job 的数据库 UUID。带聊天记录或文件的备份还要登记 `target.workspace_claim` 及其 StorageClass。计划恢复后登录运行入口时，提前登记 `target.runtime_secret`。

用平台的常规安装流程创建目标 CM。目标部署使用 `target.image` 的完整 digest 镜像、`target.service_account` 指定的服务账号，以及独立的管理入口库、登录密钥和观测日志持久卷。若平台通过清单安装，应用已经按目标 namespace 定制的清单：

```powershell
$targetInstallManifest = '<目标端安装清单路径>'
kubectl --context $targetContext -n $targetNs apply -f $targetInstallManifest
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
kubectl --context $targetContext -n $targetNs get deployment $targetDeploy `
  -o 'custom-columns=SA:.spec.template.spec.serviceAccountName,CONTAINERS:.spec.template.spec.containers[*].name,IMAGES:.spec.template.spec.containers[*].image'
```

管理入口库启用了 CM 的数据库 TLS 固定配置时，`DB_TLS_CA_FILE` 指向容器内只读的规范绝对路径普通文件，`DB_TLS_CA_SHA256` 使用该文件原始字节的 SHA-256，`DB_TLS_SERVER_NAME` 匹配数据库证书的 SAN。使用 ConfigMap 提供 CA 时，可用只读单文件 `subPath` 挂载得到普通文件。

确认目标管理员已能登录。启用页面上传需要 Pod 的 `spec.securityContext.fsGroup` 与 `target.prepare.volume_group` 一致，`hostPID` 和 `shareProcessNamespace` 均保持关闭。在平台发布配置中设置这些字段。直接管理 Deployment 且只需补充 `fsGroup` 时，可使用下面的命令：

```powershell
@{spec = @{template = @{spec = @{securityContext = @{
    fsGroup = [int]$target.prepare.volume_group
}}}}} | ConvertTo-Json -Depth 8 | Set-Content -Encoding ascii .\trial-target-pod-group.json

kubectl --context $targetContext -n $targetNs patch deployment $targetDeploy `
  --type=merge --patch-file .\trial-target-pod-group.json
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
```

### 5.2 创建空恢复库和目标端存储，配置凭据

由 DBA 在**独立于源库的目标 MySQL 实例**上创建空恢复库。以下示例使用 MySQL 客户端的 `--login-path`。提前通过 `mysql_config_editor` 或平台凭据管理配置目标 DBA 登录方式，并提供匹配目标 MySQL TLS 证书的 CA。先查服务器 UUID，再建库。 [MySQL TLS 连接选项](https://dev.mysql.com/doc/refman/8.4/en/connection-options.html)

```powershell
$targetDbaLogin = '<目标 DBA 的 MySQL login-path>'
$targetCaFile = '<目标 MySQL 的 CA bundle PEM 文件路径>'
$targetOwnerDsnFile = '<目标恢复库准备账号的受保护 DSN 文件路径>'

$targetMysqlArgs = @("--login-path=$targetDbaLogin", '--ssl-mode=VERIFY_IDENTITY', "--ssl-ca=$targetCaFile")
$actualUuid = mysql @targetMysqlArgs --batch --skip-column-names --execute 'SELECT @@server_uuid'
if ($LASTEXITCODE -ne 0 -or $actualUuid -ne $target.prepare.server_uuid) {
    throw '实际连接的目标 MySQL UUID 与登记不符'
}
if ($targetRestoreDb -cnotmatch '^cm_drill_trial_[a-z0-9_]+$') {
    throw '恢复库名称必须以 cm_drill_trial_ 开头'
}

# 恢复库尚未创建时执行
mysql @targetMysqlArgs --execute "CREATE DATABASE $targetRestoreDb CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
# 准备 Job 首次运行前应返回 0
mysql @targetMysqlArgs "--database=$targetRestoreDb" --execute 'SELECT COUNT(*) AS tables_present FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE()'
```

准备账号要能在目标恢复库执行镜像内置的迁移，并能创建、解锁恢复账号和授予所需权限。它的 DSN 应指向 `$targetRestoreDb`，远程 MySQL 使用 `用户:密码@tcp(MySQL地址:3306)/恢复库名?tls=true` 形式。准备 Job 将生成恢复写账号和用于独立校验的只读账号。登记了 `target.runtime_secret` 时，还会生成独立的运行账号。

目标端至少需要专用导入 PVC。恢复 workspace 文件或 Hermes 聊天时，还需要独立的目标 workspace PVC。以下是动态供卷示例，容量按本次交付包及要恢复的内容调整，访问模式按实际存储能力选取。CM 与准备 Job 都需访问相关 PVC。`target.instance_storage_class` 用于恢复后创建实例自身的存储。

```powershell
$targetClaims = @(
    @{ name = $target.artifact_claim; storageClass = $target.artifact_storage_class; size = '2Gi' }
)
if ($target.workspace_claim) {
    $targetClaims += @{ name = $target.workspace_claim; storageClass = $target.workspace_storage_class; size = '10Gi' }
}

# 仅在需要创建 PVC 时执行。示例采用支持 ReadWriteMany 的 StorageClass
foreach ($claim in $targetClaims) {
    @{
        apiVersion = 'v1'
        kind = 'PersistentVolumeClaim'
        metadata = @{ name = $claim.name; namespace = $targetNs }
        spec = @{
            storageClassName = $claim.storageClass
            accessModes = @('ReadWriteMany')
            resources = @{ requests = @{ storage = $claim.size } }
        }
    } | ConvertTo-Json -Depth 8 | kubectl --context $targetContext -n $targetNs apply -f -
    if ($LASTEXITCODE -ne 0) { throw "创建目标 PVC $($claim.name) 失败" }
}
kubectl --context $targetContext -n $targetNs get pvc

# 凭据和 CA 资源首次创建时执行
kubectl --context $targetContext -n $targetNs create secret generic `
  $target.prepare.owner_dsn_secret "--from-file=dsn=$targetOwnerDsnFile"
kubectl --context $targetContext -n $targetNs create configmap `
  $target.prepare.ca_configmap "--from-file=ca-bundle.crt=$targetCaFile"
```

### 5.3 创建目标准备 Job 并配置解密密钥

```powershell
# 首次创建占位 Secret，准备 Job 会填充对应凭据
py -3 -B $targetRegister --config $targetConfig --side target placeholders |
  kubectl --context $targetContext -n $targetNs create -f -

py -3 -B $targetRegister --config $targetConfig --side target render |
  kubectl --context $targetContext -n $targetNs apply -f -

kubectl --context $targetContext -n $targetNs get job $targetJob -o json |
  py -3 -B $targetRegister --config $targetConfig --side target profile --job - |
  kubectl --context $targetContext -n $targetNs apply -f -
```

Job 此时处于挂起状态。内网镜像仓库需要认证时，给目标准备 Job 的服务账号配置镜像拉取凭据。

- **同集群跨 namespace**。使用第 2.3 节源端登记已生成的权限。源端完成 **准备试行环境** 后，目标准备 Job 会按该权限读取源端试行密钥并写入目标 Secret。
- **跨集群**。源端准备完成后，由平台通过独立的密钥管理渠道交付匹配的试行密钥。在启动目标 Job 前把它写入已经创建的目标 `key_secret`。通过既有密钥同步流程写入时，数据字段为 `key`，原值为 64 个十六进制字符。手动配置时，可用下列命令从受保护的交付文件读取。命令把密钥通过标准输入交给 `kubectl`。

```powershell
$targetKeyFile = '<通过独立渠道交付的受保护密钥文件路径>'
$targetKey = (Get-Content -Raw -LiteralPath $targetKeyFile).Trim()
if ($targetKey -notmatch '^[0-9a-fA-F]{64}$') { throw '试行密钥格式错误' }

@{
    apiVersion = 'v1'
    kind = 'Secret'
    metadata = @{ name = $target.key_secret; namespace = $targetNs }
    data = @{ key = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes($targetKey)) }
} | ConvertTo-Json -Depth 5 |
    kubectl --context $targetContext -n $targetNs apply --server-side -f -
$targetKeyResult = $LASTEXITCODE
Remove-Variable targetKey
if ($targetKeyResult -ne 0) { throw '写入目标试行密钥失败' }
```

已填充的密钥 Secret 要保留。解密材料和 `.tar` 交付包分别交付。 [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)

### 5.4 配置上传接收器和内容恢复

用 `--artifact-import` 生成目标 CM 补丁。它添加目标上传接收器、归档 PVC 和环境配置。接收器写入导入卷，CM 从只读挂载校验和恢复。Helm 或 GitOps 管理的环境把补丁描述的配置合入原发布配置后发布。直接管理 Deployment 时执行以下命令。

```powershell
$targetPatchFile = '.\trial-target-app-patch.json'

$targetPatch = kubectl --context $targetContext -n $targetNs get deployment $targetDeploy -o json |
  py -3 -B $targetRegister --config $targetConfig --side target app-patch `
    --deployment - --container $targetContainer --artifact-import

if ($LASTEXITCODE -ne 0 -or -not $targetPatch) { throw '生成目标 Deployment 补丁失败' }
$targetPatch | Set-Content -LiteralPath $targetPatchFile -Encoding ascii

kubectl --context $targetContext -n $targetNs patch deployment $targetDeploy `
  --type=json --patch-file $targetPatchFile
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
```

如果补丁应用时遇到 Deployment 版本变更，重新获取 Deployment 并生成补丁。入口经过反向代理时，上传请求体大小和超时需允许本次交付包通过。

登记了 `target.workspace_claim` 后，补丁会自动挂载目标工作区，并配置 `SYSTEM_BACKUP_TRIAL_WORKSPACE_FILES_TARGET_ROOT`。交付包包含 Hermes 聊天时，还需在目标 CM Pod 内启用 Hermes 恢复适配器，使用与源端采集兼容且固定摘要的 Hermes 镜像。先创建脚本 ConfigMap：

```powershell
kubectl --context $targetContext -n $targetNs create configmap trial-target-hermes `
  --from-file=history.py=.\scripts\system-backup-trial-hermes-history.py
```

将以下配置合入目标 Deployment。直接管理 Deployment 时，保存为 `trial-target-hermes-patch.yaml`，替换 CM 容器名、镜像和运行 UID/GID 后应用。适配器和 CM 使用补丁已配置的 `trial-app-workspace` 卷。UID 需能按目标 Hermes 实例的身份写入工作区，GID 与 `target.prepare.volume_group` 一致。

```yaml
spec:
  template:
    spec:
      volumes:
        - name: trial-target-hermes-script
          configMap:
            name: trial-target-hermes
        - name: trial-target-hermes-socket
          emptyDir: {}
        - name: trial-target-hermes-no-token
          emptyDir: {}
      containers:
        - name: <CM 容器名>
          env:
            - name: SYSTEM_BACKUP_TRIAL_HERMES_HISTORY_TARGET_ROOT
              value: /var/run/clawmanager-trial/workspace
            - name: SYSTEM_BACKUP_TRIAL_HERMES_HISTORY_SOCKET
              value: /run/trial-history/history.sock
          volumeMounts:
            - name: trial-target-hermes-socket
              mountPath: /run/trial-history
              readOnly: true
        - name: trial-target-hermes-history
          image: <兼容的 Hermes 镜像@sha256:摘要>
          command: ["/opt/hermes-agent/.venv/bin/python", "-B", "/trial-script/history.py"]
          args: ["serve", "--workspace-root", "/var/run/clawmanager-trial/workspace", "--socket", "/run/trial-history/history.sock"]
          env:
            - name: PYTHONPATH
              value: /opt/hermes-agent
          securityContext:
            runAsNonRoot: true
            runAsUser: <目标 Hermes 工作区 UID，整数>
            runAsGroup: <target.prepare.volume_group，整数>
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
            seccompProfile:
              type: RuntimeDefault
          volumeMounts:
            - name: trial-target-hermes-script
              mountPath: /trial-script
              readOnly: true
            - name: trial-target-hermes-socket
              mountPath: /run/trial-history
            - name: trial-app-workspace
              mountPath: /var/run/clawmanager-trial/workspace
            - name: trial-target-hermes-no-token
              mountPath: /var/run/secrets/kubernetes.io/serviceaccount
```

```powershell
kubectl --context $targetContext -n $targetNs patch deployment $targetDeploy `
  --type=strategic --patch-file .\trial-target-hermes-patch.yaml
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
```

### 5.5 在目标页面准备、导入和恢复

1. 打开目标管理入口的 **备份**（`/admin/backups/records`）。点击 **检查试行就绪状态**，展开 **查看源和目标配置**，确认当前是登记的目标端。
2. 首次点击 **准备试行环境**。等待 Job 完成，再检查目标库读写权限、解密密钥、导入卷和上传接收器。需要在命令行跟踪 Job 时可执行以下命令。

   ```powershell
   kubectl --context $targetContext -n $targetNs wait --for=condition=complete "job/$targetJob" --timeout=10m
   kubectl --context $targetContext -n $targetNs logs "job/$targetJob"
   ```

3. 点击 **导入备份交付包**，选择源端下载的原始 `.tar`。页面会填入备份 ID，并绑定源端的密文摘要。
4. 点击 **检查此备份**，核对备份 ID、密文 SHA-256 和预期内容，确认显示 **产物读回和恢复内容预检通过**。
5. 点击 **恢复到隔离环境**。等待任务显示 **独立读回校验通过**，记录恢复任务 ID 和内容计数。下一节使用的是页面上的**备份 ID**。

同集群跨 namespace 和跨集群均可导入同一种 `.tar`，导入成功后即可预检。交付包绑定导出时登记的源端和目标端。若换了目标集群、namespace 或导入卷，先在源、目标两端更新登记并核对配置，再从源端为新目标重新下载交付包。核对重新导出的备份 ID 和密文 SHA-256 与原备份一致。通过其他方式传输备份原件时，在源端展开 **单独传输备份原件** 下载交接单。将同一备份的 `.trial` 密文和 `.receipt` 回执放入目标导入卷，再在目标端展开 **已有备份原件**，点击 **选择恢复交接单** 导入 JSON。

### 5.6 使用恢复的账号和实例

要用恢复后的账号登录，需要启用隔离运行入口。平台可另行发布运行入口，或在保留管理入口配置的前提下，把本次试行的目标 CM 切换到运行模式。以下示例是切换目标 CM 的配置。

需要让旧版和新版两个运行入口同时工作时，分别使用独立的运行 namespace 和工作区。运行调度器在各自 namespace 使用固定的 `clawmanager-runtime-scheduler` Lease。

确认 5.5 的恢复校验已通过，且目标登记包含 `runtime_secret`。保留目标只读归档挂载、密钥、恢复复核账号和所需工作区。Kubernetes namespace、实例 namespace、实例与工作区 StorageClass 都要符合目标登记。运行配置关闭企业认证、Northbound、IEI 和旧备份功能开关。为目标运行入口配置自己的访问地址。目标环境尚未部署运行池时，按 5.7 接入并启用调度。使用支持隔离运行模式的试行镜像。运行账号只授予目标业务库的 SELECT、INSERT、UPDATE 和 DELETE。该模式只配置历史归档读服务，跳过月表 DDL 和归档维护任务。

直接管理 Deployment 时，将以下配置保存为 `trial-target-runtime-patch.yaml`。替换 CM 容器名、备份 ID、恢复库、目标 namespace、StorageClass 和运行 Secret。以下示例包含工作区卷。把 `RUNTIME_WORKSPACE_PVC_CLAIM` 的值替换为 `target.workspace_claim`，`RUNTIME_WORKSPACE_ROOT` 使用 5.4 的挂载路径。仅恢复账号和实例且未登记工作区卷时，去掉 `RUNTIME_WORKSPACE_PVC_CLAIM` 这一项，并把 `RUNTIME_WORKSPACE_ROOT` 指向目标运行配置已有的工作区挂载。环境变量在原 Deployment 中通过 `valueFrom` 注入时，应用这些新值前移除对应的旧引用。

```yaml
spec:
  template:
    spec:
      volumes:
        - name: trial-runtime-dsn
          secret:
            secretName: <target.runtime_secret>
            defaultMode: 288
            items:
              - key: dsn
                path: dsn
      containers:
        - name: <CM 容器名>
          env:
            - name: DB_NAME
              valueFrom: null
              value: <target.prepare.database>
            - name: K8S_NAMESPACE
              value: <目标 namespace>
            - name: RUNTIME_NAMESPACE
              value: <目标 namespace>
            - name: K8S_STORAGE_CLASS
              value: <target.instance_storage_class>
            - name: K8S_INSTANCE_STORAGE_CLASS
              value: <target.instance_storage_class>
            - name: K8S_WORKSPACE_STORAGE_CLASS
              value: <target.instance_storage_class>
            - name: RUNTIME_WORKSPACE_ROOT
              value: /var/run/clawmanager-trial/workspace
            - name: RUNTIME_WORKSPACE_PVC_CLAIM
              value: <target.workspace_claim>
            - name: AUTH_ENTERPRISE_ENABLED
              value: "false"
            - name: CLAWMANAGER_NORTHBOUND_ENABLED
              value: "false"
            - name: IEISYSTEM_SSO_ENABLED
              value: "false"
            - name: SYSTEM_BACKUP_SQL_GATE_SCOPE
              value: ""
            - name: SYSTEM_BACKUP_READ_SESSIONS_ENABLED
              value: "false"
            - name: SYSTEM_BACKUP_READ_LIVE_ENABLED
              value: "false"
            - name: SYSTEM_BACKUP_CONFIG_MUTATION_ENABLED
              value: "false"
            - name: SYSTEM_BACKUP_V2_EXECUTION_ENABLED
              value: "false"
            - name: SYSTEM_BACKUP_TRIAL_RUNTIME_BACKUP_ID
              value: "<恢复已验证的备份 ID>"
            - name: SYSTEM_BACKUP_TRIAL_RUNTIME_DSN_FILE
              value: /var/run/trial-runtime/dsn
          volumeMounts:
            - name: trial-runtime-dsn
              mountPath: /var/run/trial-runtime
              readOnly: true
```

```powershell
kubectl --context $targetContext -n $targetNs patch deployment $targetDeploy `
  --type=strategic --patch-file .\trial-target-runtime-patch.yaml
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
```

使用恢复后的账号在目标运行入口登录。核对账号所属的实例，按需启动实例，再检查 Hermes 聊天内容、顺序及 workspace 文件。恢复后的实例初始为停止状态。可以再用另一个普通账号核对实例访问范围。

### 5.7 空目标环境接入 Hermes Lite 运行池

同一份备份里选中的实例会随本次任务一起恢复，保留各自的类型和模式。运行依赖按类型配置。以下命令用于 Hermes Lite。目标包含 OpenClaw Lite、OpenCode Lite 或 DeepSeek Harness Lite 时，分别按标准部署清单配置对应的池。按现场使用的类型和模式抽样启动，再核对本次备份有纳入的聊天和文件。

恢复后的实例初始为 **停止**。空目标环境需要先配置运行池，账号登录、实例启动和内容读取才构成完整验收。5.4 的 Hermes 恢复适配器负责恢复聊天数据。这里的 `hermes-runtime` 是供调度器启动实例的独立 Deployment。

以下 PowerShell 示例接续 5.1 和 5.6 的变量，`$targetDeploy` 指向目标**运行入口**的 Deployment。由 Helm 或 GitOps 管理时，把下面的资源和环境变量合入目标发布配置。目标已有同类型池时，核对工作区、凭据、镜像和回连配置，复用已就绪的目标池。

#### 准备目标工作区、内部服务和凭据

- 目标 CM、Hermes 恢复适配器和运行池使用同一个 `target.workspace_claim`。运行池和 CM 必须使用同一个 PVC，并把 PVC 挂到**相同的绝对路径**。调度器会把 CM 中的实例工作区绝对路径传给运行池。存储需支持两类 Pod 的访问方式，并保留已恢复文件的 UID/GID。
- 使用池 Pod 能访问的 **目标 CM 内部 Service**。标准部署使用 HTTP 9001。核对 Service 的 EndpointSlice 指向目标运行入口。导入管理入口独立部署时，运行池回连运行入口。启用新版的受管代理策略时，目标 namespace 还要有名为 `clawmanager-frontend` 的 Service，其选择器指向目标 CM Pod，调度器用它发现需信任的代理地址。
- CM 和池从同一个目标 Secret 读取控制 token 和上报 token。平台已有对应的 Secret 时可复用。需要手动创建时，从受保护的文件导入目标专用凭据。
- 准备目标可访问的独立 Redis，供 CM 运行协调使用。通过 Secret 给 CM 配置 `PLATFORM_REDIS_URL`，已有对应目标 Secret 时可沿用。
- 首次启动前，在目标运行入口的 **AI 网关 → 模型**（`/admin/models`） 保存并启用可用的聊天模型，确认实例归属的恢复账号有使用权限。试行备份会保留实例启动配置中的模型名称或引用，平台模型目录（`llm_models`）、其中的提供商凭据和访问策略需要在目标单独配置。实例显式引用某个模型时，核对目标可用的模型标识。模型网关地址使用目标内部服务。

```powershell
$poolName = 'hermes-runtime'
$poolSecret = 'trial-hermes-runtime-agent'
$poolImage = '<目标集群可拉取的 Hermes Lite 镜像@sha256:摘要>'
$poolWorkspace = $target.workspace_claim
$poolWorkspaceRoot = '<目标 CM 的 RUNTIME_WORKSPACE_ROOT，使用 5.6 的实际路径>'
$poolCmService = '<目标运行入口的 Kubernetes Service 名>'
$poolCmUrl = "http://$poolCmService.$targetNs.svc.cluster.local:9001"
$poolProxyCidrs = '<覆盖目标 CM 代理 Pod IP 的受信 CIDR>'
$poolControlFile = '<目标控制 token 文件路径，单行无结尾换行>'
$poolReportFile = '<目标上报 token 文件路径，单行无结尾换行>'
$poolRedisSecret = 'trial-runtime-redis'
$poolRedisKey = 'redis-url'
$poolRedisFile = '<目标 Redis URL 文件路径，单行无结尾换行>'

# 目标 CM 监听标准 API 端口且还没有内部 Service 时，先创建
# kubectl --context $targetContext -n $targetNs expose deployment $targetDeploy --name=$poolCmService --type=ClusterIP --port=9001 --target-port=9001

$poolFrontendPort = '8443' # 按目标 CM 实际 HTTPS 端口调整
# 尚无 clawmanager-frontend Service 时执行，选择器继承自目标 CM Deployment
# kubectl --context $targetContext -n $targetNs expose deployment $targetDeploy --name=clawmanager-frontend --type=ClusterIP --port=443 --target-port=$poolFrontendPort

kubectl --context $targetContext -n $targetNs get svc clawmanager-frontend
kubectl --context $targetContext -n $targetNs get pvc $poolWorkspace
kubectl --context $targetContext -n $targetNs get svc $poolCmService
kubectl --context $targetContext -n $targetNs get endpointslice -l "kubernetes.io/service-name=$poolCmService"

# 仅在首次创建时执行
kubectl --context $targetContext -n $targetNs create secret generic $poolSecret `
  "--from-file=runtime-agent-control-token=$poolControlFile" `
  "--from-file=runtime-agent-report-token=$poolReportFile"

# 尚未配置目标 Redis URL Secret 时执行
kubectl --context $targetContext -n $targetNs create secret generic $poolRedisSecret "--from-file=$poolRedisKey=$poolRedisFile"
```

网络策略应允许池 Pod 访问目标 CM API，以及目标 CM 访问运行池的 Agent 19090 和所配置的网关端口。保留目标 namespace 的 DNS 通路。新版 Hermes Lite 要求 `CLAWMANAGER_BACKEND_URL` 使用 Kubernetes Service DNS。Agent 报 `clock_reference_unavailable` 时，从运行池所在网络对同一地址执行 `curl -I --noproxy '*' --max-time 3 <目标 CM Service URL>`，检查是否收到 HTTP `Date`，并核对连接用的协议、端口和节点时间。受信 CIDR 按实际代理流量填写。运行池需要私有仓库凭据时，在其 Pod 发布配置中添加 `imagePullSecrets`。

#### 核对目标调度权限

标准安装已有目标调度权限时，复用现有绑定。缺少基本权限时，给登记的目标 CM 服务账号添加 namespace 内的发现、扩缩容和选主权限。模型和 Redis 等依赖按运行入口的实际配置提供目标端点。

```powershell
@"
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: trial-hermes-runtime-scheduler
  namespace: $targetNs
rules:
  - apiGroups: [""]
    resources: ["pods", "endpoints"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["services"]
    resourceNames: ["clawmanager-frontend"]
    verbs: ["get"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments/scale"]
    resourceNames: ["$poolName"]
    verbs: ["get", "update", "patch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: trial-hermes-runtime-scheduler
  namespace: $targetNs
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: trial-hermes-runtime-scheduler
subjects:
  - kind: ServiceAccount
    name: $($target.service_account)
    namespace: $targetNs
"@ | kubectl --context $targetContext -n $targetNs apply -f -
```

#### 配置目标 CM 的运行池凭据

先给目标 CM 配置 Agent 凭据和 Hermes Web 控制入口，让运行池发布后能完成上报。此时保持调度关闭。保留 5.6 的 `RUNTIME_WORKSPACE_ROOT` 和恢复业务库配置。

```powershell
$runtimeEnv = @(
    @{ name = 'RUNTIME_NAMESPACE'; value = $targetNs; valueFrom = $null }
    @{ name = 'RUNTIME_WORKSPACE_PVC_CLAIM'; value = $poolWorkspace; valueFrom = $null }
    @{ name = 'RUNTIME_SCHEDULER_ENABLED'; value = 'false'; valueFrom = $null }
    @{ name = 'HERMES_RUNTIME_IMAGE'; value = $poolImage; valueFrom = $null }
    @{ name = 'CLAWMANAGER_CONTROL_UI_ORIGIN'; value = $poolCmUrl; valueFrom = $null }
    @{ name = 'CLAWMANAGER_LLM_GATEWAY_BASE_URL'; value = "$poolCmUrl/api/v1/gateway/llm"; valueFrom = $null }
    @{
        name = 'PLATFORM_REDIS_URL'
        value = $null
        valueFrom = @{ secretKeyRef = @{ name = $poolRedisSecret; key = $poolRedisKey } }
    }
    @{
        name = 'RUNTIME_AGENT_CONTROL_TOKEN'
        value = $null
        valueFrom = @{ secretKeyRef = @{ name = $poolSecret; key = 'runtime-agent-control-token' } }
    }
    @{
        name = 'RUNTIME_AGENT_REPORT_TOKEN'
        value = $null
        valueFrom = @{ secretKeyRef = @{ name = $poolSecret; key = 'runtime-agent-report-token' } }
    }
)
$runtimeContainer = @{ name = $targetContainer; env = $runtimeEnv }
$runtimePatch = @{ spec = @{ template = @{ spec = @{ containers = @($runtimeContainer) } } } }
$runtimePatchFile = '.\trial-target-hermes-scheduler.json'
$runtimePatch | ConvertTo-Json -Depth 16 | Set-Content -LiteralPath $runtimePatchFile -Encoding ascii

kubectl --context $targetContext -n $targetNs patch deployment $targetDeploy `
  --type=strategic --patch-file $runtimePatchFile
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
```

#### 发布目标 Hermes Lite 运行池

要验收 Hermes 聊天页，运行池镜像需与目标 CM 的 Hermes Desktop Web 契约兼容。当前分支固定 Hermes `v0.22.5` 及其源码提交，实际要求以 [CM 的版本锁](../hermes-desktop-web/upstream.lock.json) 为准。用目标环境已验收的 digest，并核对 Agent 上报的 `hermes_desktop_web` 能力、版本、源码提交和 RPC 协议。池 Pod Ready、实例 Running 和聊天页可用分别检查。 [运行 Agent 契约](clawmanager-agent-v2-contract.md)

从旧版 Hermes 恢复到新版时，源端用兼容源版本的采集镜像取得会话数据。目标端先准备空的 Hermes 工作区，用与目标 CM 配对的 Hermes 镜像运行 5.4 的恢复适配器，由新版原生接口导入会话。核对数量、顺序和独立读回后，在同一目标工作区启动新版运行池。保留原工作区以便核对和回退。旧版的 `state.db` 无需用新版池原地打开。

下列命令从仓库标准清单提取 **hermes-runtime** Deployment，复用其 Agent、网关端口和 Pod 身份配置。生成的文件仅包含该 Deployment，并替换镜像、回连、工作区与 Secret 引用。 [Hermes Lite 标准部署清单](../deployments/k8s/cluster/clawmanager.yaml)

```powershell
$standard = Get-Content -Raw -LiteralPath .\deployments\k8s\cluster\clawmanager.yaml
$poolTemplates = @($standard -split '(?m)^---[ \t]*\r?$' | Where-Object {
    $_ -match '(?m)^kind: Deployment\r?$' -and
    $_ -match '(?m)^metadata:\r?\n  name: hermes-runtime\r?$'
})
if ($poolTemplates.Count -ne 1) { throw '未找到唯一的标准 Hermes Deployment' }

$poolJson = $poolTemplates[0] |
    kubectl --context $targetContext create --dry-run=client --validate=false -f - -o json
if ($LASTEXITCODE -ne 0 -or -not $poolJson) { throw '解析标准 Hermes Deployment 失败' }

$poolSpec = $poolJson | Out-String | ConvertFrom-Json
$poolSpec.metadata.namespace = $targetNs
$poolPod = $poolSpec.spec.template.spec
$poolContainer = $poolPod.containers | Where-Object name -eq 'runtime'
$poolContainer.image = $poolImage
($poolPod.volumes | Where-Object name -eq 'workspaces').persistentVolumeClaim.claimName = $poolWorkspace
($poolContainer.volumeMounts | Where-Object name -eq 'workspaces').mountPath = $poolWorkspaceRoot

$poolEnv = @{
    CLAWMANAGER_BACKEND_URL = $poolCmUrl
    CLAWMANAGER_CONTROL_UI_ORIGIN = $poolCmUrl
    CLAWMANAGER_TRUSTED_PROXY_CIDRS = $poolProxyCidrs
    CLAWMANAGER_RUNTIME_IMAGE_REF = $poolImage
    RUNTIME_WORKSPACE_ROOT = $poolWorkspaceRoot
}
foreach ($entry in $poolContainer.env) {
    if ($poolEnv.ContainsKey($entry.name)) { $entry.value = $poolEnv[$entry.name] }
    if ($entry.valueFrom.secretKeyRef) { $entry.valueFrom.secretKeyRef.name = $poolSecret }
}

$poolManifest = '.\trial-target-hermes-pool.json'
$poolSpec | ConvertTo-Json -Depth 32 | Set-Content -LiteralPath $poolManifest -Encoding ascii
kubectl --context $targetContext -n $targetNs apply --dry-run=server -f $poolManifest
if ($LASTEXITCODE -ne 0) { throw '目标运行池清单预检失败' }
kubectl --context $targetContext -n $targetNs apply -f $poolManifest
kubectl --context $targetContext -n $targetNs rollout status "deployment/$poolName"
```

#### 启用目标调度并验证实例

运行池 Pod 和目标 CM 均就绪后，为目标运行入口启用调度。直接管理 Deployment 时执行以下命令：

```powershell
kubectl --context $targetContext -n $targetNs set env "deployment/$targetDeploy" `
  --containers=$targetContainer RUNTIME_SCHEDULER_ENABLED=true
kubectl --context $targetContext -n $targetNs rollout status "deployment/$targetDeploy"
kubectl --context $targetContext -n $targetNs get pods -l 'clawmanager.io/runtime-type=hermes'
```

在 CM 的运行池页面确认目标 Hermes Pod 已上报且可调度。用恢复后的账号对一个 Hermes Lite 实例点一次 **启动**，等实例 Running、网关可用，再打开原会话和文件。若状态提示模型配置缺失，先在目标配置可用的模型路由，再启动验证。记录实例 ID、启动状态、会话读回和文件校验结果。

## 常见问题

| 现象 | 怎么处理 |
| --- | --- |
| 备份或恢复按钮不可用 | 检查当前端的就绪和准备任务状态，按缺项联系平台管理员。 |
| 找不到某场会话或某个目录 | 检查是否选择了对应实例，再核对平台登记的可采集范围和该内容在源端是否可读。 |
| 导入提示交付包无效或源和目标不匹配 | 按 5.5 核对交付包的目标登记，必要时从源端为当前目标重新导出。 |
| 启动时报 `no active models are configured` | 按 5.7 在目标端启用一个实例归属账号可用的聊天模型，然后重新点击启动。 |
| 导入后摘要不一致 | 核对源端下载的原始包、目标登记和备份 ID。用原始交付包重新预检。 |
| 目标已有业务数据 | 由平台准备新的空隔离目标，确认好环境后再恢复。 |
| Hermes 页面从父会话入口打开续会话 | 这是 Hermes 的会话展示方式。选定续会话的内容可在该入口核对。 |
| 需要复验或提报故障 | 保留备份 ID、任务 ID、摘要和配置检查报告。通过独立渠道交接测试账号凭据或密钥。 |
