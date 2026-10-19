# ClawManager 北向接口版本升级说明

本文说明如何把已有 ClawManager 升级到本分支的北向接口版本，包含 OpenClaw、Hermes、
OpenCode、DeepSeek Harness 的 Lite/Pro 实例、owner 隔离和智慧协作平台单点登录页面。本文适用于仓库中的 Kubernetes 和 K3s 部署；自定义
部署可按文末的组件清单完成等价升级。

升级采用增量方式，不需要重建现有 MySQL、Workspace PVC 或 Lite Runtime。现有用户、
实例和 ShareLink 都会保留，现有用户可以直接使用原用户名和密码完成北向 JWE 登录。

> 不要把新版 `deployments/k8s/*/clawmanager.yaml` 直接覆盖应用到已运行的定制集群。
> 完整清单包含默认 Secret、存储和基础组件，可能覆盖现场配置。升级时只更新应用镜像，
> 再应用 `deployments/k8s/northbound/` 下的北向增量资源。

## 1. 升级内容

本次升级增加以下组件和数据结构：

| 项目 | 变更 | 升级影响 |
| --- | --- | --- |
| Core 应用 | 增加北向内部服务和异步 Operation Worker | 与现有管理页面共用应用镜像；内部端口为 `9002` |
| 北向 Gateway | 新增独立进程 `clawreef-northbound-gateway` | 唯一新增的对外入口，NodePort 为 `38443` |
| 数据库 | 自动执行北向、owner 与 Runtime ENUM 迁移 | 新增北向表、owner 字段并保留所有现有 Runtime 类型 |
| 登录 | 新增一次性挑战和 JWE 登录 | 兼容现有用户；用户名和密码不会作为明文请求字段传输 |
| Lite 实例 | 新增异步创建、查询接口 | 仅操作当前登录用户自己的 Lite 实例 |
| ShareLink | 新增启用、重置 URL、重置密码接口 | 不会自动开启现有或新建实例的 ShareLink |
| IEI 页面 | 新增 `/ieisystem/list-instances` | 使用平台 AES Token 换取独立会话，并按 owner 展示受支持的 Lite/Pro 实例 |

北向 Gateway 只通过 mTLS 访问 Core 的 `9002` 端口。不要对外暴露 Core `9002`、后端
`9001` 或数据库 `3306`。

## 2. 升级前检查

以下示例假设：

- Namespace 为 `clawmanager-system`；
- Core Deployment 和容器名均为 `clawmanager-app`；
- MySQL Pod 带有标签 `app=mysql`，数据库名为 `clawmanager`；
- 从仓库根目录执行命令；
- 操作终端为 PowerShell。

现场名称不同时，应替换命令中的对应值。

### 2.1 记录当前版本和资源状态

```powershell
$Namespace = "clawmanager-system"
$PreviousImage = kubectl get deployment clawmanager-app -n $Namespace -o jsonpath="{.spec.template.spec.containers[?(@.name=='clawmanager-app')].image}"
$PreviousImage

kubectl get pods -n $Namespace
kubectl rollout status deployment/clawmanager-app -n $Namespace --timeout=5m
kubectl get deployment clawmanager-app -n $Namespace -o yaml | Out-File -Encoding utf8 .\clawmanager-app.pre-northbound.yaml
```

把 `$PreviousImage` 和 Deployment 备份文件保存到受控位置。备份文件可能包含现场配置，
不得提交到 Git。

### 2.2 备份数据库

升级会自动执行数据库迁移，必须先生成可恢复的完整备份：

```powershell
$MySqlPod = kubectl get pod -n $Namespace -l app=mysql -o jsonpath="{.items[0].metadata.name}"
$BackupName = "clawmanager-pre-northbound-$(Get-Date -Format yyyyMMdd-HHmmss).sql"

kubectl exec -n $Namespace $MySqlPod -- sh -c 'mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers --events clawmanager > /tmp/clawmanager-pre-northbound.sql'
kubectl cp "${Namespace}/${MySqlPod}:/tmp/clawmanager-pre-northbound.sql" ".\$BackupName"
kubectl exec -n $Namespace $MySqlPod -- rm -f /tmp/clawmanager-pre-northbound.sql
Get-Item ".\$BackupName"
```

应把备份复制到集群之外的受控存储，并按现有恢复流程验证备份可用。Workspace、对象存储
和其他业务数据仍按现场灾备策略备份。

### 2.3 检查 NodePort 和网络条件

仓库清单使用 `38443` 作为 NodePort。Kubernetes 默认 NodePort 范围通常不包含该端口；
API Server 或 K3s 必须配置一个包含 `38443` 的范围，例如 `30000-40000`。如果不能修改
范围，应把 Service 改为 `ClusterIP`，通过负载均衡器或 Ingress 在外部监听 `38443`。

同时确认：

- 防火墙或 WAF 只允许合作方地址访问 `38443`；
- CNI 支持并执行 Kubernetes `NetworkPolicy`；
- 北向域名或节点 IP 已确定，Gateway 服务端证书的 SAN 必须包含调用方实际使用的名称或 IP；
- 集群节点、调用端和可信时间源的时钟处于合理范围。

## 3. 准备新版镜像

Core 和 Gateway 必须使用同一个不可变镜像标签或镜像摘要。该镜像必须同时包含：

```text
/usr/local/bin/clawreef-server
/usr/local/bin/clawreef-northbound-gateway
```

从目标提交构建并推送镜像，示例：

```powershell
$Image = "<registry>/clawmanager:northbound-owner-ieisystem"
docker build --pull -t $Image .
docker push $Image
```

不要使用会被重复覆盖的标签进行生产升级。私有仓库还应提前配置相应的
`imagePullSecrets`。

## 4. 先升级 Core 并执行数据库迁移

先只更新现有 Core 镜像，此时不要部署 Gateway：

```powershell
kubectl set image deployment/clawmanager-app -n $Namespace clawmanager-app=$Image
kubectl rollout status deployment/clawmanager-app -n $Namespace --timeout=10m
kubectl get pods -n $Namespace -l app=clawmanager-app -o wide
```

新版 Core 启动时会按顺序执行所有尚未记录的嵌入式迁移，并把结果写入
`schema_migrations`。不要在 Core 成功启动后再次手工执行 `045_add_northbound_api.sql`
或 `047_add_instance_owner.sql`。

验证迁移时先进入 MySQL：

```powershell
kubectl exec -it -n $Namespace $MySqlPod -- sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" clawmanager'
```

然后执行：

```sql
SELECT filename, applied_at
FROM schema_migrations
WHERE filename IN ('045_add_northbound_api.sql', '047_add_instance_owner.sql', '055_reconcile_instance_type_enum.sql')
ORDER BY filename;

SHOW TABLES LIKE 'northbound_%';
SHOW COLUMNS FROM instances LIKE 'type';
```

预期结果包含迁移 `045_add_northbound_api.sql`、`047_add_instance_owner.sql`、
`055_reconcile_instance_type_enum.sql`，`instances.type` 至少包含 `openclaw`、`hermes`、
`opencode` 和 `deepseek-harness`，以及以下
三张表：

```text
northbound_auth_challenges
northbound_sessions
northbound_operations
```

如果 Core 启动或迁移失败，应停止升级并检查日志，不要启动 Gateway：

```powershell
kubectl logs -n $Namespace deployment/clawmanager-app --since=15m
```

## 5. 创建北向专用数据库账号

Gateway 不执行迁移，也不应使用 Core 的数据库账号。迁移成功后，以数据库管理员身份
登录 MySQL：

```powershell
kubectl exec -it -n $Namespace $MySqlPod -- sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" clawmanager'
```

执行以下 SQL，并把示例密码替换为独立生成的强密码：

```sql
CREATE USER IF NOT EXISTS 'clawmanager_northbound'@'%' IDENTIFIED BY '<strong-random-password>';
ALTER USER 'clawmanager_northbound'@'%' IDENTIFIED BY '<strong-random-password>';

GRANT SELECT ON clawmanager.users TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_admin_settings TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_caller_policies TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_auth_challenges TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_sessions TO 'clawmanager_northbound'@'%';
GRANT INSERT ON clawmanager.audit_events TO 'clawmanager_northbound'@'%';
FLUSH PRIVILEGES;
```

该账号不需要访问 `northbound_operations`、`instances`、实例密钥、Runtime Token、
迁移表或 Kubernetes API；这些业务操作由 Gateway 通过 mTLS 调用 Core 完成。

## 6. 准备密钥和证书

### 6.1 应用密钥

需要三个互不相同、至少 32 字节的随机值：

- `jwt-secret`：签发北向 Access Token；
- `refresh-token-pepper`：保护 Refresh Token；
- `internal-jwt-secret`：Gateway 调用 Core 的内部令牌密钥。

这些值不能复用现有 Web `JWT_SECRET`，并且在多个 Gateway 副本之间必须保持一致。
可以使用 `openssl rand -base64 48` 分别生成。推荐由 Secret 管理系统创建
`clawmanager-northbound-secrets`。如需使用文件导入，文件格式如下：

```dotenv
db-user=clawmanager_northbound
db-password=<strong-random-password>
jwt-secret=<at-least-32-random-bytes>
refresh-token-pepper=<different-at-least-32-random-bytes>
internal-jwt-secret=<another-at-least-32-random-bytes>
```

把文件保存在仓库之外并限制读取权限，然后执行：

```powershell
$NorthboundSecretFile = "<secure-path>/northbound-secrets.env"
kubectl create secret generic clawmanager-northbound-secrets -n $Namespace --from-env-file=$NorthboundSecretFile --dry-run=client -o yaml | kubectl apply -f -
```

导入后应安全移除临时文件。不要应用
`deployments/k8s/northbound/secrets.example.yaml` 中的占位值。

启用智慧协作平台页面时，还需要创建 `clawmanager-iei-sso` Secret。其字段名与仓库模板
一致：`aes-key` 是双方约定的 16 字节 AES 密钥，`aes-iv` 是双方约定的 16 字节系统
标识，`session-secret` 是至少 32 字节的独立随机值。生产值应由 Secret 管理系统注入，
不得写入仓库；字段和环境变量映射见 `deployments/k8s/northbound/secrets.example.yaml`
及 `core-patch.yaml`。

### 6.2 证书和 JWE 密钥

必须准备彼此分离的证书用途：

| Kubernetes Secret | 内容和要求 |
| --- | --- |
| `clawmanager-northbound-gateway-tls` | 公网 Gateway 服务端证书；SAN 包含调用方实际使用的 DNS 或 IP |
| `clawmanager-northbound-gateway-client-tls` | Gateway 访问 Core 的客户端证书，包含 Client Auth 用途 |
| `clawmanager-northbound-core-tls` | Core 服务端证书；SAN 包含 `clawmanager-northbound-core.clawmanager-system.svc.cluster.local` |
| `clawmanager-northbound-core-ca` | 签发或验证 Core 服务端证书的 `ca.crt` |
| `clawmanager-northbound-gateway-client-ca` | 专门验证 Gateway 客户端身份的 `ca.crt` |
| `clawmanager-northbound-jwe` | 至少 3072 位的 RSA 私钥 `private.pem` |

用于 Core mTLS 客户端身份的 CA 应为专用 CA，不能让 Core `9002` 信任通用客户端 CA。
生成 JWE 私钥时直接写入受控目录：

```powershell
$JWEPrivateKey = "<secure-path>/private.pem"
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out $JWEPrivateKey
```

假设证书已由现场 CA 正确签发，先设置文件路径，再创建或更新 Secret：

```powershell
$GatewayCert = "<secure-path>/gateway.crt"
$GatewayKey = "<secure-path>/gateway.key"
$GatewayClientCert = "<secure-path>/gateway-client.crt"
$GatewayClientKey = "<secure-path>/gateway-client.key"
$CoreCert = "<secure-path>/core.crt"
$CoreKey = "<secure-path>/core.key"
$CoreCA = "<secure-path>/core-ca.crt"
$GatewayClientCA = "<secure-path>/gateway-client-ca.crt"

kubectl create secret tls clawmanager-northbound-gateway-tls -n $Namespace --cert=$GatewayCert --key=$GatewayKey --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret tls clawmanager-northbound-gateway-client-tls -n $Namespace --cert=$GatewayClientCert --key=$GatewayClientKey --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret tls clawmanager-northbound-core-tls -n $Namespace --cert=$CoreCert --key=$CoreKey --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic clawmanager-northbound-core-ca -n $Namespace --from-file=ca.crt=$CoreCA --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic clawmanager-northbound-gateway-client-ca -n $Namespace --from-file=ca.crt=$GatewayClientCA --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic clawmanager-northbound-jwe -n $Namespace --from-file=private.pem=$JWEPrivateKey --dry-run=client -o yaml | kubectl apply -f -
```

私钥、数据库密码和应用密钥不得提交到仓库。客户端只分发验证 Gateway 所需的 CA
证书，不分发任何私钥。

## 7. 启用 Core 内部服务

应用增量 Patch，使 Core 在 `9002` 上启动 mTLS 服务：

```powershell
kubectl patch deployment clawmanager-app -n $Namespace --type strategic --patch-file deployments/k8s/northbound/core-patch.yaml
kubectl rollout status deployment/clawmanager-app -n $Namespace --timeout=10m
kubectl logs -n $Namespace deployment/clawmanager-app --since=10m | Select-String "Northbound Core"
```

预期日志包含 Core mTLS 服务在 `:9002` 启动。该 Patch 不创建对外 Service。

## 8. 部署北向 Gateway

`deployments/k8s/northbound/gateway.yaml` 默认镜像是示例 `latest`。应用前必须生成现场副本，
将其中 Gateway Deployment 的 `image` 替换为第 3 节的 `$Image`，不要把现场镜像地址或
Secret 写回公共模板。

先做服务端校验；该步骤也会检查集群是否允许 NodePort `38443`：

```powershell
$GatewayManifest = "<secure-or-temporary-path>/gateway.yaml"
kubectl apply --dry-run=server -f $GatewayManifest
```

校验通过后部署：

```powershell
kubectl apply -f $GatewayManifest
kubectl rollout status deployment/clawmanager-northbound-gateway -n $Namespace --timeout=10m
kubectl get deployment,pod,service,networkpolicy -n $Namespace | Select-String "northbound"
```

预期：

- Gateway 有 2 个 Ready 副本；
- `clawmanager-northbound-gateway` Service 的 NodePort 为 `38443`；
- `clawmanager-northbound-core` 为 `ClusterIP`，端口为 `9002`；
- Core `9002` 只允许带 Gateway 标签的 Pod 访问；
- Gateway 不能访问 Core 的 `9001`。

如果现场有可信反向代理，并由它写入 `X-Forwarded-For`，应在 Gateway Deployment 中把
`NORTHBOUND_TRUSTED_PROXIES` 设置为明确的代理 IP 或 CIDR 列表。不要填写不受控的宽泛
网段；不经过反向代理时保持为空。

## 9. 升级验证

### 9.1 验证 TLS 和挑战接口

从集群外、位于允许名单内的客户端执行：

```powershell
$GatewayCAFile = "<secure-path>/gateway-ca.crt"
$NorthboundBaseURL = "https://<northbound-host>:38443"
curl.exe --cacert $GatewayCAFile -i -X POST "$NorthboundBaseURL/api/northbound/v1/auth/challenge"
```

预期返回 `201 Created`、可信的 `Date` 响应头，以及包含 `challenge_id`、`nonce` 和
`public_jwk` 的 JSON。不得使用 `-k` 或关闭 TLS 校验。

### 9.2 使用现有用户验证登录

按 [北向接口使用说明](./northbound-api-guide.md) 配置 Python 环境和 `.env`，然后执行：

```powershell
python examples/northbound_client.py me
```

预期返回当前用户信息和 Scope。用户名、密码只在客户端内存中用于构造 JWE，不会作为
明文 JSON 字段发送。升级不会创建新的默认用户，也不会修改现有用户密码。

### 9.3 验证 OpenClaw Lite 创建

先使用测试用户和唯一实例名称，再执行：

```powershell
python examples/northbound_client.py create
```

检查创建 Operation 最终为 `succeeded`，取得 `instance_id` 后继续查询，直到实例
`status=running` 且 `availability=available`。如果需要 ShareLink，必须显式启用；默认
`workspace_access=none` 不包含 Workspace 文件。完整调用顺序和参数范围见
[北向接口使用说明](./northbound-api-guide.md)。

### 9.4 验证全部受支持 Runtime

分别设置 `NORTHBOUND_INSTANCE_TYPE` 为 `openclaw`、`hermes`、`opencode` 和
`deepseek-harness`，并通过 `NORTHBOUND_INSTANCE_MODE` 选择 `lite` 或 `pro`。每种组合都应
完成创建 Operation、实例就绪和查询验证；启用 ShareLink 时还需验证 URL、密码和
Workspace 权限边界。调用方不能提交自定义镜像、CPU、内存或磁盘参数。

### 9.5 验证网络边界

正式开放前至少确认：

1. 外部不能访问节点或 Pod 的 `9001`、`9002`；
2. 非允许来源不能访问 `38443`；
3. Gateway 不能通过网络直接调用 Core `9001`；
4. 没有正确 Gateway 客户端证书时，访问 Core `9002` 的 TLS 握手失败；
5. Gateway 日志、Ingress 日志和审计日志中没有用户名密码、Token、JWE 明文或
   ShareLink 密码。

## 10. 回滚

### 10.1 只关闭北向接口

出现北向接口问题但现有管理功能正常时，优先关闭外部入口：

```powershell
kubectl delete deployment/clawmanager-northbound-gateway -n $Namespace
kubectl delete service/clawmanager-northbound-gateway service/clawmanager-northbound-core -n $Namespace
kubectl delete networkpolicy/northbound-gateway-boundaries networkpolicy/northbound-core-ingress -n $Namespace
kubectl set env deployment/clawmanager-app -n $Namespace CLAWMANAGER_NORTHBOUND_ENABLED=false
kubectl rollout status deployment/clawmanager-app -n $Namespace --timeout=10m
```

先保留北向 Secret 和数据库表，便于排障和重新启用。确认不再需要后，再按现场 Secret
销毁流程处理敏感材料。

### 10.2 回滚 Core 镜像

使用第 2.1 节记录的确切旧镜像，不要仅依赖 `latest`：

```powershell
kubectl set image deployment/clawmanager-app -n $Namespace clawmanager-app=$PreviousImage
kubectl rollout status deployment/clawmanager-app -n $Namespace --timeout=10m
```

迁移 `045`、`047` 没有自动 Down Migration。旧版 Core 会忽略新增表和新增的可空字段，
因此普通应用回滚不需要删除数据库结构。不要直接 `DROP` 北向表或 `instances.owner`
字段；这样可能破坏北向会话、Operation 记录、owner 隔离以及已经提交的实例创建关联。

只有必须完整恢复到升级前数据库状态时，才在维护窗口内使用第 2.2 节的备份恢复。完整
恢复会丢失备份之后产生的所有业务数据，包括通过页面或北向接口创建的实例记录。

## 11. 自定义部署对应关系

不使用仓库 Kubernetes 清单时，仍必须保持以下边界：

```text
调用方 -> HTTPS :38443 -> northbound Gateway
northbound Gateway -> mTLS :9002 -> ClawManager Core
northbound Gateway -> 最小权限账号 -> MySQL
ClawManager Core -> 现有高权限数据库账号和 Kubernetes Runtime 管理能力
```

关键环境变量对应关系如下；证书路径必须是进程容器内的可读路径：

| 进程 | 必需配置 |
| --- | --- |
| Core | `CLAWMANAGER_NORTHBOUND_ENABLED=true`、`NORTHBOUND_CORE_INTERNAL_ADDRESS=:9002`、`NORTHBOUND_INTERNAL_JWT_SECRET`、`NORTHBOUND_CORE_TLS_CERT_FILE`、`NORTHBOUND_CORE_TLS_KEY_FILE`、`NORTHBOUND_CORE_CLIENT_CA_FILE` |
| Gateway | `CLAWMANAGER_NORTHBOUND_ENABLED=true`、`NORTHBOUND_GATEWAY_ADDRESS=:9443`、`NORTHBOUND_CORE_BASE_URL`、`NORTHBOUND_GATEWAY_TLS_CERT_FILE`、`NORTHBOUND_GATEWAY_TLS_KEY_FILE`、`NORTHBOUND_GATEWAY_CLIENT_CERT_FILE`、`NORTHBOUND_GATEWAY_CLIENT_KEY_FILE`、`NORTHBOUND_CORE_CA_FILE`、`NORTHBOUND_JWE_PRIVATE_KEY_FILE`、`NORTHBOUND_JWE_KEY_ID`、`NORTHBOUND_JWT_SECRET`、`NORTHBOUND_REFRESH_TOKEN_PEPPER`、`NORTHBOUND_INTERNAL_JWT_SECRET`，以及最小权限 `DB_*` 配置 |

Core 与 Gateway 的 `NORTHBOUND_INTERNAL_JWT_SECRET` 必须完全一致；Gateway 的
`9443` 是容器内部监听端口，对外仍通过 Service、负载均衡器或 Ingress 发布为 `38443`。
只有经过可信反向代理时才配置 `NORTHBOUND_TRUSTED_PROXIES`。

启动顺序固定为：

1. 备份数据库并部署包含迁移的新版 Core；
2. 确认迁移 `045`、`047` 成功；
3. 配置 Core `9002` 的 mTLS；
4. 使用最小权限数据库账号启动 Gateway；
5. 只暴露 Gateway `38443`；
6. 完成 TLS、登录、创建和网络负向验证。

仓库的 Docker Compose 文件面向本地开发，没有完整配置生产所需的独立 Gateway、证书、
mTLS 和网络边界，不应直接作为对外北向接口的生产升级方案。
