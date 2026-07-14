# ClawManager Agent V2 开发规范

本文整理当前 ClawManager 代码中的最新 agent 约定，面向 OpenClaw、Hermes 以及后续新增的托管 runtime。开发或改造 runtime 镜像时，应优先遵守本文；字段级契约以 `docs/clawmanager-agent-v2-contract.md` 和后端代码为准。

## 1. 架构定位

最新架构中有两类 agent 通信面：

1. **Runtime Pod Agent，也就是本文主线的 V2 agent**
   运行在共享 runtime Pod 内，负责本 Pod 内多个 gateway 子进程的创建、删除、端口、workspace、资源隔离、健康检查和状态上报。ClawManager 通过它把一个实例绑定到 `pod_ip + gateway_port`。

2. **Instance Agent Control Plane，旧的 `/api/v1/agent/*` 协议**
   面向单个实例的状态、命令、skill inventory、skill package 上传和配置 revision 下载。当前后端仍保留这套协议，runtime 可以继续复用它完成实例级状态与 skill 管理；但 gateway 生命周期管理必须走 Runtime Pod Agent。

一句话原则：**Runtime Pod Agent 是 Pod 内 gateway 管理器，不是一个阻塞式启动脚本。**

## 2. 支持范围

当前 V2 托管 runtime 类型：

| 类型 | 说明 |
| --- | --- |
| `openclaw` | OpenClaw runtime |
| `hermes` | Hermes runtime |

当前平台默认值：

| 项 | 默认值 |
| --- | --- |
| workspace root | `/workspaces` |
| workspace path | `/workspaces/{runtime}/user-{user_id}/instance-{instance_id}` |
| gateway 端口范围 | `20000-20299` |
| 单 Runtime Pod 容量 | `100` |
| 实例 UID/GID | `200000 + instance_id` |
| agent 控制端口 | `19090` |

## 3. Runtime Pod Agent 职责

Runtime Pod Agent 必须实现：

- 启动本地控制服务：`GET /v1/health`、`POST /v1/gateways`、`DELETE /v1/gateways/{gateway_id}`、`POST /v1/drain`。
- 向 ClawManager 上报 runtime pod 注册、heartbeat、metrics、gateway 状态和 skill 状态。
- 管理本 Pod 内 gateway 子进程生命周期。
- 管理端口池，避免同一 Pod 内端口冲突。
- 创建并校验 workspace，设置正确 UID/GID。
- 将 ClawManager 下发的 LLM、代理、实例 token、runtime 配置写入实例自己的 workspace。
- 实现 drain，拒绝新 gateway，保持已有 gateway 运行，等待 ClawManager 迁移和删除。
- 避免任何敏感 token、API key、Authorization header 出现在日志中。

不应该由 Runtime Pod Agent 实现：

- 直接对浏览器暴露 gateway 自签地址。
- 管理 ClawManager 用户权限。
- 删除用户 workspace。
- 把外部 NodePort、外部 HTTPS 入口或固定内网 IP 写死进 runtime 配置。

## 4. 运行环境变量

当前 Runtime Deployment 构建器注入以下变量，agent 实现应优先支持：

| 变量 | 说明 |
| --- | --- |
| `CLAWMANAGER_RUNTIME_TYPE` | `openclaw` 或 `hermes` |
| `CLAWMANAGER_AGENT_PORT` | agent 控制端口，当前为 `19090` |
| `CLAWMANAGER_GATEWAY_PORT_START` | gateway 起始端口 |
| `CLAWMANAGER_GATEWAY_PORT_END` | gateway 结束端口 |
| `CLAWMANAGER_AGENT_CONTROL_TOKEN` | ClawManager 调 agent 控制接口使用 |
| `CLAWMANAGER_AGENT_REPORT_TOKEN` | agent 上报 ClawManager 使用 |

为了兼容早期文档和旧镜像，agent 建议同时识别以下别名：

| 新变量 | 兼容别名 |
| --- | --- |
| `CLAWMANAGER_AGENT_CONTROL_TOKEN` | `RUNTIME_AGENT_CONTROL_TOKEN` |
| `CLAWMANAGER_AGENT_REPORT_TOKEN` | `RUNTIME_AGENT_REPORT_TOKEN` |
| `CLAWMANAGER_GATEWAY_PORT_START` | `RUNTIME_GATEWAY_PORT_START` |
| `CLAWMANAGER_GATEWAY_PORT_END` | `RUNTIME_GATEWAY_PORT_END` |
| `CLAWMANAGER_AGENT_PORT` | `RUNTIME_AGENT_PUBLIC_PORT` |

建议支持的附加变量：

| 变量 | 默认值/要求 |
| --- | --- |
| `RUNTIME_WORKSPACE_ROOT` | 默认 `/workspaces` |
| `RUNTIME_AGENT_LISTEN_ADDR` | 默认 `0.0.0.0:19090` |
| `CLAWMANAGER_BACKEND_URL` | ClawManager backend 内部地址，推荐 Kubernetes Service DNS |
| `CLAWMANAGER_RUNTIME_IMAGE_REF` | 当前镜像标识，用于注册上报 |
| `POD_NAME` / `POD_NAMESPACE` / `POD_IP` / `NODE_NAME` | 建议通过 Downward API 注入 |
| `CLAWMANAGER_TRUSTED_PROXY_CIDRS` | ClawManager app/gateway 的可信代理网段 |

`CLAWMANAGER_BACKEND_URL` 必须使用集群内部地址，例如：

```text
http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001
```

不要在 runtime 内部配置中写入浏览器外部入口，例如 `https://172.16.1.12:39443`。

## 5. Agent 到 ClawManager 的上报接口

所有 Runtime Pod Agent 上报请求必须带：

```http
X-ClawManager-Agent-Token: ${CLAWMANAGER_AGENT_REPORT_TOKEN}
Content-Type: application/json
```

后端统一返回：

```json
{
  "success": true,
  "message": "...",
  "data": {}
}
```

### 5.1 注册 Runtime Pod

```http
POST {CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/register
```

必填字段：

- `runtime_type`
- `namespace`
- `pod_name`
- `deployment_name`
- `image_ref`

推荐完整 payload：

```json
{
  "runtime_type": "openclaw",
  "namespace": "clawmanager-system",
  "pod_name": "openclaw-runtime-6f77f8b8c7-abcde",
  "pod_uid": "pod-uid",
  "pod_ip": "10.42.0.31",
  "node_name": "node-a",
  "deployment_name": "openclaw-runtime",
  "image_ref": "ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest",
  "agent_endpoint": "http://10.42.0.31:19090",
  "state": "ready",
  "capacity": 100,
  "used_slots": 0,
  "draining": false,
  "metrics": {
    "agent_version": "0.1.0"
  },
  "reported_at": "2026-06-08T09:00:00Z"
}
```

要求：

- `agent_endpoint` 必须是 ClawManager Pod 可以访问的地址，通常是 `http://{POD_IP}:19090`，不能是 `127.0.0.1`。
- `capacity <= 0` 时后端会按 `100` 处理，但 agent 应主动上报真实容量。
- `state` 为空时后端按 `ready` 处理；agent 不应在初始化未完成时上报 ready。
- `metrics` 必须是合法 JSON。

### 5.2 Heartbeat

```http
POST {CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/heartbeat
```

payload：

```json
{
  "pod_id": 17,
  "namespace": "clawmanager-system",
  "pod_name": "openclaw-runtime-6f77f8b8c7-abcde",
  "state": "ready",
  "used_slots": 37,
  "draining": false,
  "reported_at": "2026-06-08T09:00:02Z"
}
```

要求：

- 优先使用注册返回的 `data.pod.id` 作为 `pod_id`。
- 没有 `pod_id` 时必须带 `namespace + pod_name`。
- `state` 必填。
- `used_slots` 必须来自当前真实 `starting/running` gateway 数量，不要使用过期缓存。
- 建议 heartbeat 间隔 2 秒；如果环境压力较大，可放宽，但必须小于 ClawManager 的 heartbeat 超时窗口。

### 5.3 Metrics

```http
POST {CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/metrics/report
```

payload：

```json
{
  "pod_id": 17,
  "cpu_millis_used": 13600,
  "memory_bytes_used": 42949672960,
  "disk_bytes_used": 214748364800,
  "network_rx_bytes": 9223372,
  "network_tx_bytes": 19223372,
  "metrics": {
    "gateway_count": 37,
    "load_1m": 2.4
  },
  "reported_at": "2026-06-08T09:00:05Z"
}
```

要求：

- CPU 单位是 millicore。
- memory/disk/network 单位是 bytes。
- network 字段必须是单调递增 counter，不要填瞬时速率。
- `metrics` 是扩展 JSON，必须合法。

### 5.4 Gateway Report

```http
POST {CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/gateways/report
```

payload：

```json
{
  "pod_id": 17,
  "gateways": [
    {
      "instance_id": 123,
      "gateway_id": "gw-123-7",
      "gateway_port": 20017,
      "gateway_pid": 8842,
      "state": "running",
      "generation": 7,
      "health_at": "2026-06-08T09:00:05Z"
    }
  ]
}
```

要求：

- `gateways` 必填。
- 每个 gateway 的 `instance_id`、`state`、`generation` 必填。
- 后端只接受“当前绑定 Pod + 当前 generation”的上报；旧 Pod 或旧 generation 上报会被忽略。
- `state=running` 或 `state=healthy` 会把绑定更新为 running。
- `starting`、`stopped`、`error`、`unhealthy` 等状态会更新绑定状态。
- 错误状态必须带 `error_message`，不要只上报空状态。

### 5.5 Skills Report

```http
POST {CLAWMANAGER_BACKEND_URL}/api/v1/runtime-agent/skills/report
```

当前后端接收并发布任意 JSON payload。推荐按实例分组：

```json
{
  "pod_id": 17,
  "runtime_type": "openclaw",
  "mode": "full",
  "reported_at": "2026-06-08T09:00:05Z",
  "instances": [
    {
      "instance_id": 123,
      "workspace_path": "/workspaces/openclaw/user-45/instance-123",
      "skills": [
        {
          "skill_id": "weather",
          "skill_version": "1.0.0",
          "identifier": "weather",
          "install_path": "/workspaces/openclaw/user-45/instance-123/.openclaw/skills/weather",
          "content_md5": "0123456789abcdef0123456789abcdef",
          "source": "runtime",
          "type": "agent-skill"
        }
      ]
    }
  ]
}
```

skill inventory 必须按 `instance_id` 和 `workspace_path` 隔离，不要把一个实例的 skill 混到另一个实例。

## 6. ClawManager 到 Agent 的控制接口

所有控制请求必须校验：

```http
X-ClawManager-Control-Token: ${CLAWMANAGER_AGENT_CONTROL_TOKEN}
```

token 错误返回 `401`。不要返回 `3xx` redirect；后端会把 redirect 当成失败。

### 6.1 Health

```http
GET /v1/health
```

任意 HTTP 2xx 都会被后端视为成功。建议响应：

```json
{
  "status": "ready"
}
```

以下情况必须返回 `503`：

- 必填环境变量缺失。
- workspace root 不可访问。
- 端口池不可用。
- control/report token 缺失。
- clock skew 超过健康阈值。
- agent 正在关闭，不再接受新请求。

### 6.2 Create Gateway

```http
POST /v1/gateways
```

请求：

```json
{
  "instance_id": 123,
  "user_id": 45,
  "agent_type": "openclaw",
  "workspace_path": "/workspaces/openclaw/user-45/instance-123",
  "port_range": {
    "start": 20000,
    "end": 20299
  },
  "uid": 200123,
  "gid": 200123,
  "cpu_cores": 2,
  "memory_mb": 4096,
  "disk_quota_mb": 20480,
  "generation": 7,
  "environment": {
    "CLAWMANAGER_LLM_BASE_URL": "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm",
    "CLAWMANAGER_LLM_API_KEY": "instance-token",
    "CLAWMANAGER_LLM_MODEL": "[\"auto\"]",
    "CLAWMANAGER_LLM_PROVIDER": "openai-compatible",
    "CLAWMANAGER_INSTANCE_TOKEN": "instance-token",
    "OPENAI_API_KEY": "instance-token"
  }
}
```

响应：

```json
{
  "gateway_id": "gw-123-7",
  "port": 20017,
  "pid": 8842,
  "status": "starting"
}
```

实现要求：

- handler 必须快速返回，通常 1 到 3 秒内返回 `starting` 或已有 gateway 元数据。
- 不能在 HTTP handler 内阻塞等待前台 gateway 进程完整运行。
- `agent_type` 必须等于当前 Pod runtime type。
- Pod draining 时返回 `409` 或 `503`，拒绝新建。
- 以 `instance_id + generation` 为幂等键；重复请求不能重复启动进程。
- 收到更高 generation 时，停止同实例旧 generation gateway 并释放端口。
- 端口必须来自请求的 `port_range`。
- 没有可用端口时返回：

```http
HTTP/1.1 409 Conflict
Content-Type: text/plain

no free port
```

- 启动失败、健康检查失败、配置写入失败时，上报 gateway `error` 并释放端口。

### 6.3 Delete Gateway

```http
DELETE /v1/gateways/{gateway_id}
```

要求：

- `gateway_id` 是 URL path escaped，agent 必须 decode 后再查找。
- 优雅停止进程组，超时后强制 kill。
- 释放端口、cgroup、进程表和本地状态。
- 不删除 workspace。
- gateway 不存在时建议返回 `204`，保持删除幂等。
- 成功状态可以是 `200`、`202` 或 `204`。

### 6.4 Drain

```http
POST /v1/drain
```

请求：

```json
{
  "draining": true
}
```

要求：

- 设置本地 draining 状态。
- 立即拒绝新的 `POST /v1/gateways`。
- 已有 gateway 不主动停止，继续健康检查和上报。
- 立即补发 heartbeat，`state=draining, draining=true`。
- 等 ClawManager 迁移实例后，逐个收到 DELETE 再清理。

## 7. Gateway 生命周期

推荐状态机：

```text
requested -> reserved_port -> starting -> running
                              -> error
running -> stopping -> stopped
running -> unhealthy -> running/error
```

创建流程：

1. 校验 token、runtime type、instance/user/generation、workspace、端口范围。
2. 如果 draining，拒绝创建。
3. 查本地状态，处理幂等。
4. 如果 generation 更高，清理旧 generation。
5. 加锁分配端口，标记为 reserved。
6. 创建 workspace 和 home，设置 owner 为请求中的 `uid/gid`。
7. 写入 runtime 配置和 LLM/proxy 配置。
8. 持久化 gateway 元数据为 `starting`。
9. 后台启动 gateway 子进程，立即返回。
10. 健康检查通过后上报 `running`。
11. 失败时上报 `error`，记录短错误信息并释放端口。

`running` 必须以真实进程和真实健康检查为准，不能只读旧状态文件。

## 8. 端口管理

agent 必须实现并发安全的端口分配器：

```text
reserve(instance_id, generation, count) -> port_set
commit(instance_id, generation, port_set)
release(instance_id, generation)
list_used()
```

要求：

- 分配时持有互斥锁。
- 在锁内检查 agent 内存状态和系统监听端口。
- 启动失败、健康检查失败、DELETE 成功后必须释放端口。
- 如果 runtime 需要多个端口，必须一次性分配端口组，避免部分成功。
- 同一 Pod 内端口不能冲突；不同 Pod 可以复用相同端口，因为路由使用 `pod_ip + port`。

## 9. Workspace 与资源隔离

workspace 固定格式：

```text
/workspaces/{runtime}/user-{user_id}/instance-{instance_id}
```

agent 必须：

- 对 workspace root 和请求路径执行 realpath。
- 拒绝 `..`、绝对路径逃逸、符号链接逃逸、跨用户、跨实例路径。
- 创建 `${workspace}/home`，并设置 `HOME=${workspace}/home`。
- owner 设置为请求中的 `uid/gid`，当前默认 `200000 + instance_id`。
- token、provider、配置文件权限建议 `0600`。
- 删除 gateway 时不删除 workspace。

资源限制要求：

- 每个 gateway 使用独立进程组。
- 业务进程最终以实例 UID/GID 运行。
- CPU/Memory 使用 cgroup 限制。
- Disk 优先使用 filesystem quota；不支持时至少周期扫描并在超限时上报错误。
- 资源限制无法启用时必须在 report 或错误消息中体现，不要静默忽略。

## 10. Runtime 配置和 LLM 注入

ClawManager 会通过 `POST /v1/gateways` 的 `environment` 字段下发实例级 LLM 环境变量。agent 只能转发白名单变量：

```text
CLAWMANAGER_LLM_BASE_URL
CLAWMANAGER_LLM_API_KEY
CLAWMANAGER_LLM_MODEL
CLAWMANAGER_LLM_PROVIDER
CLAWMANAGER_INSTANCE_TOKEN
OPENAI_BASE_URL
OPENAI_API_BASE
OPENAI_API_KEY
OPENAI_MODEL
```

要求：

- LLM base URL 使用 ClawManager 内部 Service DNS。
- API key 使用当前实例 token。
- 不要求用户在 runtime 页面手工填写 OpenAI key。
- 不打印 token/API key。
- 如果缺少 LLM key，不能上报 gateway `running`；应上报 `error` 并说明缺少实例 LLM token。

runtime 配置必须写在实例 workspace 内，例如：

| Runtime | 推荐路径 |
| --- | --- |
| OpenClaw | `{workspace}/home/.openclaw/openclaw.json` |
| Hermes | `{workspace}/home/.hermes/hermes.json` 或 Hermes 原生配置路径 |

JSON merge 规则：

- 对象字段递归 merge。
- 保留用户已有未知字段。
- 平台必须控制的字段可以覆盖，但要在代码中显式列出。
- 数组 append 后去重，例如 `allowedOrigins`。
- 写入前先生成临时文件，再原子 rename。

## 11. 代理和浏览器访问

浏览器访问链路必须是：

```text
Browser HTTPS -> ClawManager HTTPS -> ClawManager backend proxy -> Runtime Pod HTTP gateway
```

agent 写 runtime 配置时必须保证：

- gateway 支持 `/api/v1/instances/{instance_id}/proxy` base path。
- allowed origin 使用 ClawManager 内部 origin，例如 `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001`。
- trusted proxies 来自 `CLAWMANAGER_TRUSTED_PROXY_CIDRS` 或等价 env。
- 不使用外部地址作为 runtime 内部 trusted origin。
- 通过 ClawManager proxy 访问时，不应要求用户手工粘贴 gateway token。

## 12. 时间同步

agent 必须处理 clock skew：

- 注册和 heartbeat 响应如果带 `server_time`，记录本地时间和服务端时间偏移。
- 偏移超过 30 秒时记录 warning，并在 health 中暴露 `clock_skew_warning`。
- 偏移超过 120 秒时不应上报 `ready` 或 gateway `running`。
- 所有签名、token 时间和上报时间使用 UTC。
- 不在容器里修改系统时间，时间同步由 Kubernetes 节点 NTP/chrony 负责。

常见症状包括 `device signature expired`、JWT/WebSocket 鉴权异常、签名 URL 过期和 gateway 实际状态与 ClawManager 展示不一致。

## 13. Instance Agent v1 兼容协议

当前后端仍提供 `/api/v1/agent/*`，用于实例级状态、命令和 skill 管理。托管 runtime 如果需要复用这套能力，应由具体实例的 gateway/agent 逻辑使用实例级环境变量。

ClawManager 会为支持托管集成的实例注入：

| 变量 | 说明 |
| --- | --- |
| `CLAWMANAGER_AGENT_ENABLED` | `true` 时启用实例 agent |
| `CLAWMANAGER_AGENT_BASE_URL` | ClawManager API base URL，不带 `/api/v1/agent` 后缀 |
| `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` | 首次注册使用 |
| `CLAWMANAGER_AGENT_INSTANCE_ID` | 当前实例 ID |
| `CLAWMANAGER_AGENT_PROTOCOL_VERSION` | 当前为 `v1` |
| `CLAWMANAGER_AGENT_PERSISTENT_DIR` | 当前实例持久化目录 |
| `CLAWMANAGER_AGENT_DISK_LIMIT_BYTES` | 实例磁盘配额 |

### 13.1 Register

```http
POST {base}/api/v1/agent/register
Authorization: Bearer ${CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN}
```

payload：

```json
{
  "instance_id": 123,
  "agent_id": "openclaw-123-main",
  "agent_version": "0.1.0",
  "protocol_version": "v1",
  "capabilities": [
    "runtime.status",
    "runtime.health",
    "metrics.report",
    "skills.inventory",
    "skills.upload",
    "commands.poll",
    "llm.gateway"
  ],
  "host_info": {
    "runtime_type": "openclaw",
    "runtime_name": "OpenClaw",
    "image": "ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest",
    "persistent_dir": "/config"
  }
}
```

响应 `data` 包含：

- `session_token`
- `session_expires_at`
- `heartbeat_interval_seconds`，当前默认 `15`
- `command_poll_interval_seconds`，当前默认 `5`
- `server_time`

session token 每次成功 heartbeat 会续期 24 小时。收到 `401` 时删除本地 session，并用 bootstrap token 重新注册。

### 13.2 Heartbeat

```http
POST {base}/api/v1/agent/heartbeat
Authorization: Bearer ${session_token}
```

payload：

```json
{
  "agent_id": "openclaw-123-main",
  "timestamp": "2026-06-08T09:00:15Z",
  "openclaw_status": "running",
  "current_config_revision_id": null,
  "summary": {
    "runtime_type": "openclaw",
    "runtime_status": "running",
    "runtime_pid": 245,
    "openclaw_pid": 245,
    "skill_count": 8,
    "disk_used_bytes": 2147483648,
    "disk_limit_bytes": 10737418240
  }
}
```

兼容要求：

- `openclaw_status`、`openclaw_pid`、`openclaw_version` 是历史字段名；非 OpenClaw runtime 也需要填自己的主进程状态、PID 和版本，直到协议升级。
- 后端 45 秒内收到 heartbeat 展示 online，45 到 120 秒展示 stale，超过 120 秒展示 offline。
- heartbeat 响应里的 `has_pending_command=true` 时，应立即拉取命令。

### 13.3 State Report

```http
POST {base}/api/v1/agent/state/report
Authorization: Bearer ${session_token}
```

payload：

```json
{
  "agent_id": "openclaw-123-main",
  "reported_at": "2026-06-08T09:00:20Z",
  "runtime": {
    "openclaw_status": "running",
    "openclaw_pid": 245,
    "openclaw_version": "openclaw-2026.5.4",
    "current_config_revision_id": null
  },
  "system_info": {
    "runtime_type": "openclaw",
    "cpu": {
      "cores": 2,
      "load": {
        "1m": 0.64,
        "5m": 0.52,
        "15m": 0.4
      }
    },
    "memory": {
      "mem_total_bytes": 4294967296,
      "mem_available_bytes": 2147483648
    },
    "disk": {
      "root_total_bytes": 10737418240,
      "root_free_bytes": 8589934592
    },
    "network": {
      "interfaces": [
        {
          "name": "eth0",
          "status": "up",
          "addresses": ["10.42.0.12"],
          "rx_bytes": 123456789,
          "tx_bytes": 98765432
        }
      ]
    }
  },
  "health": {
    "runtime_process": "ok",
    "agent": "ok",
    "metrics_collector": "ok"
  }
}
```

network 的 `rx_bytes` / `tx_bytes` 必须是单调递增 counter。

### 13.4 Commands

拉取：

```http
GET {base}/api/v1/agent/commands/next
Authorization: Bearer ${session_token}
```

开始：

```http
POST {base}/api/v1/agent/commands/{id}/start
Authorization: Bearer ${session_token}
```

完成：

```http
POST {base}/api/v1/agent/commands/{id}/finish
Authorization: Bearer ${session_token}
```

finish 只允许：

- `succeeded`
- `failed`

当前支持的命令类型：

```text
start_openclaw
stop_openclaw
restart_openclaw
collect_system_info
apply_config_revision
reload_config
health_check
install_skill
update_skill
uninstall_skill
remove_skill
disable_skill
quarantine_skill
handle_skill_risk
sync_skill_inventory
refresh_skill_inventory
collect_skill_package
```

未知命令必须 finish 为 `failed`，`error_message` 写明 `unsupported command type: <type>`。

### 13.5 Skills

inventory：

```http
POST {base}/api/v1/agent/skills/inventory
Authorization: Bearer ${session_token}
```

upload：

```http
POST {base}/api/v1/agent/skills/upload
Authorization: Bearer ${session_token}
Content-Type: multipart/form-data
```

上传表单字段：

- `file`
- `agent_id`
- `skill_id`
- `skill_version`
- `identifier`
- `content_md5`
- `source`

要求：

- `identifier` 和 `content_md5` 必须稳定。
- `mode=full` 的 inventory 代表全量结果，平台会用它对齐实例 skill 状态。
- `content_md5` 按目录内容指纹计算，不是 zip 文件 MD5；算法见 `docs/skill-content-md5-spec.md`。
- 打包和解压必须防止路径逃逸。

## 14. 安全规范

- 控制接口只接受正确 `X-ClawManager-Control-Token`。
- 上报接口只使用 `X-ClawManager-Agent-Token`。
- 实例级接口使用 Bearer bootstrap/session token。
- 日志不得打印 token、API key、完整 Authorization header、channel secret。
- 启动进程使用 argv 数组，不使用 shell 拼接：

```text
execve(binary, ["openclaw", "gateway", "run", "--port", "20017"], env)
```

- 不允许用户环境变量覆盖 agent 自己的 control/report token。
- 不允许 `allowedOrigins=["*"]` 作为生产配置。
- 所有用户输入路径都要边界校验。
- 命令执行必须有超时，超过 `timeout_seconds` 主动终止并 finish failed。

## 15. 测试要求

单元测试至少覆盖：

- Runtime type 只接受 `openclaw` / `hermes`。
- create gateway 幂等。
- 更高 generation 替换旧 generation。
- 端口并发分配无冲突。
- 端口被系统占用时跳过。
- 端口耗尽返回 `409 no free port`。
- workspace path 越权被拒绝。
- JSON merge 保留用户字段，数组去重。
- LLM env 白名单转发。
- 缺少 LLM token 时 gateway 不报 `running`。
- trusted proxy 和 allowed origin 配置生成。
- clock skew 阈值判断。
- redirect 响应不被当成成功。
- DELETE gateway id path escape/decode。

集成测试至少覆盖：

- Runtime Pod 注册为 `ready`，capacity/used_slots/draining 正确。
- heartbeat 更新 Pod 状态。
- metrics 上报后管理端 Runtime Pods 页面能看到指标。
- 创建 100 个 gateway 端口唯一，第 101 个触发扩容或 no capacity。
- 重复 create 同一 `instance_id + generation` 不重复启动。
- gateway report 旧 generation 不覆盖新 generation。
- ClawManager 通过 proxy 能访问 gateway 页面和 WebSocket。
- 通过 ClawManager proxy 访问时不要求用户手工粘贴 gateway token。
- Pod drain 后拒绝新 create，已有 gateway 持续上报，DELETE 后释放端口。
- Pod 重启后不误报旧 gateway running。
- instance agent register/heartbeat/state/commands/skills 全链路可用。
- 日志中没有 token 和 API key。

## 16. Do / Don't

| Do | Don't |
| --- | --- |
| 使用 Kubernetes Service DNS 访问 ClawManager backend | 写死外部 NodePort、`172.16.1.12` 或浏览器 HTTPS 入口 |
| `POST /v1/gateways` 快速返回 `starting` | 在 HTTP handler 中阻塞等待前台进程退出 |
| 以 `instance_id + generation` 幂等 | 重试时重复启动多个 gateway |
| 以真实进程和健康检查决定 `running` | 只读旧状态文件就上报 `running` |
| JSON merge runtime 配置 | 覆盖用户整个配置文件 |
| 只转发白名单 env | 把用户请求里的全部 env 原样传给进程 |
| trusted proxy 模式下通过 ClawManager 自动进入 | 要求用户手工粘贴 gateway token |
| 检测并上报 clock skew | 忽略时间导致签名和 WebSocket 鉴权问题反复出现 |
| DELETE gateway 不删除 workspace | 清理进程时顺手删除用户数据 |

## 17. 代码位置

后端关键实现：

- Runtime Pod 上报接口：`backend/internal/handlers/runtime_agent_handler.go`
- ClawManager 调 agent 客户端：`backend/internal/services/runtime_agent_client.go`
- Runtime 调度器：`backend/internal/services/runtime_scheduler.go`
- Runtime Deployment 构建：`backend/internal/services/k8s/runtime_deployment_service.go`
- Runtime 常量与路径：`backend/internal/services/runtime_capacity.go`
- 实例 agent handler：`backend/internal/handlers/agent_handler.go`
- 实例 agent service：`backend/internal/services/instance_agent_service.go`
- 实例命令 service：`backend/internal/services/instance_command_service.go`
- 实例 runtime 状态 service：`backend/internal/services/instance_runtime_status_service.go`

相关文档：

- `docs/clawmanager-agent-v2-contract.md`
- `docs/runtime-agent-integration-guide.md`
- `docs/hermes-lite-pro-agent-development.md`
- `docs/hermes-runtime-agent-development.md`
- `docs/skill-content-md5-spec.md`
