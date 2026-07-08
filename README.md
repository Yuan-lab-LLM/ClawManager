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

ClawManager brings AI agent instance operations to Kubernetes and layers three higher-level control planes on top of that runtime foundation. Teams use it to govern AI access, orchestrate runtime behavior through agents, and manage reusable channels and skills with scanning and bundle-based delivery.

It is designed for:

- platform teams running AI agent instances for multiple users
- operators who need runtime visibility, command dispatch, and desired-state control
- builders who want governed AI access and reusable resource injection instead of manual per-instance setup

<a id="team-workspaces"></a>
## Team Workspaces

Team workspaces extend ClawManager from single-instance operations into coordinated multi-agent runtime management. A user can create a Team, assign one leader and multiple members, and let ClawManager provision the member runtimes, inject collaboration configuration, and keep task and event state visible from the control plane.

The current MVP focuses on OpenClaw member orchestration and the Redis Team Bus loop:

- one-click Team creation with a validated leader/member roster
- member runtime Pods created with Team role, member id, control-plane URL, and shared mount configuration
- Redis-backed inbox, events, presence, and DLQ keys injected through controlled environment and Secret references
- shared PVC mounted at `/team` for context, artifacts, snapshots, and task results
- Team detail view with leader desktop access, team chat, member roster, dispatch panel, task progress, and event/result history
- DB-backed Team, member, task, and event records so Redis remains the message bus, not the source of truth

<a id="runtime-integrations"></a>
## Runtime Integrations

ClawManager currently supports the following managed runtimes:

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: the default OpenClaw-style workspace runtime used by ClawManager-managed desktop instances
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: a Webtop-based runtime integration with a persistent `.hermes` workspace and embedded Hermes agent

Runtime previews:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![openclaw](./docs/images/openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![hermes](./docs/images/hermes.png)

Runtime authors can follow the [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md), the [Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md), and the [Skill Content MD5 Spec](./docs/skill-content-md5-spec.md) to build compatible agents.

## Get Started

ClawManager now separates the Kubernetes distribution from the storage profile. Choose `k3s` or `k8s` first, then choose the storage profile that matches the cluster shape:

- k3s single-node HostPath: [deployments/k3s/single-node/clawmanager.yaml](./deployments/k3s/single-node/clawmanager.yaml)
- k3s cluster CSI/RWX example: [deployments/k3s/cluster/clawmanager.yaml](./deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes single-node HostPath: [deployments/k8s/single-node/clawmanager.yaml](./deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes cluster CSI/RWX example: [deployments/k8s/cluster/clawmanager.yaml](./deployments/k8s/cluster/clawmanager.yaml)
- Operations-oriented quick start and first login flow: [User Guide](./docs/use_guide_en.md)
- Deployment notes and architecture-level context: [Deployment Guide](./docs/deployment.md)

The cluster profile is validated with Longhorn (`longhorn` for RWO data and `longhorn-rwx` for RWX workspaces), but these StorageClass names are examples. You can replace them with any CSI classes that provide the same access modes.

## Three Control Planes

### AI Gateway

AI Gateway is the governance plane for model access inside ClawManager. It gives managed agent runtimes a unified OpenAI-compatible entry point while adding policy and audit controls on top of upstream providers.

- Unified gateway entry for model traffic
- Secure model routing and policy-aware model selection
- End-to-end audit and trace records
- Built-in cost accounting and usage analysis
- Risk control rules that can block or reroute requests

See the [AI Gateway Guide](./docs/aigateway.md).

### Agent Control Plane

Agent Control Plane is the runtime orchestration layer for managed AI agent instances. It turns each instance into a managed runtime that can register, report status, receive commands, and stay aligned with platform-side desired state.

- Agent registration with secure bootstrap and session lifecycle
- Heartbeat-driven runtime status and health reporting
- Desired-state synchronization between the control plane and the instance
- Runtime command dispatch for start, stop, config apply, health checks, and skill operations
- Instance-level visibility into agent status, channels, skills, and command history

See the [Agent Control Plane Guide](./docs/agent-control-plane.md).

### Resource Management

Resource Management is the reusable asset layer for AI agent workspaces. It helps teams prepare channels and skills once, organize them into bundles, inject them into instances, and keep security review in the loop.

- Channel management for workspace connectivity and integration templates
- Skill management for reusable packaged capabilities
- Skill Scanner workflows for risk review and scan operations
- Bundle-based resource composition for repeatable workspace setup
- Injection snapshots and runtime-level visibility into what was applied

See the [Resource Management Guide](./docs/resource-management.md) and the [Security / Skill Scanner Guide](./docs/security-skill-scanner.md).

## Product Gallery

The product is designed to feel coherent across administration, workspace access, and AI governance. Instead of treating these as separate tools, ClawManager brings them into one control surface.

### Lite Mode Deployment

Lite mode provisions instances through a shared gateway runtime pool. Each workspace runs as an isolated gateway process inside managed runtime Pods, which keeps startup fast and lowers dedicated CPU, memory, storage, and GPU allocation overhead while preserving workspace access, Share Link / Password access, channel and skill injection, and admin visibility.

![](./docs/main/liteopenclaw.png)

### Pro Mode Deployment

Pro mode provisions a dedicated desktop runtime for each instance, backed by its own Kubernetes Deployment, Service, and PVC. Use it when users need stronger isolation, full desktop resources, runtime events, instance skill management, and the complete desktop management experience.

![](./docs/main/proopenclaw.png)

### Team Workspace

The Team workspace page brings the leader desktop, Team chat, member table, and dispatch workflow into one operational view, so users can follow collaboration progress without leaving ClawManager.

<p align="center">
  <img src="./docs/main/team-workspace.png" alt="ClawManager Team workspace" width="100%" />
</p>

### Admin Console

The admin console brings together users, quotas, runtime operations, security controls, and platform-level policies in one place. It is the operational center for teams running AI agent infrastructure at scale.

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager admin console" width="100%" />
</p>

### Portal Access

The portal experience gives users a clean entry point into their workspaces, with browser-based access and runtime visibility that stays connected to the control plane instead of exposing infrastructure details directly.

<p align="center">
  <img src="./docs/main/portal.png" alt="ClawManager portal access" width="100%" />
</p>

### AI Gateway

AI Gateway extends the workspace experience with governed model access, audit trails, cost visibility, and risk-aware routing, making AI usage manageable as part of the platform rather than an isolated integration.

<p align="center">
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## How It Works

1. Admins define governance policies and reusable resources.
2. Users create or enter managed AI agent workspaces on Kubernetes.
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

See the [Developer Guide](./docs/developer-guide.md).

## Documentation

- [User Guide](./docs/use_guide_en.md)
- [Deployment Guide](./docs/deployment.md)
- [Admin and User Guide](./docs/admin-user-guide.md)
- [Agent Control Plane Guide](./docs/agent-control-plane.md)
- [AI Gateway Guide](./docs/aigateway.md)
- [Security / Skill Scanner Guide](./docs/security-skill-scanner.md)
- [Resource Management Guide](./docs/resource-management.md)
- [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md)
- [Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md)
- [Skill Content MD5 Spec](./docs/skill-content-md5-spec.md)
- [Developer Guide](./docs/developer-guide.md)

## License

This project is licensed under the MIT License.

## Open Source

Issues and pull requests are welcome.

## Star History

<a href="https://www.star-history.com/?repos=Yuan-lab-LLM%2FClawManager&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
 </picture>
</a>
