# ClawManager 北向接口 Pro/Lite 接入与镜像配置说明

## 1. 版本与交付物

本文不绑定某个临时分支、测试标签或内网 Registry。部署时应从目标发布提交构建并记录不可变的 ClawManager 镜像摘要。ClawManager 应用镜像不包含 OpenClaw、Hermes、OpenCode、DeepSeek Harness 等 Runtime 镜像；部署方应分别准备并验证每个启用 Runtime 的镜像。

## 2. 本次北向接口增加的能力

北向接口在 Lite 创建流程之外，提供独立的 Pro 创建、查询和镜像解析能力。

### 2.1 支持的 Runtime 矩阵

| Runtime | Lite 接口 | Pro 接口 | 说明 |
|---|---:|---:|---|
| OpenClaw | 支持 | 支持 | Lite 使用共享 Gateway Pool；Pro 使用独立桌面实例 |
| Hermes | 支持 | 支持 | Lite 使用共享 Gateway Pool；Pro 使用独立桌面实例 |
| OpenCode | 支持 | 支持 | Lite 使用共享 Gateway Pool；Pro 使用独立桌面实例 |
| DeepSeek Harness | 支持 | 支持 | Lite 使用共享 Gateway Pool；Pro 使用独立桌面实例 |

### 2.2 新增 Pro 接口

```text
POST /api/northbound/v1/pro-instances
GET  /api/northbound/v1/pro-instances
GET  /api/northbound/v1/pro-instances/{id}
```

Pro 接口支持的 `type`：

```text
openclaw
hermes
opencode
deepseek-harness
```

四种 Runtime 都通过 Pro 接口创建独立桌面实例。

### 2.3 Lite 接口

```text
POST /api/northbound/v1/lite-instances
GET  /api/northbound/v1/lite-instances
GET  /api/northbound/v1/lite-instances/{id}
```

Lite 接口支持的 `type`：

```text
openclaw
hermes
opencode
deepseek-harness
```

服务端根据 `type` 决定实际模式：

- OpenClaw、Hermes、OpenCode、DeepSeek Harness：创建 Lite 实例。
- 调用方不能在请求体中传 `mode`、镜像、CPU、内存或磁盘参数。

### 2.4 ShareLink 对 Lite 和 Pro 保持统一

以下路径继续使用 `lite-instances` 前缀，但同时适用于受支持的 Lite 实例和 Pro 实例：

```text
POST /api/northbound/v1/lite-instances/{id}/external-access/password
POST /api/northbound/v1/lite-instances/{id}/external-access/share-link/reset
POST /api/northbound/v1/lite-instances/{id}/external-access/password/reset
```

这样调用方不需要根据实例模式维护两套 ShareLink 接口。

## 3. Lite 与 Pro 的核心区别

| 项目 | Lite | Pro |
|---|---|---|
| 创建路径 | `/lite-instances` | `/pro-instances` |
| 运行方式 | 共享 Runtime Gateway Pool | 每个实例独立桌面 Pod |
| 镜像来源 | 集群中的对应 Lite Runtime Deployment | ClawManager“设置 → 镜像设置”中启用的 DESKTOP 镜像 |
| 调用方是否传镜像 | 不允许 | 不允许 |
| 调用方是否传资源 | 不允许 | 不允许 |
| 工作区 | 每个实例独立目录，底层由 Lite Pool 挂载共享 Workspace | 每个实例独立持久卷/工作区 |
| 返回与查询 | 异步 Operation，完成后返回实例 ID | 异步 Operation，完成后返回实例 ID |
| ShareLink | 统一接口 | 统一接口 |

Lite 使用共享 Pool 不代表实例共用工作区或核心配置。每个 Lite 实例仍按实例 ID 和用户进行隔离，只有 Runtime 进程承载方式采用共享 Gateway Pool。

Pro 资源由服务端固定：

| Pro 类型 | CPU | 内存 | 磁盘 | GPU |
|---|---:|---:|---:|---:|
| OpenClaw / Hermes / OpenCode / DeepSeek Harness | 4 核 | 8 GB | 50 GB | 无 |

## 4. 北向认证流程

北向 Gateway 只提供 HTTPS。客户端需要信任该部署自己的北向 Gateway CA，不能复用另一套服务器的 CA。

认证流程：

1. 客户端调用 `POST /auth/challenge` 获取一次性 challenge 和 RSA 公钥。
2. 客户端在本地将用户名、密码、challenge、nonce 和时间戳封装成 JWE。
3. 客户端调用 `POST /auth/login`，外层请求只携带 `challenge_id` 和密文 `credential_jwe`。
4. 服务端返回 Access Token 和轮换式 Refresh Token。
5. 后续请求使用 `Authorization: Bearer <access_token>`。
6. Refresh Token 每次刷新后立即失效，调用方必须保存新返回的 Refresh Token。
7. 使用结束后调用 `POST /auth/logout` 注销会话。

认证接口：

```text
POST /api/northbound/v1/auth/challenge
POST /api/northbound/v1/auth/login
POST /api/northbound/v1/auth/refresh
POST /api/northbound/v1/auth/logout
GET  /api/northbound/v1/auth/me
```

因为登录请求包含 JWE 构造，不建议直接手写普通 `curl` 登录。仓库内的 `examples/northbound_client.py` 已完整实现 CA 校验、challenge、JWE 登录、令牌刷新和各实例接口。

## 5. 调用方环境配置

在 `examples/.env` 中填写：

```dotenv
NORTHBOUND_BASE_URL=https://<服务器地址>:<北向端口>
CLAWMANAGER_PUBLIC_BASE_URL=https://<服务器地址>:<门户端口>
NORTHBOUND_CA_FILE=/absolute/path/to/northbound-ca.crt

NORTHBOUND_USERNAME=<北向账号>
NORTHBOUND_PASSWORD=<北向密码>
NORTHBOUND_OWNER=user@example.com

NORTHBOUND_HTTP_TIMEOUT_SECONDS=60
NORTHBOUND_POLL_INTERVAL_MS=2000
NORTHBOUND_POLL_TIMEOUT_MS=900000
NORTHBOUND_WAIT_CREATE=true
```

注意：

- `NORTHBOUND_OWNER` 必须是实例归属用户的完整邮箱。
- CA 文件必须来自当前北向 Gateway 部署。
- `.env`、CA 以外的私钥、Token、密码不得提交到 Git。
- 调用方不需要 TLS 私钥，只需要用于验证服务器证书的 CA 公钥证书。
- 北向 Gateway 的 TLS 私钥和内部 mTLS/JWE 私钥只保存在 Kubernetes Secret 中。

安装 Python 依赖：

```bash
python -m pip install -r examples/requirements-northbound.txt
```

验证登录：

```bash
python examples/northbound_client.py me
```

## 6. 创建 Lite 实例

请求示例：

```http
POST /api/northbound/v1/lite-instances
Authorization: Bearer <access_token>
Idempotency-Key: <8至128字符的唯一键>
Content-Type: application/json

{
  "name": "demo-openclaw-lite",
  "owner": "user@example.com",
  "type": "openclaw",
  "description": "北向接口创建的 OpenClaw Lite 实例"
}
```

请求字段：

| 字段 | 必填 | 限制 |
|---|---:|---|
| `name` | 是 | 3～50 个字符 |
| `owner` | 是 | 完整邮箱，归一化后精确匹配 |
| `type` | 是 | Lite 支持的 Runtime 类型 |
| `description` | 否 | 最长 2000 字符 |

使用示例客户端：

PowerShell：

```powershell
$env:NORTHBOUND_INSTANCE_MODE = "lite"
$env:NORTHBOUND_INSTANCE_TYPE = "openclaw"
$env:NORTHBOUND_INSTANCE_NAME = "demo-openclaw-lite"
$env:NORTHBOUND_DESCRIPTION = "OpenClaw Lite 验收实例"
$env:NORTHBOUND_IDEMPOTENCY_KEY = "demo-openclaw-lite-001"

python examples/northbound_client.py create
```

将 `NORTHBOUND_INSTANCE_TYPE` 分别改为：

```text
openclaw
hermes
opencode
deepseek-harness
```

即可创建四种 Lite 实例。

## 7. 创建 Pro 实例

请求体字段与 Lite 保持一致，只改变接口路径：

```http
POST /api/northbound/v1/pro-instances
Authorization: Bearer <access_token>
Idempotency-Key: <8至128字符的唯一键>
Content-Type: application/json

{
  "name": "demo-opencode-pro",
  "owner": "user@example.com",
  "type": "opencode",
  "description": "北向接口创建的 OpenCode Pro 实例"
}
```

使用示例客户端：

```powershell
$env:NORTHBOUND_INSTANCE_MODE = "pro"
$env:NORTHBOUND_INSTANCE_TYPE = "opencode"
$env:NORTHBOUND_INSTANCE_NAME = "demo-opencode-pro"
$env:NORTHBOUND_DESCRIPTION = "OpenCode Pro 验收实例"
$env:NORTHBOUND_IDEMPOTENCY_KEY = "demo-opencode-pro-001"

python examples/northbound_client.py create
```

Pro 接口当前支持：

```text
openclaw
hermes
opencode
deepseek-harness
```

服务端固定读取 ClawManager 中已保存且启用的对应 DESKTOP 镜像，调用方不能提交任意镜像地址。

## 8. 异步 Operation

创建接口返回 Operation，不保证 Runtime 已经立即可访问。调用方必须轮询：

```text
GET /api/northbound/v1/operations/{operation_id}
```

直到：

```json
{
  "status": "succeeded",
  "instance_id": 123
}
```

可能状态：

```text
pending
running
succeeded
failed
```

正确流程：

```text
提交创建
  → 保存 operation_id
  → 轮询 operation
  → succeeded 后保存 instance_id
  → 查询实例 availability
  → available 后向用户展示“进入实例”
```

Operation 成功只表示创建流程完成。前端仍应根据实例 `availability` 判断 Runtime 是否已真正可访问。

## 9. 查询实例

Lite/兼容视图：

```text
GET /api/northbound/v1/lite-instances?owner=user@example.com&page=1&limit=20
GET /api/northbound/v1/lite-instances/{id}
```

Pro 视图：

```text
GET /api/northbound/v1/pro-instances?owner=user@example.com&page=1&limit=20
GET /api/northbound/v1/pro-instances/{id}
```

示例客户端：

```powershell
$env:NORTHBOUND_INSTANCE_MODE = "lite"
python examples/northbound_client.py list

$env:NORTHBOUND_INSTANCE_MODE = "pro"
python examples/northbound_client.py list
```

## 10. Lite Runtime 镜像配置

Lite 实例不会为每个实例单独创建 Runtime Pod，而是进入对应共享 Gateway Pool。因此 Lite 镜像需要落实到集群 Deployment。

当前可提供的源 Registry 为 `10.130.15.40:5000`。以下 tag 已通过 Registry 接口确认存在：

| Runtime | Deployment | 建议镜像 |
|---|---|---|
| OpenClaw Lite | `openclaw-runtime` | `10.130.15.40:5000/agentsruntime/openclaw-lite:master-20260824-737ad4c` |
| Hermes Lite | `hermes-runtime` | `10.130.15.40:5000/agentsruntime/hermes-lite:profile-skill-dedupe-20260826-0bf0a76` |
| OpenCode Lite | `opencode-runtime` | `10.130.15.40:5000/agentsruntime/opencode-lite:master-20260825-987f05d` |
| DeepSeek Harness Lite | `deepseek-harness-runtime` | `10.130.15.40:5000/agentsruntime/deepseek-harness-lite:master-20260824-737ad4c` |

部署方必须在自己实际使用的 k3s/Kubernetes 工作负载配置中写入上述 Lite 镜像，并确保四个 Deployment 都存在且能够挂载 Workspace。仅在管理后台保存 Lite 卡片不会自动创建缺失的 Pool Deployment，也不会自动更新正在运行的 Pool。

k3s 注意事项：

- 所有可能运行 Runtime Pod 的节点都必须能访问源 Registry 或部署方自己的目标 Registry。
- 如果 Registry 使用 HTTP 或内部 CA，需要先完成 k3s Registry 镜像源/信任配置。
- 多节点集群不能只在一个节点本地导入镜像，除非所有 Runtime 都被固定到该节点。
- 正式部署应固定本文 tag 或 digest，不要使用浮动的 `latest`。
- Runtime 桌面镜像解压后体积较大，节点需要预留足够磁盘空间。

更新后检查：

```bash
kubectl -n <system-namespace> get deploy   openclaw-runtime hermes-runtime opencode-runtime deepseek-harness-runtime

kubectl -n <system-namespace> get pods   -l clawmanager.io/runtime-type=openclaw

kubectl -n <system-namespace> rollout status deployment/openclaw-runtime
kubectl -n <system-namespace> rollout status deployment/hermes-runtime
kubectl -n <system-namespace> rollout status deployment/opencode-runtime
kubectl -n <system-namespace> rollout status deployment/deepseek-harness-runtime
```

## 11. Pro Runtime 镜像配置

Pro 镜像不再由北向 YAML 写死，也不允许调用方在请求体传入。北向服务在创建时读取 ClawManager 数据库中已启用的 `desktop` 镜像卡片。

在管理后台进入：

```text
设置 → 镜像设置
```

为每个 Runtime 保存对应的 Pro/DESKTOP 镜像。

OpenClaw、Hermes、OpenCode 当前 Lite/Pro 使用相同构建内容。部署方可以从 `10.130.15.40:5000` 拉取对应 Lite tag，再在自己的 Registry 中增加不带 `-lite` 的 Pro tag。建议配置：

| Runtime | `instance_type` | `runtime_type` | `runtime_variant` | 建议镜像 |
|---|---|---|---|---|
| OpenClaw Pro | `openclaw` | `desktop` | 空 | `<目标Registry>/agentsruntime/openclaw:master-20260824-737ad4c` |
| Hermes Pro | `hermes` | `desktop` | 空 | `<目标Registry>/agentsruntime/hermes:profile-skill-dedupe-20260826-0bf0a76` |
| OpenCode Pro | `opencode` | `desktop` | 空 | `<目标Registry>/agentsruntime/opencode:master-20260825-987f05d` |
| DeepSeek Harness Pro | `deepseek-harness` | `desktop` | 空 | `<目标Registry>/agentsruntime/deepseek-harness:<immutable-tag>` |

转存示例：

```bash
docker pull 10.130.15.40:5000/agentsruntime/openclaw-lite:master-20260824-737ad4c
docker tag \
  10.130.15.40:5000/agentsruntime/openclaw-lite:master-20260824-737ad4c \
  <目标Registry>/agentsruntime/openclaw:master-20260824-737ad4c
docker push <目标Registry>/agentsruntime/openclaw:master-20260824-737ad4c
```

Hermes 和 OpenCode 按相同方式转存。分开命名不是因为镜像内容必须不同，而是为了避免运维时把 Gateway/Lite 与 Desktop/Pro 配置混淆。

后台保存接口：

```text
GET    /api/v1/system-settings/images
PUT    /api/v1/system-settings/images
DELETE /api/v1/system-settings/images/{id-or-instanceType}
```

保存一个 OpenCode Pro 镜像的请求示例：

```json
{
  "instance_type": "opencode",
  "runtime_type": "desktop",
  "display_name": "OpenCode Pro",
  "image": "<目标Registry>/agentsruntime/opencode:master-20260825-987f05d"
}
```

北向创建时按 `instance_type + runtime_type=desktop` 精确选择镜像，防止同一个 Runtime 同时存在 Lite/Pro 卡片时错误命中 Lite 镜像。

如果没有启用对应 Desktop 镜像，Pro 创建返回：

```text
PRO_IMAGE_NOT_CONFIGURED
```

## 12. 镜像关系说明

OpenClaw、Hermes、OpenCode 的 Lite/Pro 镜像可以来自相同构建内容，但建议使用不同 Registry 仓库名：

```text
agentsruntime/openclaw-lite:<tag>
agentsruntime/openclaw:<tag>

agentsruntime/hermes-lite:<tag>
agentsruntime/hermes:<tag>

agentsruntime/opencode-lite:<tag>
agentsruntime/opencode:<tag>
```

即使镜像 digest 相同，分开命名仍能明确表达部署模式，并避免运维人员把 Gateway Pool 镜像误配置到 Desktop 卡片。

DeepSeek Harness 同样分别配置 Lite Pool 和 Pro Desktop 镜像：

```text
agentsruntime/deepseek-harness-lite:<tag>
agentsruntime/deepseek-harness:<tag>
```

## 13. 在 k3s 中导入 ClawManager TAR

校验：

```bash
sha256sum -c clawmanager-hxc-app_northbound-all-pro_20260827-2d3456c.tar.sha256
```

导入 k3s 使用的 containerd：

```bash
sudo k3s ctr images import \
  clawmanager-hxc-app_northbound-all-pro_20260827-2d3456c.tar

sudo k3s crictl images | grep clawmanager-hxc-app
```

多节点集群中，需要保证可能运行 `clawmanager-app` 和 `clawmanager-northbound-gateway` 的节点都能获取该镜像。部署方可以逐节点导入，也可以导入后转存到所有节点可访问的 Registry。

## 14. k3s 部署注意事项

部署方应将 ClawManager 应用镜像引用写入自己的 k3s 安装配置，并保证 `clawmanager-app` 与 `clawmanager-northbound-gateway` 使用同一版本。

只导入或替换 ClawManager 应用镜像不会自动补齐以下内容：

- 北向 Gateway Deployment 和 Service。
- 北向外部 TLS CA/证书。
- Gateway 与 Core 之间的内部 mTLS。
- JWE 私钥及北向 JWT/Refresh Token Secret。
- IEI Owner 门户使用的 SSO Secret。
- 四个 Lite Runtime Pool。
- Workspace 和 Pro 持久化存储。
- 对外端口、防火墙、证书域名及 Runtime 公网 URL 模板。

这些部署要素需要由部署方结合自己的 k3s 安装方式处理。

更新后必须确认应用和 Gateway 使用同一版本镜像：

```bash
kubectl -n <system-namespace> get deploy   clawmanager-app clawmanager-northbound-gateway   -o custom-columns=NAME:.metadata.name,IMAGE:.spec.template.spec.containers[*].image

kubectl -n <system-namespace> rollout status deployment/clawmanager-app
kubectl -n <system-namespace> rollout status deployment/clawmanager-northbound-gateway
```

## 15. 验收清单

### 北向基础能力

- CA 能验证北向 Gateway 的 TLS 证书。
- challenge、JWE login、me、refresh、logout 全部成功。
- Access Token Scope 同时包含 Lite、Pro、ShareLink 权限。
- 相同 `Idempotency-Key` 和相同请求不会重复创建。
- 相同 `Idempotency-Key` 搭配不同请求返回冲突。

### Lite

- OpenClaw、Hermes、OpenCode、DeepSeek Harness 均能提交创建。
- Operation 最终为 `succeeded`。
- 实例最终为 `availability=available`。
- 四个 Runtime Pool Deployment 均为 Ready。
- 每个实例工作区彼此隔离。
- OpenCode/DeepSeek Harness 的 HTTPS 页面、静态资源和 WebSocket 正常。

### Pro

- OpenClaw、Hermes、OpenCode、DeepSeek Harness 通过 `/pro-instances` 创建。
- 创建时命中管理后台保存的 Desktop 镜像，而不是 Lite 镜像。
- Pro 实例 Pod 使用预期镜像和资源规格。
- 实例重启后工作区仍保留。

### ShareLink 与门户

- Lite 和 Pro 都能启用密码 ShareLink。
- URL 重置后旧 URL 立即失效。
- 密码重置后旧密码和旧会话立即失效。
- IEI Owner 门户能列出同一 owner 的 Lite 与 Pro 实例。
- “进入实例”只在实例真正可用后开放。

## 16. 验收记录要求

每次发布都应保存对应提交、镜像摘要、环境和时间，并记录以下矩阵与接口检查的实际结果；不得沿用其他版本的静态通过数字：

```text
OpenClaw Lite
Hermes Lite
OpenCode Lite
DeepSeek Harness Lite
OpenClaw Pro
Hermes Pro
OpenCode Pro
DeepSeek Harness Pro
```

接口检查至少覆盖：

```text
challenge/login
me
refresh
Lite/Pro create
operation polling
Lite/Pro list
Lite/Pro get
ShareLink password enable
ShareLink URL reset
ShareLink password reset
logout
IEI Owner 门户 URL 生成
```
