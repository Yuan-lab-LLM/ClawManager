# ClawManager 北向接口设计

- 状态：首版实现完成，待测试环境安全验收
- 日期：2026-08-10
- 范围：通过北向 API 自动创建和查询 Lite 实例

## 1. 背景与目标

ClawManager 当前已经提供面向管理前端的实例接口，例如 `POST /api/v1/instances`，并通过现有 `InstanceService` 完成用户配额检查、实例记录创建、工作目录初始化以及 Runtime Scheduler 调度。

现有接口主要服务于网页登录会话，不应直接作为外部系统的稳定北向契约。北向接口需要满足以下目标：

1. 外部系统能够通过 API 自动创建 OpenClaw、Hermes、OpenCode 或 DeepSeek Harness Lite 实例。
2. 北向登录过程中，用户名和密码不得以明文形式出现在 HTTP 请求体中。
3. 核心 ClawManager 后端不直接暴露到公网。
4. 外部契约与内部实例模型解耦，不暴露镜像、Pod、内部 Token 等实现细节。
5. 创建操作具备幂等、限流、审计、稳定错误码和故障恢复能力。
6. 实例创建继续复用现有 `InstanceService`，不在北向服务中复制实例业务逻辑。

## 2. 已确认的设计决策

1. 单独部署 `Northbound Gateway`，作为唯一北向公网入口。
2. ClawManager Core 只在集群内提供服务，不配置公网 Ingress、LoadBalancer 或 NodePort。
3. Gateway 与 Core 通过专用端口、NetworkPolicy 和双向 TLS 通信。
4. 北向登录使用 TLS 1.3、JWE 加密凭证和一次性挑战。
5. 首版兼容现有 bcrypt 用户密码数据，不引入 OPAQUE，也不要求用户迁移密码。
6. 北向 Access Token 与现有网页登录 JWT 完全隔离，使用独立密钥、Token 类型和 Audience。
7. 首版只开放 Lite 实例创建和查询，不开放删除、启动、停止、批量创建以及任意运行时参数。
8. Lite 实例创建采用异步操作模型，通过 Operation 查询创建结果。

## 3. 总体架构

```text
外部调用方
    │
    │ HTTPS / TLS 1.3
    ▼
Northbound Gateway（唯一公网入口）
    ├─ 路由白名单
    ├─ JWE 挑战登录
    ├─ 北向 Token 校验
    ├─ WAF、限流、请求大小限制
    ├─ Request ID 和接入审计
    └─ 路径及方法规范化
    │
    │ mTLS + 内部短期 JWT
    ▼
ClawManager Core（仅集群内访问）
    ├─ Northbound Provisioning 模块
    ├─ 用户状态与 Scope 复核
    ├─ 幂等、配额、容量和资源归属校验
    ├─ 现有 InstanceService
    └─ Runtime Scheduler
```

### 3.1 Northbound Gateway 职责

- 对外终止 TLS，并只暴露北向路由。
- 提供 JWE 一次性挑战登录协议。
- 校验北向 Access Token、请求方法、Content-Type 和请求大小。
- 实施来源 IP 限制、WAF、速率限制和并发限制。
- 生成或透传 `X-Request-ID`。
- 使用 mTLS 调用 Core 的北向内部端口。
- 不直接写入实例、配额和 Runtime 相关数据。

### 3.2 ClawManager Core 职责

- 验证 Gateway 客户端证书和内部 JWT。
- 再次验证用户状态、Scope、资源归属和幂等键。
- 维护北向操作记录，并执行权威配额和容量校验。
- 将外部 DTO 转换为内部 `services.CreateInstanceRequest`。
- 调用现有 `InstanceService` 创建 Lite 实例。
- 将内部错误映射为稳定的北向错误码。

### 3.3 网络与端口

建议 Core 使用独立监听端口：

```text
9001：现有管理端和内部 API，仅管理网络或集群内部可达
9002：北向内部 API，仅 Northbound Gateway 可达
```

Kubernetes NetworkPolicy 只允许 Gateway Pod 访问 Core 的 `9002` 端口，禁止 Gateway 访问 `9001`。Core Service 使用 `ClusterIP`，不得直接对公网暴露。测试集群仅通过 Northbound Gateway 的 NodePort `38443` 提供北向 HTTPS 入口，并应在节点防火墙或上游设备上配置调用方 IP 白名单。

## 4. 北向路由

公网 Gateway 只允许以下路由和 HTTP 方法：

| 方法 | 路径 | Scope | 用途 |
|---|---|---|---|
| POST | `/api/northbound/v1/auth/challenge` | 无 | 获取一次性登录挑战和加密公钥 |
| POST | `/api/northbound/v1/auth/login` | 无 | 提交 JWE 登录凭证 |
| POST | `/api/northbound/v1/auth/refresh` | 无 | 轮换 Refresh Token 并获取新令牌 |
| POST | `/api/northbound/v1/auth/logout` | 已登录 | 注销当前北向会话 |
| GET | `/api/northbound/v1/auth/me` | 已登录 | 查询当前用户和 Scope |
| POST | `/api/northbound/v1/lite-instances` | `lite-instances:create` | 提交 Lite 实例创建操作 |
| GET | `/api/northbound/v1/lite-instances` | `lite-instances:read` | 查询当前用户的 Lite 实例 |
| GET | `/api/northbound/v1/lite-instances/{id}` | `lite-instances:read` | 查询指定 Lite 实例状态 |
| POST | `/api/northbound/v1/lite-instances/{id}/external-access/password` | `lite-instances:share-link:manage` | 显式启用密码模式并生成 ShareLink URL/密码 |
| POST | `/api/northbound/v1/lite-instances/{id}/external-access/share-link/reset` | `lite-instances:share-link:reset` | 重置当前用户 Lite 实例的 ShareLink URL |
| POST | `/api/northbound/v1/lite-instances/{id}/external-access/password/reset` | `lite-instances:share-link:reset` | 重置当前用户 Lite 实例的 ShareLink 密码 |
| GET | `/api/northbound/v1/operations/{id}` | create/read | 查询异步操作状态 |

ShareLink 启用和重置接口为同步操作，并且必须通过北向 Bearer Token 鉴权和实例归属校验：

- 创建 Lite 实例默认不开放外部访问；调用方取得 `instance_id` 后显式启用密码模式。
- 密码模式没有独立用户名，凭据为 ShareLink URL 和密码；启用接口默认有效期 24 小时、工作区权限为 `none`。
- 启用接口会替换已有外部访问 URL 和凭据，响应必须带 `Cache-Control: no-store`。
- URL 重置后旧 URL 和已有分享会话立即失效；密码、有效期和工作区权限保持不变。
- 密码重置仅适用于密码模式；URL 保持不变，旧密码和已有分享会话立即失效。
- 密码只在密码模式启用或密码重置响应中返回，不写入北向访问日志和审计详情。
- 非当前用户所有的实例统一返回 `INSTANCE_NOT_FOUND`，避免泄露实例是否存在。

未列入白名单的路径、方法以及异常编码路径统一返回 `404`。Gateway 不得使用通配规则将整个 `/api/` 转发给 Core。

## 5. JWE 一次性挑战登录

### 5.1 安全目标

- 用户名和密码不以明文形式出现在 HTTP 请求体、代理日志或链路抓包中。
- 每次登录密文只能使用一次，避免重放攻击。
- 继续使用现有用户表和 bcrypt 密码摘要完成身份验证。
- JWE 作为 HTTPS 之上的应用层保护，不替代 TLS。

协议依据：

- [RFC 7516: JSON Web Encryption](https://www.rfc-editor.org/rfc/rfc7516.html)
- [RFC 7518: JSON Web Algorithms](https://www.rfc-editor.org/rfc/rfc7518.html)
- [RFC 8446: TLS 1.3](https://www.rfc-editor.org/rfc/rfc8446.html)

### 5.2 获取挑战

请求：

```http
POST /api/northbound/v1/auth/challenge
Content-Type: application/json
```

响应：

```json
{
  "challenge_id": "nbc_01K...",
  "nonce": "base64url-random-value",
  "expires_at": "2026-08-10T10:31:00+08:00",
  "encryption": {
    "kid": "nb-login-2026-08",
    "alg": "RSA-OAEP-256",
    "enc": "A256GCM",
    "public_jwk": {
      "kty": "RSA",
      "kid": "nb-login-2026-08",
      "n": "...",
      "e": "AQAB"
    }
  }
}
```

挑战有效期建议为 60 秒，只能原子消费一次。

### 5.3 客户端加密

客户端在本地构造以下明文载荷，但不得将该载荷直接发送到服务端：

```json
{
  "username": "alice",
  "password": "********",
  "challenge_id": "nbc_01K...",
  "nonce": "server-nonce",
  "client_nonce": "client-random-value",
  "issued_at": 1786329000
}
```

客户端使用挑战响应中的公钥生成 JWE Compact Serialization，固定使用：

```text
alg = RSA-OAEP-256
enc = A256GCM
```

不允许客户端协商或降级算法。RSA 密钥建议至少 3072 位。

### 5.4 提交登录

请求：

```http
POST /api/northbound/v1/auth/login
Content-Type: application/json
```

```json
{
  "challenge_id": "nbc_01K...",
  "credential_jwe": "eyJraWQiOiJuYi1sb2dpbi0yMDI2LTA4IiwiYWxn..."
}
```

服务端处理顺序：

1. 校验挑战存在、未过期且未消费。
2. 强制校验受保护 JWE Header 中的 `kid`、`alg` 和 `enc`。
3. 根据 `kid` 从 Kubernetes Secret 或 KMS 加载私钥并解密。
4. 比对内外层 `challenge_id`、服务端 nonce、客户端 nonce 和时间戳。
5. 原子消费挑战，阻止同一密文并发重放。
6. 使用现有 bcrypt 摘要验证用户密码，并检查用户 `is_active`。
7. 签发独立的北向 Access Token 和 Refresh Token。
8. 避免将解密后的字符串写入日志，并尽快释放包含敏感信息的缓冲区。

失败统一返回 `INVALID_CREDENTIALS`，不得区分用户不存在、密码错误或账号禁用。

### 5.5 禁止的实现方式

- 禁止仅依赖 HTTPS 后直接发送 `{username, password}`。
- 禁止固定公钥直接加密密码而不绑定一次性挑战。
- 禁止使用 RSA PKCS#1 v1.5 加密。
- 禁止仅在客户端对密码做哈希后将哈希作为登录凭证；该哈希会成为可重放的等价密码。
- 禁止在 URL、Query、Header、访问日志和错误日志中记录 JWE 解密结果。

## 6. 北向 Token 与会话

### 6.1 Access Token

北向 Access Token 与现有网页登录 JWT 不得互相通用。建议声明：

```json
{
  "sub": "123",
  "typ": "northbound_access",
  "sid": "nbs_01K...",
  "jti": "nbj_01K...",
  "iss": "clawmanager",
  "aud": "clawmanager-northbound",
  "scope": [
    "lite-instances:create",
    "lite-instances:read",
    "lite-instances:share-link:manage",
    "lite-instances:share-link:reset"
  ]
}
```

建议有效期：

- Access Token：30 分钟。
- Refresh Token：7 天。

北向 JWT 使用独立的 `NORTHBOUND_JWT_SECRET` 或非对称签名密钥，生产环境不得提供弱默认值。

### 6.2 Refresh Token

- 使用至少 32 字节安全随机数。
- 数据库只保存 HMAC 或密码学摘要。
- 每次刷新都签发新 Refresh Token，并立即作废旧 Token。
- 检测到已轮换 Token 被重复使用时，撤销整个北向会话。
- 用户禁用、修改密码、主动注销或管理员强制下线时，撤销关联会话。

### 6.3 Gateway 到 Core 的内部身份

Gateway 调用 Core 时同时使用：

1. 双向 TLS 客户端证书。
2. 短期内部 JWT，固定 `aud=clawmanager-internal-northbound`。
3. 签名声明中的用户 ID、Scope、Session ID、Request ID。

Core 不得仅依据 `X-User-ID` 等普通 Header 信任 Gateway 传入的用户身份。

## 7. Lite 实例创建契约

### 7.1 创建请求

```http
POST /api/northbound/v1/lite-instances
Authorization: Bearer <northbound_access_token>
Idempotency-Key: crm-order-20260810-001
Content-Type: application/json
```

```json
{
  "name": "ops-agent-001",
  "type": "openclaw",
  "description": "Created by CRM workflow"
}
```

字段约束：

| 字段 | 必填 | 约束 |
|---|---|---|
| `name` | 是 | 3 至 50 个字符，同一用户下唯一 |
| `type` | 是 | `openclaw`、`hermes`、`opencode` 或 `deepseek-harness` |
| `description` | 否 | 最大长度应在实施阶段统一确定 |

请求不得包含 `user_id`。实例归属用户只能由北向 Token 的 `sub` 决定。

### 7.2 服务端固定参数

外部接口不开放内部运行参数，服务端固定转换为：

```text
mode             = lite
instance_mode    = lite
runtime_type     = gateway
cpu_cores        = 2
memory_gb        = 4
disk_gb          = 20
gpu_enabled      = false
gpu_count        = 0
os_type          = type
os_version       = latest
image            = 系统镜像设置
storage_class    = 系统默认设置
```

首版禁止外部设置：

- `image_registry`、`image_tag`
- `environment_overrides`
- `runtime_type`、`instance_mode`
- CPU、内存、磁盘和 GPU
- Pod、Namespace、Gateway Token、Agent Token
- 任意 Kubernetes 或 Runtime 调度参数

配置包、技能注入和批量创建作为后续兼容性扩展，不纳入首版。

### 7.3 异步响应

```http
HTTP/1.1 202 Accepted
Location: /api/northbound/v1/operations/op_01K...
X-Request-ID: req_01K...
```

```json
{
  "operation_id": "op_01K...",
  "status": "queued",
  "resource_type": "lite_instance",
  "created_at": "2026-08-10T10:30:00+08:00"
}
```

操作成功表示实例记录已经创建并交给 Runtime Scheduler，不代表实例已经处于 `running`。调用方应继续查询实例状态。

### 7.4 操作状态

```text
queued → processing → succeeded
                    └→ failed
```

成功结果包含 `instance_id` 和实例查询 URL。失败结果只返回稳定错误码和安全错误描述，不返回数据库、文件系统或 Kubernetes 原始错误。

## 8. 幂等与并发控制

`POST /lite-instances` 必须携带 `Idempotency-Key`。

幂等唯一约束：

```text
user_id + operation_type + idempotency_key
```

处理规则：

- 相同 Key、相同请求体：返回原 Operation，不重复创建实例。
- 相同 Key、不同请求体：返回 `409 IDEMPOTENCY_CONFLICT`。
- 请求摘要使用规范化 JSON 计算 SHA-256，不存储敏感原文。
- 实例记录通过 `provisioning_operation_id` 与 Operation 一对一关联。
- Worker 重启后先按 `provisioning_operation_id` 查找实例，避免重复创建。

现有创建逻辑存在“先查配额、再插入实例”的并发窗口。北向自动化会放大该风险，因此权威的用户名、用户配额和全局 Lite 容量校验必须在数据库事务或数据库锁保护下完成。数据库唯一键继续作为最终防线。

## 9. 数据模型

### 9.1 `northbound_auth_challenges`

```text
id
challenge_id
nonce_hash
key_id
status                  issued / processing / consumed / expired
source_ip
expires_at
used_at
created_at
```

### 9.2 `northbound_sessions`

```text
id
session_id
user_id
refresh_token_hash
scopes
status                  active / revoked / expired
access_expires_at
refresh_expires_at
last_used_at
last_ip
user_agent
created_at
revoked_at
```

### 9.3 `northbound_operations`

```text
id
operation_id
user_id
session_id
operation_type
idempotency_key_hash
request_hash
request_payload
status                  queued / processing / succeeded / failed
instance_id
attempt_count
lease_owner
lease_expires_at
error_code
error_message
created_at
started_at
finished_at
updated_at
```

唯一约束建议：

```text
UNIQUE(user_id, operation_type, idempotency_key_hash)
UNIQUE(operation_id)
```

### 9.4 `instances` 扩展

新增可空字段：

```text
provisioning_operation_id
```

该字段建立唯一约束，用于北向创建的来源追踪和 Exactly-Once 防重。

### 9.5 审计

复用现有 `audit_logs` 表并补充 Repository，至少记录：

- 挑战申请、登录成功、登录失败、刷新和注销。
- Token 重放、挑战重放、算法降级和异常路径请求。
- Lite 创建已受理、成功、失败和幂等重放。
- 用户 ID、Session ID、Request ID、来源 IP、结果和实例 ID。

禁止记录密码、解密后的凭证、完整 Token、JWE 私钥以及完整幂等键。

## 10. 错误模型

北向接口使用稳定错误结构：

```json
{
  "code": "QUOTA_EXCEEDED",
  "message": "Instance quota exceeded",
  "request_id": "req_01K...",
  "details": {}
}
```

主要错误码：

| HTTP | Code | 场景 |
|---|---|---|
| 400 | `INVALID_REQUEST` | JSON、Header 或请求格式错误 |
| 401 | `INVALID_CREDENTIALS` | 登录失败 |
| 401 | `AUTH_INVALID` | Access Token 无效、过期或会话已撤销 |
| 403 | `SCOPE_DENIED` | 缺少所需 Scope |
| 404 | `INSTANCE_NOT_FOUND` | 实例不存在或不属于当前用户 |
| 409 | `NAME_CONFLICT` | 用户下实例名称重复 |
| 409 | `IDEMPOTENCY_CONFLICT` | 相同幂等键对应不同请求 |
| 409 | `QUOTA_EXCEEDED` | 用户实例配额不足 |
| 422 | `VALIDATION_ERROR` | 业务字段校验失败 |
| 429 | `RATE_LIMITED` | 触发速率或并发限制 |
| 503 | `LITE_CAPACITY_EXHAUSTED` | 全局 Lite 容量不足 |
| 503 | `DEPENDENCY_UNAVAILABLE` | 数据库、KMS 或 Runtime 暂时不可用 |

非本人资源统一返回 `404`，避免通过 ID 探测资源是否存在。

## 11. 限流与防护策略

建议初始策略：

- 挑战申请：每 IP 10 次/分钟。
- 登录尝试：每 IP 和账户组合 5 次/分钟。
- 创建实例：每用户 10 次/分钟，burst 为 3。
- 查询接口：每用户 120 次/分钟。
- ShareLink 启用和重置：每用户合计 10 次/分钟。
- 每用户最多 5 个未完成创建 Operation。
- 登录请求体最大 32 KB。
- 实例创建请求体最大 64 KB。

Gateway 必须配置：

- TLS 1.3，必要时允许受控 TLS 1.2 兼容配置。
- WAF、IP 白名单或合作方网络白名单。
- 请求超时、连接数、Header 大小和 Body 大小限制。
- 拒绝重复 Header、异常 Host、路径穿越、编码斜杠和双重 URL 编码。
- 只信任明确配置的代理节点传入的 `X-Forwarded-For`。

## 12. 密钥管理

### 12.1 JWE 密钥

- RSA 私钥存放于 Kubernetes Secret、Vault 或云 KMS。
- 每个密钥具有唯一 `kid`。
- Gateway 只公布公钥 JWK。
- 密钥轮换期间保留当前和上一版本私钥，直到旧挑战全部过期。
- 私钥不得写入日志、数据库、镜像和普通配置文件。

### 12.2 JWT 密钥

- 北向 JWT 与现有网页登录 JWT 使用不同密钥。
- 内部 Gateway-to-Core JWT 再使用独立密钥或证书。
- 配置缺失时服务应拒绝启动，不得使用开发默认值。

### 12.3 mTLS 证书

- Core 只信任 Northbound Gateway 专用客户端证书。
- 证书应具有明确的 SAN 和较短有效期。
- 支持双证书平滑轮换和吊销。

## 13. Worker 与故障恢复

Northbound Operation Worker 建议运行在 Core 的 leader-only 后台循环中，与现有主节点选举机制一致。

处理要求：

- 数据库租约领取 Operation，防止多副本重复执行。
- 临时错误指数退避重试，设置最大次数和退避上限。
- 参数错误、配额不足、名称冲突不重试。
- 数据库、KMS、临时文件系统或 Runtime 故障可重试。
- Worker 崩溃后，租约超时的 Operation 可被新 Leader 重新领取。
- 通过 `provisioning_operation_id` 判断实例是否已经创建。
- Operation 成功后，实例运行失败仍由实例状态反映，不回滚已经成功创建的资源记录。

## 14. 可观测性

建议指标：

```text
northbound_auth_challenge_total
northbound_auth_login_total{result}
northbound_auth_refresh_total{result}
northbound_request_total{route,status}
northbound_request_duration_seconds{route}
northbound_rate_limit_total{route}
northbound_operations_total{type,status}
northbound_operation_duration_seconds{type}
northbound_operation_retry_total{reason}
```

日志使用结构化字段：

```text
request_id
operation_id
session_id
user_id
route
result
error_code
duration_ms
source_ip
```

所有日志和指标标签都不得包含用户名、密码、Token、JWE 密文原文或幂等键原文。

## 15. 测试与验收

### 15.1 认证测试

- 正确 JWE 凭证能够登录现有 bcrypt 用户。
- 请求体和日志中不存在明文用户名、密码。
- 过期、已消费、nonce 不一致和时间戳异常的挑战被拒绝。
- 同一挑战并发提交时只能有一个请求成功。
- 错误 `kid`、`alg`、`enc` 和篡改后的认证标签被拒绝。
- Refresh Token 正常轮换，旧 Token 重放时会话被撤销。
- 网页 JWT 不能调用北向接口，北向 JWT 不能调用网页接口。

### 15.2 授权测试

- 缺少 Scope 的用户被拒绝。
- 禁用用户无法登录，已有会话立即失效。
- 用户无法创建到其他用户账户下。
- 用户无法读取其他用户实例，返回 `404`。
- 用户无法为其他用户实例启用或重置 ShareLink，返回 `404`。

### 15.3 创建与幂等测试

- OpenClaw、Hermes、OpenCode 和 DeepSeek Harness Lite 创建成功。
- 外部请求不能覆盖 Mode、Runtime、镜像和环境变量。
- 相同幂等键和相同请求只创建一个实例。
- 相同幂等键和不同请求返回 `IDEMPOTENCY_CONFLICT`。
- 多副本并发创建不会绕过名称、用户配额和全局容量限制。
- Worker 重启、Leader 切换和请求超时不会重复创建实例。
- 创建成功后可显式启用密码模式 ShareLink，并仅在响应中取得新 URL/密码。

### 15.4 网络测试

- 公网只能访问 Gateway 白名单路由。
- 公网无法连接 Core 的 9001 和 9002。
- Gateway 无法访问 Core 的 9001。
- 非 Gateway 客户端证书无法访问 9002。
- 路径双重编码、异常斜杠和目录穿越无法绕过白名单。

## 16. 发布计划

1. 新增数据表、Core 北向内部端口和 Feature Flag，默认关闭。
2. 部署 Northbound Gateway，但暂不开放公网 DNS。
3. 完成 JWE 客户端示例、OpenAPI 文档和自动化安全测试。
4. 在测试环境验证登录、幂等、配额并发和 Leader 故障恢复。
5. 通过指定合作方 IP 白名单进行灰度。
6. 观察认证失败率、创建成功率、延迟和 Runtime 容量。
7. 逐步开放正式北向域名。

建议配置开关：

```text
CLAWMANAGER_NORTHBOUND_ENABLED
NORTHBOUND_JWE_KEY_ID
NORTHBOUND_JWE_PRIVATE_KEY_REF
NORTHBOUND_JWT_SECRET_REF
NORTHBOUND_INTERNAL_JWT_SECRET_REF
NORTHBOUND_CHALLENGE_TTL_SECONDS
NORTHBOUND_ACCESS_TOKEN_TTL_MINUTES
NORTHBOUND_REFRESH_TOKEN_TTL_HOURS
```

## 17. 首版范围外能力

以下能力不纳入首版：

- Pro 实例创建。
- Lite 批量创建和批量删除。
- 外部指定镜像、环境变量、资源规格或 Kubernetes 参数。
- 配置包、Skill 和 Workspace 压缩包注入。
- 管理员代用户创建实例。
- API Key 或 OAuth Client Credentials 登录。
- OPAQUE 密码认证。
- Webhook 创建结果通知。

这些能力应在首版契约和安全基线稳定后通过向后兼容方式扩展。

## 18. 与现有代码的关系

实施时应复用以下现有能力：

- `backend/internal/services/instance_service.go`：实例创建、配额和 Lite Runtime 创建。
- `backend/internal/services/runtime_capacity.go`：Lite 模式和 Runtime 类型规范化。
- `backend/internal/services/auth_service.go`：现有用户密码摘要验证逻辑。
- `backend/internal/repository/instance_repository.go`：实例持久化。
- `backend/cmd/server/main.go`：服务初始化、路由和后台 Leader 生命周期。

现有 `/api/v1/instances` 保持为管理前端内部接口。北向 Gateway 和 Core 北向模块使用独立 DTO、错误码、Token 和路由，不直接将现有 Handler 暴露给外部。

## 19. 首版实现产物

- OpenAPI 契约：`docs/northbound-openapi.yaml`
- JWE 客户端示例：`examples/northbound_client.py`
- Gateway 入口：`backend/cmd/northbound-gateway/main.go`
- Core 北向模块：`backend/internal/northbound/`
- 数据库迁移：`backend/internal/db/migrations/045_add_northbound_api.sql`
- Kubernetes 部署附加包：`deployments/k8s/northbound/`

功能仍由 `CLAWMANAGER_NORTHBOUND_ENABLED` 控制且默认关闭。生产开放前必须完成第 15 节的认证、并发、网络和故障恢复验收，并配置 WAF 或合作方 IP 白名单。
