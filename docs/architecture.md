# ClawManager 架构文档

> 代码仓库：<https://github.com/a2d2-dev/ClawManager>
> 版本基线：`main` @ `0881cad`（2026-08）· 配套图示见 [`docs/main/clawmanager-architecture.html`](./main/clawmanager-architecture.html) 与 `clawmanager-architecture-full.png`
> 术语以 [`CONTEXT.md`](../CONTEXT.md) 为准；架构决策以 [`docs/adr/`](./adr/) 为准。

---

## 1. 系统概述

ClawManager 是一个 **Kubernetes 原生的 AI Agent 实例控制平面**。它做三件事：

1. **供给（Provision）**：为每个用户拉起并托管一个长期驻留的 Agent 运行时（小龙虾 / Hermes / OpenClaw），按 **三种隔离档位** 落到不同的 Kubernetes 形态上。
2. **治理（Govern）**：所有实例的模型调用统一经过 **AI Gateway**——模型白名单、敏感内容风险扫描、安全模型改路、按用户/实例/模型的成本核算与审计，全部在这一层收口。
3. **复用（Reuse）**：把技能（Skill）、渠道（Channel）、会话模板、团队（Team）等可复用资源跨实例统一管理与下发。

核心设计取向：**边缘计算模式**（控制面与运行时都跑在贴近用户的边缘节点/集群），**同节点多租户**，以及按威胁模型分档的运行时隔离（见 ADR-0001）。

### 1.1 顶层组件

```
┌────────────────────────────────────────────────────────────────────┐
│                          用户 / 管理员浏览器                          │
└───────────────┬────────────────────────────────────┬───────────────┘
                │ HTTPS (REST + WebSocket)            │
        ┌───────▼────────┐                    ┌───────▼───────────┐
        │  Frontend       │                    │  实例访问 / 终端   │
        │  React 19 + Vite│  ← 反代 /api/v1 →  │  iframe / xterm   │
        └───────┬────────┘                    └───────┬───────────┘
                │                                     │
┌───────────────▼─────────────────────────────────────▼───────────────┐
│                    ClawManager Backend (Go, 单体)                     │
│  Handlers(gin) → Services → Repository(upper/db) → MySQL             │
│  ├─ AI Gateway（模型治理 / 风险 / 成本 / 审计）                        │
│  ├─ 三档 RuntimeBackend（lite / isolated / pro）                     │
│  ├─ Agent Control Plane（命令下发 / 心跳 / 状态回收）                  │
│  ├─ 后台循环（Leader 选举下：SyncService / RuntimeScheduler）         │
│  └─ AuditLogger（JSON-lines 审计流）                                  │
└───┬──────────────┬───────────────┬───────────────┬──────────────────┘
    │ client-go    │ HTTP          │ S3            │ Redis
    ▼              ▼               ▼               ▼
┌────────┐  ┌──────────────┐  ┌────────┐   ┌──────────────┐
│  K8s   │  │ Runtime Pod  │  │ MinIO  │   │ Team Redis   │
│  API   │  │  (Agent+GW)  │  │ 对象存储│   │ 事件/协同     │
└───┬────┘  └──────────────┘  └────────┘   └──────────────┘
    │ 创建 Deployment/Sandbox/PVC/Service/NetworkPolicy
    ▼
┌───────────────────────────────────────────────────────────────────┐
│  运行时载体：Lite 共享 Runtime Pod ／ Isolated agent-sandbox ／ Pro  │
│  独立 Webtop Deployment。每实例一个 gateway 子进程 + 一个 workspace  │
└───────────────────────────────────────────────────────────────────┘
```

---

## 2. 技术栈

| 层 | 技术 |
|---|---|
| **后端** | Go 1.26 · gin（HTTP）· upper.io/db（MySQL ORM）· k8s.io/client-go v0.32（含 dynamic client）· gorilla/websocket |
| **前端** | React 19 · Vite 8 · react-router 7 · Zustand（客户端态）+ TanStack Query（服务端态）· Tailwind 3 · axios · @xterm/xterm（终端）· 自研 i18n（en/zh/ja/ko/de） |
| **存储** | MySQL（主数据）· MinIO / S3 兼容（对象存储：技能包、导出、备份）· Redis（团队事件/协同） |
| **编排底座** | Kubernetes · `kubernetes-sigs/agent-sandbox` CRD（Isolated 档，v0.5.1 / v1beta1）· gVisor/Kata（可选 RuntimeClass 透传） |
| **e2e** | Playwright（admin / api / instances / smoke / teams，按 @p0/@p1/@p2 分级） |

---

## 3. 运行时与实例模型（核心域）

这是 ClawManager 最有分量的设计。术语严格遵循 `CONTEXT.md`。

### 3.1 两条正交轴

实例由两个**互相正交**的维度描述，代码任何一处都不得从一个推导另一个——调用方显式传两者：

- **`runtime_type`** —— 跑的是**什么**：`gateway` / `desktop` / `shell`
- **`instance_mode`** —— **多隔离/多贵**：`lite` / `isolated` / `pro`

### 3.2 三档隔离（ADR-0001）

| 档位 | 载体 | 隔离级别 | 典型场景 |
|---|---|---|---|
| **lite** | 多实例打包进一个**共享 Runtime Pod**（每 Pod 至多 `capacity` 个，如 100） | 仅正确性级跨用户隔离（UID/GID + workspace 路径 + cgroup） | 低成本网关实例 |
| **isolated** | 每实例一个 **agent-sandbox `Sandbox`** 无头单元，持久 workspace，无桌面栈 | 劫持级隔离——假设 Agent 会被提示注入劫持，把爆炸半径收敛到单 Pod | CI 编码 Agent、无头自动化 |
| **pro** | 每实例一个**独立 Runtime Pod + Webtop(KasmVNC) 桌面** | 独占 Pod | 完整图形桌面实例 |

**永不跨档迁移**——创建时就选定正确档位（ADR-0001）。

### 3.3 RuntimeBackend 统一接缝

三档生命周期全部走同一个 Go 接口，`instance_mode` → backend 的分派是唯一接缝，模式特定逻辑不外泄到共享代码：

```go
type RuntimeBackend interface {
    Create(ctx, userID, req, instanceMode, runtimeType, envOverridesJSON) (*Instance, error)
    Start(ctx, instance, runtimeType) error
    Stop(ctx, instance) error
    Delete(ctx, instance) error
    Status(ctx, instance) (*InstanceStatus, error)
    Endpoint(ctx, instance) (*RuntimeEndpoint, error)
    AttachPolicy(ctx, instance, policy) error   // 预留给出向策略（part B）
    Suspend(ctx, instance) error
}
```

- `liteBackend` —— 共享 Runtime Pod 内新增/删除 gateway 子进程（走 Runtime Agent）
- `proBackend` —— 独立 Deployment/Service 的桌面生命周期（含 `desktop` 与 `shell` 两种 runtime_type）
- `sandboxBackend` —— 操作 agent-sandbox `Sandbox` CR（dynamic client）

分派规则（`runtimeBackendForInstance`）：由持久化的 `instance_mode` 决定 backend；空/非法值**显式报错，绝不静默误路由**。

### 3.4 Isolated 档的关键机制（ADR-0003）

- **强制出向（egress）**：`HTTP(S)_PROXY` 无条件注入 Sandbox podTemplate（部署期 `CLAWMANAGER_EGRESS_PROXY_URL` 推导）；代理 env 是**保留键**，用户 override 含它们即在 Create/Update 被拒，且注入在 override 合并**之后**重放，防止被覆盖。
- **Fail-closed**：Create 与 Start 各做一次 server 侧 proxy 可达性预检（URL parse + TCP dial 2s×1 重试），失败以稳定错误码 `egress_proxy_unreachable`（→503）拒绝；无可算 proxy URL ⇒ Isolated 档不可用（显式错误）。
- **生命周期观察**：状态直接读 `Sandbox.Status.Conditions`（按 `lastTransitionTime` 取最新，规避陈旧条件陷阱），不建平行状态机。
- **自愈**：`Finished(PodFailed)` 时用 **`Suspended → Running` 翻转**恢复（实测该翻转保留 PVC 数据；直接删 CR 会因 PVC ownerReference 级联删除工作区——见 spike）；带冷却 + 上限，超限置 error。
- **镜像**：Isolated 只允许平台默认镜像（拒绝用户自定义 `image_registry`）；加固代理镜像本身作为跟进项跟踪。

---

## 4. 后端架构（分层）

单体 Go 服务（module `clawreef`），入口 `backend/cmd/server/main.go`。经典四层 + K8s 操作子层：

```
handlers/         gin HTTP 处理器，参数绑定/鉴权/响应；不含业务逻辑
   ↓
services/         业务逻辑核心（~60 个 service 文件）
   ├─ services/k8s/     纯 K8s 资源操作（Deployment/Pod/PVC/Service/Namespace/NetworkPolicy/ConfigMap/Secret/Cleanup）
   └─ aigateway/        AI Gateway 独立子系统
   ↓
repository/       upper/db 数据访问，一实体一 repo
   ↓
models/           领域实体（db tag + json tag）
db/migrations/    038 个幂等 SQL 迁移
```

### 4.1 关键服务分组

| 分组 | 代表 service | 职责 |
|---|---|---|
| **实例生命周期** | `instance_service` · `lite_backend` · `pro_backend` · `sandbox_backend` · `sandbox_egress` · `runtime_placement` | 三档 CRUD、egress 强制、placement 透传 |
| **实例访问/代理** | `instance_proxy_service` · `instance_access_service` · `instance_external_access_service` · `instance_shell_service` | JWT 访问令牌、反向代理到 gateway、外部分享链接、shell 流 |
| **Agent 控制面** | `instance_agent_service` · `instance_command_service` · `runtime_agent_client` · `instance_config_revision_service` | 命令下发（start/stop/apply_config/install_skill）、心跳、期望态、配置版本 |
| **运行时池** | `runtime_scheduler` · `runtime_capacity` · `runtime_leader` · `runtime_events` · `sync_service` | 共享 Runtime Pod 调度、容量、Leader 选举、状态回收循环 |
| **配置注入** | `instance_env` · `openclaw_config_service` · `openclaw_transfer_service` · `desktop_stream_profile` · `skill_service` | Bootstrap manifest、渠道/技能/模板注入、导入导出 |
| **工作区** | `workspace_file_service` · `runtime_workspace_file_service` · `workspace_path_guard` · `object_storage_service` | 每实例持久目录读写、路径越权防护、S3 |
| **能力探测** | `runtime_capabilities` · `system_image_setting_service` | 启动时探测 agent-sandbox CRD、镜像设置 |
| **可观测/审计** | `audit_logger` · `audit_emitters` · `audit_event_service` · `ai_observability_service` · `cost_record_service` · `model_invocation_service` | JSON-lines 审计流、AI 调用审计、成本 |
| **团队/多智能体** | `team_service` · `team_redis` · `chat_session_service` · `chat_message_service` | 团队编排、事件 outbox、工作流账本 |
| **安全** | `risk_detection_service` · `risk_rule_service` · `risk_hit_service` · `security_scan_service` · `skill_scanner_client` · `secret_ref_service` | 敏感内容风险、技能扫描、密钥引用 |
| **账户/配额** | `auth_service` · `user_service` · `quota_service` | JWT 认证、用户、配额 |

### 4.2 启动装配顺序（main.go）

1. **能力探测** `ProbeRuntimeCapabilities`（探测 agent-sandbox CRD，失败不影响启动）
2. 连 MySQL、构建全部 repository
3. `NewAuditLoggerFromEnv`（`CLAWMANAGER_AUDIT_LOG_ENABLED`，默认 ON）
4. 组装 services（注入 capabilities + auditLogger）与 handlers
5. **Leader 选举**（`CLAWMANAGER_LEADER_ELECTION`）：仅 Leader 副本启动后台循环
   - `SyncService.Start()` —— 周期回收实例真实状态（Isolated 走无副作用的 `ObserveStatus`）
   - `RuntimeScheduler.Start()` —— 共享 Runtime Pod 的调度/扩缩
6. gin 路由挂载，监听 `SERVER_ADDRESS`

---

## 5. AI Gateway（模型治理层）

所有实例的 LLM 调用经 `CLAWMANAGER_LLM_BASE_URL` 注入，统一流经 `backend/internal/aigateway/`。`ChatCompletions` / `StreamChatCompletions` 的治理管线（`prepareChatRequest`）：

```
请求 → 解析 traceID/会话
  ├─ RecordMessages（会话消息落库）
  ├─ 审计 gateway.request.received
  ├─ 风险扫描 riskDetector.AnalyzeText
  │     └─ 命中敏感 → 审计 gateway.risk.detected
  ├─ resolveTargetModel（模型白名单 + 风险动作）
  │     ├─ 拒绝 → recordBlockedInvocation + gateway.request.blocked (阻断)
  │     └─ 改路安全模型 → gateway.request.rerouted
  ├─ 调 Provider（callOpenAICompatible / callAnthropic，支持流式）
  ├─ 成功 → recordSuccess（token 计数 / 延迟 / 成本）
  └─ 失败 → recordFailure + gateway.request.failed
```

- **多 Provider**：OpenAI 兼容协议 与 Anthropic 原生协议双通道，`buildProviderRequestBody` 按模型协议构造。
- **风险动作**：`RouteSecureModel`（敏感内容改路到安全模型）等，规则可在 admin 侧配置（`risk_rules`，`is_enabled` 开关）。
- **审计与成本**：写入 `audit_events` 表（`gateway.*` 事件族）+ `model_invocations` / `cost_records`，按 user/instance/model/trace 归因；admin 侧有 AI 审计与成本总览。

> 注：`gateway.request.*` 走 DB 审计表，与第 6 节的 stdout JSON-lines 审计流是**两条独立管线**（v1 未统一，见 spec）。

---

## 6. 平台审计流（AuditLogger）

net-new 的可注入审计接缝，与业务主路径完全解耦（best-effort，任何失败不影响 Status/生命周期）：

- **输出**：每事件一行 JSON 到 stdout，供日志采集器摄取
- **开关**：`CLAWMANAGER_AUDIT_LOG_ENABLED`（部署期 env，默认 ON），OFF 时整流静默
- **归因**：mode-agnostic，每事件带 `instance_mode`，`instance_id` 可空（`create.refused` 时为 null）
- **v1 事件目录**：
  - 实例生命周期 `instance.{create,start,stop,delete}` 及 `.refused` 变体（带稳定 refusal code，如 `egress_proxy_unreachable`）
  - 凭证 `credential.minted`
  - Agent 面 `agent.registered` / `agent.command.{started,completed}`
  - 技能 `skill.{install,uninstall}.requested`
  - Sandbox 底座 `sandbox.{ready,finished,recreated}`（由 conditions 观察驱动，at-most-once）
  - 保留未发：`egress.*`（待代理硬化）

---

## 7. Agent Control Plane 与 Runtime Agent 契约

每个 Runtime Pod 内驻留一个 **Runtime Agent**（控制进程），负责端口分配、workspace 创建、gateway 子进程生命周期、健康检查，并向 ClawManager 上报。契约见 [`docs/agent-runtime-development-spec.md`](./agent-runtime-development-spec.md)。

### 7.1 下行（ClawManager → Agent）

| 端点 | 用途 |
|---|---|
| `POST /v1/gateways` | 异步创建 gateway 子进程（1–3s 返回 `status=starting`，不阻塞等业务进程起来） |
| `DELETE /v1/gateways/{id}` | 删除 gateway（连带删 workspace） |
| `POST /v1/drain` | 排空 Pod |
| `GET /v1/health` | 只有能接受控制指令时才 2xx |

命令通过 `instance_command` 队列下发（`install_skill` / `uninstall_skill` / `apply_config` 等）。

### 7.2 上行（Agent → ClawManager，`/api/v1/runtime-agent/*`）

`register` · `heartbeat` · `metrics/report` · `gateways/report` · `skills/report`——push 上报模型。

另有 `/api/v1/agent/*`（业务 Agent 侧）：`register` · `heartbeat` · `commands/next` · `commands/{id}/{start,finish}` · `state/report` · `skills/{inventory,upload}` · `config/revisions/{id}`。

### 7.3 Pod 侧 `/metrics`（拉模型契约）

规范文档已定义 `GET /metrics`（Prometheus text，复用 agent 端口 19090）：build-info、per-gateway up、command 计数/时延、skill install、cpu/mem/disk gauge；身份走 pod label + relabeling（基数安全），抓取靠 podTemplate 的 `prometheus.io/*` annotation。实现交付由 runtime-agent 轨道跟进。

### 7.4 凭证与信任

- **Instance token**：`CLAWMANAGER_INSTANCE_TOKEN` / `CLAWMANAGER_LLM_API_KEY` —— 界定 gateway 子进程调 AI Gateway 的权限
- **Runtime Agent token**：`RUNTIME_AGENT_CONTROL_TOKEN`（控制）/ `RUNTIME_AGENT_REPORT_TOKEN`（上报）
- **Trusted proxy**：runtime gateway 把 ClawManager 内网 CIDR（`CLAWMANAGER_TRUSTED_PROXY_CIDRS`）配为可信，经该路径来的请求跳过自身设备鉴权握手，实现单点穿透访问

---

## 8. API 面（`/api/v1`）

| 分组 | 前缀 | 摘要 |
|---|---|---|
| 认证 | `/auth` | register / login / refresh / logout / me / change-password |
| 用户 | `/users` | 用户 CRUD、导入、角色、配额（管理员子组） |
| 运行时能力 | `/runtime-capabilities` | 探测结果（如 Isolated 是否可用） |
| **实例** | `/instances` | CRUD、批量 lite、start/stop/restart、status、runtime 命令、配置版本、access、shell、sync、导入导出、外部访问、workspace 文件、技能 |
| 实例代理 | `/instances/:id/proxy[/*path]` | 反向代理到实例 gateway（JWT 保护，Sandbox 保持 cluster-private） |
| 短链访问 | `/s/:code[/*path]` | 外部分享短链 |
| 团队 | `/teams` | 团队 CRUD、任务派发、事件、workspace、成员 |
| OpenClaw 配置 | `/openclaw-configs` | 资源/bundle CRUD、编译预览、注入快照 |
| 技能 | `/skills` | 列表、导入、下载、版本、扫描结果 |
| **AI Gateway** | `/gateway/llm` | `models` · `chat/completions`（治理入口） |
| Runtime Agent | `/runtime-agent` · `/agent` | 见 §7 |
| 系统设置 | `/system-settings` | 镜像设置、集群资源总览 |
| 管理后台 | `/admin/*` | 实例总览、runtime-pods、models、ai-audit、costs、risk-rules、skills、security 扫描 |
| 出向代理 | `/egress-proxy`（handler） | Isolated 档的平台 egress proxy（HTTP + HTTPS CONNECT） |
| WebSocket | `/ws` | 实时推送（topic：`user` / `runtime_admin`） |

---

## 9. 数据模型（MySQL，038 迁移）

按域分组的主要实体（`backend/internal/models/`）：

- **实例域**：`Instance`（含 `runtime_type` / `instance_mode` / `status` / `pod_*` / `workspace_path`）· `InstanceRuntimeBinding` · `InstanceRuntimeStatus` · `InstanceDesiredState` · `InstanceCommand` · `InstanceConfigRevision` · `InstanceExternalAccess` · `InstanceUsage` · `InstanceAgent`
- **运行时池**：`RuntimePod` · `RuntimeRollout` · `PersistentVolume`
- **AI 治理**：`LLMModel` · `LLMProtocol` · `ModelInvocation` · `CostRecord` · `AuditEvent` · `RiskRule` · `RiskHit` · `SecurityScan`
- **资源**：`Skill` · `OpenClawConfig` · `SystemImageSetting`
- **团队/协同**：`Team` · `TeamMember`（`instance_id` + `role`）· `TeamTask` · `TeamEvent` · `TeamWorkItem` · `TeamWorkflowPhase` · `TeamEventOutbox`（事件 outbox）· `ChatSession` · `ChatMessage`
- **账户**：`User` · `UserQuota`
- **备份**：`Backup` · `BackupSchedule`

---

## 10. 部署拓扑

单租户清单 `deployments/k8s/clawmanager-tenant.yaml`（`{TENANT_SUFFIX}` 支持多租户后缀），一个 tenant namespace 内包含：

| 组件 | 形态 | 作用 |
|---|---|---|
| **clawmanager-app** | Deployment + ServiceAccount + ClusterRoleBinding(cluster-admin) | 后端 + 前端（nginx 托管），需集群权限创建运行时资源 |
| **mysql** | Deployment + PV/PVC | 主数据库 |
| **minio** | Deployment + PV/PVC | S3 兼容对象存储 |
| **clawmanager-team-redis** | Deployment + Service | 团队事件/协同 |
| **workspace-store** | Deployment + PV/PVC | 每实例 workspace 持久卷载体 |
| **secrets / mysql-init** | Secret / ConfigMap | 凭证与初始化 |

- **应用镜像**：根 `Dockerfile` 构建 `clawreef-server` + 前端产物，跑在 `nginx:1.27-alpine` 上（`deployments/container/start.sh` 同时起 server 与 nginx）。
- **运行时镜像**（外部）：`ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite`、`.../hermes-lite`，由 `system_image_settings` 与 `clawmanager-apply.sh` 的 `{OPENCLAW_RUNTIME_IMAGE}` / `{HERMES_RUNTIME_IMAGE}` 变量注入。
- **形态变体**：`deployments/k8s/single-node`、`deployments/k8s/cluster`、`deployments/k3s`；arm64 部署见 `docs/arm64-deployment.md`。
- **Isolated 前置**：集群需装 `kubernetes-sigs/agent-sandbox` CRD，否则该档报告不可用。

---

## 11. 关键配置面（环境变量）

| 域 | 变量（节选） |
|---|---|
| 服务/DB | `SERVER_ADDRESS` · `SERVER_MODE` · `DB_{HOST,PORT,USER,PASSWORD,NAME}` · `JWT_SECRET` · `KUBECONFIG` |
| Leader 选举 | `CLAWMANAGER_LEADER_ELECTION` · `..._LEASE_{NAME,DURATION}` · `..._{RENEW_DEADLINE,RETRY_PERIOD}` |
| AI Gateway（注入实例） | `CLAWMANAGER_LLM_{BASE_URL,API_KEY,MODEL,PROVIDER}` · `CLAWMANAGER_LLM_GATEWAY_{SERVICE,PORT,BASE_URL}` |
| Egress（Isolated） | `CLAWMANAGER_EGRESS_PROXY_{URL,SERVICE,PORT,SERVICE_NAME,SERVICE_PORT}` · `CLAWMANAGER_NO_PROXY` |
| Agent/Runtime | `RUNTIME_AGENT_{CONTROL,REPORT}_TOKEN` · `CLAWMANAGER_AGENT_*` · `CLAWMANAGER_INSTANCE_TOKEN` · `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` |
| 资源上限 | `CLAWMANAGER_LITE_MAX_CPU_CORES` · `CLAWMANAGER_ISOLATED_MAX_CPU_CORES` |
| 注入内容 | `CLAWMANAGER_HERMES_*_JSON`（agents/channels/skills/session_templates/scheduled_tasks/…）· `CLAWMANAGER_OPENCLAW_*_JSON` |
| 对象存储 | `OBJECT_STORAGE_{ENDPOINT,BUCKET,ACCESS_KEY,SECRET_KEY,REGION,USE_SSL,...}` |
| 审计/技能扫描 | `CLAWMANAGER_AUDIT_LOG_ENABLED` · `SKILL_SCANNER_{ENABLED,BASE_URL,API_KEY,TIMEOUT_SECONDS}` |
| 桌面（Pro） | `CLAWMANAGER_DESKTOP_ENABLED` · `CLAWMANAGER_DESKTOP_STREAM_PROFILE` |

---

## 12. 前端架构

`frontend/`，React 19 + Vite 8 SPA，构建产物由后端 nginx 托管，`/api/v1` 反代到后端。

```
src/
├─ pages/        路由页面（按域分子目录）
│   ├─ admin/         管理后台（Dashboard/用户/实例/模型/AI网关/AI审计/成本/风控/RuntimePod/系统设置/技能/security）
│   ├─ auth/          登录 / 注册
│   ├─ dashboard/     用户仪表盘
│   ├─ instances/     实例（列表/创建/详情/Portal）
│   ├─ teams/         团队（列表/创建/详情）
│   ├─ openclaw/      OpenClaw 配置中心
│   └─ settings/      用户设置
├─ services/     后端 API 封装（基于 src/services/api.ts 的 axios 实例，baseURL=/api/v1）
├─ stores/       Zustand 全局态（authStore）
├─ contexts/     Auth / I18n Context
├─ hooks/        useWebSocket（topic: user / runtime_admin）· useInstanceDesktopAccess
├─ lib/          i18n 翻译表（en/zh/ja/ko/de）+ 各类模板
└─ router/       react-router 路由定义
```

- **状态分工**：客户端态 Zustand，服务端态 TanStack Query。
- **实时**：`useWebSocket` 订阅 `/ws`，用于实例详情、RuntimePod 页、shell 终端。
- **终端/桌面**：`@xterm/xterm` 接 shell 流；Pro 桌面走 iframe，`VITE_BACKEND_ORIGIN` 可覆盖实例访问源。

---

## 13. 质量与测试

- **后端**：Go 单测（`go test ./...`），k8s 层用 `fake.NewSimpleClientset` / fake dynamic client 在 backend 接缝断言；race 检查用于并发路径（如 AuditLogger）。
- **e2e**：`e2e/` Playwright，分组 `smoke`（login/navigation）· `api`（auth/runtime-agent）· `admin`（access-control/runtime-pods）· `instances`（lite-pro-modes / isolated-gateway-access）· `teams`（team-lite-mode），按 `@p0/@p1/@p2` 分级运行；Page Object 于 `e2e/pages/`。Isolated 全链已在真集群跑通（创建 → 网关经访问 URL 可达 → stop/start 工作区存活）。
- **前端**：无单测，`tsc -b && vite build` + `eslint` 为质量门。

---

## 14. 架构决策记录（ADR）

| ADR | 决策 |
|---|---|
| [0001](./adr/0001-three-tier-runtime-by-trust-and-form.md) | 按信任模型与形态分三档运行时（lite/isolated/pro），不跨档迁移；Isolated 强依赖 agent-sandbox，无原生 Pod 回退 |
| [0002](./adr/0002-ai-gateway-single-layer.md) | AI Gateway 单层收口所有模型治理；NodeGroup 等留在企业层，backend 只做 placement 透传 |
| [0003](./adr/0003-hijack-safe-runtime-mandatory-http-proxy.md) | Isolated 档强制 HTTP_PROXY 出向 + 镜像 fail-closed；NetworkPolicy 因 CNI 可移植性被否为主机制，仅作 v1.x 纵深防御 |

相关：Isolated 模式 RFC [`docs/rfc/isolated-instance-mode.md`](./rfc/isolated-instance-mode.md)（已提上游 `Yuan-lab-LLM/ClawManager#166`）；agent-sandbox 实测 gap list [`docs/spike/agent-sandbox/substrate-gap-list.md`](./spike/agent-sandbox/substrate-gap-list.md)。

---

## 15. 已知边界与跟进项

- **RUNTIME_AGENT_REPORT_TOKEN 是集群级共享密钥**，在 Isolated 劫持威胁模型下属 scope-bleed——列为 v1.x 跟进（非 v1 阻塞）。
- **egress 策略**（FQDN 白名单、认证代理、内容过滤、被阻断流量 UX）与 `egress.*` 审计事件延后，前置是"硬化 trust-all 代理"。
- **加固代理镜像**：Isolated 的 egress 保证目前落在 env 注入层，ADR-0003 要求的镜像级 fail-closed 由外部 `agentsruntime` 镜像管线跟进（跟踪 issue #22）。
- **部署期默认 placement**：公开 API 拒绝 placement 后，运维需要受控的默认 nodeSelector 配置面（跟踪 issue #26）。
- **两条审计管线未统一**：AI Gateway `gateway.request.*`（DB）与平台 AuditLogger（stdout）v1 分立。
- **Pro → agent-sandbox 迁移、warm pool、跨档迁移**：均刻意延后。
```
