# OpenCode 分享入口初始化 401 修复（2026-11-10）

## 原因与改动

受保护的 Runtime 根入口要求 `X-ClawManager-Ingress-Token`，随后才由 OpenCode 校验 Basic Auth。普通 HTTP/WebSocket 代理已设置该入口凭据，但初始化会话的独立 HTTP 请求只设置 Basic Auth，导致 `/api/info` 在到达 OpenCode 前被拒绝。

从已验证目标取得的 `protectedGateway` 标志现在贯穿初始化调用，并在统一 `openCodeBootstrapAPI` 中复用 `setNativeIngressToken`。覆盖所有信息、会话查询/分页、创建及失败后重查请求；普通实例页和分享页共用此路径。旧的非保护入口不发送此凭据，浏览器伪造头不能覆盖平台凭据。未关闭鉴权、未改分享权限或把凭据交给浏览器。

## 验证

- 现场只读对照：同一 `/api/info` 请求仅含正确 Basic Auth 时401；增加入口凭据后200。当前上游返回v1 HTML，保留既有原生主页回退。
- 新回归在修复前复现相同401，修复后通过。覆盖保护入口的v1/v2、owner/share绑定、会话复用、浏览器头伪造及凭据不出现在响应/重定向中；既有非保护入口测试要求入口头保持为空。
- 相关 services 与 handlers 的 OpenCode、代理、分享、外部访问鉴权测试通过（0.272s / 0.103s）。使用项目既有Darwin Linux-only NFS测试临时overlay及显式Go1.27.1工具链；未把全仓库回归记为本轮已重跑。
- 独立窄审查：READY，无Critical/Important问题。审查者的独立运行受本机Go工具链默认版本不匹配阻断，运行通过证据来自上述显式工具链命令。
- Chrome匿名上下文使用现有分享链接：修复前iframe502，正文包含初始化401；修复后iframe200并显示OpenCode原生主页，脚本/样式无失败，会话、配置、provider、目录等接口200，`/global/event` 为200事件流，浏览器无console/page错误。未发送聊天、模型请求或创建真实会话/项目。

## 发布与保护

代码提交 `3b9ff01afb29da78fb25e1a6e727b1452cb9e926`，已推送 `preview`，不建PR。

管理应用镜像：`clawmanager.local/clawmanager@sha256:3df9133c077927dfeb669c1891fd4fb2eabbd2691c66895d9290f7fa2f646966`。

运行中API版本及五个Linux二进制SHA256均已核验。只切换管理应用，Runtime171的Pod UID、全部ContainerID/restartCount保持不变，118仍为running/G11并绑定171。PVC UID、证书SHA256、现有分享URL哈希保持不变，没有旋转分享链接或安装证书。

项目7文件采样中6个未变，`blog/index.html`在观察期间于16:21:06 +0800有内容变化（应用新Pod创建16:19:48）。本轮修复/验收没有执行任何工作区写入或回滚；记录并保留当前内容，不把这项误记为7文件全部未变。

节点私有证据：`/root/cm-opencode-share-401-20261110/` 下的 `before-probe.json`、`before.json`、`deployment-image.json`、`verified.json`。Chrome前后截图和网络结果、真实RED/GREEN日志保存在本机 `/tmp/cm-opencode-share-401/`。含现有分享URL的临时文件不提交仓库，验收后清理；未创建Kubernetes测试Pod。
