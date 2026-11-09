[← 返回 README](../README.zh-CN.md)

# Runtime Agent 通用接入规范

> 2026-10-20 本轮收尾范围已收回到“模型按用户授权 + OpenCode 新旧兼容”，详见文末[收尾验收清单](#model-access-closeout)。Team 改造和通用无损升级系统不属于本次交付。后文第四至第七阶段保留为已写代码的历史说明，不表示需要继续推进，也不表示可以直接操作业务实例升级。

本文定义任意新 runtime 接入 ClawManager Agent Control Plane 的通用方案。后续新增 OpenClaw、Hermes、OpenCode、DeepSeek Harness 以外的 runtime 时，应优先遵守本文，再补充该 runtime 自己的镜像构建细节。

## 接入目标

每个可托管 runtime 镜像内都应包含一个常驻 agent。Agent 不需要暴露端口，只需要向 ClawManager 发起出站 HTTP 请求，并完成：

- 注册和 session token 续期。
- 心跳和在线状态上报。
- 运行时状态、CPU、内存、磁盘、网络、健康信息上报。
- skill inventory 同步和 skill 包上传。
- 平台命令轮询、执行、完成结果回传。
- 使用 ClawManager 注入的 AI Gateway 环境变量访问 LLM。

Agent 应把 runtime 自身名称写入 `host_info.runtime_type`、`summary.runtime_type`、`system_info.runtime_type`。当前 v1 协议里仍保留 `openclaw_status`、`openclaw_pid`、`openclaw_version` 这几个历史字段名，新 runtime 在协议升级前需要复用这些字段承载自己的主进程状态、PID 和版本。

## 平台侧新增 runtime type 检查清单

新增 runtime 不只是做镜像。ClawManager 平台侧至少需要完成下面几项：

| 模块 | 必做事项 |
| --- | --- |
| 数据库 | 在 `instances.type` 枚举迁移中加入新的 `runtime_type` |
| 后端 runtime 支持 | 在 runtime 类型校验、镜像解析、默认端口、持久化目录、代理路径逻辑中加入新类型 |
| 后端托管能力 | 在 `supportsManagedRuntimeIntegration` 加入新类型，否则不会注入 Agent 和 AI Gateway 环境变量，注册也会被拒绝 |
| Agent 注册 | 确认注册 allowlist、错误映射、测试覆盖都包含新类型 |
| Runtime Image Cards | 在系统镜像设置的支持类型、默认镜像、默认启用项中加入新类型 |
| 创建实例页 | 在前端 `INSTANCE_TYPES`、类型文案、图标、默认 env 模板中加入新类型 |
| 图标 | 提供 runtime 官方图标或明确授权图标，放入 `frontend/public` 或现有资产路径 |
| 测试 | 覆盖 env 注入、agent 注册、runtime 状态上报、创建实例表单、系统镜像卡片 |

如果新 runtime 采用 Webtop/KasmVNC 基础镜像，默认约定通常是：

- 桌面端口：`3001`
- 持久化目录：`/config`
- 代理路径：由 ClawManager 写入 `SUBFOLDER=/api/v1/instances/{instance_id}/proxy/`

如果新 runtime 不基于 Webtop，必须在平台侧明确它的服务端口、健康检查、代理路径、持久化目录和用户数据目录，不能让镜像和平台各自猜测。

## 镜像和 Agent 运行要求

Runtime 镜像必须满足：

- Agent 随容器启动自动运行，例如 systemd、s6 overlay、supervisord 或入口脚本。
- Agent 不占用 runtime 的业务端口或桌面端口。
- Agent 通过环境变量读取所有 ClawManager 配置，不写死地址、实例 ID、token、路径。
- Agent 的本地状态必须写入持久化目录，例如 `${CLAWMANAGER_AGENT_PERSISTENT_DIR}/<runtime>-agent/`。
- Agent 日志中不能打印 bootstrap token、session token、AI Gateway API key。
- Agent 可以在 ClawManager 暂时不可达时退避重试，不应退出导致容器不可用。

建议本地状态目录：

```text
${CLAWMANAGER_AGENT_PERSISTENT_DIR}/<runtime>-agent/
  session.json
  state.json
  logs/
  cache/
```

## ClawManager 注入的环境变量

### Agent 控制面

| 变量 | 说明 |
| --- | --- |
| `CLAWMANAGER_AGENT_ENABLED` | 为 `true` 时启动 agent |
| `CLAWMANAGER_AGENT_BASE_URL` | ClawManager API 根地址，不带 `/api/v1/agent` 后缀 |
| `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` | 首次注册使用的一次性 bootstrap token |
| `CLAWMANAGER_AGENT_INSTANCE_ID` | 当前实例 ID |
| `CLAWMANAGER_AGENT_PROTOCOL_VERSION` | 当前为 `v1` |
| `CLAWMANAGER_AGENT_PERSISTENT_DIR` | 当前实例持久化目录 |
| `CLAWMANAGER_AGENT_DISK_LIMIT_BYTES` | 实例磁盘配额字节数 |

Agent 启动时如果 `CLAWMANAGER_AGENT_ENABLED` 不是 `true`，应进入空闲状态或直接不启动控制面逻辑。

### AI Gateway

支持托管 runtime 的实例会被注入 OpenAI-compatible 网关变量：

| 变量 | 说明 |
| --- | --- |
| `CLAWMANAGER_LLM_BASE_URL` | ClawManager AI Gateway OpenAI-compatible base URL |
| `CLAWMANAGER_LLM_API_KEY` | 当前实例专属 Gateway API key |
| `CLAWMANAGER_LLM_MODEL` | 平台注入的模型目录 JSON，首项通常包含 `auto` |
| `CLAWMANAGER_LLM_CONTEXT_WINDOW` | 默认逻辑模型的安全上下文窗口；`0` 表示未知，Runtime 不应覆盖自身默认值 |
| `CLAWMANAGER_LLM_CONTEXT_WINDOWS` | 按显示名称和逻辑模型组索引的上下文窗口 JSON；逻辑路由使用所有可回退部署中的最小值 |
| `CLAWMANAGER_LLM_PROVIDER` | 当前为 `openai-compatible` |
| `CLAWMANAGER_LLM_REASONING` | 按模型显示名称索引的 Thinking 开关 JSON；平台保存的关闭状态是权威上限 |
| `CLAWMANAGER_LLM_REASONING_CONTROL` | 按模型显示名称索引的供应商 Thinking 控制协议 JSON；空值表示 Runtime 不应猜测或强行开启 |
| `CLAWMANAGER_INSTANCE_TOKEN` | 当前实例 token，和 Gateway API key 同源 |
| `OPENAI_BASE_URL` | OpenAI SDK 兼容别名 |
| `OPENAI_API_BASE` | OpenAI SDK 兼容别名 |
| `OPENAI_API_KEY` | OpenAI SDK 兼容别名 |
| `OPENAI_MODEL` | 默认模型，通常为 `auto` |

Runtime 在启动 Agent 前应将正数上下文窗口写入该 Agent 的原生模型配置，用于上下文预算和自动压缩。解析顺序为：当前模型显示名称、逻辑模型组、`CLAWMANAGER_LLM_CONTEXT_WINDOW`。值为 `0` 时表示平台没有可靠信息，必须保留 Runtime 自身默认值。ClawManager 会直接把窗口写入其生成的 OpenCode `provider.models.*.limit.context`；其他 Runtime 镜像应在各自的配置物化层完成等价映射。

Runtime 内的应用和 agent 如果需要调用模型，优先使用这些变量，不要让用户在镜像内手工写入 provider key。Runtime 可以把受支持的会话级开关或强度随请求传给 AI Gateway，但不能仅凭模型名称猜测供应商 Thinking 协议，也不能绕过平台关闭的模型级开关。

### LLM Session 归因

托管 runtime 的每次 LLM 请求应携带稳定 session 标识，供平台按会话统计 token：

| 项 | 要求 |
| --- | --- |
| Header | `x-openclaw-session-key: {sessionKey}` |
| 示例 | `main` 会归一化为 `agent:openclaw:main` |
| 托管实例 Gateway Token 默认 | 未显式传 key 时，ClawManager 默认使用 `main` |
| 备选 | 请求体 `session_id` 或 OpenAI `user` 字段 |
| 禁止 | 长期依赖 Gateway 自动生成的 `sess_{traceID}`（用户 JWT 等非托管调用仍会 fallback） |

Agent state report 可选上报 LLM 配置指纹，便于平台检测配置漂移：

```json
{
  "runtime": {
    "llm_config_status": "gateway",
    "llm_provider_base_url": "http://clawmanager-gateway.../api/v1/gateway/llm",
    "llm_config_fingerprint": "sha256..."
  }
}
```

### Egress 代理与实例归因

托管 runtime 实例会注入 egress 代理环境变量，并将实例 ID 写入 `CLAWMANAGER_EGRESS_INSTANCE_ID`。默认代理 URL 带有按实例生成的代理认证信息，标准代理客户端会在 HTTP 请求和 HTTPS CONNECT 中自动发送 `Proxy-Authorization`，因此 Lite 共享 Pod 中的多个实例也能分别匹配私网例外。当 egress 拦截直连 LLM 提供商域名时，平台会把该事件记为 `egress.llm.blocked` 审计。

| 变量 / Header | 说明 |
| --- | --- |
| `HTTP_PROXY` / `HTTPS_PROXY` | 指向 ClawManager egress proxy，默认携带实例 ID 和专用于代理的认证信息；不要记录完整 URL |
| `CLAWMANAGER_EGRESS_INSTANCE_ID` | 当前实例 ID，供代理客户端上报 |
| `Proxy-Authorization` | 标准代理客户端根据代理 URL 自动发送的 Basic 认证；优先用于实例身份识别 |
| `X-ClawManager-Instance-Id` | 旧客户端自报的实例 ID（与 `X-ClawManager-Egress-Instance-Id` 等价）；不作为实例认证或私网授权依据，不转发到目标服务 |

代理客户端应保留平台注入的代理 URL 中的认证信息。独立 Pod 和 Lite 共享 Pod 的私网访问都必须使用实例代理认证，不能仅凭来源 IP 或自报实例 ID 授权，以免 Pod IP 复用时继承旧实例的权限。升级后需重启已有实例，使进程取得新的代理环境变量。环境变量覆盖完成后，仍指向同一个平台代理端点的 URL 会重新附上当前实例认证；指向其他代理的 URL，以及系统级显式配置的上游用户名/密码会保留。

私网例外按实例/用户、CIDR 和端口匹配，即使开启域名 `allowlist` 模式也可生效；例外不会放行未列入域名白名单的公网地址，也不会覆盖 `denylist` 明确禁止的域名。CIDR 必须整体位于私网或链路本地网段内，历史数据中的过宽网段也不会执行。IPv4 映射的 IPv6 CIDR（如 `::ffff:10.255.25.3/128`）会规范化为 IPv4 CIDR（`10.255.25.3/32`），历史数据加载和目标地址匹配使用相同规则。

普通 HTTP 上游连接不复用，每次请求重新检查规则。新的 HTTPS CONNECT 会重新检查规则，已建立的隧道会保持到连接关闭；CONNECT 的预读数据会先通过缓冲读取器转发。多后端副本的缓存有效期为 30 秒；缓存过期或本地规则被修改后，如果数据库刷新失败，将停止私网放行。共享刷新使用独立的两秒超时，各请求最多等待两秒，取消请求只结束自己的等待，不取消其他请求共享的刷新。实际刷新失败后间隔一秒再重试；查询期间不持有全局缓存锁，撤销规则会使正在加载的旧快照失效。域名策略已允许且只解析到公网地址的请求不查询私网规则。未列入域名白名单且没有适用私网例外的请求会在 DNS 查询前返回 403 并记录审计；存在适用例外时，真实 DNS 故障仍返回 502。拦截审计复用本次已解析的身份，数据库写入最多等待两秒，并响应请求取消；审计超时或失败会记录服务日志，不阻止返回拦截结果。

管理页面会分页加载全部实例和用户选项。两类选项独立加载，单个接口失败不会清空另一类；失败时显示对应错误，点击“刷新”可重试。列表、错误提示和加载状态仅接受最新请求的结果，切换语言、重复加载或组件卸载后，旧请求不会覆盖当前列表。

可选网络加固：在 ClawManager 后端设置 `CLAWMANAGER_INSTANCE_NETWORK_LOCK=true` 后，新建的 **Pro（独立 Pod）** OpenClaw/Hermes 实例会自动创建 egress NetworkPolicy。**Lite（gateway 池）** 实例共享 runtime Pod，不适用按实例 NetworkPolicy。参考 `deployments/k8s/single-node/instance-egress-networkpolicy.yaml`。

## Agent 生命周期

推荐主循环：

1. 读取环境变量，确认 agent 已启用。
2. 生成稳定 `agent_id`，建议格式为 `<runtime_type>-<instance_id>-main`。
3. 从持久化目录读取 session token；没有 token 时用 bootstrap token 注册。
4. 发送一次完整 state report。
5. 按注册响应里的 `heartbeat_interval_seconds` 发送 heartbeat。
6. 按注册响应里的 `command_poll_interval_seconds` 拉取命令；heartbeat 返回 `has_pending_command=true` 时立即拉取。
7. 按 5 到 10 秒间隔采样并发送 state report。
8. 定期同步 skill inventory，或在 skill 变化后立即同步。
9. 接口返回 401、session 过期或本地 token 丢失时，重新注册。

注册响应里的 session token 当前为滚动续期。每次成功 heartbeat 都会延长有效期。Agent 仍应能处理 401 并自动重新注册。

## API 约定

以下 `{base}` 都表示 `CLAWMANAGER_AGENT_BASE_URL`，认证头均为：

```http
Authorization: Bearer <token>
Content-Type: application/json
```

### 注册

```http
POST {base}/api/v1/agent/register
Authorization: Bearer {CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN}
```

```json
{
  "instance_id": 123,
  "agent_id": "myruntime-123-main",
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
    "runtime_type": "myruntime",
    "runtime_name": "My Runtime",
    "image": "registry.example.com/myruntime:latest",
    "desktop_base": "webtop",
    "persistent_dir": "/config",
    "port": 3001,
    "arch": "amd64"
  }
}
```

响应：

```json
{
  "data": {
    "session_token": "agt_sess_xxx",
    "session_expires_at": "2026-04-28T10:00:00Z",
    "heartbeat_interval_seconds": 15,
    "command_poll_interval_seconds": 5,
    "server_time": "2026-04-27T10:00:00Z"
  }
}
```

Agent 必须缓存 `session_token`，后续接口都使用它认证。

### 心跳

```http
POST {base}/api/v1/agent/heartbeat
Authorization: Bearer {session_token}
```

```json
{
  "agent_id": "myruntime-123-main",
  "timestamp": "2026-04-27T10:00:15Z",
  "openclaw_status": "running",
  "summary": {
    "runtime_type": "myruntime",
    "runtime_status": "running",
    "runtime_pid": 245,
    "runtime_version": "0.4.0",
    "openclaw_pid": 245,
    "skill_count": 8,
    "disk_used_bytes": 2147483648,
    "disk_limit_bytes": 10737418240
  }
}
```

兼容要求：

- `openclaw_status` 当前仍是 v1 状态槽位。新 runtime 填自己的主进程状态。
- `summary.openclaw_pid` 是当前后端识别 PID 的兼容别名；建议同时上报 `runtime_pid` 和 `openclaw_pid`，后续协议升级后再收敛。
- 状态建议使用 `starting`、`running`、`stopped`、`error`、`unknown`。
- 心跳按服务端响应间隔执行，默认约 15 秒。
- ClawManager 45 秒内收到心跳显示 online，45 到 120 秒显示 stale，超过 120 秒显示 offline。

### 完整状态和监测数据上报

```http
POST {base}/api/v1/agent/state/report
Authorization: Bearer {session_token}
```

```json
{
  "agent_id": "myruntime-123-main",
  "reported_at": "2026-04-27T10:00:20Z",
  "runtime": {
    "openclaw_status": "running",
    "openclaw_pid": 245,
    "openclaw_version": "myruntime-0.4.0",
    "current_config_revision_id": null
  },
  "system_info": {
    "runtime_type": "myruntime",
    "runtime_name": "My Runtime",
    "sampled_at": "2026-04-27T10:00:20Z",
    "cpu": {
      "cores": 2,
      "load": {
        "1m": 0.64,
        "5m": 0.52,
        "15m": 0.40
      }
    },
    "memory": {
      "mem_total_bytes": 4294967296,
      "mem_available_bytes": 2147483648
    },
    "disk": {
      "mount_path": "/config",
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
    "desktop": "ok",
    "agent": "ok",
    "metrics_collector": "ok",
    "metrics_sample_interval_seconds": 5
  }
}
```

后端会原样保存 `system_info` 和 `health`。前端实例详情页当前按以下字段绘制指标：

| 路径 | 类型 | 单位 | 说明 |
| --- | --- | --- | --- |
| `system_info.cpu.cores` | number | 核数 | 容器可用 CPU 核数 |
| `system_info.cpu.load.1m` | number | load average | 1 分钟 load |
| `system_info.cpu.load.5m` | number | load average | 5 分钟 load |
| `system_info.cpu.load.15m` | number | load average | 15 分钟 load |
| `system_info.memory.mem_total_bytes` | number | bytes | 内存上限或总内存 |
| `system_info.memory.mem_available_bytes` | number | bytes | 可用内存 |
| `system_info.disk.root_total_bytes` | number | bytes | 持久化目录所在文件系统总容量 |
| `system_info.disk.root_free_bytes` | number | bytes | 持久化目录所在文件系统剩余容量 |
| `system_info.network.interfaces[].rx_bytes` | number | bytes | 网卡累计接收字节数 |
| `system_info.network.interfaces[].tx_bytes` | number | bytes | 网卡累计发送字节数 |

CPU 百分比由前端按 `load.1m / cores * 100` 计算。内存百分比由 `(mem_total_bytes - mem_available_bytes) / mem_total_bytes * 100` 计算。磁盘百分比由 `(root_total_bytes - root_free_bytes) / root_total_bytes * 100` 计算。网络速率由相邻两次 `rx_bytes`、`tx_bytes` counter 差值计算，所以 agent 必须上报单调递增的累计 counter，不要把瞬时速率填进这两个字段。

推荐采样来源：

- CPU load：`/proc/loadavg`。
- CPU cores：优先 cgroup CPU quota，例如 cgroup v2 `/sys/fs/cgroup/cpu.max`；没有 quota 时使用 `/proc/cpuinfo` 或语言运行时 CPU 数。
- 内存：优先 cgroup memory limit/current，例如 cgroup v2 `/sys/fs/cgroup/memory.max`、`/sys/fs/cgroup/memory.current`；没有限制时使用 `/proc/meminfo` 的 `MemTotal` 和 `MemAvailable`。
- 磁盘：对 `CLAWMANAGER_AGENT_PERSISTENT_DIR` 调用 `statvfs`。
- 网络：读取 `/proc/net/dev`，默认排除 `lo`。

上报频率：

- 启动成功后立即发送一次。
- 正常运行每 5 秒发送一次；资源敏感时可放宽到 10 秒。
- 采样间隔不要短于 2 秒。
- runtime 状态变化、skill inventory 变化、命令完成后立即补发一次。

### 命令轮询和执行

拉取命令：

```http
GET {base}/api/v1/agent/commands/next
Authorization: Bearer {session_token}
```

如果没有命令，响应的 `data` 可能为空。拿到命令后，agent 必须先标记开始，再执行：

```http
POST {base}/api/v1/agent/commands/{id}/start
Authorization: Bearer {session_token}
```

```json
{
  "agent_id": "myruntime-123-main",
  "started_at": "2026-04-27T10:01:00Z"
}
```

完成后必须 finish，失败也要 finish：

```http
POST {base}/api/v1/agent/commands/{id}/finish
Authorization: Bearer {session_token}
```

```json
{
  "agent_id": "myruntime-123-main",
  "status": "succeeded",
  "finished_at": "2026-04-27T10:01:05Z",
  "result": {
    "message": "system info collected"
  },
  "error_message": ""
}
```

当前通用命令类型：

| 命令 | Agent 行为 |
| --- | --- |
| `collect_system_info` | 立即采样，发送 state report，并在 finish result 中带上同一份摘要 |
| `health_check` | 检查主进程、桌面入口、agent、metrics collector，并发送 state report |
| `sync_skill_inventory` | 扫描 skill 目录并上报完整 inventory |
| `refresh_skill_inventory` | **已废弃**。请使用 `sync_skill_inventory` |
| `collect_skill_package` | 打包指定 skill 并上传 |
| `install_skill` | 下载并安装平台指定 skill version |
| `update_skill` | 更新已安装 skill |
| `uninstall_skill` / `remove_skill` | 移除指定 skill |
| `disable_skill` | 禁用指定 skill |
| `quarantine_skill` | 隔离指定 skill |
| `handle_skill_risk` | 按平台风控 payload 处理 skill |
| `apply_config_revision` | 获取并应用配置 revision |
| `reload_config` | 重新加载 runtime 配置 |

`start_openclaw`、`stop_openclaw`、`restart_openclaw` 是历史命名命令。非 OpenClaw runtime 不应误执行，除非平台侧明确把它们映射为该 runtime 的启动、停止、重启语义。

Agent 遇到未知命令时，应 finish 为 `failed`，`error_message` 写明 `unsupported command type: <type>`。

### Skill inventory

上报：

```http
POST {base}/api/v1/agent/skills/inventory
Authorization: Bearer {session_token}
```

```json
{
  "agent_id": "myruntime-123-main",
  "reported_at": "2026-04-27T10:02:00Z",
  "mode": "full",
  "trigger": "startup",
  "skills": [
    {
      "skill_id": "filesystem-name-or-manifest-id",
      "skill_version": "1.0.0",
      "identifier": "vendor.skill-name",
      "install_path": "/config/myruntime/skills/vendor.skill-name",
      "content_md5": "0123456789abcdef0123456789abcdef",
      "source": "runtime",
      "type": "agent-skill",
      "size_bytes": 12345,
      "file_count": 12,
      "collected_at": "2026-04-27T10:02:00Z",
      "metadata": {
        "runtime_type": "myruntime"
      }
    }
  ]
}
```

要求：

- `identifier` 和 `content_md5` 必须稳定。
- `mode=full` 表示这次 inventory 是全量结果，平台会用它对齐实例 skill 状态。
- skill 内容变化后要重新计算 `content_md5` 并上报。
- 如果支持上传 skill 包，使用 `POST {base}/api/v1/agent/skills/upload`，multipart 表单中带 `file`、`agent_id`、`skill_id`、`skill_version`、`identifier`、`content_md5`、`source`。
- `content_md5` 必须匹配控制面契约测试所使用的规范化目录内容指纹，不能直接计算 ZIP 文件 MD5。

### 配置和安装包下载

应用配置 revision：

```http
GET {base}/api/v1/agent/config/revisions/{id}
Authorization: Bearer {session_token}
```

下载 skill version：

```http
GET {base}/api/v1/agent/skills/versions/{external_version_id}/download
Authorization: Bearer {session_token}
```

下载内容只能应用到当前实例，不应泄露到其他实例或日志。

## 错误处理和重试

Agent 应按下面规则处理错误：

| 情况 | 处理 |
| --- | --- |
| HTTP 401 | 删除本地 session token，使用 bootstrap token 重新注册 |
| HTTP 403 | 停止重试当前认证路径，记录简短错误，等待配置修复 |
| HTTP 404 | 对命令或资源类请求标记失败，错误写入 finish |
| HTTP 429 / 5xx / 网络错误 | 指数退避重试，保留本地状态 |
| JSON 校验失败 | 修正 payload；命令执行场景下 finish 为 failed |
| 采样部分失败 | 上报可用字段，并在 `health.metrics_collector` 写 `degraded` 或 `error` |

推荐退避：1s、2s、5s、10s、30s，最大 60s，并加入少量 jitter。

## 安全要求

- 不记录 token、API key、完整 Authorization header。
- session token 只写入当前实例持久化目录，文件权限建议 `0600`。
- 不接受来自用户输入的任意 URL 覆盖 `CLAWMANAGER_AGENT_BASE_URL`。
- 上传 skill 包前需要限制目录边界，不能打包持久化目录以外的文件。
- 解压平台下发的 skill 包时必须防止 zip slip，例如拒绝 `../` 和绝对路径。
- 命令执行要做超时控制，超过 `timeout_seconds` 应主动终止并 finish 为 failed。

## 验收标准

新增 runtime agent 接入完成后，至少满足：

1. 创建实例后容器环境中存在 Agent 和 AI Gateway 变量。
2. Agent 能注册成功，实例详情页显示 agent online。
3. 心跳持续发送，45 秒内不会变 stale。
4. `GET /api/v1/instances/{instance_id}/runtime` 能看到 `runtime.system_info` 和 `runtime.health`。
5. CPU、Memory、Disk、Network 指标在实例详情页 10 秒内开始刷新。
6. `collect_system_info` 命令能成功完成，并更新 state report。
7. `health_check` 命令能成功完成，并体现 runtime、agent、metrics collector 状态。
8. Skill inventory 能上报全量结果，skill 变化后能重新同步。
9. session token 过期或 401 后能自动重新注册。
10. 日志中没有 bootstrap token、session token、AI Gateway API key。

## 最小实现伪代码

```text
load env
if CLAWMANAGER_AGENT_ENABLED != "true":
  sleep forever

agent_id = "<runtime_type>-" + CLAWMANAGER_AGENT_INSTANCE_ID + "-main"
session = load session from persistent dir
if session missing:
  session = register_with_bootstrap_token()
  save session

report_state(sample_system_info(), health_check())
sync_skill_inventory(trigger="startup")

loop:
  every heartbeat_interval:
    resp = heartbeat(summary)
    if resp.has_pending_command:
      poll_and_execute_commands()

  every command_poll_interval:
    poll_and_execute_commands()

  every 5 seconds:
    report_state(sample_system_info(), health_snapshot)

  on 401:
    delete session
    session = register_with_bootstrap_token()
    save session
```

## 模型用户授权与运行时任务联动（协议 1）

模型权限按实例所有者判断，部署记录（model ID）是授权单元，不按上游模型名称合并。管理端修改权限后，Gateway 始终负责最终拦截；运行时协议补充目录同步、整轮任务中止和取消回执，不替代 Gateway 的权限校验。

### 接口和身份

下列路径相对于实例的 `CLAWMANAGER_LLM_BASE_URL`，使用该实例的 Gateway 凭据。个人 Key 不能冒充实例调用这些接口。不要发送上游 API Key、完整提示词或工具参数。

| 接口 | 用途 |
| --- | --- |
| `GET /runtime/catalog` | 返回当前所有者与 Key 权限交集内的模型、上下文窗口、工具能力和目录 revision；支持 ETag；没有可用模型时返回空数组 |
| `POST /runtime/tasks/check` | 检查本轮任务；首次调用建立记录，撤权或已结束返回 `403 model_unavailable` |
| `POST /runtime/tasks/finish` | 执行范围完全退出后的回执；`cancelled: true` 表示运行时确认取消，普通完成不能冒充撤权取消回执 |

任务请求体包含 `generation`、`session_id`、`task_id`。其中 task ID 必须是用户输入轮次的稳定 ID，工具执行、重试和 Auto 切换模型不能改变它；新用户输入使用新 ID。实例 ID、所有者 ID 来自服务端鉴权，不能由请求体指定。generation 必须匹配当前实例代次。

每次模型请求还要携带：

```text
X-ClawManager-Session-ID: <原始运行时会话 ID>
X-ClawManager-Task-ID: <本轮用户输入 ID>
X-ClawManager-Runtime-Generation: <当前实例代次>
```

不能把记账用的 `main`、请求 Trace ID、提示词哈希当作可靠的任务标识。OpenAI Chat、Responses 和 Anthropic 三个入口使用相同的任务检查和绑定逻辑。

### 撤权与回执边界

1. Gateway 在实际模型调用前把部署绑定到任务。数据库迁移 `063_add_gateway_runtime_tasks.sql` 保存任务状态及使用过的部署。
2. 模型撤权/禁用/删除与相关任务的撤权标记在同一数据库事务中完成。撤权标记不可逆；随后重新授权不会恢复旧任务。
3. 运行时在每次模型调用和工具启动前检查任务，并在执行期间每秒检查一次。Gateway 同时保留流式请求的持续授权检查。轮询与网络超时意味着这不是零延迟、硬实时取消。
4. 运行时退出本轮执行范围、取消并等待工具执行纤程结束后才发送回执。不停止整个实例，不取消无关会话；已完成的工具副作用不回滚。外部工具是否响应取消仍取决于工具实现，不能把发出取消信号当作所有外部操作已停止。
5. 未收到回执时仅能认为 Gateway 请求已停止，不能宣称整轮任务已停止。回执保存在任务表，并记录 `gateway.model_access.runtime_acknowledged` 审计事件，包含 task ID 和 generation。
6. 回执发送失败时，当前运行时进程保留待重试项，在后续目录轮询中重试；进程异常退出会丢失尚未投递的内存回执，服务端继续保留“未确认”，不伪造成功。

任务表保留已结束/撤权任务的记录，防止旧任务 ID 重放；实例删除时通过外键清理。未来若增加历史清理，必须保留等价的不可恢复标记，不能直接清空撤权记录。

### 当前适配与升级要求

> 历史问题：2026-10-19 打包复核发现，最初的补丁和 129 项核心测试针对开发快照 `4643e65ad6334de3e4e68dedc201d5fbb828c9fe`（包版本 1.18.18），不是本机正式版 2.0.27。这些历史测试不能用于证明正式运行时兼容。现已重新移植到正式版 `v2.0.27`、提交 `1f4d07a4a519185955ae8ab2ea3607805509e386`；源码测试与真实镜像/实例验收仍须区分。正式构建入口参见 [v2.0.27 CLI 构建脚本](https://github.com/anomalyco/opencode/blob/v2.0.27/packages/cli/script/build.ts)。

`D:\AgentsRuntime\opencode\model-governance` 包含正式版的 `governance.ts`、`governance.test.ts`、版本校验安装器和 Dockerfile。ClawManager 内 `deployments/opencode-governance` 保存同一套交付来源；同步脚本使用 apply_patch 更新对应交付文件。旧 `.mjs` 客户端及 effect 实验文件保留但不参与新构建。补丁修改运行时核心，不改 OpenCode UI；正式版目录变化通过现有 `model.updated` 事件通知界面，轮询间隔约 3 秒。

构建上下文是 `opencode/model-governance` 目录，显式提供现有 Lite/Pro 运行时镜像作为 `BASE_IMAGE`。安装器同时检查精确提交、包版本和各目标文件与上游的一致性，所有锚点通过才写入，并记录补丁文件哈希。正式 CLI 使用 `--target=opencode-linux-x64`，输出目录为 `packages/cli/dist/cli-linux-x64`；交付构建固定 Bun 1.4.2。不能只重建 ClawManager 镜像就认为运行时适配已部署。

部署顺序：先升级兼容的 ClawManager 并执行数据库迁移，再构建独立测试运行时并验证。现有实例不自动换镜像；普通重启不应作为运行时升级入口。旧运行时仍有 Gateway 权限拦截，但没有可靠的整轮任务取消回执和热更新目录；启动前已存在的旧任务也不会凭空获得新的任务身份。自动排空、备份、分池迁移和受控回滚尚未交付，不能据此对繁忙实例进行滚动替换。

### 新旧实例兼容与能力协商（本次新增）

- 迁移 `064_add_runtime_protocol.sql` 仅创建空表 `gateway_runtime_protocols`，不回填、不修改旧实例镜像、令牌或配置。
- 未协商的实例继续接受不带任务标识的原有请求；模型 ACL、Auto 过滤仍生效。四个生成路由（Chat Completions、Responses 及其别名、Anthropic Messages）统一读取协议要求。
- 新运行时向 `POST /api/v1/gateway/llm/runtime/capabilities` 上报协议 1、代次、版本及能力。实例和所有者来自认证上下文，数据库事务再次核对所有者及当前代次，不信任请求体中的用户/实例 ID。
- 必需能力为 `task_identity`、`turn_cancel`、`cancel_receipt`、`live_catalog`。后续 `cm-governance-2` 增加 `maintenance_drain`；它仅代表实例排空检查能力，不代表自动升级、备份或回滚已完成。
- 协商后协议要求跨重启保留；缺失任务标识返回 409，不能自动降回旧协议。运行时没有自行撤销协商的 API，受控降级/回滚入口仍待实现。
- `GET /api/v1/instances/:id/runtime-governance` 只供实例所有者或管理员读取。状态分别为 `legacy`、`governed`、`awaiting_runtime`；后者覆盖已协商但代次/所有者不匹配、停止运行或报告超过 30 秒的情况。治理状态不可读取时界面明确显示未知，不显示成兼容模式。
- 管理端实例详情页显示模式。能力上报不等于某个任务已经取消，取消完成仍须对应回执。
- 正式版接入主请求、重试、工具续跑、工具启动前检查、自动/手动压缩、标题、会话生成以及无会话辅助生成。辅助请求使用独立身份；任务内的压缩/通用生成沿用当前任务身份。
- 已撤权任务的同一会话/用户消息身份不能借新运行代次恢复；新用户消息不受旧任务的不可恢复标记影响。

OpenClaw、Hermes、DeepSeek Harness 等运行时尚未接入上述整轮执行生命周期钩子，不能宣称已经具备同等的整轮取消能力。新增适配必须验证工具执行期间撤权、普通流式请求撤权、重新授权后旧任务不恢复、其他会话不受影响，以及回执失败不误报成功。

### 历史开发快照验证记录（不是正式版验收）

- ClawManager 授权、任务身份、目录过滤、容量跳过、取消回执相关测试通过；相关 Go 包回归通过。回归显式排除了既有的 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames` 分类规则失败项，本次没有修改该规则。
- OpenCode 核心类型检查通过；目录、任务协调器、模型解析、执行器及取消测试共 129 项通过。测试机器上协调器压力测试曾超过默认 5 秒，使用 30 秒测试超时后通过。
- 运行时 HTTP 适配器另有 8 项测试，包含自动压缩保留已认证任务 ID、未知上下文窗口保留运行时默认值，以及回执失败重试。没有已认证活动任务时，不能用压缩摘要或默认会话名猜测任务身份。
- 独立 MySQL 8.0.46 空库中的真实仓储集成测试通过：授权绑定、撤权、快速重新授权不恢复旧任务、取消确认、新任务、模型删除和实例代次更新。测试没有使用业务数据库。
- 前端相关选择、权限保留、单次/批量保存回归共 24 项通过。

上述单元、核心执行器和仓储集成测试不等于真实实例的完整浏览器端到端验收。后续本机打包已构建管理端 `clawmanager-local:model-access-20261019`，运行时因上述版本不匹配未构建发布镜像、未替换现有 OpenCode。发布运行时前须完成匹配正式版本的适配，再验证目录隐藏、旧选项拒绝、流式中止和工具执行期间撤权。运行时适配不修改 OpenCode UI 源码。

### 正式版移植验证与剩余发布门槛

- 正式版 2.0.27 的模型、请求钩子、执行器、工具、压缩、标题及两类生成入口原生测试共 337 项通过（本机 Bun 1.3.14；交付用 Bun 1.4.2 镜像仍须重跑）。
- 正式版核心类型检查通过；新增适配器测试 9 项、35 条断言通过，覆盖任务身份、辅助请求隔离、取消清理顺序、工具启动前拦截、授权恢复不复活旧任务、目录读取失败关闭，以及正式 `SessionModelRequest` 的四种请求身份注入。
- 后端 models、repository、handlers、db、aigateway、services 包回归通过，但显式排除了原有 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames`：单独复核仍因 `o3` 被分为未知模型失败，本次未改动该分类逻辑。独立 MySQL 8.0.46 空测试库验证了协议迁移、可信所有者/代次、重复协商、禁止降级及跨重启的撤权不可恢复。未使用业务数据库。
- 管理端构建通过，治理状态/模型提示/授权编辑相关前端测试 12 项通过。
- 本次未更新 K8s、未替换现有实例、未对业务数据库执行新迁移。现有镜像仍为上次部署版本。
- 待完成：Bun 1.4.2 的交付镜像、Lite/Pro 真实实例验收、后台/子任务取消联动验收、首次未打补丁旧版升级的可靠接单闸门及排空、停旧后的一致性备份、共享池逐实例迁移、升级故障恢复和受控回滚。未通过前不能标记“整套无损升级完成”。

### 实例级排空检查（cm-governance-2，第二阶段）

数据库迁移 `065_add_runtime_maintenance.sql` 仅建空表，不替换镜像、不排空旧实例。部署顺序仍是先升级管理端，再验证独立测试运行时；这次代码执行没有更新本机 K8s。

- 管理员在实例详情的“升级前排空检查”中显式开始，默认 300 秒。只允许当前所有者/运行代次匹配、最近 30 秒内上报 `maintenance_drain` 的 OpenCode 运行时；旧运行时返回 409，但日常调用不受该功能影响。
- 管理接口：`POST /api/v1/instances/:id/runtime-maintenance`，参数 `timeout_seconds`（10～600）；`GET` 同一路径查询；`POST .../runtime-maintenance/cancel` 必须提供对应的 `operation_id`。查询限所有者/管理员，操作限管理员。
- 运行时通过实例凭据每秒调用 Gateway `/runtime/maintenance`。关闭新 Session 请求、命令、Shell、技能、压缩、生成、恢复等接单入口；已有任务及其延续工作继续执行，不以强杀方式凑出“空闲”。
- 排空证据包括进程内活动执行、尚未结算的 Gateway 任务、正在接单的操作、后台 Job scope 以及持久化队列。执行计数为保守计数，不是去重的会话数。后台 Job 必须完成 scope 清理才从计数中扣除，不能因为返回了后台结果就当作进程已退出。
- 只有匹配操作 ID/代次、明确关闭接单、全部计数归零且上报不足 5 秒时，管理端才显示“运行时报告已排空”。未执行的 admit-only 输入不会被丢弃或强制运行。查询失败、报告过期、代次变化均不能被当成已排空。
- 超时或显式取消后，运行时在下一次成功轮询时恢复接单。断网不能自动打开已关闭的闸门；控制状态超过 5 秒未确认时，新接单失败关闭，连接恢复后重新确认。晚到的旧操作回执不能确认下一次排空，也不能重新激活已取消的排空。
- OpenCode 原有共享池滚动替换 API 现在返回 409；调度器也拒绝执行此类待处理 rollout，避免绕过安全迁移流程直接更新整个池。其他运行时的原滚动更新逻辑未变。既有实例不自动换镜像。

**边界：排空报告不是停机证明或备份凭据。** 管理页面的排空功能没有调用停止、镜像替换或数据恢复。旧 RuntimeAgent `DeleteGateway` 会忽略进程停止错误且提前释放记录，不能据其返回成功直接开始一致性备份；新版停止确认底层接口见下一节。跨副本调度隔离、旧版首次迁移和回滚协议仍需后续补齐。未打补丁的旧版不会因新管理端强制要求排空而不能使用，也不能被描述为已经获得了安全自动升级能力。

本阶段验证：OpenCode 11 项治理测试、352 项原生执行/模型/后台测试通过；新增接单相关原生测试 75 项通过、1 项跳过（不可读目录场景）。管理端构建通过，相关前端测试 16 项通过；隔离 MySQL 空库验证了排空能力门槛、重复操作拒绝、旧代次拒绝、匹配取消、迟到回执隔离与超时失效。仍未进行 Lite/Pro 真正镜像升级和恢复演练。

### RuntimeAgent 优雅停止确认底层接口（第三阶段，尚未接入自动升级）

代码分别位于 AgentsRuntime `clawmanager-agent/internal/gateway/safe_stop.go`、`internal/control/safe_stop.go` 和 ClawManager `backend/internal/services/runtime_agent_stop.go`。此接口由控制令牌保护，不是面向用户的管理 API，管理页面的“排空检查”不会自动调用它。

- `GET /v1/stop-capabilities`：返回协议版本、Pod UID、当前 Agent 启动标识 `agent_epoch`、是否支持优雅停止。仅 Linux OpenCode 运行时启用该能力；`automatic_upgrade` 仍为 `false`。
- `POST /v1/gateway-stop`：必须指定实例、所有者、generation、gateway ID、32 位小写十六进制 operation ID、Pod UID、Agent 启动标识及 1～20 秒超时。缺失代次、未知字段、超大请求、附加 JSON 均拒绝。
- 首次只接受已经 running 的进程，不对仍在启动的实例发送停止。Linux 只发送 SIGTERM，不因超时升级为 SIGKILL；成功必须同时观察到主进程已经被回收和原进程组不存在。
- 开始停止后，在当前 Agent 内保留该实例的进程记录和端口/容量预留，拒绝同代重建、新代替换和普通 DELETE。失败、超时或客户端断开都不会自动解除该隔离；相同操作可重试，不同操作不能接管。进程退出监听不能把正在确认的记录清掉，也不能修改另一进程的记录。
- 回执绑定实例、代次、操作、Pod UID 和 Agent 启动标识。ClawManager 客户端校验全部身份字段和确认范围；旧 Agent 的 404/501、错误回执、空响应都按未确认处理，绝不降级为普通 DELETE 或删除 Pod。
- 普通 DELETE 也修复了“停止失败仍返回成功、提前删除记录”的问题；启动中的实例返回冲突以避免启动线程晚到。正常停止后的信号退出不误报失败。但它仍是旧生命周期接口，不具有备份授权含义。

**安全限制和后续工作：** 当前确认范围明确为 `process_group`，不包括通过 setsid 等方式脱离进程组的工具进程；不能称为全量工作进程退出证明。Agent 本地隔离是内存状态，Agent 重启会变化启动标识，但不能替代数据库中的持久化调度隔离。该接口没有提供自动释放隔离或自动恢复服务的入口，请勿作为生产手工升级命令使用。管理端持久化隔离及调度/启停/删除联动已在下一阶段补充，但独立工作进程隔离、一致性备份和升级协调器仍未完成。当前没有上线一键无损升级，也没有替换现有实例镜像。

本阶段验证：RuntimeAgent 全部内部包在 Windows 通过回归；Linux 一次性、无网络、只读源码挂载容器中 gateway/control 测试通过，覆盖真实 SIGTERM 退出、重复确认、超时不强杀、主进程退出但进程组仍存活、进程组完全退出后重试。ClawManager services/handlers/models/repository 回归通过，仍跳过已有且与此次改动无关的 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames`。`-race` 因现有环境缺少可用 CGO/C 编译器没有完成，不应记为通过。尚无实际实例升级、备份恢复或跨池迁移演练。

### 持久化升级隔离（第四阶段，2026-10-20）

本阶段为后续升级协调器提供持久化安全边界，**没有开放发起升级的 HTTP API，也没有让排空按钮自动停机或换镜像**。新增迁移 `066_add_runtime_upgrade_fence.sql` 只创建空表 `gateway_runtime_upgrades`；没有隔离记录的旧实例继续走原有生命周期，不会被自动登记或升级。

#### 状态与排空联动

内部仓储 `RuntimeUpgradeRepository` 校验实例所有者、运行代次、OpenCode 类型和 32 位小写十六进制操作 ID；变更与普通生命周期使用相同的 MySQL 连接级命名锁，并在事务内锁定实例行。

| 状态 | 进入条件与行为 | 是否可由普通排空取消解除 |
| --- | --- | --- |
| `reserved` | 实例运行中；同一操作的闸门关闭、所有计数归零、报告不足 5 秒；持久化隔离开始 | 否；仅内部 `CancelReservation` 可在尚未提交停止意图时撤销 |
| `stop_intent` | 从 `reserved` 进入，再次验证新鲜排空证据；仅记录意图，尚不等于发送停止或停止成功 | 否 |
| `recovery_required` | 活动操作进入人工/协调器恢复阶段；保留隔离，不自动重新调度 | 否 |
| `cancelled` | 仅允许从 `reserved` 撤销；在同一事务内取消排空 | 隔离已解除；不能复活相同操作 |

未知状态按仍在隔离处理。当前没有“完成升级”或“恢复完成”的写入接口，不能直接修改数据库状态来代替恢复校验。相同操作的重复调用可幂等处理，不同操作不能接管正在执行的升级。

隔离存在时，排空租约到期不再自动打开闸门，运行时仍可更新同一操作的计数。但“已排空”始终要求新鲜报告；不能因为隔离仍在，就使用旧的零计数报告开始停止。普通取消、新建排空以及身份不匹配均拒绝。管理页面显示“升级隔离中”，禁用普通取消按钮，不再提示会在五分钟后自动恢复。

#### 已接入的保护入口

- 普通启动、停止、重启、环境重建、更新、强制同步和删除共用生命周期锁；后台删除重新获取锁并核对当前所有者及删除状态。
- 调度器候选查询排除隔离实例；创建/协调和 Pod 故障转移在锁内重新读取状态，避免旧快照把已停止或已隔离实例拉起。周期性 K8s 同步同样受保护。
- 实例状态、代次、工作区路径及运行绑定的写入在数据库事务内再次检查隔离。绑定清理、丢失报告清理和错误端口回收不能释放被隔离实例的绑定。
- 同一个共享 Pod 上报多个实例时，被隔离实例的状态写入被跳过，不阻止其他实例的正常上报。
- 活动隔离会阻止普通实例删除；外键 `ON DELETE RESTRICT` 也阻止直接误删。合法取消后的普通删除在同一事务内清理终态隔离记录。
- 对外的生命周期冲突返回 HTTP 409，而不是误报操作成功。数据库读取失败不会被当作“没有隔离”。

#### 部署要求与未覆盖边界

必须先完成 066 迁移，升级**所有参与生命周期和调度的管理端副本**，再由后续经过验证的协调器启用隔离。新旧管理端混跑时，旧代码不识别此表，不能宣称具有完整保护。本阶段没有激活任何业务实例的升级隔离，没有重建镜像或替换本机 K8s。

持久化记录可跨管理端重启保留，但不是任意外部写入者的隔离证明：手工 kubectl、旧控制器、失去数据库连接后仍在进行的远程操作，以及脱离进程组的工具进程，仍需要后续命令 fencing、工作进程隔离和协调器处理。此表也不替代 RuntimeAgent 的停止确认。

剩余工作包括：未打补丁旧实例首次升级的可靠接单闸门、停止接口与持久化状态机的协调、所有工作区写入者停止证明、停旧后的备份及完整性校验、Lite 共享池逐实例迁移、新实例验证、恢复/回滚与故障演练。因此当前只能称为“升级隔离基础能力”，不能称为完整的一键无损升级。

本阶段验证：models、db、repository、services、handlers、utils 包回归通过，仍明确跳过既有分类规则失败项 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames`；独立 MySQL 8.0.46 测试库验证隔离与命名锁互斥、身份校验、租约到期仍关闭闸门、新鲜报告要求、停止意图后的取消拒绝、数据库写入保护及取消后正常删除。前端排空面板/治理状态相关测试 8 项通过，生产构建通过。以上不代表真实运行时迁移、备份恢复或浏览器端到端验收已经完成。

### 排空后的停止协调（第五阶段，2026-10-20）

新增内部 `RuntimeUpgradeStopCoordinator.StopReserved`，串联持久化隔离、停止意图与 RuntimeAgent 回执。**未注册为 HTTP API、后台作业或页面按钮，不会自行排空/隔离，也不会改变旧实例的日常使用。** 第四阶段的停止协调由本节补充，但仅覆盖进程组停止确认，不是完整升级协调器。

#### 持久化目标与执行顺序

迁移 `067_add_runtime_upgrade_stop.sql` 仅建空表，保存实例、所有者、代次、操作 ID、Runtime Pod ID、Pod UID、Agent 启动标识、控制地址、Gateway ID、预期 PID 及停止回执，不修改旧实例记录。

1. 只接收匹配身份的 `reserved` 操作，或同一目标的 `stop_intent` / `process_group_confirmed` 重试；`recovery_required` 不自动继续。
2. 从服务端仓储读取实例、绑定及 Pod，不接受调用者提供控制地址。检查 Agent 优雅停止能力及新增的 `expected_pid_guard` 能力。
3. `PrepareStop` 在同一事务中核对并锁定实例、绑定、Pod，检查新鲜排空报告，保存不可变目标并记录 `stop_intent`。事务出错或提交结果不明时，不发送停止请求。
4. 停止请求携带 `expected_pid`，Agent 在进程记录锁内、发送 SIGTERM 前核对实际 PID。优雅停止等待 20 秒，HTTP 阶段最多 25 秒，单次能力查询最多 5 秒，调用者取消也会传播。
5. 校验回执身份及 PID，再次查询 Agent 启动标识检查中途重启。数据库事务再次验证目标，落库回执及 `process_group_confirmed` 状态后才返回确认结果。

`process_group_confirmed` **仍是活动隔离状态**。不会释放容量、删除绑定、增加代次、启动新实例、恢复接单或开始备份。它仅代表原 Agent 确认指定进程组退出，不包括脱离进程组的工具，也不保证历史确认之后没有新的外部写入。

#### 超时、重试与兼容

- 停止超时、连接中断或回执写库失败不能解释为“没有停止”或“已经完成”。保留意图与隔离，只能重试同一目标、同一操作，不回退到普通 DELETE、删除 Pod 或强杀。
- 管理端重启后读取持久化目标，不根据当前实例重新猜测目标。有停止意图但没有目标记录的历史/异常状态转入待恢复，不自动接管。
- 重试前发现 Pod、控制地址、PID 或 Agent 启动标识变化，停止推进并标记 `recovery_required`；不向改变后的地址发送停止。回执身份不符也进入恢复状态，隔离不解除。
- 重试会核对当前 Agent 身份并再次请求确认，可能返回更新的时间戳；底层可复用已确认的进程组退出结果，不能将新时间戳当作重新检查了全部写入者。仓储接受同一目标的重复确认，保留首次确认时间；不会只凭数据库历史回执直接返回成功。
- Agent 的 PID 请求字段为向后兼容的可选字段；新协调器始终要求能力声明并发送正数 PID。旧 Agent 不支持此能力时不能参与该升级阶段，但原有运行和调用不受影响。
- 数据库异常或 Agent 不可达不自动释放隔离。解除隔离须由未来的恢复/回滚流程完成验证，当前没有绕过校验的公开入口。

#### 代码位置与验证

- ClawManager：`backend/internal/services/runtime_upgrade_stop_coordinator.go`、`backend/internal/repository/runtime_upgrade_stop_repository.go`、`backend/internal/models/runtime_upgrade_stop.go`。
- AgentsRuntime：`clawmanager-agent/internal/gateway/safe_stop.go`，已同步 PID 能力声明、停止前校验及测试，未替换运行中的 Agent。
- 后端相关包回归通过，仍跳过前述既有模型分类失败项；独立 MySQL 8.0.46 空库验证目标固定、过期证据拒绝、插入目标后注入故障时整笔事务回滚、重复确认及保留首次回执、确认后隔离仍生效、Pod 替换拒绝、迟到回执不能覆盖恢复状态。
- 实际 AgentsRuntime 全部 Go 包测试通过；一次性、无网络、只读源码 Linux 容器的 gateway/control 测试通过，覆盖真实优雅停止及新增 PID 校验。本阶段没有前端改动，未重新验收浏览器流程。

部署需先执行 066/067 迁移并升级全部管理端副本，再在独立测试运行时部署支持 PID 校验的 Agent。当前没有公开激活入口，不能将本节当作手工停机指南。剩余工作包括全量写入者隔离、旧版首次排空、一致性备份、跨池迁移、新实例验证与受控回滚。本轮未构建交付镜像、未更新 K8s、未对业务实例执行停止或迁移。

### 管理端工作区写入与命令队列隔离（第六阶段，2026-10-20）

排空报告和模型进程组停止并不覆盖管理端上传文件、安装技能或尚未执行的管理命令。本阶段补上这些实际写入路径，**仍未开放自动备份或完整升级入口**。

#### 已接入的路径

- 实例工作区的上传、建目录、重命名和删除统一经过 `WithWorkspaceUpgradeGuard`。本地挂载工作区和通过 K8s exec 操作的 Pro 工作区均在 `cmd/server/main.go` 注入保护；普通用户、管理员和共享链接复用相同服务，不能借共享链接绕过隔离。
- 写入期间持有与升级预留相同的实例生命周期锁。锁内重新读取当前所有者、运行代次、工作区路径及本地/远程后端类型，旧请求的快照不匹配时拒绝。取消、失败和正常返回均释放锁；未配置持久化锁时不静默降为无保护写入。
- 列目录、预览和下载保留原来的读取行为；授权校验不变。并发写入与启停等操作冲突会返回 409，调用方应等待当前操作完成再重试。
- 技能安装（含 Hub 批量安装）、恢复和移除，从读取实例到文件修改、元数据更新、命令入队均持有同一生命周期锁。保护放在服务入口，不仅是页面按钮上；内部文件辅助函数要求调用方已持锁。
- `instance_commands` 的创建及非终态更新，在实例行锁内检查升级隔离，阻止新的入队、派发和开始运行。`succeeded` / `failed` 完成回执仍可上报；不可把已结束命令重新改回运行，也不能通过改 `instance_id` 更新其他实例的命令。
- `instance_desired_state` 创建/更新也检查隔离，防止命令完成后的迟到操作重新设置启停或配置意图；更新同时核对记录与实例身份。

#### 与升级状态机的衔接

升级预留、首次停止意图，以及目标停止确认事务都会检查 `instance_commands`。除 `succeeded` / `failed` 外的命令均保守视为未完成，包括待执行、已派发、运行中及未知状态；返回明确的“实例仍有未完成的管理命令”冲突。

命令写入和升级检查锁定同一实例行，避免“刚看到队列为空，另一个请求就插入命令”的窗口。已经派发但未确认完成的命令不会被超时清零来凑出升级条件；必须完成或由后续恢复机制核实。真实测试还模拟旧控制器绕过入口插入命令，验证停止确认会拒绝推进、完成回执仍能正常收尾。

本阶段没有新增数据库迁移，复用已有控制平面命令表及 066/067 升级表；仍要求先完成迁移并升级所有管理端副本。旧实例未自动登记升级，普通读写和命令流程保留；只在真实生命周期并发或已有升级隔离时拒绝不安全写入。

#### 尚不能作为备份凭据

这些锁仅保护已接入的平台调用，并非“所有工作区写入者已经退出”的证明。仍需处理 Team 共享工作区和上下文同步、运行时脱离进程组的工具、外部挂载写入，以及数据库连接丢失后的未完成操作。K8s exec 返回/取消也不等于远端写入进程已退出；命令标记失败同样不等于外部进程已停止。

因此，本阶段不能授权一致性备份，不能释放升级隔离，更不能升级整个共享池。后续仍需运行时全量工作进程隔离、跨入口写入者收尾确认、备份校验、逐实例迁移和恢复回滚。当前没有执行文件备份、业务停机或 K8s 替换。

验证：services、handlers、repository、models、utils 回归通过，服务端入口编译通过；仍显式跳过既有模型分类失败项 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames`。新增测试覆盖四类文件写操作、代次/所有者/路径变化、上传未结束时生命周期操作被阻止、错误释放锁、普通读写兼容、Pro 与本地 HTTP 409、共享链接无法绕过、技能入口阻断。独立 MySQL 8.0.46 空库验证管理命令与升级互斥、终态回执、拒绝复活旧命令、期望状态写入保护及前五阶段回归。未修改前端或 AgentsRuntime，未进行浏览器端到端升级验收。

### 远端工作区写入结果持久化（第七阶段，2026-10-20）

第六阶段的生命周期锁在 K8s exec 返回后释放，但断线或取消不代表容器中的 Python 写入进程已经结束。本阶段增加持久化的未完成写入记录，弥补仅靠管理端内存/连接锁无法覆盖的异常窗口。**本阶段仍不授权备份，不提供完整升级入口。**

#### 执行与确认规则

- 新增迁移 `068_add_workspace_write_intents.sql`，仅创建空表，不登记旧实例、不停止运行时。记录操作 ID、实例 ID、所有者、代次、操作类型和开始时间，不保存文件内容、密钥或命令参数。
- 远端工作区上传、建目录、重命名、删除通过参数校验后，在实例行锁内核对身份及升级隔离、提交写入意图，然后才调用 K8s exec。记录失败不发起执行；重复操作 ID 不覆盖旧记录，也不授权重复执行。
- 只有正常退出、K8s 返回明确的进程退出状态，或执行器证明根本未发起 exec 时，才能删除对应记录。确认使用独立的、最多 5 秒的上下文，避免浏览器取消导致已完成操作无法收尾。
- 此退出规则仅允许用于平台内置、同步执行且不产生后台子进程的四类 Python 文件操作脚本。代码校验操作类型、脚本及参数个数，禁止复用于任意 shell、用户工具或 Team 远程命令。
- 网络异常、超时、取消以及管理进程崩溃留下的记录不自动过期、不因运行代次变化而忽略。stderr 中的错误文字不是进程退出证明。返回信息包含操作 ID，并提醒先检查文件结果、不要盲目重试。
- 已确认退出后的响应 JSON 解析错误不等于进程仍在运行；写入结束但确认落库失败时保留记录，并返回错误，不谎报成功。

#### 与升级的事务互斥

升级预留、停止意图、停止目标准备/重试及停止确认，统一检查管理命令和所有代次的未完成写入。记录创建、收尾及升级检查锁定同一实例行。已进入升级隔离后不能创建新写入记录；准确匹配原操作的完成回执仍可收尾。旧代次、旧管理进程和长时间没有变化的记录都不会被当作已完成。

记录只阻止受保护升级继续，不会自动把普通实例登记升级、禁止模型调用或要求旧运行时立即更新。读取工作区无需创建记录。正常删除实例会随外键删除其记录；这不是升级恢复路径，已有升级隔离仍禁止普通删除。发布前须执行 068 迁移并升级所有管理端副本，未升级副本仍可能绕过新保护。

#### 恢复边界和待完成工作

当前没有按超时清除、忽略记录或管理员强行放行的接口。异常记录说明“平台未获得结束证明”，不一定表示进程仍在写入。不能只检查文件存在就清除记录；后续恢复流程需要核实对应运行时/容器的完整写入者退出情况。本表不是 Pod/PID 级进程取证记录，也不能单独完成恢复。

本次检查还确认 Team 共享目录及上下文同步可能跨多个实例写入，不能拿单实例进程组回执当作共享目录静止证明；本阶段尚未实现 Team 全成员隔离，也没有新增绕过它的备份路径。本地写入连接丢失、技能外部命令、运行时脱离进程组的工具和外部挂载写入仍须补齐。整体剩余事项仍包括首次排空、全量写入者确认、一致性备份、逐实例迁移、新实例验证及受控回滚。

#### 代码与验证

- 服务入口：`backend/internal/services/runtime_workspace_write.go`；`runtime_workspace_file_service.go` 四类变更操作接入，`cmd/server/main.go` 注入持久化仓储。
- 仓储与状态机：`workspace_write_repository.go`、`runtime_upgrade_writers.go`；不新增公开停止/清除接口。
- 单元测试覆盖四类操作、失败前不执行、正常退出/明确退出回执、断线/取消/超时、伪造 stderr、请求取消后的独立收尾、确认落库失败、JSON 解析失败、只读兼容及拒绝任意脚本。
- services、handlers、repository、db、models、utils 回归通过，服务端入口编译通过；仍跳过前述既有模型名称分类失败项。独立 MySQL 8.0.46 空库验证身份、重复操作、重建仓储后记录仍阻止升级、旧时间戳不失效、错误回执拒绝、隔离后不能发起写入、旧代次未结束写入阻止停止重试与确认。

本轮未构建交付镜像、未更新 K8s、未停止或迁移业务实例；未修改 OpenCode UI 或 AgentsRuntime。

<a id="model-access-closeout"></a>

### 模型权限与 OpenCode 兼容：收尾验收清单（2026-10-20）

#### 本次范围

本次只收尾模型授权、Gateway 校验、OpenCode 治理协议及旧实例兼容。不接手 Team，不继续增加升级阶段，不以完成 Team、备份或迁移系统作为模型授权功能的前置条件。此前已写的升级保护暂保留，不擅自回滚混合工作区中的代码，也不激活内部停止协调器。

#### 已实现的行为与边界

| 场景 | 当前实现 |
| --- | --- |
| 默认授权 | 新增模型默认全员；管理员可改为指定用户，当前指定范围至少选一人 |
| 用户身份 | 以实例所有者的用户 ID 校验；邮箱用于展示和搜索，改邮箱不会改变授权身份 |
| 多个 URL 下同名模型 | 按模型配置记录分别保存授权，不按上游模型名称合并 |
| 指定模型、Auto、故障切换和安全模型改路由 | 都受用户授权约束；不可见模型不能靠直接调用接口或路由回退绕过 |
| 撤权后继续发送 | 拒绝调用并返回 `model_unavailable` / “当前模型不可用”，不把权限拒绝当作普通故障切到其他模型 |
| 正在进行的模型请求 | 本副本通知加数据库轮询检查授权并中断请求；不是零延迟硬实时，也不等于所有外部工具已经停止 |
| 旧实例（未登记治理协议） | 保持原调用方式，ACL 和 Auto 过滤仍执行；不因发布管理端自动登记协议或强制更换运行时 |
| 旧实例模型列表 | 不保证运行时缓存立即刷新；残留列表项不代表仍可调用。实时目录同步需要适配运行时 |
| 已登记的新 OpenCode 实例 | 必须提供完整任务身份和当前运行代次；删除请求头、重启或报告过期不能悄悄降级成旧协议 |
| 整轮取消 | 已适配运行时负责停止受影响轮次并回传确认；已完成的文件写入/外部副作用不回滚，未适配运行时不承诺相同能力 |
| 能力状态 | 管理页区分“兼容模式”“完整协议已连接”“等待运行时确认”和状态读取失败；连接状态不代替具体取消回执 |

不对旧实例承诺“无需适配即可整轮取消”，也不把“新旧调用兼容”描述为“完整无损迁移已实现”。

#### 发布和验收顺序

1. **先发布管理端并执行随版本提供的全部迁移。** 当前仓库迁移已到 068，不能只手工执行 062/064。授权和协议迁移不会自动登记旧实例。所有管理端副本都应更新，不能让旧副本绕过新授权校验。
2. **先保留旧实例镜像验证兼容。** 用实例所有者分别验证普通调用、指定模型拒绝及 Auto 过滤；不要为了开启模型授权而先替换整个 OpenCode 共享池。
3. **用独立测试实例验证新运行时。** 固定 OpenCode 2.0.27 核心补丁；新旧实例并存。不修改 OpenCode UI，不覆盖现有实例数据。已有源码和 Dockerfile 不代表交付镜像已完成验收。
4. **验证撤权和恢复授权。** 长响应中撤权后检查请求中断；新运行时检查整轮停止及取消回执。重新授权后用新轮次测试，原先已撤销的任务不得复活。
5. **不要把已登记实例直接回滚到无补丁镜像。** 登记是持久的，不会因镜像回退而消失；目前没有已验收的受控协议回滚/无损迁移流程。

推荐使用 A、B、C 三个普通测试用户，以及两个不同 URL 的模型配置：

| 验收操作 | 预期 |
| --- | --- |
| 模型 1 仅授权 A/B，模型 2 全员 | C 的受控目录不出现模型 1；A/B 正常访问；两个部署的授权互不影响 |
| C 在旧会话指定模型 1，或直接请求接口 | 明确拒绝“当前模型不可用” |
| C 选择 Auto，并触发候选切换 | 不命中模型 1，包括重试/回退 |
| A 正在使用模型 1 时撤销 A 授权 | 后续调用被拒绝，活动模型请求中断；适配运行时还需确认整轮取消 |
| 管理员浏览 C 的实例 | 不因管理员自身身份扩大 C 实例的模型可用范围 |
| 发布新管理端，保持旧运行时不变 | 仍能使用有权模型，不被要求补交新协议任务头 |
| 已登记实例省略任务头或带旧代次 | 拒绝，不自动降级；恢复合法完整身份后再按授权处理 |

#### 本轮验证记录

- 新增 `internal/middleware/runtime_protocol_test.go`，覆盖旧实例、已登记实例、旧代次登记不被忽略、数据库失败不被解释为旧模式、非实例凭证不查询实例协议。
- 补充 `internal/handlers/gateway_runtime_governance_test.go`，覆盖完整/缺失/部分任务身份、旧代次拒绝及合法旧调用不合成任务。
- 后端 aigateway、handlers、middleware、models、repository、services、db、utils 相关回归通过，server 入口编译通过；仍显式排除既有 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames` 分类失败项，不声称整个仓库无条件全绿。
- 前端授权/路由契约/治理状态/排空面板共 17 项测试通过；TypeScript 和 Vite 生产构建通过，存在非阻塞的大分块提示。
- OpenCode 固定版本补丁校验通过；`governance.ts`、`governance.test.ts`、安装器、Dockerfile 与 `D:\AgentsRuntime\opencode\model-governance` 对应文件一致。
- **本轮 OpenCode 核心测试未跑通环境准备**：本地源码缺少 `effect` 依赖，现有 Bun 为 1.3.14，固定源码要求交付使用 1.4.2。不能据此宣称运行时核心测试或真实 Lite/Pro 端到端验收通过。
- 本轮没有重跑 MySQL 集成测试；此前独立测试库记录保留。本轮没有更新业务数据库、构建 Docker 镜像、更新 K8s 或停止实例。

本次新增内容为回归测试和交付边界说明，没有扩大运行时功能或修改 Team 代码。后续镜像构建、独立测试实例验收按发布任务执行，不再以继续扩展升级系统作为收尾条件。

#### 正式版交付构建进展（2026-10-21）

- 实际 `D:\AgentsRuntime\clawmanager-agent` 执行 `go test ./...` 通过。这不是 OpenCode 核心或真实实例端到端验收结果。
- 官方固定提交 `1f4d07a4a519185955ae8ab2ea3607805509e386` 拉取成功，治理补丁应用成功。构建使用 Bun **1.4.2**；没有降低版本要求，也没有修改 OpenCode UI。
- Docker Hub 鉴权端点连接超时后，改用官方 npm `@oven/bun-linux-x64@1.4.2`，核对发布的 SHA-512 完整性后制作本地工具链 `clawmanager-local/opencode-build:bun1.4.2-20261021`。它是构建工具链，不是可部署的治理运行时。
- `bun install --frozen-lockfile` 完成，安装 2,397 个包，首次安装约 15 分钟。随后已进入 `packages/core` 类型检查，但尚未取得完成结果；不能记为核心测试通过。
- 主机剩余物理内存降到约 377 MiB，随后 Docker Engine `/_ping` 返回 500、Docker Desktop 的 K8s API 请求超时。已停止本次构建，低并行度重试未能启动。没有重启 Docker Desktop、没有替换共享运行时池或已有实例镜像。
- 构建脚本已补充类型检查的并行度和软内存限制（`GOMAXPROCS=2`、`GOMEMLIMIT=2GiB`），仅作用于构建。**这些限制尚未完成重试验证**，也不能代替足够的主机内存。

恢复后从以下断点继续，不得跳过测试或直接将兼容实例登记为完整治理实例：

1. 确认主机内存、Docker Engine 和本机 K8s 恢复；如需要重启 Docker Desktop，先获得用户确认，因为会中断已有实例。
2. 用固定 Bun 1.4.2 和低并行度重新运行核心类型检查、治理测试及会话/工具/后台任务/压缩回归。
3. 构建并检查治理版 Lite/Pro 镜像，记录实际镜像 digest；将经过核对的交付文件同步到 AgentsRuntime。
4. 创建隔离测试实例。不能把测试 Lite Agent 直接注册到业务共享池，否则普通新实例也可能被调度到测试镜像。
5. 验收 A/B/C 用户目录隔离、显式/Auto/备用路由授权、撤权整轮中止及真实取消回执，再检查旧实例仍可用。

当前**没有完成治理运行时镜像，没有更新运行时 K8s，没有完成整轮取消或升级恢复验收**。Team 和通用迁移平台不在本轮范围。

#### 环境恢复后的受限验证（2026-10-21）

Docker Engine 恢复响应、K8s Pod 就绪后，改用独立构建容器，限制 2 CPU、2.25 GiB 内存、禁用该容器交换内存，专用卷保存源码与依赖。未修改 Docker Desktop 的全局资源配置或已有实例。

验证结果：

| 检查 | 结果与边界 |
| --- | --- |
| 固定版本源码、补丁应用、锁定依赖安装 | 通过；Bun 1.4.2、OpenCode 2.0.27 固定提交，2,397 个包安装完成 |
| 治理测试 | 11 通过、0 失败、46 次断言，约 9.7 秒 |
| 原有核心回归 | 427 通过、1 跳过、0 失败、1,928 次断言，16 个测试文件，约 118 秒；跳过项为上游的不可读源目录恢复用例 |
| 完整核心类型检查，2.25 GiB 容器 | 未通过执行门槛：`OOMKilled=true`，类型检查进程被 SIGKILL；不能据此断言源码没有类型错误 |
| 完整核心类型检查，2.5 GiB / 2 CPU / 240 秒重试 | `OOMKilled=false`，退出码 124（超时）；仍未取得类型检查结果 |
| 现有 K8s | 管理端及 OpenCode 共享运行时 Deployment 均为 1/1 可用；未更换其镜像 |

核心回归覆盖会话续跑与重试、工具调用与中断、后台任务、手动及自动压缩等。但这些源码测试**不替代真实实例的目录授权、整轮取消回执、后台/子任务联动和升级恢复验收**。

本轮交付文件改进：

- Dockerfile 将锁定依赖安装前置，只复制实际补丁文件，避免修改文档/补丁时重复下载整套依赖。
- 增加 `build-local.sh`，支持专用卷和受限容器，失败即停止；类型检查默认最多 600 秒，可显式调整。
- Dockerfile、README 和 `build-local.sh` 已同步到 `D:\AgentsRuntime\opencode\model-governance`；连同三份原有补丁文件共六份文件逐一核对一致。同步前检查旧文件哈希，没有覆盖未知更改。
- 测试容器已结束，保留专用卷 `cm-opencode-governance-build-20261021`，后续无需重新准备依赖。没有修改 OpenCode UI、Team 或业务模型配置。

结束检查时 Windows 剩余物理内存约 537 MiB，未继续提高构建内存压力。下一步需要先提供足够的构建资源，完成完整类型检查与 CLI/原版 Web UI 构建，再打包运行时并执行隔离实例验收。当前仍**没有新的治理运行时镜像，也没有执行 K8s 镜像替换**。
