[← Back to README](../README.md)

# ClawManager User Manual

This is the main operating manual for the current ClawManager interface. It covers ordinary users and administrators, and links to separate guides only when a topic needs a longer workflow or reference.

## Contents

- [1. Deploy and sign in](#deploy-and-sign-in)
- [2. Understand roles and navigation](#roles-and-navigation)
- [3. Configure model access](#configure-model-access)
- [4. Create a managed workspace](#create-a-workspace)
- [5. Operate an instance](#operate-an-instance)
- [6. Manage resources and skills](#resources-and-skills)
- [7. Use Team collaboration](#team-collaboration)
- [8. Administer users, instances, and runtimes](#administration)
- [9. Update Runtime images and Lite pools](#runtime-images-and-rollout)
- [10. Use AI Gateway and Session Usage](#ai-gateway)
- [11. Use Security Protection](#security-protection)
- [12. Clipboard, input, and desktop notes](#clipboard-and-desktop)
- [13. Troubleshooting and acceptance](#troubleshooting)
- [14. Focused guides](#focused-guides)

<a id="deploy-and-sign-in"></a>
## 1. Deploy and sign in

ClawManager provides four maintained deployment profiles: k3s or Kubernetes, each with a single-node HostPath option and a multi-node CSI/RWX option. Apply one complete profile only. Do not combine manifests or repair a multi-node installation with temporary HostPath volumes.

After the core workloads and storage claims are ready, open the configured web address and sign in with an account created by the platform administrator. See the [Deployment Guide](./deployment.md) for manifests, storage requirements, ARM64 checks, and production preparation.

<a id="roles-and-navigation"></a>
## 2. Understand roles and navigation

The user workspace contains:

- **Workbench**: an entry view for resources you can access.
- **My Instances**: OpenClaw, Hermes, OpenCode, and DeepSeek Harness workspaces owned by or shared with you.
- **Teams**: multi-agent collaboration, chat, Kanban, files, and member results.
- **Resource Management**: channels, uploaded skills, scheduled tasks, resource packs, and injection records.
- **Skill Hub**: the shared, versioned skill catalog for all supported runtimes.
- **Settings**: personal preferences and supported account options.

The administrator workspace adds global users, instances, Runtime pools, Security Protection, AI Gateway, and system settings. Permissions and quotas determine which actions are visible; a missing button is not always a UI fault.

<a id="configure-model-access"></a>
## 3. Configure model access

Before users create model-backed workspaces or custom Teams, an administrator must open **AI Gateway → Models**, add an upstream provider, enable at least one ordinary model, and test its health. A security model is optional and is required only when a Risk Rule routes sensitive requests to it.

Managed **Thinking** is a persistent model setting. It is available only for provider/model combinations ClawManager can control reliably. Turning it on can increase latency and reasoning-token usage; it does not expose private chain-of-thought text.

<a id="create-a-workspace"></a>
## 4. Create a managed workspace

Open **My Instances → Create**, then choose the Runtime and mode:

| Runtime | Typical use | Lite | Pro |
|---|---|---|---|
| OpenClaw | conversations, tools, scheduled tasks, Team Leader/Worker | shared Runtime pool | dedicated desktop workload |
| Hermes | native Hermes sessions and tools; optional Team Worker | shared Runtime pool | dedicated desktop workload |
| OpenCode | managed coding workspace with AI Gateway, files, terminal, and desktop | shared Runtime pool | dedicated desktop workload |
| DeepSeek Harness | managed agent workspace with AI Gateway, skills, workspace files, and a native browser UI | shared Runtime pool | dedicated Webtop workload |

The form displays only options supported by the selected Runtime and mode. Depending on that choice, configure the system image, resource preset or custom CPU/memory/storage, desktop stream profile, environment variables, archive import, resource pack, manual resources, or initial skills.

**Lite does not create a Kubernetes Pod per instance.** It starts an isolated workspace/process inside the corresponding shared Runtime Pod. Pro uses a dedicated workload and stronger resource isolation.

Skill Hub is not an OpenCode-only feature. OpenClaw, Hermes, OpenCode, and DeepSeek Harness all consume skills from the same catalog; only the installation path and reload method differ. If creation does not show skill preselection for a Runtime, install the skill after the instance is ready.

<a id="operate-an-instance"></a>
## 5. Operate an instance

The instance page combines lifecycle actions, the embedded Runtime UI or desktop, workspace files, and runtime-specific panels.

- **Start / Stop / Restart**: use ClawManager actions instead of editing generated Kubernetes resources. A restart with environment overrides applies supported environment changes to that instance.
- **Delete**: removes the managed instance and may remove its dedicated data according to the selected storage flow. Download required files or archives first.
- **Share Link**: configure access credentials, expiry, and allowed workspace access before distributing a link. Revoke links that are no longer required.
- **Workspace**: browse, upload, download, edit, or delete files when the current Runtime/storage path supports the action.
- **Desktop stream profile**: Low, Standard, and High trade bandwidth for visual quality. A saved profile normally takes effect after the requested restart/apply action.
- **Skill Management**: shows installed and discovered skills and lets you verify the effective version.
- **Session Usage**: summarizes reported token use and estimated cost for the instance. Missing runtime metadata remains missing rather than being guessed.
- **Runtime Overview / Events**: dedicated instances may expose workload health and event information for troubleshooting.

OpenClaw and Hermes preserve their own native conversation/session behavior. OpenCode and DeepSeek Harness expose their native workspaces rather than being given a separate ClawManager conversation model.

<a id="resources-and-skills"></a>
## 6. Manage resources and skills

**Resource Management** prepares reusable startup content:

- **Resources**: channels, uploaded skill packages, and scheduled tasks. The Agent type is currently reserved and is not edited as a normal resource.
- **Resource Packs**: combine compatible resources for reuse during instance creation.
- **Injection Records**: read-only evidence of the compiled content applied to an instance.

**Skill Hub** is the cross-Runtime skill management and delivery platform. It is related to Resource Management, but it owns the reusable catalog lifecycle: Browse, My Skills, versions, ownership, tags, scan status, publication, installation, and instance-side verification. Uploads require a ZIP containing `SKILL.md`. A failed scan remains visible so the owner can correct and upload a new version; scan completion does not by itself guarantee publication or installation approval.

OpenClaw, Hermes, OpenCode, and DeepSeek Harness are all supported. Runtime-specific storage paths, reload behavior, and image capabilities can still differ. See the [Resource Management Guide](./resource-management.md) and [Skill Hub Guide](./skill-hub-guide_en.md).

<a id="team-collaboration"></a>
## 7. Use Team collaboration

Open **Teams → Create** and choose one of eight immutable built-in templates or one of your custom templates. The Leader is OpenClaw Lite. Workers can use OpenClaw Lite or Hermes Lite when the required runtime image is enabled.

Custom templates can be generated from a natural-language intent with 2–6 members. You can rename the template, revise the intent and member count, regenerate the whole Team, adjust each role with natural language, delete it, and reuse it during Team creation. Leader adjustments extend the fixed orchestration role; they do not replace its responsibility to understand the request, delegate work, collect member results, and publish the final answer.

During execution, the Team page shows chat, the latest-query Execution Kanban, files, artifacts, member deliveries, and the final result. New user questions select the newest task group by default; historical groups remain available. See the [Team Collaboration Guide](./team-workspaces-guide_en.md).

<a id="administration"></a>
## 8. Administer users, instances, and runtimes

Administrators should understand these operational boundaries:

- **Users**: create accounts, assign roles and quotas, import supported CSV data, and remove accounts according to the platform retention policy.
- **Instances**: search all instances, inspect ownership/runtime/status, and perform global start, stop, restart, sync, or delete actions.
- **Runtime**: view shared Runtime Pods, capacity and health, and drain a pod before maintenance. Draining stops new assignment and lets the scheduler replace or relocate managed Lite work.
- **Settings**: configure default/enabled Runtime images and start controlled Lite rolling upgrades.
- **Security Protection** and **AI Gateway** are separate administrator workspaces; neither belongs to Resource Management.

<a id="runtime-images-and-rollout"></a>
## 9. Update Runtime images and Lite pools

Open **Admin Console → Settings** to manage Runtime images.

![Runtime image settings and Lite rolling upgrade](./main/runtime-settings-rollout.png)

The page deliberately separates **saving an image setting** from **updating a running Lite pool**:

1. In the Lite or Pro Runtime card, enter the desired image and click **Save**. This persists the selectable/default image for later provisioning; it does not replace the running shared Lite Pod.
2. For an existing Lite pool, use **Lite Runtime Rolling Upgrade** at the top of the page. Select OpenClaw Lite, Hermes Lite, OpenCode Lite, or DeepSeek Harness Lite, confirm the current and target gateway images, and set batch size and maximum unavailable.
3. Click **Start Rolling Upgrade**. ClawManager drains and replaces shared Runtime capacity in controlled batches until the target image is active.
4. Recheck Runtime health and open a test instance after the rollout.

Keep the rollout target equal to the image you saved. A larger batch is faster but reduces available shared capacity. Active Lite sessions can be interrupted while their Runtime process is drained, so use conservative values and a suitable maintenance window. Pro cards define images for future dedicated provisioning; saving a Pro image does not silently replace existing Pro instances.

<a id="ai-gateway"></a>
## 10. Use AI Gateway and Session Usage

AI Gateway has five areas:

- **Models**: provider, protocol, credentials, price, health, security role, and Thinking.
- **AI Audit**: request traces, routing, errors, latency, and policy evidence.
- **Costs**: token-based estimates and internal accounting views.
- **Session Usage**: usage by runtime, user, instance, and conversation.
- **Risk Rules**: ordered allow, block, or secure-route decisions.

Session Usage is an observability view, not a conversation editor or billing ledger. Filter by time, user, Runtime, instance, or session; compare input/output/cached/reasoning tokens when reported; then use AI Audit for request-level evidence. Provider totals can differ because runtimes and providers report token categories differently. See the [AI Gateway Guide](./aigateway.md).

<a id="security-protection"></a>
## 11. Use Security Protection

Security Protection is a separate administrator area. Its overview combines current alert metrics, recent cross-product events, Pod Live Aegis configuration, report export, emergency controls, and the KSecure defense model. Detailed pages cover runtime defense, host/container isolation, trust, identity and outbound governance, policy, collaboration, quotas/approvals, Skill Scanner, and full-chain audit.

Skill Scanner participates in both workflows: users see the scan lifecycle in Skill Hub, while administrators manage scanner health, policies, and security evidence in Security Protection. See the [Security Protection Guide](./security-platform.md).

<a id="clipboard-and-desktop"></a>
## 12. Clipboard, input, and desktop notes

Clipboard behavior depends on the Runtime image and its desktop configuration: bidirectional synchronization, host-to-desktop paste only, or disabled. Environment changes normally require a desktop or instance restart. Test plain text, then Unicode/CJK text. Clipboard transport and keyboard/IME input are separate paths, and browser permission can block clipboard access. Never test with passwords or API keys.

<a id="troubleshooting"></a>
## 13. Troubleshooting and acceptance

| Symptom | What to check |
|---|---|
| No Runtime or image option | Administrator must save and enable the corresponding image. |
| Saved Lite image is not running | Saving changes the setting; start a Lite rolling upgrade to update the shared pool. |
| No model available | Enable at least one ordinary model; it need not be a security model. |
| Lite instance has no dedicated Pod | Expected: Lite instances share a Runtime Pod. |
| PVC stays pending | Storage profile, StorageClass, access mode, node label, and capacity. |
| Share Link cannot access files | Link permissions, expiry, credential, and workspace-sharing option. |
| Skill scan failed | Read the retained error, correct the package, and upload a new version. |
| Installed skill is not visible | Refresh Skill Management, verify version/path, and restart or reload the Runtime when required. |
| Session Usage is empty | Time range, filters, and whether the Runtime reported usage. |
| Team shows historical work | Select the latest task/query group; new questions default to the newest group. |

Final acceptance should prove: core workloads and PVCs are healthy; an ordinary model works; each exposed Runtime can create and open a test instance; files and lifecycle actions work; a reviewed skill installs; AI Audit/Session Usage receive data; and, if Teams are used, chat, Kanban, files, member delivery, and final results all work.

<a id="focused-guides"></a>
## 14. Focused guides

- [Deployment Guide](./deployment.md)
- [Team Collaboration Guide](./team-workspaces-guide_en.md)
- [AI Gateway Guide](./aigateway.md)
- [Security Protection Guide](./security-platform.md)
- [Resource Management Guide](./resource-management.md)
- [Skill Hub Guide](./skill-hub-guide_en.md)
- [OpenCode Workspace Guide](./opencode-lite-pro-agent-development_en.md)
