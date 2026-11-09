# Laya CPU 离线镜像（独立服务）

此目录只负责 Laya 打包和部署，不启用 ClawManager 的 Laya 分类器。
镜像内置完整多语言权重，无需 GPU、PVC 或运行时外网下载。

## 固定版本

- 平台：`linux/amd64`。
- 上游 Laya：`9d955671415fc19f069b9cc998928075c1f255ec`（包版本 0.3.21）。
- 模型：`convaiinnovations/laya-multilingual`。
- 模型 revision：`e4e9ddf21a7b1903b7acffd8814ad4307bf63a67`。
- 权重 SHA256：`9d628fd971b700382ac6f65920a86f149777b2e748e0c955fb3b19695aa8f204`。
- Torch 2.14.0 CPU；完整已解析依赖保存在镜像 `/opt/laya/requirements-resolved.txt`。
- 本次 Python 基础镜像：`python:3.11-slim-bookworm@sha256:a36c24f9cbdf4fd0f52d67f0823eeac19c2028c637cecc392d97f980d4fec56b`。
- 镜像：`10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1`。
- 已推送摘要：`sha256:c8baa39f0f5b03f033a2a34ef694709a026c0a00332b7ef98f749a6f2d45e1e4`；K8s 清单固定该摘要，防止标签变化影响部署。

镜像使用独立启动器注入本地 Router，保留官方 HTTP 服务。不填写 model 时也固定使用 multilingual；显式请求 english/typed-decisions 返回 422。模型不是聊天生成模型，不提供 `/v1/chat/completions`。

## 本机测试与构建

### 本机 Python API 测试

`call_laya.py` 为独立客户端，仅使用 Python 3.9+ 标准库，不需要安装模型或第三方依赖。
默认地址 `http://10.130.14.23:30800` 要求你已经将远端 Service 开放为 NodePort 30800，并允许本机访问；本目录原始 K8s 清单仍为 ClusterIP。使用 SSH 隧道时传 `--url http://127.0.0.1:18000`。

```powershell
# 无需密钥的健康检查
python .\deployments\laya\call_laya.py --health
# 交互模式：按提示输入密钥（不显示），然后连续输入提问
python .\deployments\laya\call_laya.py
# 单条测试，输出完整响应
python .\deployments\laya\call_laya.py --text "帮我写一个 Python 排序函数" --raw
# 五条内置提问，测试能力档位而非提问类别
python .\deployments\laya\call_laya.py --batch --task tier
```

也可通过 `LAYA_API_KEY` 环境变量提供密钥，不提供命令行密钥参数，避免在命令历史中留下密钥。
类别及能力档位定义在客户端 `QUESTIONS` 中，可自行调整。批量请求顺序发送，失败即停止；结果是分类与概率，不是聊天答案或经过校准的业务准确率。默认绕过系统代理直连，确实需要代理时使用 `--use-proxy`。HTTP 只用于可信内网，跨不可信网络使用 HTTPS 或 SSH 隧道。

客户端回归测试（不访问远端）：

```powershell
python -m unittest discover -s deployments/laya -p test_call_laya.py -v
```

### 构建镜像

准备上述固定源码到 `.codex-temp/laya-offline-build/source`，并准备上述模型 snapshot 到 `.codex-temp/laya-model-cache`（保留 HF 标准缓存目录结构）。

```powershell
.\deployments\laya\build.ps1
```

脚本会将 HF 缓存中的符号链接展开为普通文件并校验权重。Dockerfile 只复制必要文件，不会复制 API Key、开发环境或其他项目数据。
镜像构建需要联网拉取 Python 和依赖，部署和推理不需要外网。
其他依赖的确切版本见镜像中的 resolved 文件；再次构建需用新标签，不要覆盖已发布版本。

先运行离线测试脚本：它用 `--network none`、只读根文件系统启动临时容器，验证中文/英文请求、鉴权和输入限额，结束后只清理该测试容器。通过后再推送：

```powershell
.\deployments\laya\test-offline.ps1
docker --context desktop-linux push 10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1
```

## 在服务器部署

前提：node1 上的 containerd 必须能拉取 `10.130.14.23:5000`。当前仓库使用 HTTP；如果出现 `HTTP response to HTTPS client`，需要为此特定仓库配置 containerd 的 HTTP hosts，并确认 CRI 的 certs.d 配置路径生效。不要全局关闭 TLS 校验，也不要直接覆盖现有 containerd 配置。
本机 Docker 的 insecure-registry 设置不会自动同步到服务器 containerd。

检查节点标签和调度条件：

```bash
kubectl get node node1 --show-labels
kubectl describe node node1
```

将 `k8s.yaml` 上传到服务器，执行：

```bash
kubectl create namespace laya-system --dry-run=client -o yaml | kubectl apply -f -
# 密钥只保存在本次 shell 变量和 Kubernetes Secret 中，不写入 YAML 或镜像。
LAYA_DEPLOY_KEY=$(openssl rand -hex 32)
printf '%s' "$LAYA_DEPLOY_KEY" | kubectl -n laya-system create secret generic laya-api --from-file=api-key=/dev/stdin
unset LAYA_DEPLOY_KEY
kubectl apply -f k8s.yaml
kubectl -n laya-system rollout status deployment/laya --timeout=10m
kubectl -n laya-system get pods -o wide
kubectl -n laya-system logs deployment/laya --tail=100
```

如果 Secret 已存在，不重复创建或覆盖它，直接部署。启动需要密钥，未设置则拒绝启动。
Pod 固定在 node1；如果该节点不可调度，先明确选择目标节点，不要取消安全污点来强行调度。
资源初值 request 2 CPU/4 GiB，limit 4 CPU/8 GiB；CPU 推理四线程，最多接收四个并发请求（并非四路同时推理）。性能需在服务器实测。

## 验证与访问

先在 Pod 内做一次 HTTP 冒烟测试，无需取出密钥：

```bash
kubectl -n laya-system exec deployment/laya -- python /opt/laya/smoke_test.py
```

集群内部地址：`http://laya.laya-system.svc.cluster.local:8000`。
在服务器开一个终端转发：

```bash
kubectl -n laya-system port-forward service/laya 18000:8000 --address 127.0.0.1
```

另一个服务器终端验证：

```bash
curl --fail http://127.0.0.1:18000/health
```

本机如需访问，另开 SSH 隧道（将 SSH 登录地址替换为实际管理节点地址）：

```powershell
ssh -N -L 18000:127.0.0.1:18000 root@10.130.14.23
```

分类请求用 `POST /v1/systemone`，Authorization 使用 `Bearer <Secret中的密钥>`，JSON 示例：

```json
{"model":"multilingual","state":"帮我写一个 Python 排序函数","questions":{"category":{"type":"choice","instructions":"判断问题类别","criteria":{"chat":"闲聊问候","coding":"编写代码","math":"数学证明"}}}}
```

只开放 ClusterIP，不暴露无加密的公网分类接口。若未来需要跨集群生产访问，再增加 TLS 入口和访问限制。
本次部署不自动接入 ClawManager，也不能把历史测试成绩当作该版本的分类准确率。

## 本机构建验证（2026-09-28）

- 镜像构建成功，`pip check` 通过，权重 SHA256 在复制后及镜像构建中均通过校验。
- 使用 `--network none --read-only --cap-drop ALL --security-opt no-new-privileges` 启动成功。
- `/health` 返回 CPU、已加载 multilingual。
- 中文及英文排序函数请求均分类为 coding；实际请求固定加载内置 multilingual，无网络下载。
- 未带密钥返回 401；请求未内置的 english 模型返回 422；超出 token 限额返回 422。
- 两条请求耗时约 2.898 秒（首次中文）、0.439 秒（后续英文）；内存快照约 1.892 GiB。
- 以上只是本机两条冒烟测试，不是服务器性能或分类准确率基准。
- Windows Docker CLI 在测试期间出现过原生异常日志；重试后测试脚本以 0 退出，临时容器已清理。容器内服务无 OOM/崩溃，所有上述 HTTP 检查通过。
- 未执行远端 `kubectl apply`，未修改 ClawManager 的分类策略。
