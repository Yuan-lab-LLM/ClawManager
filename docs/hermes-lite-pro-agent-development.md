# Hermes Lite / Pro Agent 开发说明

本文写给 Hermes agent 侧开发者，用来区分 ClawManager 中 Hermes Lite 和 Hermes Pro 两种形态。先读本文，再分别参考：

- Lite: `docs/agent-runtime-development-spec.md` 和 `docs/clawmanager-agent-v2-contract.md`
- Pro: `docs/hermes-runtime-agent-development.md` 和 `docs/agent-control-plane.md`

一句话原则：

- **Hermes Lite 是 Runtime Pod Agent V2**：一个共享 `hermes-runtime` Pod 内只有一个 pod-level agent，它负责创建、删除和上报多个 Hermes gateway 子进程。
- **Hermes Pro 是 Instance Agent Control Plane**：每个 Hermes Pro 实例有自己的桌面容器和 instance-level agent，它只负责这个实例自己的状态、命令、skill 和 metrics。

不要只根据镜像名或 `hermes` 这个 runtime 类型判断开发目标。Hermes Lite 和 Hermes Pro 都叫 Hermes，但 agent 协议、进程模型、目录和端口完全不同。

## 1. 模式对照

| 项目 | Hermes Lite | Hermes Pro |
| --- | --- | --- |
| ClawManager mode | `lite` | `pro` |
| Runtime backend | `gateway` | `desktop` |
| Kubernetes 资源 | 共享 Hermes runtime Deployment/Pod | 每个实例一个专属 Deployment + Service |
| agent 定位 | Pod 级 gateway 管理器 | 实例级状态和命令 agent |
| agent 入站端口 | 必须监听 `0.0.0.0:19090` | 不需要实现 runtime-pod 控制端口 |
| ClawManager 调 agent | `GET /v1/health`、`POST /v1/gateways`、`DELETE /v1/gateways/{id}`、`POST /v1/drain` | 不适用 |
| agent 上报 ClawManager | `/api/v1/runtime-agent/*` | `/api/v1/agent/*` |
| 用户实例进程 | 每个 Lite 实例是一个 gateway 子进程 | 整个桌面容器就是实例 |
| 用户访问端口 | agent 分配 `20000-20299` 中的 gateway 端口 | Webtop/KasmVNC `3001` |
| workspace | `/workspaces/hermes/user-{user_id}/instance-{instance_id}` | `/config/.hermes` |
| 典型实现参考 | 按 OpenClaw Lite 的 Runtime Pod Agent V2 做 | 按当前 Hermes Webtop guide 做 |

## 2. 如何判断当前该跑哪种 agent

建议 Hermes 镜像里可以放同一个 `hermes-agent` 二进制，但必须拆成两个清晰入口：

```bash
hermes-agent runtime-pod   # Lite
hermes-agent instance      # Pro
```

也可以自动检测环境变量：

| 检测条件 | 应进入的模式 |
| --- | --- |
| `CLAWMANAGER_RUNTIME_TYPE=hermes` 且存在 `CLAWMANAGER_AGENT_PORT` 或 `RUNTIME_AGENT_PUBLIC_PORT` | Lite runtime-pod agent |
| `CLAWMANAGER_AGENT_ENABLED=true` 且存在 `CLAWMANAGER_AGENT_INSTANCE_ID` 和 `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` | Pro instance agent |

如果两组变量同时存在，优先按明确的启动参数决定；没有启动参数时建议拒绝启动并打印清晰错误，避免一个进程同时承担两套协议。

## 3. Hermes Lite 应该怎么开发

Hermes Lite 要仿照当前 OpenClaw Lite 做。差异只有 runtime 名称、Hermes gateway 启动命令、Hermes 配置文件位置和健康检查细节。

Lite agent 是共享 Pod 内的控制进程，不是某个用户实例内部的 agent。它启动后必须：

- 监听 `0.0.0.0:19090`。
- 校验 `X-ClawManager-Control-Token: ${CLAWMANAGER_AGENT_CONTROL_TOKEN}`。
- 实现 `GET /v1/health`、`POST /v1/gateways`、`DELETE /v1/gateways/{gateway_id}`、`POST /v1/drain`。
- 向 `${CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/*` 注册、heartbeat、metrics、gateway 状态和 skill inventory。
- 管理本 Pod 内多个 Hermes gateway 子进程，端口默认从 `20000-20299` 分配。
- 按 `instance_id + generation` 做幂等创建，重复请求不能启动多个进程。
- `POST /v1/gateways` 必须快速返回 `starting`，后台继续启动和健康检查。
- gateway 健康后再上报 `running`；缺少 LLM token、workspace 越权、端口冲突或 Hermes 配置失败时上报 `error`。
- 每个实例使用独立 workspace，不能共用一个全局 `/config/.hermes`。

Lite runtime Pod 会收到这些环境变量：

| 变量 | 用途 |
| --- | --- |
| `CLAWMANAGER_RUNTIME_TYPE=hermes` | runtime 类型，注册和上报都用 `hermes` |
| `CLAWMANAGER_BACKEND_URL` | ClawManager backend 内部地址 |
| `CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME` | 当前 runtime Deployment 名 |
| `CLAWMANAGER_RUNTIME_IMAGE_REF` | 当前 runtime 镜像 |
| `CLAWMANAGER_AGENT_PORT=19090` | agent 控制端口 |
| `CLAWMANAGER_AGENT_CONTROL_TOKEN` | ClawManager 调 agent 控制接口使用 |
| `CLAWMANAGER_AGENT_REPORT_TOKEN` | agent 上报 ClawManager 使用 |
| `RUNTIME_WORKSPACE_ROOT=/workspaces` | workspace 根目录 |
| `RUNTIME_AGENT_LISTEN_ADDR=0.0.0.0:19090` | agent 监听地址 |
| `RUNTIME_AGENT_PUBLIC_PORT=19090` | agent 对 ClawManager 暴露的端口 |
| `RUNTIME_GATEWAY_PORT_START` / `RUNTIME_GATEWAY_PORT_END` | gateway 端口范围 |
| `POD_NAME` / `POD_NAMESPACE` / `POD_IP` / `NODE_NAME` | Pod 身份和调度信息 |
| `CLAWMANAGER_TRUSTED_PROXY_CIDRS` | ClawManager 反代来源网段，用于 Hermes trusted proxy |

ClawManager 创建 Lite 实例时会调用 runtime-pod agent：

```http
POST /v1/gateways
X-ClawManager-Control-Token: ${CLAWMANAGER_AGENT_CONTROL_TOKEN}
Content-Type: application/json
```

```json
{
  "instance_id": 123,
  "user_id": 45,
  "generation": 7,
  "agent_type": "hermes",
  "workspace_path": "/workspaces/hermes/user-45/instance-123",
  "gateway_port_start": 20000,
  "gateway_port_end": 20299,
  "uid": 200123,
  "gid": 200123,
  "environment": {
    "CLAWMANAGER_LLM_BASE_URL": "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm",
    "CLAWMANAGER_LLM_API_KEY": "instance-token",
    "CLAWMANAGER_LLM_MODEL": "[{\"id\":\"gpt-4.1\"}]",
    "CLAWMANAGER_INSTANCE_TOKEN": "instance-token",
    "OPENAI_BASE_URL": "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm",
    "OPENAI_API_KEY": "instance-token"
  }
}
```

Lite agent 的建议处理流程：

1. 校验控制 token、`agent_type=hermes`、`workspace_path` 必须在 `/workspaces/hermes/` 下。
2. 用 `instance_id + generation` 查本地状态；如果已经创建过，返回同一个 gateway 元数据。
3. 创建 workspace，并把 owner 设置为请求里的 `uid/gid`。
4. 分配未占用 gateway 端口，记录端口、pid、workspace、generation。
5. 把请求里的 `environment` 白名单变量传给 Hermes gateway 子进程。
6. 写入 Hermes gateway 需要的 LLM、workspace、trusted proxy 和 instance token 配置。
7. 后台启动 Hermes gateway，例如 `hermes gateway run --port ${port}`，实际命令由 Hermes 项目确定。
8. 立即返回 `status=starting`。
9. 后台轮询健康检查，成功后上报 `/api/v1/runtime-agent/gateways/report` 为 `running`。
10. 删除时停止对应进程组并释放端口，但不要删除 workspace。

Lite 下浏览器访问链路是：

```text
Browser -> ClawManager -> Runtime Pod HTTP gateway
```

ClawManager 会向 Lite runtime gateway 代理请求注入实例 token。Hermes gateway 不应该要求用户手工粘贴 gateway token；它需要信任来自 ClawManager 的代理来源，并正确处理 `Authorization`、`X-Forwarded-*` 和内部 base URL。

## 4. Hermes Pro 应该怎么开发

Hermes Pro 继续按现有 Hermes Webtop 文档实现。Pro 是专属桌面实例，不需要实现 Lite 的 `/v1/gateways` 协议，也不需要在一个 Pod 里管理多个用户实例。

Pro image 的固定约定：

- 基于 Webtop/KasmVNC。
- 对外服务端口是 `3001`。
- 持久化目录是 `/config/.hermes`。
- ClawManager 会把 `SUBFOLDER` 设置为 `/api/v1/instances/{instance_id}/proxy/`。
- agent 作为 s6 longrun 或等价守护进程运行在这个实例容器里。

Pro instance agent 会收到这些环境变量：

| 变量 | 用途 |
| --- | --- |
| `CLAWMANAGER_AGENT_ENABLED=true` | 启用实例 agent |
| `CLAWMANAGER_AGENT_BASE_URL` | ClawManager API base URL，不带 `/api/v1/agent` 后缀 |
| `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` | 首次注册使用的一次性 token |
| `CLAWMANAGER_AGENT_INSTANCE_ID` | 当前实例 ID |
| `CLAWMANAGER_AGENT_PROTOCOL_VERSION=v1` | instance agent 协议版本 |
| `CLAWMANAGER_AGENT_PERSISTENT_DIR=/config/.hermes` | Hermes 持久化目录 |
| `CLAWMANAGER_AGENT_DISK_LIMIT_BYTES` | 实例磁盘配额 |

Pro agent 的职责：

- 读取 `CLAWMANAGER_AGENT_*` 变量。
- 向 `${CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/register` 注册。
- 使用 bootstrap 后返回的 session token 持续 heartbeat。
- 上报 runtime state、health、system metrics、config revision 和 skill inventory。
- 轮询并执行 ClawManager 下发的 instance command。
- 管理 `/config/.hermes/skills` 里的 skill 下载、安装、上传和 inventory。
- 所有状态都只代表这个 Pro 实例自身。

Pro agent 不应该实现：

- `POST /v1/gateways`
- gateway 端口池
- 多实例 workspace 隔离
- `/api/v1/runtime-agent/*` runtime pod 上报
- Lite 的 drain 和 gateway 迁移逻辑

## 5. OpenClaw 对照开发法

如果你已经看过 OpenClaw 的实现，可以这样映射：

| OpenClaw 概念 | Hermes Lite 应替换为 |
| --- | --- |
| `CLAWMANAGER_RUNTIME_TYPE=openclaw` | `CLAWMANAGER_RUNTIME_TYPE=hermes` |
| `agent_type=openclaw` | `agent_type=hermes` |
| `/workspaces/openclaw/user-{uid}/instance-{id}` | `/workspaces/hermes/user-{uid}/instance-{id}` |
| OpenClaw gateway 启动命令 | Hermes gateway 启动命令 |
| OpenClaw workspace 配置文件 | Hermes workspace 内的实例级配置文件 |
| OpenClaw 健康检查 | Hermes gateway 健康检查 |

不要把 OpenClaw Pro/Desktop 的 agent 逻辑直接套到 Hermes Lite。Lite 的关键不是“启动一个桌面”，而是“在共享 Pod 中稳定管理多个 gateway 子进程”。

## 6. 验收清单

Hermes Lite 验收：

- runtime pod agent 可以注册为 `runtime_type=hermes`。
- `GET /v1/health` 返回 ready 状态和真实容量。
- 创建 100 个 Hermes gateway 时端口唯一，状态最终变为 `running`。
- 重复 `POST /v1/gateways` 不会启动重复进程。
- 删除 gateway 会停止进程并释放端口，不会删除 workspace。
- `POST /v1/drain` 后拒绝新 create，已有 gateway 持续上报。
- Pod 重启后不会把不存在的旧 gateway 误报为 `running`。
- 缺少 LLM token 时 gateway 不报告 `running`，错误信息明确。
- 通过 ClawManager proxy 访问 Hermes Lite 不需要用户手工粘贴 token。
- 每个 Lite 实例的 skill inventory、配置和 workspace 互相隔离。

Hermes Pro 验收：

- Pro 实例的 Webtop 端口 `3001` 可通过 ClawManager proxy 访问。
- `/config/.hermes` 持久化，实例重启后数据仍在。
- instance agent 能注册、heartbeat、上报 state 和 metrics。
- instance command 能 poll、start、finish 并返回失败原因。
- `/config/.hermes/skills` 的 skill 下载、安装、上传和 inventory 可用。
- Pro agent 不监听或实现 `/v1/gateways`。

## 7. 常见错误

| 错误 | 正确做法 |
| --- | --- |
| 只实现 Pro instance agent，然后期望 Lite 可用 | Lite 必须实现 Runtime Pod Agent V2 |
| Lite agent 监听 `3001` | Lite agent 监听 `19090`，gateway 子进程监听 `20000-20299` |
| Lite 所有实例共用 `/config/.hermes` | Lite 每个实例使用自己的 `/workspaces/hermes/user-{uid}/instance-{id}` |
| `POST /v1/gateways` 阻塞到 Hermes gateway 完全启动 | 快速返回 `starting`，后台健康检查后上报 `running` |
| 重试 create 时启动多个 gateway | 按 `instance_id + generation` 幂等 |
| 把外部 HTTPS/NodePort 写进 runtime 内部配置 | runtime 内部用 ClawManager backend service DNS |
| 要求用户粘贴 gateway token | 使用 ClawManager 注入的 instance token 和 trusted proxy |
| Lite 上报 `/api/v1/agent/*` 当作生命周期依据 | Lite 生命周期必须上报 `/api/v1/runtime-agent/*` |
| Pro 实现 `/v1/gateways` 和端口池 | Pro 只实现 instance-level `/api/v1/agent/*` |
