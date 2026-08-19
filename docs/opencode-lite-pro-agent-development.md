[← 返回 README](../README.zh-CN.md)

# OpenCode 工作空间指南

OpenCode 是 ClawManager 提供的受管编码工作空间，运行官方 OpenCode 版本，并通过平台 AI Gateway 获取模型访问能力。

## Lite 与 Pro

| 模式 | 运行形态 | 适用场景 | 关键边界 |
|---|---|---|---|
| Lite | 在共享 Runtime Pod 中运行隔离的 OpenCode 进程和工作区 | 快速启动、节省共享容量 | 每个实例没有独立 Pod |
| Pro | 独立桌面工作负载和工作区 | 更强隔离、完整桌面 | 修改默认镜像不会自动替换已有实例 |

两种模式都会按照所选存储 profile 持久化用户工作区。Lite 的 Portal 子路径由 ClawManager 适配；Pro 从独立桌面进入 OpenCode。

## 创建前准备

1. 管理员启用兼容的 OpenCode Lite 或 Pro 镜像。
2. **AI 网关 → 模型**至少启用一个普通模型。
3. Lite 共享 OpenCode Runtime 池健康；如果刚保存新镜像，还必须完成 Lite 滚动更新。
4. 用户具备足够的 CPU、内存、存储配额和所需资源包。

OpenCode 会收到 ClawManager 管理的 AI Gateway Provider 配置。除非管理员明确为该环境设计了其他 Provider，否则不要在 OpenCode 的连接界面自行添加无关外部 Key。

## 创建与进入工作区

进入 **我的实例 → 创建**，选择 OpenCode 和 Lite/Pro，再选择已启用镜像、资源、环境变量及当前支持的启动资源。创建完成后，实例页提供生命周期操作、OpenCode 终端/桌面和工作区文件。

通过 ClawManager 执行启动、停止、重启和删除。直接修改生成的工作负载可能被期望状态同步覆盖；删除前先保存需要的文件。

## 文件、终端与持久化

- 项目文件应保存在 ClawManager 展示的工作区路径，不要放在临时系统目录。
- 右侧文件面板在当前存储支持时提供上传、下载、编辑和删除。
- Pro 桌面流质量保存后通常还需执行页面提示的应用/重启。
- Share Link 应设置有效期、凭据和最小必要的工作区权限。
- 如果终端可用但重启后文件消失，优先检查存储 profile 和实际工作区路径。

## 模型与 AI Gateway

OpenCode 使用平台已启用模型和管理员配置的请求协议。生产使用前应验证流式输出和工具调用。请求失败时，先检查实例状态，再检查模型健康和 AI 审计。普通 OpenCode 使用不要求安全模型。

## Skill Hub 兼容

Skill Hub 是 OpenClaw、Hermes、OpenCode、DeepSeek Harness 共用的平台能力，并非 OpenCode 功能。本节只说明 OpenCode 的兼容差异：Lite 通常落盘到 `{workspace}/home/.opencode/skills`，受管 HostPath Pro 使用 `/config/workspace/.opencode/skills`。

创建页没有技能预选时，可在实例就绪后安装，并在实例技能管理中核对实际版本。非 HostPath OpenCode Pro 还依赖所选 Runtime Agent 镜像实现远程安装/卸载；HostPath 场景验证成功不代表所有存储后端能力相同。

## 当前边界

- OpenCode 不继承 OpenClaw 配置计划、OpenClaw 工作区归档或 Team 角色注入。
- 标准 Team 创建流程当前不使用 OpenCode 作为 Leader 或 Worker。
- 只有当前界面明确展示时，才应认为定时任务可用。
- 即使都标记为 OpenCode，不同 Runtime 镜像和存储后端的能力仍可能不同。

## 常见问题

| 现象 | 检查 |
|---|---|
| 看不到 OpenCode 镜像 | 管理后台设置中的镜像启用状态和用户配额。 |
| Lite 仍运行旧镜像 | 只保存不够，还要完成 Lite 滚动更新。 |
| Portal 无法访问 | 实例/Runtime 健康、重启结果和共享池事件。 |
| 模型调用失败 | 启用模型、Provider 健康、协议和 AI 审计。 |
| 文件无法持久化 | 工作区路径、PVC/存储 profile 和卷健康。 |
| 技能安装不完整 | 实例技能管理、落盘路径和 Runtime Agent 能力。 |

验收应覆盖创建、启停重启、Portal/桌面、AI Gateway 流式与工具调用、工作区持久化、Share Link 权限，以及需要时的技能清单/安装/收录和明确错误反馈。

相关说明：[用户手册](./use_guide_cn.md)、[AI Gateway 指南](./aigateway_cn.md)、[Skill Hub 指南](./skill-hub-guide.md)。
