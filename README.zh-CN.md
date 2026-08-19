# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  一个面向 AI Agent 实例管理的 Kubernetes 原生控制平面，提供受治理的 AI 访问、运行时编排，以及适用于多种 Agent Runtime 的可复用资源管理能力。
</p>

<p align="center">
  <strong>语言:</strong>
  <a href="./README.md">English</a> |
  简体中文 |
  <a href="./README.ja.md">日本語</a> |
  <a href="./README.ko.md">한국어</a> |
  <a href="./README.de.md">Deutsch</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Control%20Plane-e25544?style=for-the-badge" alt="ClawManager Control Plane" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
  <a href="https://discord.gg/9RwgbGJD5R">
    <img src="https://img.shields.io/badge/Discord-Join%20Us-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="加入 ClawManager Discord 社区" />
  </a>
</p>

<p align="center">
  <a href="#product-tour">了解产品</a> |
  <a href="#team-workspaces">Team 协作</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime 接入</a> |
  <a href="#resource-management">资源管理</a> |
  <a href="#security-protection-platform">安全防护</a> |
  <a href="#get-started">快速开始</a>
</p>

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
    <img src="https://img.shields.io/github/stars/Yuan-lab-LLM/ClawManager?style=for-the-badge&logo=github&label=Star%20ClawManager" alt="Star ClawManager on GitHub" />
  </a>
</p>

<h2 align="center">60 秒认识 ClawManager</h2>

<p align="center">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-launch-60s-hd.gif" alt="ClawManager 产品演示" width="100%" />
</p>

<p align="center">
  快速了解 Agent 实例创建、Skill 管理与扫描，以及 AI Gateway 治理能力。
</p>

## 最新动态

这里展示最近的重要产品与文档更新。

- [2026-08-19] 新增受管 OpenCode 工作空间，更新实例桌面体验，并将 Skill Hub 能力交付扩展到 OpenClaw、Hermes 与 OpenCode Runtime。详见 [OpenCode 工作空间指南](./docs/opencode-lite-pro-agent-development.md)。
- [2026-08-18] 完善 Team 协作：8 个只读内置模板、自然语言生成的自定义 Team、可选 Hermes Lite Worker、实时 Execution Kanban、共享产物与成员会话查看。
- [2026-08-17] 新增模型级托管 Thinking、AI Gateway 会话用量、定时任务编辑，以及更完善的 Lite 实例生命周期和批量操作。
- [2026-08-16] 新增 DeepSeek Harness Lite / Pro：支持共享运行时池隔离、专属 Webtop 桌面、AI Gateway 模型注入、Skill 与工作空间集成，以及 Lite 专用浏览器域名。
- [2026-07-07] 新增安全防护平台（secplane）前端控制台——覆盖运行时防御（输入面/状态面/决策面/输出面、资产防篡改、人因审批）、主机加固与容器隔离、出站可信端点治理、策略治理、应急熔断、全链路审计、SecureClaw 数据与组件可信审计、协同接入治理及输入检测，4 层防护统一管理界面，5 语言 i18n 完整支持。
- [2026-06-14] 新增 Lite / Pro 运行时模式与滚动升级支持，Lite 实例可通过共享 gateway 运行时池运行，Pro 实例保留专属 desktop deployment 以获得更强隔离。
- [2026-05-18] 新增 Team 工作空间 MVP 介绍与界面预览，覆盖一键创建 Team、OpenClaw 成员编排、Redis Team Bus 配置注入、共享存储、成员状态、任务派发，以及事件和结果查看。
- [2026-04-29] 新增 Hermes Runtime 接入支持，覆盖基于 Webtop 的实例创建、Agent Control Plane 注册、AI Gateway 注入、channel 与 skill 引导注入，以及 `.hermes` 导入导出流程。使用方式见 [用户手册](./docs/use_guide_cn.md#create-a-workspace)。
- [2026-04-08] 平台新增了 Skill 管理与 Skill 扫描工作流，见 [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52)。
- [2026-03-26] AI Gateway 文档已更新，补充了模型治理、审计追踪、成本核算与风险控制能力，见 [AI Gateway 使用指南](./docs/aigateway_cn.md)。
- [2026-03-20] ClawManager 进一步演进为面向 AI Agent 工作空间的控制平面，强化了运行时控制、可复用资源与安全扫描工作流。

> 如果 ClawManager 对你的团队有帮助，欢迎为项目点一个 Star，帮助更多用户和开发者发现它。

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

## 社区交流

欢迎加入 ClawManager 开源社区，可通过微信群或 Discord 获取产品更新、交流使用经验，并与贡献者一起讨论共建。

<table align="center">
  <tr>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_group_chat.jpg" alt="ClawManager 微信群二维码" height="300" />
      <br /><br />
      <strong>微信群</strong>
      <br />
      扫描二维码加入微信群
    </td>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_discord.jpg" alt="ClawManager Discord 邀请二维码" height="300" />
      <br /><br />
      <strong>Discord</strong>
      <br />
      <a href="https://discord.gg/9RwgbGJD5R">扫描二维码加入 Discord 服务器</a>
    </td>
  </tr>
</table>

<a id="product-tour"></a>
## 产品介绍

ClawManager 把 AI Agent 的运行、协作和治理集中到一个 Kubernetes 原生产品中：受管 Runtime、Team 协作、模型访问、资源与 Skill Hub，以及平台安全。用户通过浏览器使用桌面工作空间，管理员无需向用户暴露 Kubernetes 细节，也能保持运行和策略可见性。

它适合以下场景：

- 面向多用户运行 AI Agent 实例的平台团队
- 需要运行时可观测性、命令下发与期望态控制的运维团队
- 希望以可复用资源而不是手工配置方式交付 Agent 工作空间的开发团队

<a id="runtime-integrations"></a>
## Runtime 接入

ClawManager 当前支持以下受管 Runtime：

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`：支持 Lite / Pro、原生会话、工具、定时任务和 Team 的工作空间
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`：支持 Lite / Pro、持久化 `.hermes` 目录、原生会话和 Team Worker 的工作空间
- <img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> `OpenCode`：接入 AI Gateway 的受管编码工作空间，提供桌面/终端与文件能力。详见 [OpenCode 工作空间指南](./docs/opencode-lite-pro-agent-development.md)
- <img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> `DeepSeek Harness`：支持 Lite 共享池与 Pro 专属桌面，通过 AI Gateway 注入受管模型，并集成 Skill、工作空间文件和隔离浏览器访问

Runtime 预览：

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![OpenClaw 受管工作空间](./docs/main/runtime-openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![Hermes 受管工作空间](./docs/main/runtime-hermes.png)

**<img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> OpenCode**

![OpenCode 受管工作空间](./docs/main/runtime-opencode.png)

**<img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> DeepSeek Harness**

![DeepSeek Harness 受管工作空间](./docs/main/runtime-deepseek-harness.png)

<a id="get-started"></a>
## 快速开始

ClawManager 现在将 Kubernetes 发行版与存储 profile 拆开。先选择 `k3s` 或 `k8s`，再选择匹配集群形态的存储 profile：

- k3s 单节点 HostPath: [deployments/k3s/single-node/clawmanager.yaml](./deployments/k3s/single-node/clawmanager.yaml)
- k3s 多节点 CSI/RWX 示例: [deployments/k3s/cluster/clawmanager.yaml](./deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes 单节点 HostPath: [deployments/k8s/single-node/clawmanager.yaml](./deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes 多节点 CSI/RWX 示例: [deployments/k8s/cluster/clawmanager.yaml](./deployments/k8s/cluster/clawmanager.yaml)
- 首次登录与操作流程: [用户指南](./docs/use_guide_cn.md)
- 部署说明与架构背景: [部署指南](./docs/deployment_cn.md)

多节点 cluster profile 使用 Longhorn 作为官方验证示例，其中 `longhorn` 用于 RWO 数据卷，`longhorn-rwx` 用于 RWX workspace。项目不绑定 Longhorn，用户可以替换为具备相同访问模式能力的 CSI StorageClass。

## 平台核心能力

### Runtime 与实例管理

创建 OpenClaw、Hermes、OpenCode 或 DeepSeek Harness 的 Lite / Pro 工作空间，选择已启用的系统镜像和资源规格，并在同一界面管理生命周期、桌面、文件、Shell、环境变量、归档、Share Link 与 Lite 批量操作。

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway 是受管 Runtime 访问模型的统一入口，用户界面分为模型、AI 审计、成本、会话用量和风控规则 5 个模块。

- 兼容 OpenAI Chat Completions、OpenAI Responses 与 Anthropic Messages
- 持久化模型、Provider 健康、价格以及受支持模型的 Thinking 配置
- 安全模型仅用于敏感策略路由，普通模型无需全部标记为安全模型
- 端到端审计与追踪记录
- 内建成本核算，以及按实例和会话查看 Token 用量
- 可阻断或改道路由的风险控制规则

参见 [AI Gateway 使用指南](./docs/aigateway_cn.md)。

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane 是受管 AI Agent 实例的运行时编排层。它让每一个实例都成为可注册、可汇报状态、可接收命令，并持续对齐平台期望态的受管运行时。

- 基于安全引导与会话生命周期的 Agent 注册
- 依靠心跳机制进行运行时状态与健康上报
- 控制平面与实例之间的期望态同步
- 支持启动、停止、配置应用、健康检查与 Skill 操作的命令下发
- 在实例维度查看 Agent 状态、channel、skill 与命令历史

生命周期、状态、重启、Runtime 健康与管理员操作见 [用户手册](./docs/use_guide_cn.md#operate-an-instance)。

<a id="resource-management"></a>
### 资源管理

资源管理是面向用户的 OpenClaw 启动配置中心，分为“资源”“资源包”“注入记录”三个页签，与管理端安全防护相互独立。

- 通道模板、表单/JSON 编辑、克隆及启用状态管理
- Skill ZIP 导入、冲突处理、下载和删除；Skill Hub 负责目录、归属、版本、发布与安装
- 定时任务的简易/高级编辑；智能体资源入口当前仅展示、暂不可配置
- 资源包的新建、编辑、克隆，以及在创建实例时复用
- 只读注入快照，记录模式、资源、环境变量、状态和创建时间

参见 [资源管理指南](./docs/resource-management_cn.md) 与 [Skill Hub 使用指南](./docs/skill-hub-guide.md)。

<a id="team-workspaces"></a>
### Team 协作

Team 采用 Leader 中介协作流程。从 8 个不可修改的内置模板或用户自己的自定义模板创建 Team，在群聊中描述目标后，由 OpenClaw Lite Leader 制定计划、拆解任务、派发工作、核验成员交付并发布统一结果。

- 每个 Team 只有一个 OpenClaw Lite Leader；每个 Worker 可在可用时选择 OpenClaw Lite 或 Hermes Lite
- 自定义 Team 可根据自然语言意图生成，按角色细化、整体重新生成并重复使用
- 团队群聊记录计划、派发、进度、验收、交付和最终汇总
- Execution Kanban 跟踪当前问题、任务拆分和成员交付状态
- 文件和共享产物保留协作结果；Hermes Lite Worker 可从实例界面查看原生 Team 会话

参见 [Team 协作快速指南](./docs/team-workspaces-guide.md)，了解创建、协作阶段和查看交付结果的流程。

<a id="security-protection-platform"></a>
### 安全防护平台

安全防护是管理端独立工作区，首页展示四项实时告警指标、跨产品事件、Pod 实时 Aegis 配置、报告导出和应急熔断。当前总览把 KSecure 模型标为 7 大风险面、15 个防护场景、4 个防护层级，并可进入 Runtime 防御、主机/容器隔离、组件可信、出站与身份治理、策略、协作、配额、审批、Skill Scanner 和全链路审计。

参见 [安全防护平台指南](./docs/security-platform_cn.md)。

## 产品界面

ClawManager 的设计目标，是让管理、访问与 AI 治理体验形成统一的产品界面，而不是分散在多个孤立工具中。

### Lite 模式部署

Lite 模式通过共享 gateway 运行时池创建 OpenClaw、Hermes、OpenCode 与 DeepSeek Harness 实例。每个工作空间作为受管 runtime Pod 中的独立 gateway 进程运行，启动更快，并减少专属 CPU、内存、存储和 GPU 配额开销，同时保留工作空间访问、Share Link / Password 访问、受支持的 channel 与 skill 注入，以及管理端可见性。

![](./docs/main/liteopenclaw.png)

### Pro 模式部署

Pro 模式为每个实例创建专属 desktop runtime，并使用独立 Kubernetes Deployment、Service 和 PVC。适用于需要更强隔离、完整桌面资源、runtime events、实例级 skill 管理和完整桌面管理体验的场景。

![](./docs/main/proopenclaw.png)

### Team 工作空间

Team 工作空间把真实协作过程集中在一个页面：左侧展示用户、Leader 和 Worker 的消息，右侧 Execution Kanban 展示当前问题、任务拆分、交付状态和产物详情。用户既能看到最终结果，也能跟踪中间过程。

<p align="center">
  <img src="./docs/main/team-collaboration.png" alt="ClawManager Team 群聊与 Execution Kanban" width="100%" />
</p>

### 资源管理

用户可以在同一配置中心管理 OpenClaw 通道、技能、定时任务、资源包和注入记录；安全防护仍是独立的管理端功能。

<p align="center">
  <img src="./docs/main/resource-management-current.png" alt="ClawManager OpenClaw 资源管理" width="100%" />
</p>

### 安全防护

管理员通过独立的安全防护工作区查看实时指标和事件、进入 KSecure 分层防御场景、配置 Pod Aegis、导出证据并管理应急熔断。

<p align="center">
  <img src="./docs/main/security-protection-current.png" alt="ClawManager 安全防护总览" width="100%" />
</p>

### 管理控制台

管理控制台将用户、配额、运行时操作、安全控制与平台级策略集中到一起，是团队管理 AI Agent 基础设施的核心工作台。

<p align="center">
  <img src="./docs/main/admin-current.png" alt="ClawManager 管理控制台与集群容量概览" width="100%" />
</p>

### Portal 访问

Portal 为用户提供统一的工作空间入口。用户可以通过浏览器访问实例，并查看与控制平面保持一致的运行时状态，而不需要直接面对底层基础设施细节。

<p align="center">
  <img src="./docs/main/portal-current.png" alt="ClawManager 桌面 Portal、实例列表与工作区文件" width="100%" />
</p>

### AI Gateway

AI Gateway 将模型访问治理纳入工作空间体验本身，提供审计记录、成本可见性与风险路由能力，让 AI 使用成为平台能力的一部分，而不是零散接入。

<p align="center">
  <img src="./docs/main/ai-gateway-current.png" alt="ClawManager AI Gateway 功能入口" width="100%" />
</p>

## 工作方式

1. 管理员先定义治理策略与可复用资源。
2. 用户创建或进入 OpenClaw、Hermes、OpenCode 或 DeepSeek Harness 的 Lite / Pro 工作空间。
3. Team 工作空间可以一次编排多个成员 Runtime，并注入 Redis Team Bus 与共享存储配置。
4. Agent 回连控制平面并上报运行时状态。
5. Channel、skill 与 bundle 被编译并应用到实例中。
6. AI 流量通过 AI Gateway 进入上游服务，并附带审计、风险与成本控制。

## 开发者概览

ClawManager 是一个 Kubernetes 原生平台，包含 React 前端、Go 后端、MySQL 状态存储，以及 `skill-scanner` 与对象存储等支撑组件。代码库按产品子系统组织，因此更适合从对应能力的指南切入，再进入代码实现。

- 前端管理界面与用户界面位于 `frontend/`
- 后端服务、handler、repository 与 migration 位于 `backend/`
- 部署资产位于 `deployments/`
- 产品文档与素材位于 `docs/`

面向 Runtime 和协议实现者的技术规范仍保留在 `docs/`；下方公开文档按用户实际操作流程组织。

## 文档

- [用户指南](./docs/use_guide_cn.md)
- [Team 协作快速指南](./docs/team-workspaces-guide.md)
- [部署指南](./docs/deployment_cn.md)
- [AI Gateway 使用指南](./docs/aigateway_cn.md)
- [安全防护平台指南](./docs/security-platform_cn.md)
- [资源管理指南](./docs/resource-management_cn.md)
- [Skill Hub 使用指南](./docs/skill-hub-guide.md)
- [OpenCode 工作空间指南](./docs/opencode-lite-pro-agent-development.md)

## 许可证

本项目基于 MIT License 开源。

## 开源协作

欢迎提交 Issue 与 Pull Request。

## Star History

<a href="https://github.com/Yuan-lab-LLM/ClawManager/actions/workflows/update-star-history.yml">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
   <img alt="Star History Chart" src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
 </picture>
</a>
