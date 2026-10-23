# OpenCode Pro v2 本机部署

Pro 保留独立 Webtop 桌面和原有 30443 入口，OpenCode 固定为已与 Lite 验证一致的 **2.0.27**。本目录提供本机增量构建，Runtime 正式源码位于 `D:\AgentsRuntime\opencode`。

## 构建

```powershell
.\deployments\opencode-pro-v2\build.ps1
```

默认生成 `clawmanager-local/opencode-pro:2.0.27-20261016`，仅用于当前 Docker Desktop / Kubernetes 节点，未推送远程仓库。基础镜像包含 Webtop、2.0.27 官方二进制和已适配 v2 健康检查的 Runtime Agent；构建从 Runtime 项目读取最新 Pro 启动脚本，先执行测试，再打包。该增量构建支持 linux/amd64。

需要从 Runtime 仓库完整重建时，在其根目录运行：

```powershell
docker build -f opencode/Dockerfile -t opencode-pro:2.0.27 .
```

Runtime Dockerfile 已将默认版本改为 2.0.27，使用官方 npm 平台包，并重新编译 Runtime Agent。完整构建需要下载基础镜像及依赖；本次本机验证使用上述增量构建。

## 启动与会话恢复

- OpenCode 服务只监听容器回环地址 4096；用户从受控的 Webtop 3001 桌面进入，外部入口仍为 30443。
- 默认项目为 `/config/workspace/project`，首次启动创建目录、Git 工作树及“开始使用”会话。
- 使用 v2 `/api/session` API 和 `/server/<server-key>/session/<id>` 路由，项目位置写入会话的 `location.directory`。
- 每次启动优先恢复所选项目最近使用的未归档主会话；找不到时才初始化。实例独立持久种子与项目路径决定首个会话 ID，服务重启不会重复创建。
- `CLAWMANAGER_DEFAULT_PROJECT_PATH` 与现有界面的 “Set as project” 操作继续生效。目录必须位于工作区内；其他项目、子会话、归档会话不会被当作该项目的活动会话。
- 保留 v2 服务认证。启动器通过原生配对 API 取得签名浏览器会话，再由一次性的回环地址交付原生 HttpOnly Cookie，随后跳转到具体会话。交接完成后监听器退出，未使用时 120 秒退出；浏览器地址和启动参数不包含网关密钥或签名凭证。
- Chromium 仍按桌面用户启动，沿用启动锁和持久 profile 锁恢复逻辑。
- Chromium 默认启用浏览器沙箱，启动器不再传入 `--no-sandbox`。集群须支持非特权用户命名空间；当前受管 Pro Pod 的 Chromium 兼容策略已经过验证，不需要将容器改为 privileged。

## 测试

```powershell
python -B -m unittest discover -s deployments/opencode-pro-v2 -p test_session.py -v
python -B -m unittest discover -s D:\AgentsRuntime\opencode\tests -p test_session.py -v
```

11 项回归测试覆盖首启、重启、最近会话、分页、项目切换、归档、子会话、创建响应丢失、并发种子、原生配对及 Cookie 交接。真实浏览器验证结果和截图保存在 `outputs/opencode-pro-v2/`。

本次验证使用临时初始化模型，不发送推理请求。正式对话需要系统中配置可用模型。没有现存 Pro 实例，因此本次验证覆盖新实例及其后续重启，不包含旧版真实历史数据库的迁移验收。

## 默认镜像与回退

通过 `/api/v1/system-settings/images` 修改 **OpenCode Pro / desktop，ID 4** 的镜像。只修改该条记录，Lite / gateway 和 Lite Runtime Deployment 均保留现状。修改前设置快照保存在本机 `.tmp/opencode-pro-v2/inventory-before.json`。

回退时恢复该条 Pro 设置的原镜像 `ghcr.io/yuan-lab-llm/agentsruntime/opencode:latest`。默认镜像调整只影响之后新建的实例，不会自动替换已有实例及其数据卷。

v2 认证与配对行为参照固定版本源码：[原生认证](https://github.com/anomalyco/opencode/blob/v2.0.27/packages/server/src/auth.ts)、[配对及 Cookie 交付](https://github.com/anomalyco/opencode/blob/v2.0.27/packages/server/src/handlers/server.ts)。

## 2026-10-16 修复验证

- 新镜像：`clawmanager-local/opencode-pro:2.0.27-20261016`。Runtime 启动器已移除 `--no-sandbox`；真实 Pod 内已验证非 root 浏览器、嵌套 PID 命名空间及 seccomp，保留原有 Chromium 兼容安全策略。
- ClawManager 启动、停止、重启、带环境变量重启和删除请求共用实例互斥保护。MySQL 命名锁使用独立连接持有，覆盖整个停止、等待和启动过程，跨 API 副本生效；冲突请求返回 HTTP 409，且不会写入环境配置。
- 前端在操作等待期间禁用相关操作，包括轮询短暂显示 stopped 时的启动按钮。
- 验证包含独立数据库锁下四类真实 API 冲突、临时 Pro 真实重启及会话恢复、浏览器按钮检查，以及现有实例保留 PVC 的镜像更新。记录在 `outputs/opencode-pro-fix/`。
- 实例相关测试及 11 项 Runtime 会话测试通过。完整 services 测试另有与本次改动无关的 `TestClassifyDiscoveredModelMatchesDelimitedReasoningNames`（o3 分类）失败。
