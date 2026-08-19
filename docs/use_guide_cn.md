[← 返回 README](../README.zh-CN.md)

# ClawManager 用户手册

这是当前 ClawManager 产品界面的主操作手册，同时覆盖普通用户和管理员。只有流程较长、需要独立查阅的主题才跳转到专题文档。

## 目录

- [一、部署与登录](#deploy-and-sign-in)
- [二、角色与导航](#roles-and-navigation)
- [三、配置模型](#configure-model-access)
- [四、创建受管工作空间](#create-a-workspace)
- [五、使用和管理实例](#operate-an-instance)
- [六、资源管理与 Skill Hub](#resources-and-skills)
- [七、Team 协作](#team-collaboration)
- [八、管理员日常操作](#administration)
- [九、Runtime 镜像与 Lite 滚动更新](#runtime-images-and-rollout)
- [十、AI Gateway 与会话用量](#ai-gateway)
- [十一、安全防护](#security-protection)
- [十二、剪贴板、输入和桌面注意事项](#clipboard-and-desktop)
- [十三、排障与验收](#troubleshooting)
- [十四、专题指南](#focused-guides)

<a id="deploy-and-sign-in"></a>
## 一、部署与登录

ClawManager 提供四套维护中的部署形态：k3s 或 Kubernetes，各自包含单节点 HostPath 和多节点 CSI/RWX。一次只应用一套完整 profile，不要混用清单，也不要用临时 HostPath 修补多节点存储。

核心工作负载和存储声明就绪后，打开部署时配置的访问地址，使用平台管理员创建的账号登录。清单入口、存储要求、ARM64 检查和生产准备见 [部署指南](./deployment_cn.md)。

<a id="roles-and-navigation"></a>
## 二、角色与导航

用户工作区包含：

- **工作台**：查看当前账号可访问的入口和资源。
- **我的实例**：管理本人拥有或他人共享的 OpenClaw、Hermes、OpenCode、DeepSeek Harness 工作空间。
- **Teams**：多 Agent 协作、群聊、Kanban、文件和成员产出。
- **资源管理**：通道、上传技能、定时任务、资源包和注入记录。
- **Skill Hub**：面向所有受支持 Runtime 的统一技能目录、版本与安装平台。
- **设置**：个人偏好和当前版本支持的账号选项。

管理后台在此基础上增加全局用户、实例、Runtime 池、安全防护、AI 网关和系统设置。页面上的按钮受角色和配额控制；按钮不可见时，应先确认权限而不是直接判断为页面故障。

<a id="configure-model-access"></a>
## 三、配置模型

创建需要模型的工作空间或自定义 Team 前，管理员应进入 **AI 网关 → 模型**，添加上游 Provider，启用至少一个普通模型并测试健康状态。普通模型即可满足实例创建和自定义 Team；安全模型只在风控规则需要把敏感请求改道时使用。

受管 **Thinking** 是模型级持久设置，只对 ClawManager 能可靠控制的 Provider/模型组合开放。开启后可能增加响应时间和推理 Token，但不会向用户展示私有思维链。

<a id="create-a-workspace"></a>
## 四、创建受管工作空间

进入 **我的实例 → 创建**，选择 Runtime 和模式：

| Runtime | 主要用途 | Lite | Pro |
|---|---|---|---|
| OpenClaw | 会话、工具、定时任务、Team Leader/Worker | 共享 Runtime 池 | 独立桌面工作负载 |
| Hermes | Hermes 原生会话和工具，可作为 Team Worker | 共享 Runtime 池 | 独立桌面工作负载 |
| OpenCode | 接入 AI Gateway 的编码工作空间、文件、终端和桌面 | 共享 Runtime 池 | 独立桌面工作负载 |
| DeepSeek Harness | 接入 AI Gateway、Skill、工作区文件和原生浏览器界面的受管 Agent 工作空间 | 共享 Runtime 池 | 独立 Webtop 工作负载 |

创建页只显示当前 Runtime/模式支持的选项，可能包括：系统镜像、资源预设或自定义 CPU/内存/存储、桌面流质量、环境变量、工作区归档导入、资源包、手动资源和初始技能。

**Lite 不会为每个实例创建独立 Kubernetes Pod。** 它在对应的共享 Runtime Pod 中启动隔离的工作区/进程；Pro 使用独立工作负载，资源隔离更强。

Skill Hub 不是 OpenCode 独有功能。OpenClaw、Hermes、OpenCode、DeepSeek Harness 都从同一技能平台获取技能，只是落盘目录、生效和重载方式不同。如果某个 Runtime 的创建页没有技能预选，实例就绪后仍可从 Skill Hub 或实例详情安装。

<a id="operate-an-instance"></a>
## 五、使用和管理实例

实例详情把生命周期操作、Runtime 原生界面或桌面、工作区文件和 Runtime 专属面板集中在一起：

- **启动 / 停止 / 重启**：通过 ClawManager 操作，不要直接修改生成的 Kubernetes 对象。带环境变量覆盖的重启可把受支持的变量应用到当前实例。
- **删除**：根据存储方式移除受管实例及相关数据。删除前先下载需要保留的文件或归档。
- **Share Link**：设置访问凭据、有效期和是否允许访问工作区，再把链接交给他人；不再使用时及时撤销。
- **工作区文件**：在当前 Runtime/存储路径支持时浏览、上传、下载、编辑或删除。
- **桌面流质量**：低、标准、高三个档位在带宽和画质之间取舍；保存后通常还需按页面提示重启/应用才能生效。
- **技能管理**：查看已安装、已发现技能和实际生效版本。
- **会话 Token 用量**：查看 Runtime 已上报的会话 Token 与估算费用；未上报的数据不会被系统猜测填充。
- **Runtime 概览 / 事件**：独立实例可能展示工作负载健康和事件，用于排障。

OpenClaw 和 Hermes 保留各自的原生会话行为；OpenCode 与 DeepSeek Harness 展示各自的原生工作空间，不会被强行改造成另一套 ClawManager 会话界面。

<a id="resources-and-skills"></a>
## 六、资源管理与 Skill Hub

**资源管理**负责准备可复用的启动内容：

- **资源**：通道、上传的技能包和定时任务；Agent 类型当前是预留类型，不能像普通资源一样编辑。
- **资源包**：把兼容资源组合起来，供创建实例时复用。
- **注入记录**：只读展示某次实例注入实际编译了什么。

**Skill Hub** 是跨 Runtime 的统一技能管理与交付平台。它和资源管理有关，但专门负责可复用技能目录的完整生命周期：浏览、我的 Skill、所有权、标签、版本、扫描状态、发布、安装和实例侧核对。上传包必须是包含 `SKILL.md` 的 ZIP；扫描失败的版本会保留，方便查看原因并上传新版本。扫描完成也不等于自动允许发布或安装。

OpenClaw、Hermes、OpenCode、DeepSeek Harness 都属于支持范围，但具体落盘目录、重载方式和镜像能力可能不同。详见 [资源管理指南](./resource-management_cn.md) 和 [Skill Hub 使用指南](./skill-hub-guide.md)。

<a id="team-collaboration"></a>
## 七、Team 协作

进入 **Teams → 创建**，选择八个不可修改的内置模板之一，或选择自己的自定义模板。Leader 固定使用 OpenClaw Lite；Worker 在相应镜像已启用时可选择 OpenClaw Lite 或 Hermes Lite。

自定义模板可根据自然语言意图生成 2–6 名成员，也可修改名称、意图和人数，重新生成整个 Team，用自然语言调整单个角色，删除并在创建 Team 时复用。调整 Leader 只会扩展领域职责，不会替换其固定编排职责；Leader 仍需理解任务、派发 Worker、回收与核验成员结果并发布最终答案。

执行期间，Team 页面同时展示群聊、当前最新问题的 Execution Kanban、文件、产物、成员交付和最终结果。用户提交新问题后默认切换到最新任务组，历史任务仍可选择。详见 [Team 协作快速指南](./team-workspaces-guide.md)。

<a id="administration"></a>
## 八、管理员日常操作

管理员需要区分以下入口和边界：

- **用户**：创建账号、设置角色和配额、导入受支持的 CSV，并按平台保留策略删除账号。
- **实例**：搜索全局实例，查看归属、Runtime 和状态，执行启动、停止、重启、同步或删除。
- **运行时**：查看共享 Runtime Pod、容量与健康状态；维护前可排空 Pod，停止接收新分配，并由调度器替换或迁移受管 Lite 工作。
- **设置**：配置 Runtime 默认/可用镜像，并发起受控 Lite 滚动更新。
- **安全防护**与 **AI 网关**是独立管理工作区，都不属于资源管理页面。

<a id="runtime-images-and-rollout"></a>
## 九、Runtime 镜像与 Lite 滚动更新

进入 **管理后台 → 设置** 管理 Runtime 镜像。

![Runtime 镜像设置与 Lite 滚动更新](./main/runtime-settings-rollout.png)

这里必须区分“保存镜像配置”和“更新正在运行的 Lite 池”：

1. 在 Lite 或 Pro Runtime 卡片中填写镜像并点击 **保存**。这只会持久化后续创建/选择使用的镜像配置，不会替换当前正在运行的共享 Lite Pod。
2. 如果要让现有 Lite 池使用新镜像，在页面上方找到 **Lite 运行时滚动升级**。选择 OpenClaw Lite、Hermes Lite、OpenCode Lite 或 DeepSeek Harness Lite，核对当前镜像和目标镜像，并设置批次与最大不可用数。
3. 点击 **启动滚动升级**。ClawManager 会按批次排空并替换共享 Runtime 容量，直到目标镜像投入运行。
4. 更新完成后重新检查“运行时”健康状态，并打开测试实例验收。

滚动升级的目标镜像应与刚保存的镜像一致。批次越大，更新越快，但同时可用的共享容量越少；排空过程中活动 Lite 会话可能被中断，因此生产环境应使用保守参数并选择合适维护窗口。Pro 卡片只定义后续独立实例使用的镜像，保存 Pro 镜像不会静默替换已有 Pro 实例。

<a id="ai-gateway"></a>
## 十、AI Gateway 与会话用量

AI Gateway 包含五个入口：

- **模型**：Provider、协议、凭据、价格、健康、安全角色和 Thinking。
- **AI 审计**：请求 trace、路由、错误、延迟和策略证据。
- **成本**：Token 费用估算与内部核算视图。
- **会话用量**：按 Runtime、用户、实例和会话统计用量。
- **风控规则**：按顺序执行放行、阻断或安全模型改道。

会话用量是观测视图，不是会话编辑器或最终账单。可按时间、用户、Runtime、实例和会话过滤，在 Runtime 有上报时比较输入、输出、缓存和推理 Token，再到 AI 审计查看请求级证据。Provider 总量可能因不同 Runtime/Provider 的 Token 分类方式而不同。详见 [AI Gateway 使用指南](./aigateway_cn.md)。

<a id="security-protection"></a>
## 十一、安全防护

安全防护是独立的管理员工作区。总览集中展示实时告警指标、跨产品安全事件、Pod 实时 Aegis 配置、报告导出、应急控制和 KSecure 纵深防御模型；详细页面覆盖运行时防护、主机/容器隔离、信任、身份与出站治理、策略、协作、配额/审批、Skill Scanner 和全链路审计。

Skill Scanner 同时服务两个场景：用户在 Skill Hub 查看技能扫描生命周期；管理员在安全防护中管理 Scanner 健康、策略和安全证据。详见 [安全防护平台指南](./security-platform_cn.md)。

<a id="clipboard-and-desktop"></a>
## 十二、剪贴板、输入和桌面注意事项

剪贴板行为取决于 Runtime 镜像和桌面配置，可能是双向同步、仅从本机粘贴到桌面，或完全禁用。环境设置变化通常需要重启桌面或实例。先测试纯文本，再测试中文/Unicode；剪贴板传输和键盘/输入法是两条不同链路，浏览器权限也可能阻止访问。不要用密码或 API Key 做测试。

<a id="troubleshooting"></a>
## 十三、排障与验收

| 现象 | 优先检查 |
|---|---|
| 看不到 Runtime 或镜像 | 管理员是否保存并启用了对应镜像。 |
| 已保存 Lite 镜像但运行中仍是旧版本 | 保存只改配置，还需启动 Lite 滚动更新。 |
| 没有可用模型 | 至少启用一个普通模型，不要求标记为安全模型。 |
| Lite 实例没有独立 Pod | 正常现象：Lite 共享 Runtime Pod。 |
| PVC 一直 Pending | 存储 profile、StorageClass、访问模式、节点标签和容量。 |
| Share Link 无法访问文件 | 链接权限、有效期、凭据和工作区共享开关。 |
| 技能扫描失败 | 查看保留的错误，修正包并上传新版本。 |
| 安装后看不到技能 | 刷新技能管理，核对版本/路径，并按 Runtime 要求重启或重载。 |
| 会话用量为空 | 时间筛选、实例筛选，以及 Runtime 是否上报。 |
| Team 显示历史问题 | 切换到最新任务/问题组；新问题默认选择最新组。 |

最终验收至少应证明：核心工作负载和 PVC 健康；普通模型可用；所有对外开放的 Runtime 都能创建并打开测试实例；文件和生命周期操作正常；审核后的技能可以安装；AI 审计/会话用量有数据；如果使用 Team，群聊、Kanban、文件、成员交付和最终结果都可用。

<a id="focused-guides"></a>
## 十四、专题指南

- [部署指南](./deployment_cn.md)
- [Team 协作快速指南](./team-workspaces-guide.md)
- [AI Gateway 使用指南](./aigateway_cn.md)
- [安全防护平台指南](./security-platform_cn.md)
- [资源管理指南](./resource-management_cn.md)
- [Skill Hub 使用指南](./skill-hub-guide.md)
- [OpenCode 工作空间指南](./opencode-lite-pro-agent-development.md)
