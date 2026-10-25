# ClawManager 北向接口使用说明

本文档面向需要通过程序调用 ClawManager 的系统集成方，适用于 Northbound API `v1.4`。完整机器可读定义见 [northbound-openapi.yaml](./northbound-openapi.yaml)，可运行的 Python Demo 见 [northbound_client.py](../examples/northbound_client.py)。

已有 ClawManager 环境启用本接口前，请先按[北向接口版本升级说明](./northbound-upgrade-guide.md)完成数据库迁移、Core mTLS 和独立 Gateway 部署。

## 1. 接入约定

| 项目 | 说明 |
| --- | --- |
| Base URL | `https://<northbound-host>:38443`，示例为 `https://northbound.example.com:38443` |
| API 前缀 | `/api/northbound/v1` |
| 传输协议 | HTTPS；生产环境必须校验服务端证书 |
| 数据格式 | JSON，POST 请求应携带 `Content-Type: application/json` |
| 业务认证 | `Authorization: Bearer <access_token>` |
| 请求追踪 | 可传 `X-Request-ID`，长度 8～96，仅允许字母、数字、`-`、`_`、`.`、`:` |
| 时间格式 | RFC 3339，例如 `2026-08-11T10:30:00Z` |

接口响应直接返回业务 JSON，不使用额外的 `data` 包装层。响应头中的 `X-Request-ID` 可用于问题排查。

北向 Gateway 是唯一对外入口。调用方不得直接访问 `/internal/northbound/v1`，该地址只允许 Gateway 通过 mTLS 调用 Core。

### 1.1 通用参数规则

| 参数或约定 | 含义与取值范围 |
| --- | --- |
| 路径参数 `{id}` | 北向受支持实例 ID，十进制正整数。实例不存在、不属于受支持类型或不属于当前用户时统一返回 `INSTANCE_NOT_FOUND`。 |
| 路径参数 `{operation_id}` | 创建接口返回的操作 ID，例如 `op_xxx`；应作为不透明字符串原样保存和回传。 |
| `Authorization` | 除挑战、登录和刷新外均为必填，格式固定为 `Bearer <access_token>`。 |
| `Content-Type` | POST 请求除挑战接口外固定为 `application/json`；不支持重复的安全敏感 Header。 |
| `X-Request-ID` | 可选，长度 8～96；仅允许 ASCII 字母、数字、`-`、`_`、`.`、`:`。不合法时 Gateway 会重新生成。不得放入密码、Token 等敏感信息。 |
| 请求体大小 | 北向请求体最大 64 KiB；登录请求体进一步限制为 32 KiB。 |
| 时间 | 请求和响应中的日期时间使用 RFC 3339；JWE 的 `issued_at` 使用 Unix 秒。 |
| 字符串长度 | 服务端对部分字段还会按 UTF-8 字节数复核；中文等多字节字符会占用多个字节。具体以各字段说明为准。 |

除特别说明外，请不要发送文档未定义的字段。`null`、空字符串和省略字段含义可能不同，调用方应遵循各字段的“必填”和默认值说明。

### 1.2 门户、北向 Gateway 与 Core 的地址职责

三个入口用途不同，不能互相替代：

| 入口 | 既有部署示例 | 用途 | 调用方是否直接访问 |
| --- | --- | --- | --- |
| ClawManager 门户 | `https://<host>:30443` | 管理页面、IEI owner 门户、实例代理和 `/s/.../` ShareLink | 浏览器和 ShareLink 使用者访问 |
| 北向 Gateway | `https://<host>:32343` | 对外提供 `/api/northbound/v1`，负责 JWE 登录、Scope、限流、审计和转发 | 系统集成方访问 |
| 北向 Core | 集群内 `9002` | 处理实例、Operation 和 ShareLink 业务；Gateway 与 Core 之间使用 mTLS | 不允许外部调用方直接访问 |

上表端口是既有部署示例，不是业务协议的一部分；实际环境可以通过 NodePort、负载均衡器或 Ingress 映射到其他端口。集成方应分别配置北向 Gateway Base URL 和门户公开 Origin，不能用 Gateway 地址拼接 ShareLink。

## 2. 接口清单和权限

| 方法 | 路径 | Scope | 说明 |
| --- | --- | --- | --- |
| POST | `/auth/challenge` | 无 | 获取一次性 JWE 登录挑战 |
| POST | `/auth/login` | 无 | 使用 JWE 密文登录 |
| POST | `/auth/refresh` | 无 | 轮换 Refresh Token |
| POST | `/auth/logout` | 已登录 | 注销当前北向会话 |
| GET | `/auth/me` | 已登录 | 查询当前身份和 Scope |
| POST | `/lite-instances` | `lite-instances:create` | 创建 OpenClaw、Hermes、OpenCode 或 DeepSeek Harness Lite 实例 |
| GET | `/lite-instances?owner=...` | `lite-instances:read` | 按 owner 查询当前用户的全部受支持实例 |
| GET | `/lite-instances/{id}` | `lite-instances:read` | 查询一个受支持实例 |
| POST | `/pro-instances` | `pro-instances:create` | 创建 OpenClaw、Hermes、OpenCode 或 DeepSeek Harness Pro |
| GET | `/pro-instances?owner=...` | `pro-instances:read` | 返回 OpenClaw、Hermes、OpenCode 和 DeepSeek Harness Pro |
| GET | `/pro-instances/{id}` | `pro-instances:read` | 查询一个受支持的 Pro 实例 |
| POST | `/lite-instances/{id}/restart` | `lite-instances:restart` | 异步重启运行中的 Lite |
| POST | `/lite-instances/{id}/reset` | `lite-instances:reset` | 恢复出厂：先建立并验证全新 Lite 实例，再删除旧实例和工作区；成功 Operation 的 `instance_id` 为新 ID；必须显式确认数据删除 |
| POST | `/pro-instances/{id}/restart` | `pro-instances:restart` | 异步重启运行中的 Pro，保留 PVC |
| POST | `/pro-instances/{id}/reset` | `pro-instances:reset` | 恢复出厂：先建立并验证全新 Pro 实例和 PVC，再删除旧实例；成功 Operation 的 `instance_id` 为新 ID；必须显式确认数据删除 |
| GET | `/operations/{id}` | create 或 read | 查询异步操作状态 |
| POST | `/lite-instances/{id}/external-access/password` | `lite-instances:share-link:manage` | 启用密码模式 ShareLink 并生成 URL/密码 |
| POST | `/lite-instances/{id}/external-access/share-link/reset` | `lite-instances:share-link:reset` | 重置 ShareLink URL |
| POST | `/lite-instances/{id}/external-access/password/reset` | `lite-instances:share-link:reset` | 重置 ShareLink 密码 |

完整路径需要加上 `/api/northbound/v1` 前缀。新登录会话会获得 Lite、Pro 和 ShareLink Scope；部署新版本前创建的会话需要重新执行 JWE 登录才能获得新增 Scope。

Scope 含义：

| Scope | 允许的操作 |
| --- | --- |
| `lite-instances:create` | 通过统一入口提交受支持 Runtime 的创建 Operation，并可读取自己提交的 Operation。 |
| `lite-instances:read` | 按 owner 列出当前用户自己的全部受支持实例、查询单个实例，并可读取 Operation。 |
| `pro-instances:create` | 通过 `/pro-instances` 创建受支持的独立 Pro Runtime。 |
| `pro-instances:read` | 按 owner 查询受支持的 Pro Runtime 和单个实例。 |
| `lite-instances:restart` / `pro-instances:restart` | 重启运行中的对应模式实例。 |
| `lite-instances:reset` / `pro-instances:reset` | 恢复出厂并永久删除旧实例的持久工作区；请求必须包含 `{"confirm_data_loss":true}`。系统先验证全新替代实例，创建失败时保留旧实例；成功后调用方必须使用 Operation 返回的新 `instance_id`。 |
| `lite-instances:share-link:manage` | 为当前用户自己的受支持实例显式启用密码模式 ShareLink；会生成并返回敏感凭据。 |
| `lite-instances:share-link:reset` | 重置已经启用的 ShareLink URL 或密码；不能首次启用，也不能修改有效期或 Workspace 权限。 |

### 2.1 创建任一受支持实例的标准调用顺序

创建任一受支持实例时，推荐按以下顺序调用。Lite 使用 `/lite-instances`，OpenClaw、Hermes、OpenCode、DeepSeek Harness 的 Pro 使用 `/pro-instances`。两个创建接口的请求字段和异步返回结构保持一致。认证、异步创建和 Runtime 就绪是三个不同阶段，不能只调用创建接口后立即使用实例。

```text
申请挑战 → 本地生成 JWE → 登录取得 Token → 按 type 提交创建请求
    → 轮询 Operation 到终态 → 查询实例直到 running
    →（可选）启用密码模式 ShareLink → 保存并交付 URL/密码
```

| 顺序 | 调用或动作 | 关键输入 | 必须保存/判断的结果 | 下一步 |
| --- | --- | --- | --- | --- |
| 1 | `POST /auth/challenge` | 不带请求体 | 保存 `challenge_id`、`nonce`、`encryption` 和 HTTPS `Date` 响应头。挑战默认 60 秒有效且只能使用一次。 | 在挑战过期前执行第 2、3 步。 |
| 2 | 客户端本地生成 Compact JWE | 现有用户名和密码，以及第 1 步返回的挑战参数 | JWE Protected Header 固定使用挑战的 `kid`、`RSA-OAEP-256`、`A256GCM`；明文载荷包含新的 `client_nonce` 和 `issued_at`。 | 不发送明文用户名和密码，只发送 JWE。 |
| 3 | `POST /auth/login` | `challenge_id`、`credential_jwe` | 保存 `access_token`、`refresh_token`、`expires_in`、`refresh_expires_in` 和 `scopes`。确认 Scope 包含 `lite-instances:create` 与 `lite-instances:read`；需要第 7 步时还必须包含 `lite-instances:share-link:manage`。 | 使用 Access Token 调用创建接口。 |
| 4 | `POST /lite-instances` 或 `POST /pro-instances` | 根据目标模式选择路径；请求体只提供 `owner` 和受支持的 `type`，并使用稳定的 `Idempotency-Key` | 接口返回 `202`。保存 `operation_id` 和 `Location`；不要把 `202` 当成实例已经可用。 | 按第 5 步轮询 Operation。 |
| 5 | `GET /operations/{operation_id}` | 第 4 步的 Operation ID | `queued` 或 `processing`：继续轮询；`failed`：记录 `error_code`、`error_message` 并停止；`succeeded`：保存正整数 `instance_id`。 | 仅 `succeeded` 时进入第 6 步。 |
| 6 | `GET /lite-instances/{instance_id}` | 第 5 步的实例 ID | `status=creating`：继续轮询；`status=running` 且 `availability=available`：实例可用；其他状态按不可用处理并结合状态排查。 | 实例可直接由用户使用，或进入可选的第 7 步。 |
| 7（可选） | `POST /lite-instances/{instance_id}/external-access/password` | 先确定 `expires_mode`、有效期参数和 `workspace_access` | 保存 `share_url`、`password`、`expires_at` 和实际生效的 `workspace_access`。该调用会替换同一实例已有的外部访问凭据。 | 立即把凭据写入安全存储。 |
| 8（可选） | 客户端拼接完整 ShareLink | 门户公开 Origin 与第 7 步的相对 `share_url` | `absolute_share_url = CLAWMANAGER_PUBLIC_BASE_URL + share_url`。不要默认使用北向 Gateway 地址。 | 将完整 URL 和密码通过安全渠道交付。 |
| 9 | `POST /auth/refresh` 或 `/auth/logout` | Refresh Token，或当前 Access Token | 长期集成在 Access Token 到期前刷新并覆盖旧 Refresh Token；任务结束且无需维持会话时注销。 | 刷新后继续使用新 Token，或结束流程。 |

第 4 步的 OpenClaw 创建请求示例；创建其他 Runtime 时只需更换 `type`、名称和幂等键：

```http
POST /api/northbound/v1/lite-instances HTTP/1.1
Host: northbound.example.com:38443
Authorization: Bearer <access-token>
Content-Type: application/json
Idempotency-Key: order-20260811-openclaw-001

{
  "name": "customer-openclaw-01",
  "owner": "customer-a",
  "type": "openclaw",
  "description": "Created by northbound integration"
}
```

轮询建议：

- Operation 轮询间隔建议从 2 秒开始；收到 `429` 时遵循 `Retry-After` 并增加退避。
- Access Token 在轮询期间过期时，先调用 `/auth/refresh`，保存新 Access Token 和新 Refresh Token，再重试原查询。
- 客户端超时只表示本次等待结束，不表示服务端 Operation 失败。继续使用同一个 `operation_id` 查询，不要更换幂等键重复创建。
- Operation 的 `succeeded` 表示实例记录已创建并提交调度，不表示 Runtime 已经 `running`。
- 如果不需要对外分享，流程在第 6 步结束，不要调用 ShareLink 接口。
- 如果需要 Workspace 文件，必须在第 7 步选择 `read` 或 `write`；默认 `none` 不包含 Workspace 文件。详细权限见 5.1。

Python Demo 已封装上述顺序。`.env` 中设置 `NORTHBOUND_INSTANCE_TYPE=openclaw` 后执行：

```powershell
python examples/northbound_client.py create
```

当 `NORTHBOUND_ENABLE_SHARELINK=true` 时，Demo 会在 Operation 成功后自动执行第 7 步。Demo 当前不会继续等待实例达到 `running`，自动化调用方仍应根据第 6 步检查 Runtime 状态后再向最终用户宣告实例可用。

## 3. JWE 一次性挑战登录

用户名和密码不能作为明文 JSON 直接提交给登录接口。登录流程如下：

1. 客户端申请一次性挑战。
2. 客户端在本地使用挑战中的 RSA 公钥，将用户名、密码、挑战 ID 和随机数加密成 Compact JWE。
3. 客户端只向登录接口提交挑战 ID 和 JWE 密文。
4. 服务端消费挑战并返回 Access Token、Refresh Token 和 Scope。

挑战只能使用一次；无论登录成功还是失败，都不能再次使用。客户端不得记录明文凭据、JWE 明文载荷、Token 或新 ShareLink 密码。

### 3.1 获取挑战

```http
POST /api/northbound/v1/auth/challenge HTTP/1.1
Host: northbound.example.com:38443
```

成功响应：`201 Created`

```json
{
  "challenge_id": "nbc_example",
  "nonce": "server-generated-random-value",
  "expires_at": "2026-08-11T10:31:00Z",
  "encryption": {
    "kid": "northbound-login-v1",
    "alg": "RSA-OAEP-256",
    "enc": "A256GCM",
    "public_jwk": {
      "kty": "RSA",
      "kid": "northbound-login-v1",
      "n": "...",
      "e": "AQAB"
    }
  }
}
```

### 3.2 在客户端生成 JWE

JWE Protected Header 必须是：

```json
{
  "kid": "northbound-login-v1",
  "alg": "RSA-OAEP-256",
  "enc": "A256GCM"
}
```

JWE 明文载荷只在客户端内存中构造，不得直接传输：

```json
{
  "username": "alice",
  "password": "user-password",
  "challenge_id": "nbc_example",
  "nonce": "server-generated-random-value",
  "client_nonce": "client-generated-random-value",
  "issued_at": 1786415400
}
```

其中 `issued_at` 是 Unix 秒时间戳，且必须为每次登录生成新的 `client_nonce`。集成客户端应保持系统时钟同步；Python Demo 会在 TLS 校验成功后使用挑战响应的 HTTPS `Date` 头生成 `issued_at`，从而容忍客户端操作系统的临时校时偏差。不得在关闭 TLS 校验时信任该响应头。

JWE 明文载荷字段：

| 字段 | 必填 | 含义与约束 |
| --- | --- | --- |
| `username` | 是 | 现有 ClawManager 用户名；服务端会去除首尾空白。不存在、停用和密码错误统一返回 `INVALID_CREDENTIALS`。 |
| `password` | 是 | 对应现有用户密码。只允许存在于客户端内存和 JWE 密文中，不得放入外层 JSON、URL、日志或 `X-Request-ID`。 |
| `challenge_id` | 是 | 必须与本次挑战响应完全一致。挑战默认 60 秒过期且只能消费一次。 |
| `nonce` | 是 | 必须原样使用本次挑战返回的 `nonce`，不能复用其他挑战的值。 |
| `client_nonce` | 是 | 客户端为每次登录新生成的非空随机字符串；建议至少使用 32 字节密码学安全随机数。 |
| `issued_at` | 是 | Unix 秒时间戳；不能比服务端时间超前 30 秒以上，也不能早于本次挑战有效窗口。建议使用挑战 HTTPS 响应的 `Date`。 |

### 3.3 登录

```http
POST /api/northbound/v1/auth/login HTTP/1.1
Host: northbound.example.com:38443
Content-Type: application/json

{
  "challenge_id": "nbc_example",
  "credential_jwe": "<compact-jwe>"
}
```

登录外层请求字段：

| 字段 | 必填 | 含义与约束 |
| --- | --- | --- |
| `challenge_id` | 是 | 本次挑战 ID，必须与 JWE 明文中的 `challenge_id` 相同。 |
| `credential_jwe` | 是 | 五段 Compact JWE 字符串；算法固定为 `RSA-OAEP-256`，内容加密固定为 `A256GCM`，`kid` 使用挑战返回值。 |

成功响应：`200 OK`

```json
{
  "access_token": "<access-token>",
  "refresh_token": "<refresh-token>",
  "token_type": "Bearer",
  "expires_in": 1800,
  "refresh_expires_in": 604800,
  "scopes": [
    "lite-instances:create",
    "lite-instances:read",
    "lite-instances:share-link:manage",
    "lite-instances:share-link:reset"
  ],
  "session_id": "nbs_example"
}
```

Token 响应字段：

| 字段 | 含义 |
| --- | --- |
| `access_token` | 北向 Bearer Token，默认有效 1800 秒；只用于北向 API。 |
| `refresh_token` | 用于刷新会话，默认有效 604800 秒；每次刷新都会轮换，成功后必须覆盖旧值。 |
| `token_type` | 固定为 `Bearer`。 |
| `expires_in` | Access Token 剩余有效秒数。 |
| `refresh_expires_in` | Refresh Token 剩余有效秒数。 |
| `scopes` | 本会话拥有的权限集合；旧会话不会自动获得新版本新增的 Scope。 |
| `session_id` | 北向会话 ID，用于审计和注销；不是 Access Token。 |

### 3.4 刷新和注销

刷新接口会同时轮换 Access Token 和 Refresh Token。客户端收到成功响应后必须立即覆盖旧 Refresh Token；重复使用旧 Token 会被视为重放，并撤销当前会话。

```http
POST /api/northbound/v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "<current-refresh-token>"
}
```

注销当前会话：

```http
POST /api/northbound/v1/auth/logout
Authorization: Bearer <access-token>
Content-Type: application/json

{}
```

成功注销返回 `204 No Content`。

## 4. 创建和查询实例

### 4.1 创建 Lite 实例

创建操作是异步的，必须提供长度为 8～128 个 UTF-8 字节的 `Idempotency-Key`。

```http
POST /api/northbound/v1/lite-instances
Authorization: Bearer <access-token>
Content-Type: application/json
Idempotency-Key: create-alice-openclaw-001

{
  "name": "alice-openclaw",
  "owner": "alice",
  "type": "openclaw",
  "description": "Created by northbound API"
}
```

字段约束：

| 字段 | 必填 | 约束 |
| --- | --- | --- |
| `name` | 是 | 实例显示名称；去除首尾空白后需同时满足 3～50 个 Unicode 字符和 3～50 个 UTF-8 字节，同一用户下不能重名。不会作为 Kubernetes 参数或镜像名使用。 |
| `owner` | 是 | 创建者或业务归属标识；去除首尾空白后为 1～128 个 UTF-8 字节，不能包含控制字符。保存和列表查询采用区分大小写的精确匹配。 |
| `type` | 是 | 可选 `openclaw`、`hermes`、`opencode` 或 `deepseek-harness`。大小写会被规范为小写，其他类型不允许。 |
| `description` | 否 | 实例备注，最多 2000 个 UTF-8 字节；只作为元数据，不会注入 Runtime。可省略或传 `null`。 |

#### 4.1.1 Runtime 映射与固定资源

调用方不传 `lite`、`pro`、镜像、操作系统、CPU、内存、磁盘或 GPU 参数。服务端只根据 `type` 选择已批准的运行模式、镜像配置和资源预设：

| `type` | 产品定位 | 服务端模式 | Runtime 后端 | 固定资源 | 北向约束 |
| --- | --- | --- | --- | --- | --- |
| `openclaw` | 通用智能体工作空间，适合信息处理、资料整理和持续任务 | Lite | 共享 Gateway Runtime | 2 CPU、4 GB 内存、5 GB 存储、无 GPU | Pro 请使用 `/pro-instances` |
| `hermes` | 面向研究、知识检索和长上下文任务的智能体工作空间 | Lite | 共享 Gateway Runtime | 2 CPU、4 GB 内存、5 GB 存储、无 GPU | Pro 请使用 `/pro-instances` |
| `opencode` | 面向代码生成、终端操作和仓库协作的开发者代码工作台 | Lite | 共享 Gateway Runtime | 2 CPU、4 GB 内存、5 GB 存储、无 GPU | Pro 请使用 `/pro-instances` |
| `deepseek-harness` | 插件化智能体执行平台，适合复杂任务拆解、多代理协作和可扩展 Agent 工作流 | Lite | 共享 Gateway Runtime | 2 CPU、4 GB 内存、5 GB 存储、无 GPU | Pro 请使用 `/pro-instances` |

这些资源值是北向接口的安全预设，不代表 ClawManager 管理端支持的全部规格。即使管理端存在其他模式或镜像，北向调用方也不能借助额外字段绕过上述映射。运行镜像由服务端系统镜像设置或部署配置决定，创建响应不会返回镜像地址。

`Idempotency-Key` Header 必填，去除首尾空白后长度为 8～128 个 UTF-8 字节。建议使用业务订单号或 UUID，并保证同一业务创建请求始终使用相同 Key。相同用户、相同 Key、相同请求体会返回原 Operation；相同 Key 搭配不同请求体返回 `IDEMPOTENCY_CONFLICT`。不要在 Key 中放入用户名、密码或其他敏感数据。

成功提交返回 `202 Accepted`：

```json
{
  "operation_id": "op_example",
  "status": "queued",
  "resource_type": "lite_instance",
  "created_at": "2026-08-11T10:30:00Z",
  "updated_at": "2026-08-11T10:30:00Z"
}
```

`Location` 响应头指向操作查询地址。相同用户使用相同 `Idempotency-Key` 和相同请求体重试时，返回同一操作，并带有 `Idempotent-Replayed: true`。相同 Key 搭配不同请求体会返回 `IDEMPOTENCY_CONFLICT`。

### 4.2 创建 Pro 实例

OpenClaw、Hermes、OpenCode 和 DeepSeek Harness Pro 使用独立的 Pro 创建入口，请求字段与 Lite 完全一致：

```http
POST /api/northbound/v1/pro-instances
Authorization: Bearer <access-token>
Content-Type: application/json
Idempotency-Key: create-alice-opencode-pro-001

{
  "name": "alice-opencode-pro",
  "owner": "alice",
  "type": "opencode",
  "description": "Created by northbound API"
}
```

`type` 只允许 `openclaw`、`hermes`、`opencode` 或 `deepseek-harness`。接口路径固定选择 Pro/desktop，调用方不传 `mode`、`runtime_type`、镜像或资源字段。服务端使用 ClawManager 系统设置中对应 Runtime 已保存且启用的 DESKTOP 镜像，不依赖 YAML 中写死镜像地址；未配置或未启用对应 Pro 镜像时请求会被拒绝。四种 Runtime 固定使用 4 CPU、8 GB 内存、50 GB 存储且不启用 GPU，Operation 的 `resource_type` 为 `pro_instance`。

### 4.3 查询操作

```http
GET /api/northbound/v1/operations/op_example
Authorization: Bearer <access-token>
```

`status` 取值：

| 状态 | 说明 |
| --- | --- |
| `queued` | 已进入队列 |
| `processing` | 正在创建 |
| `succeeded` | 创建完成，响应中包含 `instance_id` |
| `failed` | 创建失败，响应中包含 `error_code` 和 `error_message` |

Operation 字段：

| 字段 | 含义与取值 |
| --- | --- |
| `operation_id` | 异步操作 ID，供 `/operations/{id}` 查询。 |
| `status` | `queued`、`processing`、`succeeded` 或 `failed`。 |
| `resource_type` | Lite 请求为 `lite_instance`；通过 `/pro-instances` 创建的 OpenClaw、Hermes、OpenCode、DeepSeek Harness 为 `pro_instance`。 |
| `instance_id` | 仅成功后出现，后续实例和 ShareLink 接口使用该正整数。 |
| `error_code` / `error_message` | 仅失败时出现；适合程序判断和运维排查，不包含底层敏感信息。 |
| `created_at` / `started_at` / `finished_at` / `updated_at` | RFC 3339 时间；尚未发生的阶段字段会省略。 |

### 4.4 查询实例

```http
GET /api/northbound/v1/lite-instances?owner=alice&page=1&limit=20
Authorization: Bearer <access-token>
```

`owner` 必填，采用区分大小写的精确匹配。`/lite-instances` 返回四种受支持的 Lite Runtime 以及 OpenClaw/Hermes/OpenCode/DeepSeek Harness Pro；`/pro-instances` 只返回这四种 Pro。两个接口都不会返回同一用户下其他 owner 或不受支持 Runtime 的实例。`page` 最小为 1；`limit` 为 1～100，默认 20。

| Query 参数 | 必填 | 类型、范围和默认值 |
| --- | --- | --- |
| `owner` | 是 | 与创建时保存的 owner 精确一致，区分大小写。 |
| `page` | 否 | 十进制整数，最小 1，默认 1。 |
| `limit` | 否 | 十进制整数，范围 1～100，默认 20；超过 100 会按 100 处理。 |

查询单个实例：

```http
GET /api/northbound/v1/lite-instances/123
Authorization: Bearer <access-token>
```

实例响应中的 `status` 是 Runtime 生命周期状态；`availability` 是便于调用方展示的派生值：`running` 对应 `available`，`creating` 对应 `starting`，其他状态对应 `unavailable`。创建 Operation 成功只说明实例记录和调度请求已创建，调用方仍应查询实例直到 `status=running`。

单实例成功响应示例：

```json
{
  "id": 123,
  "name": "alice-workspace",
  "owner": "alice@example.com",
  "description": "Created by northbound integration",
  "type": "opencode",
  "status": "running",
  "availability": "available",
  "created_at": "2026-08-24T05:30:00Z",
  "updated_at": "2026-08-24T05:31:20Z",
  "started_at": "2026-08-24T05:31:20Z"
}
```

`name`、`owner` 和 `description` 都来自创建请求并由服务端保存；服务端不会替调用方生成业务标题或 Runtime 宣传文案。`description` 未提供时会省略。响应刻意不暴露 Kubernetes namespace、Pod、镜像、内部端口和底层错误。

### 4.4 IEI owner 实例页面与单点登录

智慧协作平台使用单点登录 token 打开固定入口：

```text
https://<ip>:<port>/ieisystem/list-instances?token=<URL 编码后的标准 Base64 token>
```

token 按《智慧协作平台单点登录文档》的“方式二”生成：明文为 `邮箱+yyyy-MM-dd HH:mm:ss`，采用 AES-128-CBC、PKCS5Padding（与 16 字节分组上的 PKCS7Padding 等价）加密，然后输出标准 Base64。密钥使用双方约定的 16 字节系统密钥；业务系统 IV 固定为 16 字节标识 `CLAWMANAGETOKENS`，配置后需与智慧协作平台保持一致。Base64 中的 `+`、`/`、`=` 必须进行 URL 编码。

本仓库提供了测试 URL 生成器。先在 `examples/.env` 中填写 `NORTHBOUND_OWNER`，然后执行：

```powershell
python examples/generate_iei_url.py
```

脚本默认通过 `IEISYSTEM_KUBECONFIG` 从指定 Kubernetes Secret 读取 AES 密钥，只向标准输出写入拼接完成的 URL，不打印共享密钥。生成的 URL 默认在 24 小时内有效。

生成器的本地配置：

| 环境变量 | 默认值/回退 | 说明 |
| --- | --- | --- |
| `IEISYSTEM_OWNER_EMAIL` | 回退到 `NORTHBOUND_OWNER` | 要进入 owner 门户的邮箱；生成前会规范为小写。 |
| `IEISYSTEM_PORTAL_BASE_URL` | 回退到 `CLAWMANAGER_PUBLIC_BASE_URL` | ClawManager 门户公开 HTTPS Origin，例如 `https://<portal-host>:30443`；不是北向 Gateway 地址。 |
| `IEISYSTEM_K8S_NAMESPACE` | `clawmanager-system` | 保存 IEI SSO Secret 的 namespace。 |
| `IEISYSTEM_K8S_SECRET` | `clawmanager-iei-sso` | 保存 `aes-key` 和 `aes-iv` 的 Secret 名称。 |
| `IEISYSTEM_KUBECONFIG` | 当前 `kubectl` context | 可选 kubeconfig 路径；本机当前 context 不能读取目标 Secret 时必须设置。 |
| `IEISYSTEM_SSO_KEY` | 默认不设置 | 仅用于受控测试进程直接注入 16 字节密钥；不得写入 `.env`、命令历史或版本库。设置后不调用 `kubectl` 读取 key。 |
| `IEISYSTEM_SSO_IV` | 默认不设置 | 仅用于受控测试进程直接注入 16 字节 IV；安全要求同上。设置后不调用 `kubectl` 读取 IV。 |

生成器只负责按当前时间生成 token，实际有效期由服务端 `IEISYSTEM_SSO_TOKEN_TTL` 控制，默认和允许上限均为 24 小时。链接在有效期内属于可直接换取 IEI 会话的 bearer credential，不应发送到无关人员或写入日志。

服务端按 `Asia/Shanghai` 解析时间，默认接受 24 小时内的 token，并允许最多 5 秒的未来时钟偏差。验证成功后，原始 AES token 只用于换取独立的 HttpOnly IEI 会话，并立即从浏览器地址栏移除。后续列表、详情、工作区和同源实例代理请求均验证该会话。OpenCode、DeepSeek Harness 使用独立运行时 Origin 时，服务端签发与实例和 IEI 会话绑定的短期入口能力，并由运行时 Origin 换取自己的 HttpOnly Cookie；该能力不会超过 IEI 会话的到期时间。IEI 主会话退出后不再签发新能力，已经打开的独立 Origin 最多持续到现有入口能力到期。

owner 取解密后的邮箱并按邮箱语义进行不区分大小写的匹配。列表返回该 owner 的四种受支持 Lite 实例以及 OpenClaw/Hermes/OpenCode/DeepSeek Harness Pro；访问详情或生成实例入口时会再次校验 owner 与受支持类型。不存在、不受支持、owner 不匹配三种情况统一返回 `404`，防止枚举其他实例。

该入口不复用 ClawManager 门户登录态，也不读取或创建 ShareLink 的短码、密码、会话或外部访问记录。

主服务配置：

| 环境变量 | 要求与默认值 |
| --- | --- |
| `IEISYSTEM_SSO_ENABLED` | 设置为 `true` 才启用；默认 `false`。 |
| `IEISYSTEM_SSO_KEY` | 必填，严格 16 个 UTF-8 字节；通过 Kubernetes Secret 注入。 |
| `IEISYSTEM_SSO_IV` | 必填，严格 16 个 UTF-8 字节；当前约定为 `CLAWMANAGETOKENS`。 |
| `IEISYSTEM_SSO_TOKEN_TTL` | 默认 `24h`，必须大于 0 且不超过 `24h`。 |
| `IEISYSTEM_SESSION_SECRET` | 必填，至少 32 个 UTF-8 字节，且不得与北向 JWT 密钥复用。 |
| `IEISYSTEM_SESSION_TTL` | 默认 `24h`，必须大于 0 且不超过 `24h`；有效会话可由 ClawManager 在用户持续访问期间本地续期，无需再次请求 IEI。 |
| `IEISYSTEM_SSO_TIMEZONE` | 默认 `Asia/Shanghai`。 |
| `IEISYSTEM_COOKIE_SECURE` | HTTPS 环境必须为 `true`，默认 `true`。仅本地 HTTP 调试可设为 `false`。 |

## 5. 启用和重置 ShareLink

ShareLink 接口都是同步操作，只允许操作当前用户拥有的北向受支持实例，包括 OpenClaw、Hermes、OpenCode 和 DeepSeek Harness。ShareLink 密码模式没有单独用户名，访问凭证由 `share_url` 和 `password` 组成。

### 5.1 启用密码模式

创建实例不会默认开放外部访问。实例创建操作成功并取得 `instance_id` 后，通过以下统一接口显式启用密码模式：

```http
POST /api/northbound/v1/lite-instances/123/external-access/password
Authorization: Bearer <access-token>
Content-Type: application/json

{
  "expires_mode": "preset",
  "expires_preset": "24h",
  "workspace_access": "none"
}
```

请求字段：

| 字段 | 类型 | 必填 | 默认值 | 含义与约束 |
| --- | --- | --- | --- | --- |
| `expires_mode` | string | 否 | `preset` | 有效期计算方式，只能是 `preset`、`custom` 或 `permanent`。各模式的字段组合见下表。 |
| `expires_preset` | string | 条件必填 | `24h` | 仅用于 `preset`；可选 `1h`、`24h`、`7d`、`30d`，分别表示从服务端处理请求的时刻起 1 小时、24 小时、7 天、30 天。 |
| `expires_at` | string/date-time | 条件必填 | 无 | 仅用于 `custom`；必须是晚于服务端当前时间的 RFC 3339 时间，例如 `2026-08-20T12:00:00+08:00`。服务端会按 UTC 保存和返回。 |
| `workspace_access` | string | 否 | `none` | 共享访问者对实例 Workspace 文件的权限，只能是 `none`、`read` 或 `write`。它不控制聊天和实例页面访问，详细权限见下表。 |

`expires_mode` 组合规则：

| 值 | 行为 | 允许同时提供的字段 | 不允许提供的字段 |
| --- | --- | --- | --- |
| `preset` | 按预设时长计算到期时间；未传 `expires_preset` 时为 24 小时。 | `expires_preset` | `expires_at` |
| `custom` | 在指定的绝对时间到期。 | `expires_at`，且必须提供 | `expires_preset` |
| `permanent` | 不设置到期时间，响应中省略 `expires_at`。生产环境应谨慎使用。 | 无 | `expires_preset`、`expires_at` |

`workspace_access` 权限边界：

| 值 | 共享页面表现 | 允许的 Workspace 文件操作 |
| --- | --- | --- |
| `none` | 不显示 Workspace 文件，页面提示文件未包含在 ShareLink 中。 | 无；共享 Workspace API 返回 `403`。聊天和实例共享页面仍可访问。 |
| `read` | 显示只读 Workspace 文件面板。 | 列出目录、预览文件、下载文件；上传、建目录、重命名和删除返回 `403`。 |
| `write` | 显示可写 Workspace 文件面板。 | 包含 `read` 的全部能力，并允许上传、建目录、重命名和删除。该模式允许外部访问者修改实例文件，应仅授予可信调用方。 |

`workspace_access` 绑定在生成出的 ShareLink 上。修改客户端配置不会影响已经存在的链接；`reset-url` 和 `reset-password` 也会保留原权限。要把已有链接从 `none` 改为 `read` 或 `write`，必须使用本节接口重新启用密码模式，这会替换原 URL 和密码。

请求体传空对象 `{}` 等价于 `{"expires_mode":"preset","expires_preset":"24h","workspace_access":"none"}`，即 24 小时有效且不共享 Workspace 文件。

成功响应带有 `Cache-Control: no-store`：

```json
{
  "instance_id": 123,
  "auth_mode": "password",
  "share_url": "/s/sl_created_example/",
  "password": "pwd_generated-secret",
  "workspace_access": "none",
  "expires_at": "2026-08-12T10:30:00Z",
  "updated_at": "2026-08-11T10:30:00Z"
}
```

调用该接口会替换该实例已有的外部访问 URL 和凭据。密码只通过本次北向响应返回，调用方不得写入日志，应立即保存到安全的密钥管理系统。响应丢失时可再次调用接口并只保留最后一次成功响应中的 URL 和密码。

响应字段：

| 字段 | 含义 |
| --- | --- |
| `instance_id` | 该 ShareLink 所属的受支持实例 ID。 |
| `auth_mode` | 本接口固定返回 `password`。ShareLink 不创建独立用户名，访问凭据是 URL 与密码的组合。 |
| `share_url` | 相对于 ClawManager 门户公开 Origin 的路径，不是北向 Gateway URL；完整 URL 的拼接方式见 5.4。 |
| `password` | 新生成的 ShareLink 密码。属于敏感信息，不得记录；启用和密码重置响应会返回。 |
| `workspace_access` | 实际生效的 Workspace 权限：`none`、`read` 或 `write`。调用方应以响应值为准。 |
| `expires_at` | RFC 3339 到期时间；永久模式下省略。到期后链接不可用。 |
| `updated_at` | 本次外部访问配置更新时间，RFC 3339。 |

### 5.2 重置 URL

重置请求正文为一个空 JSON 对象 `{}`。

```http
POST /api/northbound/v1/lite-instances/123/external-access/share-link/reset
Authorization: Bearer <access-token>
Content-Type: application/json

{}
```

成功后：

- 旧 ShareLink URL 立即失效。
- 旧分享会话立即失效。
- 认证模式、密码、有效期和工作区权限保持不变。

该接口不接受有效期或 `workspace_access` 参数；传递空对象 `{}`。如果 ShareLink 尚未启用，返回 `SHARE_LINK_NOT_ENABLED`。

```json
{
  "instance_id": 123,
  "auth_mode": "password",
  "share_url": "/s/sl_new_example/",
  "workspace_access": "read",
  "expires_at": "2026-08-18T10:30:00Z",
  "updated_at": "2026-08-11T10:35:00Z"
}
```

### 5.3 重置密码

该接口只适用于已经启用密码认证的 ShareLink。

```http
POST /api/northbound/v1/lite-instances/123/external-access/password/reset
Authorization: Bearer <access-token>
Content-Type: application/json

{}
```

成功后：

- ShareLink URL 保持不变。
- 旧密码和旧分享会话立即失效。
- 新密码只在本次响应中返回，调用方应立即保存到密钥管理系统。

该接口不接受有效期或 `workspace_access` 参数；传递空对象 `{}`。它不会改变 URL、有效期或 Workspace 权限。如果实例没有启用 ShareLink，返回 `SHARE_LINK_NOT_ENABLED`；如果当前不是密码模式，返回 `SHARE_LINK_PASSWORD_NOT_ENABLED`。

```json
{
  "instance_id": 123,
  "auth_mode": "password",
  "share_url": "/s/sl_current_example/",
  "password": "pwd_new-secret",
  "workspace_access": "read",
  "expires_at": "2026-08-18T10:30:00Z",
  "updated_at": "2026-08-11T10:36:00Z"
}
```

### 5.4 拼接完整 ShareLink URL

`share_url` 是相对于 ClawManager 门户公开 Origin 的路径，不一定属于北向 Gateway 域名。不能使用 `NORTHBOUND_BASE_URL` 直接拼接，除非北向 Gateway 和门户确实使用相同 Origin。例如门户公开地址为 `https://claw.example.com` 时，完整地址是：

```text
https://claw.example.com/s/sl_current_example/
```

拼接规则为：

```text
absolute_share_url = CLAWMANAGER_PUBLIC_BASE_URL + share_url
```

`CLAWMANAGER_PUBLIC_BASE_URL` 只包含协议、主机和可选端口，例如 `https://172.16.1.12:39443`，不要包含 `/api` 或 `/s/.../`。Python Demo 配置该变量后，会保留原始 `share_url` 并额外输出 `share_url_absolute`；未配置时只输出服务端返回的相对路径。

## 6. 错误处理

所有错误都使用统一结构：

```json
{
  "code": "INSTANCE_NOT_FOUND",
  "message": "Instance not found",
  "request_id": "req_example"
}
```

常见错误码：

| HTTP | `code` | 说明 |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | 请求格式、Header 或幂等键不合法 |
| 401 | `INVALID_CREDENTIALS` | JWE、挑战、用户名或密码不正确 |
| 401 | `AUTH_INVALID` | Access/Refresh Token 无效、过期或会话已撤销 |
| 403 | `SCOPE_DENIED` | Token 缺少接口所需 Scope |
| 404 | `INSTANCE_NOT_FOUND` | 实例不存在、不是北向支持的 Runtime，或不属于当前用户 |
| 404 | `OPERATION_NOT_FOUND` | 操作不存在或不属于当前用户 |
| 409 | `IDEMPOTENCY_CONFLICT` | 幂等键已用于不同请求 |
| 409 | `SHARE_LINK_NOT_ENABLED` | 实例未启用 ShareLink |
| 409 | `SHARE_LINK_PASSWORD_NOT_ENABLED` | ShareLink 不是密码认证模式 |
| 422 | `VALIDATION_ERROR` | 实例创建或 ShareLink 参数不合法 |
| 429 | `RATE_LIMITED` | 超过接口速率或未完成操作数量限制 |
| 503 | `DEPENDENCY_UNAVAILABLE` | Core 或依赖服务暂时不可用 |

对于 `429`，客户端应读取 `Retry-After`，使用指数退避并加入随机抖动。对于 `5xx`，可在确认请求幂等性的前提下重试。密码重置响应丢失时，可以再次执行密码重置并只保存最后一次成功响应中的密码。

## 7. 速率限制

默认限制如下，实际部署可以在网关或上游入口进一步收紧：

| 接口 | 默认限制 |
| --- | --- |
| 获取挑战 | 每 IP 每分钟 10 次 |
| 登录 | 每 IP 每分钟 5 次；每账号另有登录次数限制 |
| 创建受支持实例 | 每用户每分钟 10 次，且最多 5 个未完成创建操作 |
| 查询实例/操作 | 每用户每分钟 120 次 |
| ShareLink 启用及 URL/密码重置 | 三个接口合计每用户每分钟 10 次 |

## 8. Python Demo

运行环境：Python 3.10 或更高版本。JWE 加密依赖 `cryptography`，`.env` 配置加载依赖 `python-dotenv`：

```powershell
python -m pip install -r examples/requirements-northbound.txt
```

Demo 会自动读取 `examples/.env`。测试集群可使用以下配置：

```dotenv
NORTHBOUND_BASE_URL=https://<northbound-host>:<northbound-port>
NORTHBOUND_CA_FILE=northbound-ca.crt
NORTHBOUND_USERNAME=alice
NORTHBOUND_PASSWORD=your-password
NORTHBOUND_OWNER=customer-a
NORTHBOUND_INSTANCE_TYPE=openclaw
NORTHBOUND_INSTANCE_MODE=lite
NORTHBOUND_INSTANCE_NAME=
NORTHBOUND_WAIT_CREATE=true
NORTHBOUND_ENABLE_SHARELINK=false
NORTHBOUND_SHARELINK_EXPIRES_MODE=preset
NORTHBOUND_SHARELINK_EXPIRES_PRESET=24h
NORTHBOUND_SHARELINK_WORKSPACE_ACCESS=none
CLAWMANAGER_PUBLIC_BASE_URL=https://<portal-host>:<portal-port>
NORTHBOUND_SHOW_SECRETS=false
```

`examples/.env` 已被 Git 忽略，不得提交或分享。相对证书路径以 `examples/` 为基准解析。使用内部 CA 或自签名测试证书时必须设置 `NORTHBOUND_CA_FILE`，不要通过关闭 TLS 校验绕过证书验证。调用进程中已经存在的环境变量优先于 `.env`，可用于临时覆盖配置。

Demo 环境变量说明：

| 变量 | 使用命令 | 必填/默认值 | 含义与取值范围 |
| --- | --- | --- | --- |
| `NORTHBOUND_BASE_URL` | 全部 | 默认 `https://localhost:38443` | 北向 Gateway 的绝对 HTTPS 地址，可包含端口，不包含 `/api/northbound/v1`。 |
| `NORTHBOUND_CA_FILE` | 全部 | 使用私有 CA 时必填 | 用于验证北向 Gateway 服务端证书的 CA 文件路径。不要用关闭 TLS 校验代替。 |
| `NORTHBOUND_USERNAME` | 全部 | 必填 | 现有 ClawManager 用户名。只在本地构造 JWE，不以明文发送。 |
| `NORTHBOUND_PASSWORD` | 全部 | 必填 | 现有用户密码。只在本地构造 JWE；不得提交到版本库。 |
| `NORTHBOUND_OWNER` | `create`、`list` | 必填 | 创建者或业务归属标识；列表只返回与它精确匹配的实例。 |
| `NORTHBOUND_HTTP_TIMEOUT_SECONDS` | 全部 | 默认 `30` | 单次 HTTPS 请求超时，正整数秒；空值、非整数或非正数回退到默认值。 |
| `NORTHBOUND_INSTANCE_TYPE` | `create` | 默认 `openclaw` | Lite 和 Pro 均可选 `openclaw`、`hermes`、`opencode` 或 `deepseek-harness`。 |
| `NORTHBOUND_INSTANCE_MODE` | `create`、`list`、`get` | 默认 `lite` | `lite` 使用 `/lite-instances`，`pro` 使用 `/pro-instances`。该值只控制 Demo 选择路径，不会作为请求字段发送。 |
| `NORTHBOUND_INSTANCE_NAME` | `create` | 默认自动生成 | 实例名称；空值时生成 `api-<type>-<毫秒时间戳>`，非空时必须同时满足创建接口的 3～50 Unicode 字符和 3～50 UTF-8 字节限制。 |
| `NORTHBOUND_DESCRIPTION` | `create` | 默认省略 | 实例备注，最多 2000 UTF-8 字节。 |
| `NORTHBOUND_IDEMPOTENCY_KEY` | `create` | 默认每次生成 UUID | 8～128 UTF-8 字节。要安全重试同一次业务创建，必须保存并复用相同值。 |
| `NORTHBOUND_WAIT_CREATE` | `create` | 默认 `true` | 是否轮询 Operation 到终态。启用自动 ShareLink 时必须为 `true`。 |
| `NORTHBOUND_POLL_INTERVAL_MS` | `create` | 默认 `2000` | Operation 轮询间隔，正整数毫秒。 |
| `NORTHBOUND_POLL_TIMEOUT_MS` | `create` | 默认 `180000` | Operation 总等待时间，正整数毫秒；超时不代表服务端创建一定失败，可用 Operation ID 继续查询。 |
| `NORTHBOUND_ENABLE_SHARELINK` | `create` | 默认 `false` | 适用于五种受支持 Runtime。`true` 时在创建 Operation 成功后调用密码模式启用接口。必须同时设置 `NORTHBOUND_WAIT_CREATE=true` 和 `NORTHBOUND_SHOW_SECRETS=true`。 |
| `NORTHBOUND_SHARELINK_EXPIRES_MODE` | `create`、`enable-password` | 默认 `preset` | `preset`、`custom` 或 `permanent`；语义及组合规则见 5.1。 |
| `NORTHBOUND_SHARELINK_EXPIRES_PRESET` | `create`、`enable-password` | 默认 `24h` | 预设模式下使用：`1h`、`24h`、`7d` 或 `30d`。 |
| `NORTHBOUND_SHARELINK_EXPIRES_AT` | `create`、`enable-password` | custom 模式必填 | 未来的 RFC 3339 时间；仅在 `expires_mode=custom` 时发送。 |
| `NORTHBOUND_SHARELINK_WORKSPACE_ACCESS` | `create`、`enable-password` | 默认 `none` | `none`、`read` 或 `write`；具体文件权限见 5.1。修改该变量不会改变已有链接。 |
| `CLAWMANAGER_PUBLIC_BASE_URL` | ShareLink 输出 | 默认空 | ClawManager 门户公开 Origin，例如 `https://172.16.1.12:39443`。仅用于把相对 `share_url` 生成 `share_url_absolute`，不是北向 API 地址。 |
| `NORTHBOUND_SHOW_SECRETS` | `create`、`enable-password`、`reset-password` | 默认 `false` | 只有在安全交互终端需要接收新密码时才设为 `true`；保存凭据后立即恢复为 `false`。 |
| `NORTHBOUND_INSTANCE_ID` | `get`、ShareLink 命令 | 对相关命令必填 | 十进制正整数实例 ID。 |
| `NORTHBOUND_OPERATION_ID` | `operation` | 必填 | 创建接口返回的 Operation ID，原样填写。 |
| `NORTHBOUND_PAGE` | `list` | 默认 `1` | 正整数页码。 |
| `NORTHBOUND_LIMIT` | `list` | 默认 `20` | 正整数；API 有效范围 1～100。 |

Demo 的布尔环境变量将 `1`、`true`、`yes`、`on`（不区分大小写）识别为真；建议文档化配置统一使用 `true` 或 `false`。

常用命令：

```powershell
# 查看帮助
python examples/northbound_client.py --help

# 验证登录并查询当前身份
python examples/northbound_client.py me

# 创建 Lite 实例并轮询到终态
# 先在 .env 中设置 NORTHBOUND_INSTANCE_NAME=api-openclaw-01
python examples/northbound_client.py create

# 创建完成后自动开启密码模式并显示一次 URL/密码
# 在安全终端中同时设置 NORTHBOUND_ENABLE_SHARELINK=true 和 NORTHBOUND_SHOW_SECRETS=true
python examples/northbound_client.py create

# 查询实例列表
python examples/northbound_client.py list

# 查询实例
# 先在 .env 中设置 NORTHBOUND_INSTANCE_ID=123
python examples/northbound_client.py get

# 重启或重置实例；通过 NORTHBOUND_INSTANCE_MODE 选择 lite/pro 路径
# 两个命令都会轮询异步 Operation；重启保留数据，重置会建立一个新 ID 的干净实例并永久删除旧实例数据
# reset-instance 会显式发送 confirm_data_loss=true，执行前请先完成独立备份
python examples/northbound_client.py restart-instance
python examples/northbound_client.py reset-instance

# 为已有实例开启密码模式；先设置 NORTHBOUND_INSTANCE_ID 和 NORTHBOUND_SHOW_SECRETS=true
python examples/northbound_client.py enable-password

# 重置 ShareLink URL
# 可在 .env 中设置 CLAWMANAGER_PUBLIC_BASE_URL=https://claw.example.com
python examples/northbound_client.py reset-url

# 密码重置是破坏性操作；Demo 要求在安全终端明确允许显示新密码
# 临时将 .env 中的 NORTHBOUND_SHOW_SECRETS 改为 true，执行后立即恢复为 false
python examples/northbound_client.py reset-password
```

Demo 每次执行都会获取新挑战并完成一次 JWE 登录，并使用经过 TLS 验证的网关 `Date` 响应头生成 `issued_at`，不依赖本机系统时钟。Demo 不会把用户名或密码放入登录请求明文中，也不会打印 Access Token 和 Refresh Token。
为防止新密码生成后又因脱敏输出而丢失，`enable-password`、`reset-password` 以及自动启用 ShareLink 的 `create` 在未设置 `NORTHBOUND_SHOW_SECRETS=true` 时会在调用接口前终止。自动启用还要求 `NORTHBOUND_WAIT_CREATE=true`。

## 9. 上线前验收清单

至少使用一个专用测试 owner 完成以下验收，不要只检查 HTTP `202`：

1. JWE challenge/login 成功，`/auth/me` 返回创建、读取和 ShareLink Scope；登录请求和日志中没有明文密码。
2. 分别以 `openclaw`、`hermes`、`opencode` 和 `deepseek-harness` 验证 `/lite-instances` 与 `/pro-instances`。
3. 每次创建都保存并复用稳定的 `Idempotency-Key`；相同请求重放返回同一个 Operation，不产生重复实例。
4. 轮询 Operation 到 `succeeded` 后，再轮询实例到 `status=running`、`availability=available`。
5. 验证 Lite Runtime 为 Gateway 且保持 2 CPU、4 GB、5 GB、无 GPU；OpenClaw/Hermes/OpenCode/DeepSeek Harness Pro 为 Desktop，镜像与系统设置中启用的 DESKTOP 卡片完全一致，资源为 4 CPU、8 GB、50 GB、无 GPU。
6. `/pro-instances` 能按 owner 返回四种独立 Pro。错误 owner 返回空列表；跨用户查询单实例返回 `INSTANCE_NOT_FOUND`。
7. 所有北向受支持实例都能通过统一 ShareLink 接口启用密码模式；完整 URL 使用门户 Origin 拼接，而不是北向 Gateway Origin。
8. 分别验证 `workspace_access=none`、`read` 和 `write` 的文件权限边界。
9. 验证 URL 重置后旧 URL 立即失效，密码重置后旧密码和旧会话立即失效，未启用 ShareLink 时重置返回对应 `409`。
10. 使用 IEI SSO URL 换取独立 HttpOnly 会话，确认 owner 门户只显示同邮箱实例；原始 AES token 从地址栏移除，匿名请求返回 `401`。
11. 使用受信任 CA 验证 Gateway 和门户 HTTPS；不要以关闭 TLS 校验作为验收通过条件。
12. 检查 Gateway/Core 审计日志和 `X-Request-ID`，确认没有 Access Token、Refresh Token、JWE 明文、ShareLink 密码或 IEI AES 密钥泄漏。
