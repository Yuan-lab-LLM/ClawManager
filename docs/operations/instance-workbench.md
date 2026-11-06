# 实例工作台

工作台为实例所有者和管理员提供 Git 项目、交互终端和手动网页预览。更新控制面和运行镜像后生效；旧运行镜像显示“需要升级”，不会回退到管理容器执行命令。数据库迁移 101 保存预览凭证的哈希，支持多个控制面副本。

## 可用范围

首版执行能力由启用了隔离网络的容器槽位 Runtime 提供，具体部署可参考 `deployments/k8s/lite-slots/`。现有 Pro 或传统非槽位实例即使支持文件工作区，也不会因此具备 Git、终端和预览执行协议；界面按能力接口结果显示，文件管理仍可使用。启用这些执行能力需要兼容的运行方式和镜像，仅更新前端不生效。

## 网页预览域名

开发环境默认使用 `http://p-{token}.localhost:<后端端口>`，后端端口默认 9001。浏览器需要能解析 `*.localhost`；部分系统需要本地通配 DNS。预览请求直接到后端，不能只代理 `/api`。每个预览使用独立来源，支持根路径资源、重定向和 WebSocket。

生产环境设置后端环境变量：

```sh
CLAWMANAGER_WORKBENCH_PREVIEW_ORIGIN=https://p-{token}.preview.example.net
```

可参考 `deployments/k8s/workbench/backend-patch.example.yaml` 和 `deployments/k8s/workbench/ingress.example.yaml`，并按实际后端 Service 名称、端口和 Ingress 控制器调整。

为 `*.preview.example.net` 设置通配 DNS、TLS 证书和到 CM 后端的 Ingress。必须允许任意请求路径、流式响应和 WebSocket Upgrade，并为长连接配置合适的超时。域名模板中必须恰好包含一个 `{token}`，不能包含路径、查询参数或账号信息。可以在现有后端 Deployment 中添加环境变量，在 Ingress 中添加通配主机规则；无需为每个预览创建 Kubernetes Service 或 Ingress。

建议使用与 CM 登录站点不同的可注册域，避免登录站点的父域 Cookie 被发送给用户应用。预览域不要承载 CM UI/API，也不要配置平台登录 Cookie。HTTPS 的 CM 页面需要 HTTPS 预览地址。

预览地址是临时访问凭证，默认八小时后过期；请勿公开分享。它只能访问创建时指定的实例内 loopback HTTP/HTTPS 端口。实例停止、所有者变化或运行代次变化后失效；已建立的长连接也会关闭。重新启动服务后，通过工作台手动新建预览。

使用需要浏览器确认的环境证书时，预览会在 CM 内保留说明和确认按钮。点击按钮在新标签页核对证书，确认后关闭该标签页并返回 CM，预览会重新检查连接。弹出窗口被拦截时，请允许 CM 打开新标签页后重试；不会自动离开当前工作台。

CM 会在有效预览域名上保留 `/__clawmanager_cert_check` 和 `/__clawmanager_cert_trust` 两个路径，校验预览凭证、实例所有者和运行代次后直接响应，不访问用户应用。标准转发所有路径的 Ingress 即可使用，无需额外配置 Nginx location；这两个路径不要用于应用路由。过期预览和应用错误仍分别显示原有的 404 或服务错误，不会当作证书失败。

在 OpenCode 或 DeepSeek Harness 原生界面中，普通点击 `http(s)://localhost`、`127.0.0.1`、`[::1]` 或 `0.0.0.0` 的链接会在右侧新建实例预览，不会连接操作用户电脑的同名地址。外部链接及带修饰键或中键的浏览行为保持原样；工作台不会自动扫描端口或根据 Agent 消息新建预览。

## Git 和终端

项目登记和 `.git` 保存在实例持久工作区。Git 命令及其凭证助手、终端、依赖安装均在业务沙箱身份与网络中执行。提交和推送是两个独立动作。界面不提供强制推送、丢弃修改或自动 stash。首次提交需要实例自己的 `git config user.name` 和 `git config user.email`；私有仓库使用实例已有的 Git 凭证。

切换工作台标签不会结束终端；明确点击结束才会停止该会话。自动休眠可以结束终端和调试服务，迁移会停止旧执行并拒绝旧页面继续写入。迁移保留代码和 Git 数据，不保证容器可写层中的依赖与系统文件迁移；必要时重新安装依赖。

进程面板仅列出运行时能够证明归属的进程。Agent 启动的进程可以停止；只有存在可信启动信息的会话才提供重新启动，不能重启的进程请通过原 Agent 或终端重新执行启动命令。

## 验证开发改动

前端状态测试在 `frontend` 中运行 `npm run test:workbench`。在 `e2e` 中运行同名命令会打开真实 React 工作台和实例页，使用临时 Git 仓库、Python PTY 和本地测试应用检查交互；需要本机 Chrome、Go、Git 和 Python 3，不访问生产实例。

跨仓库预览链路测试需要在同一 Linux 网络环境启动两个测试进程：

```sh
# AgentsRuntime/clawmanager-agent，先启动测试专用运行时
CM_WORKBENCH_PREVIEW_HARNESS_FILE=/tmp/cm-preview-wire.json \
  go test ./slots -run '^TestWorkbenchPreviewExternalHarness$' -count=1 -v

# ClawManager/backend，等待上面的元数据文件出现后运行
CM_WORKBENCH_PREVIEW_HARNESS_FILE=/tmp/cm-preview-wire.json \
  go test -race ./internal/services \
  -run '^TestWorkbenchPreviewExternalRuntimeRoundTrip$' -count=1 -v

# 结束测试运行时（它也会在 90 秒后自动结束）
touch /tmp/cm-preview-wire.json.stop
```

首次构建耗时较长时，请先编译两个测试二进制，再启动运行时测试进程。该测试覆盖 CM 能力查询和预览创建、运行池、私有 Unix Worker/网络 Helper，以及真实 HTTP/WebSocket 应用，检查路径、查询、认证、Cookie、控制头隔离和代次变更后的断连。真实网络命名空间、业务 UID、终端子进程清理及容器重置协议有各自的 Linux 验收测试；本地测试桥接器不替代真实集群的发布验收。

预览的 HTTPS 上游使用正常证书校验，不会自动信任调试服务的自签名证书。
