# Runtime Agent 通用接入规范

本文定义任意新 runtime 接入 ClawManager Agent Control Plane 的通用方案。后续新增 OpenClaw、Hermes 以外的 runtime 时，应优先遵守本文，再补充该 runtime 自己的镜像构建细节。

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
| `CLAWMANAGER_LLM_PROVIDER` | 当前为 `openai-compatible` |
| `CLAWMANAGER_INSTANCE_TOKEN` | 当前实例 token，和 Gateway API key 同源 |
| `OPENAI_BASE_URL` | OpenAI SDK 兼容别名 |
| `OPENAI_API_BASE` | OpenAI SDK 兼容别名 |
| `OPENAI_API_KEY` | OpenAI SDK 兼容别名 |
| `OPENAI_MODEL` | 默认模型，通常为 `auto` |

Runtime 内的应用和 agent 如果需要调用模型，优先使用这些变量，不要让用户在镜像内手工写入 provider key。

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
| `refresh_skill_inventory` | 重新扫描 skill 目录并上报完整 inventory |
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
- `content_md5` 必须按目录内容指纹计算，不是 zip 文件 MD5。完整算法见 [Skill Content MD5 Calculation Spec](skill-content-md5-spec.md)。

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
