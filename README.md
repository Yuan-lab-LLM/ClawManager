# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  A Kubernetes-native control plane for AI agent instance management, with governed AI access, runtime orchestration, and reusable resources across multiple agent runtimes.
</p>

<p align="center">
  <strong>Languages:</strong>
  English |
  <a href="./README.zh-CN.md">Chinese</a> |
  <a href="./README.ja.md">Japanese</a> |
  <a href="./README.ko.md">Korean</a> |
  <a href="./README.de.md">Deutsch</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Control%20Plane-e25544?style=for-the-badge" alt="ClawManager Control Plane" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
  <a href="https://discord.gg/9RwgbGJD5R">
    <img src="https://img.shields.io/badge/Discord-Join%20Us-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="Join ClawManager on Discord" />
  </a>
</p>

<p align="center">
  <a href="#product-tour">Explore the Product</a> |
  <a href="#team-workspaces">Team Workspaces</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime Integrations</a> |
  <a href="#resource-management">Resource Management</a> |
  <a href="#security-protection-platform">Security Protection</a> |
  <a href="#get-started">Get Started</a>
</p>

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
    <img src="https://img.shields.io/github/stars/Yuan-lab-LLM/ClawManager?style=for-the-badge&logo=github&label=Star%20ClawManager" alt="Star ClawManager on GitHub" />
  </a>
</p>

<h2 align="center">See ClawManager in 60 Seconds</h2>

<p align="center">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-launch-60s-hd.gif" alt="ClawManager product launch demo" width="100%" />
</p>

<p align="center">
  A quick look at fast agent provisioning, skill management and scanning, and AI Gateway governance.
</p>

## What's New

Recent highlights from the latest product and documentation updates.

- [2026-08-19] Added managed OpenCode workspaces, refreshed the instance desktop experience, and expanded Skill Hub delivery to OpenClaw, Hermes, and OpenCode runtimes. See the [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_en.md).
- [2026-08-18] Expanded Team collaboration with eight read-only built-in templates, natural-language custom Team templates, optional Hermes Lite Workers, live Execution Kanban, shared artifacts, and member-session visibility.
- [2026-08-17] Added model-managed Thinking, AI Gateway Session Usage, editable scheduled tasks, and improved Lite instance lifecycle and batch operations.
- [2026-08-16] Added DeepSeek Harness Lite and Pro support, including shared runtime-pool isolation, dedicated Webtop desktops, AI Gateway model injection, skills/workspace integration, and dedicated Lite browser origins.
- [2026-07-07] Added the Security Protection Platform (secplane) frontend — a comprehensive security console covering runtime defense (input/state/decision/output surface, asset tamper-proofing, human approval), host hardening & container isolation, outbound trusted-endpoint governance, policy governance, kill-switch/circuit-breaker, full-chain audit, SecureClaw data-and-component trust auditing, collaboration governance, and input detection. All 4 defense layers are accessible from a unified admin UI with full i18n for 5 languages.
- [2026-06-14] Added Lite / Pro runtime modes and rollout support, so Lite instances can run through shared gateway runtime pools while Pro instances keep dedicated desktop deployments for stronger isolation.
- [2026-05-18] Added the Team workspace MVP introduction and preview, covering one-click Team creation, OpenClaw member orchestration, Redis Team Bus injection, shared storage, member status, task dispatch, and event/result views.
- [2026-04-29] Added Hermes runtime integration support, including Webtop-based instance provisioning, Agent Control Plane registration, AI Gateway injection, channel and skill bootstrap, and `.hermes` import/export workflows. See the [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md).
- [2026-04-08] Added skill management and skill scanning workflows to the platform, via [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52).
- [2026-03-26] AI Gateway documentation was refreshed with stronger coverage for model governance, audit and trace, cost accounting, and risk control. See the [AI Gateway Guide](./docs/aigateway.md).
- [2026-03-20] ClawManager evolved into a broader control plane for AI agent workspaces, with stronger runtime control, reusable resources, and security scanning workflows.

> If ClawManager is useful to your team, please star the project to help more users and contributors discover it.

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>


## Community

Join the ClawManager open source community on WeChat or Discord for product updates, usage discussion, and contributor collaboration.

<table align="center">
  <tr>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_group_chat.jpg" alt="ClawManager WeChat group QR code" height="300" />
      <br /><br />
      <strong>WeChat</strong>
      <br />
      Scan to join the WeChat community group
    </td>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_discord.jpg" alt="ClawManager Discord invite QR code" height="300" />
      <br /><br />
      <strong>Discord</strong>
      <br />
      <a href="https://discord.gg/9RwgbGJD5R">Scan to join our Discord server</a>
    </td>
  </tr>
</table>

## Product Tour

ClawManager brings AI agent operations to Kubernetes in one product: managed runtimes, collaborative Teams, governed model access, reusable resources, Skill Hub, and platform security. Users work through browser-based desktops while administrators retain visibility and policy control without exposing Kubernetes details.

It is designed for:

- platform teams running AI agent instances for multiple users
- operators who need runtime visibility, command dispatch, and desired-state control
- builders who want governed AI access and reusable resource injection instead of manual per-instance setup

<a id="runtime-integrations"></a>
## Runtime Integrations

ClawManager currently supports the following managed runtimes:

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: Lite and Pro workspaces with native conversations, tools, scheduled tasks, and Team support
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: Lite and Pro workspaces with a persistent `.hermes` home, native sessions, and optional Team Worker support
- <img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> `OpenCode`: managed coding workspaces with AI Gateway model access, workspace files, and terminal/desktop access. See the [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_en.md).
- <img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> `DeepSeek Harness`: Lite pooled and Pro desktop workspaces with AI Gateway model injection, skills, workspace files, and isolated browser access

Runtime previews:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![OpenClaw managed workspace](./docs/main/runtime-openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![Hermes managed workspace](./docs/main/runtime-hermes.png)

**<img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> OpenCode**

![OpenCode managed workspace](./docs/main/runtime-opencode.png)

**<img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> DeepSeek Harness**

![DeepSeek Harness managed workspace](./docs/main/runtime-deepseek-harness.png)

## Get Started

ClawManager now separates the Kubernetes distribution from the storage profile. Choose `k3s` or `k8s` first, then choose the storage profile that matches the cluster shape:

- k3s single-node HostPath: [deployments/k3s/single-node/clawmanager.yaml](./deployments/k3s/single-node/clawmanager.yaml)
- k3s cluster CSI/RWX example: [deployments/k3s/cluster/clawmanager.yaml](./deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes single-node HostPath: [deployments/k8s/single-node/clawmanager.yaml](./deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes cluster CSI/RWX example: [deployments/k8s/cluster/clawmanager.yaml](./deployments/k8s/cluster/clawmanager.yaml)
- Operations-oriented quick start and first login flow: [User Guide](./docs/use_guide_en.md)
- Deployment notes and architecture-level context: [Deployment Guide](./docs/deployment.md)

The cluster profile is validated with Longhorn (`longhorn` for RWO data and `longhorn-rwx` for RWX workspaces), but these StorageClass names are examples. You can replace them with any CSI classes that provide the same access modes.

## Core Platform Capabilities

### Runtime and Instance Management

Create OpenClaw, Hermes, OpenCode, or DeepSeek Harness workspaces in Lite or Pro mode, choose an enabled system image, apply a resource preset or custom CPU/memory/storage values, and manage lifecycle, desktop access, files, shell access, environment variables, archives, Share Links, and Lite batch operations from one place.

### AI Gateway

AI Gateway is the governed model entry point for managed runtimes. Its five user-facing areas cover Models, AI Audit, Costs, Session Usage, and Risk Rules.

- OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages compatibility
- Persistent model configuration, provider health, pricing, and managed Thinking where supported
- Security-model routing for sensitive policies without requiring every normal model to be marked as secure
- End-to-end audit and trace records
- Built-in cost accounting plus per-instance and per-session token usage
- Risk control rules that can block or reroute requests

See the [AI Gateway Guide](./docs/aigateway.md).

### Agent Control Plane

Agent Control Plane is the runtime orchestration layer for managed AI agent instances. It turns each instance into a managed runtime that can register, report status, receive commands, and stay aligned with platform-side desired state.

- Agent registration with secure bootstrap and session lifecycle
- Heartbeat-driven runtime status and health reporting
- Desired-state synchronization between the control plane and the instance
- Runtime command dispatch for start, stop, config apply, health checks, and skill operations
- Instance-level visibility into agent status, channels, skills, and command history

Lifecycle, status, restart, runtime health, and administrator operations are covered in the [User Manual](./docs/use_guide_en.md#operate-an-instance).

### Resource Management

Resource Management is the user-side OpenClaw configuration center for reusable startup content. It is organized into Resources, Resource Packs, and Injection Records and is separate from the administrator Security Protection console.

- Channel configuration with supported templates, form/JSON editing, cloning, and lifecycle controls
- Skill ZIP import, conflict handling, download, and deletion; Skill Hub adds catalog, ownership, versions, publishing, and installation
- Scheduled-task editing with simple and advanced modes; Agent resources are visible but not yet configurable here
- Resource Pack creation, editing, cloning, and reuse during instance creation
- Read-only injection snapshots showing mode, resources, environment variables, status, and creation time

See the [Resource Management Guide](./docs/resource-management.md) and [Skill Hub Guide](./docs/skill-hub-guide_en.md).

<a id="team-workspaces"></a>
### Team Collaboration

Team collaboration uses a Leader-mediated workflow. Create a Team from one of eight immutable built-in templates or a user-owned custom template, then describe the goal in Team chat. The OpenClaw Lite Leader plans and decomposes the request, dispatches work, verifies member deliveries, and publishes the unified result.

- each Team has one OpenClaw Lite Leader; each Worker can use OpenClaw Lite or Hermes Lite when available
- custom Teams can be generated from natural-language intent, refined by role, regenerated, and reused
- Team chat records plans, assignments, progress, reviews, deliveries, and the final synthesis
- Execution Kanban follows the current query, task breakdown, and member delivery state
- shared files and artifacts retain collaboration output; Hermes Lite Workers expose their native Team sessions from the instance view

See the [Team Workspace Quick Guide](./docs/team-workspaces-guide_en.md) for creation, collaboration stages, and result viewing.

### Security Protection Platform

Security Protection is a separate administrator workspace. Its live overview combines four alert metrics, recent cross-product events, Pod Live Aegis configuration, report export, and an emergency circuit breaker. The overview currently presents the KSecure model as seven risk surfaces, fifteen defense scenarios, and four defense layers. Drill-down areas cover runtime defense, host/container isolation, component trust, outbound and identity governance, policy, collaboration, quotas, approvals, Skill Scanner, and full-chain audit.

See the [Security Platform Guide](./docs/security-platform.md).

## Product Gallery

The product is designed to feel coherent across administration, workspace access, and AI governance. Instead of treating these as separate tools, ClawManager brings them into one control surface.

### Lite Mode Deployment

Lite mode provisions OpenClaw, Hermes, OpenCode, and DeepSeek Harness instances through shared gateway runtime pools. Each workspace runs as an isolated gateway process inside managed runtime Pods, which keeps startup fast and lowers dedicated CPU, memory, storage, and GPU allocation overhead while preserving workspace access, Share Link / Password access, supported channel and skill injection, and admin visibility.

![](./docs/main/liteopenclaw.png)

### Pro Mode Deployment

Pro mode provisions a dedicated desktop runtime for each instance, backed by its own Kubernetes Deployment, Service, and PVC. Use it when users need stronger isolation, full desktop resources, runtime events, instance skill management, and the complete desktop management experience.

![](./docs/main/proopenclaw.png)

### Team Collaboration

The Team workspace shows the real execution flow in one view: user requests and member messages on the left, and the current query, task decomposition, delivery state, and artifact details on the Execution Kanban. The Leader coordinates members and publishes the final result without hiding the intermediate work.

<p align="center">
  <img src="./docs/main/team-collaboration.png" alt="ClawManager Team collaboration workspace with chat and Execution Kanban" width="100%" />
</p>

### Resource Management

Users manage reusable OpenClaw channels, skills, scheduled tasks, resource packs, and injection records from one configuration center. Security Protection remains a separate administrator feature.

<p align="center">
  <img src="./docs/main/resource-management-current.png" alt="ClawManager OpenClaw Resource Management" width="100%" />
</p>

### Security Protection

Administrators use the dedicated Security Protection workspace to review live security metrics and events, navigate the KSecure layered defense model, configure Pod Aegis, export evidence, and control emergency circuit breaking.

<p align="center">
  <img src="./docs/main/security-protection-current.png" alt="ClawManager Security Protection overview" width="100%" />
</p>

### Admin Console

The admin console brings together users, quotas, runtime operations, security controls, and platform-level policies in one place. It is the operational center for teams running AI agent infrastructure at scale.

<p align="center">
  <img src="./docs/main/admin-current.png" alt="ClawManager admin console and cluster capacity overview" width="100%" />
</p>

### Portal Access

The portal experience gives users a clean entry point into their workspaces, with browser-based access and runtime visibility that stays connected to the control plane instead of exposing infrastructure details directly.

<p align="center">
  <img src="./docs/main/portal-current.png" alt="ClawManager desktop portal with instance list, runtime desktop, and workspace files" width="100%" />
</p>

### AI Gateway

AI Gateway extends the workspace experience with governed model access, audit trails, cost visibility, and risk-aware routing, making AI usage manageable as part of the platform rather than an isolated integration.

<p align="center">
  <img src="./docs/main/ai-gateway-current.png" alt="ClawManager AI Gateway modules" width="100%" />
</p>

## How It Works

1. Admins define governance policies and reusable resources.
2. Users create or enter OpenClaw, Hermes, OpenCode, or DeepSeek Harness workspaces in Lite or Pro mode.
3. Team workspaces can provision multiple member runtimes with Redis Team Bus and shared storage configuration.
4. Agents connect back to the control plane and report runtime state.
5. Channels, skills, and bundles are compiled and applied to instances.
6. AI traffic flows through AI Gateway with audit, risk, and cost controls.

## Developer Snapshot

ClawManager is built as a Kubernetes-native platform with a React frontend, a Go backend, MySQL for state, and supporting services such as skill-scanner and object storage integrations. The repository is organized around product subsystems rather than a single monolith page, so the best developer experience is to start from the relevant guide and then move into the code.

- Frontend app and admin/user surfaces live under `frontend/`
- Backend services, handlers, repositories, and migrations live under `backend/`
- Deployment assets live under `deployments/`
- Supporting product docs live under `docs/`

Runtime and protocol implementation references remain under `docs/` for contributors, while the user-facing documentation below is organized by product workflow.

## Documentation

- [User Guide](./docs/use_guide_en.md)
- [Team Workspace Quick Guide](./docs/team-workspaces-guide_en.md)
- [Deployment Guide](./docs/deployment.md)
- [AI Gateway Guide](./docs/aigateway.md)
- [Security Platform Guide](./docs/security-platform.md)
- [Resource Management Guide](./docs/resource-management.md)
- [Skill Hub Guide](./docs/skill-hub-guide_en.md)
- [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_en.md)

## License

This project is licensed under the MIT License.

## Open Source

Issues and pull requests are welcome.

## Star History

<a href="https://github.com/Yuan-lab-LLM/ClawManager/actions/workflows/update-star-history.yml">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
   <img alt="Star History Chart" src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
 </picture>
</a>
