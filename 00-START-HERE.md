# ClawManager 外部测试傻瓜版

只看这一份，从上往下做，不要自己猜。

## 先说最重要的：你手里的 3 种包不是一回事

| 你看到的文件 | 它是什么 | 正确用法 | 错误用法 |
| --- | --- | --- | --- |
| `ClawManager-full-workspace-*.tar.gz` | ClawManager 完整工程包 | 在本地解压，然后按下面步骤部署 ClawManager | 不要把它上传到实例页的 `OpenClaw 导入` |
| `*.zip` | Skill 包 | 登录 `User` 界面后，去 `OpenClaw Resource Management` 上传 | 不要把它当 ClawManager 安装包 |
| `*.openclaw` | OpenClaw 工作区归档 | 只在创建实例时填到 `OpenClaw 导入` 那一栏 | 不要拿它代替 Skill 包或 ClawManager 包 |

一句话：

- `tar.gz` = 解压后部署平台
- `zip` = 上传为 Skill
- `.openclaw` = 给实例导入工作区

## 目标

把 ClawManager 在一台 macOS 机器上跑起来，并完成这一条最小验证链：

1. 平台启动成功
2. 管理员能登录
3. 配好 1 个 normal model 和 1 个 secure model
4. 创建 1 个 OpenClaw 实例
5. 实例状态变成 `running / ready / online / running`
6. 进入实例聊一句话，收到回复

这次默认不测：

- `skill-scanner`
- FastSkill
- frozen `U3`

## 第 0 步：准备环境

这台机器需要：

- macOS
- Docker Desktop
- `kubectl`
- `k3d`
- 一个 OpenAI-compatible 模型提供商参数：
  - `base_url`
  - `provider_model_name`
  - `api_key`

如果没有模型提供商参数，后面会卡在实例或聊天，不是程序坏了。

## 第 1 步：解压完整工程包

假设你拿到的文件叫：

```text
ClawManager-full-workspace-20260416-151921.tar.gz
```

在终端执行：

```bash
mkdir -p ~/Desktop/clawmanager-test
cd ~/Desktop/clawmanager-test
tar -xzf /你的下载目录/ClawManager-full-workspace-20260416-151921.tar.gz
```

注意：这个完整工程包会直接解压到你当前所在目录，所以请先进入一个空目录再解压。

执行这句确认你进对地方了：

```bash
pwd
ls
```

你应该能看到：

- `backend`
- `frontend`
- `deployments`
- `docs`
- `00-START-HERE.md`

## 第 2 步：启动 Docker Desktop

先手动打开 Docker Desktop，或者执行：

```bash
open -a Docker
```

等 Docker 完全启动。

## 第 3 步：安装命令行工具

如果你已经装过，可以跳过。

```bash
brew install kubectl
brew install k3d
```

验证：

```bash
kubectl version --client
k3d version
```

## 第 4 步：创建本地 K3D 集群

在项目根目录执行：

```bash
k3d cluster create clawmanager \
  --port "30443:30443@loadbalancer" \
  --agents 0
```

然后验证：

```bash
kubectl cluster-info
kubectl get nodes
```

预期：能看到 `k3d-clawmanager-server-0` 是 `Ready`

## 第 5 步：部署 ClawManager

执行：

```bash
kubectl apply -f deployments/k3s/clawmanager.yaml
kubectl get pods -n clawmanager-system -w
```

等到核心 Pod 至少出现：

- `mysql-xxx` -> `Running`
- `clawmanager-app-xxx` -> `Running`

检查平台健康：

```bash
curl -sk https://localhost:30443/healthz
```

预期输出：

```text
ok
```

如果不是 `ok`，先停在这里。

## 第 6 步：登录管理端

浏览器打开：

```text
https://localhost:30443/login
```

默认管理员账号：

- username: `admin`
- password: `admin123`

登录后，你会看到两个使用方向：

- `Manager`：系统设置、模型配置、实例管理
- `User`：进入实例、上传 Skill

当前这一步先留在 `Manager`。

## 第 7 步：配置 OpenClaw ARM runtime image

这一步不要猜，不要跳过。

先在终端获取管理员 token：

```bash
export CM_BASE_URL="https://localhost:30443"
export CM_API="$CM_BASE_URL/api/v1"

export CM_ADMIN_TOKEN="$(curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.access_token')"

test -n "$CM_ADMIN_TOKEN" && echo "admin token ready"
```

然后执行：

```bash
curl -sk -X PUT "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "instance_type":"openclaw",
    "display_name":"OpenClaw ARM Dev Bootstrap",
    "image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"
  }'
```

再验证：

```bash
curl -sk "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items[] | select(.instance_type=="openclaw")'
```

预期：返回的 `image` 就是上面这条 ARM image。

## 第 8 步：配置模型

先把你自己的模型参数填进环境变量：

```bash
export LLM_BASE_URL="这里填你的base_url"
export LLM_MODEL="这里填你的provider_model_name"
export LLM_API_KEY="这里填你的api_key"
```

然后创建 normal model：

```bash
curl -sk -X PUT "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"display_name\":\"A1 normal model\",
    \"provider_type\":\"openai-compatible\",
    \"protocol_type\":\"openai-compatible\",
    \"base_url\":\"${LLM_BASE_URL}\",
    \"provider_model_name\":\"${LLM_MODEL}\",
    \"api_key\":\"${LLM_API_KEY}\",
    \"is_secure\":false,
    \"is_active\":true,
    \"input_price\":0,
    \"output_price\":0,
    \"currency\":\"USD\"
  }"
```

再创建 secure model：

```bash
curl -sk -X PUT "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"display_name\":\"A1 secure model\",
    \"provider_type\":\"openai-compatible\",
    \"protocol_type\":\"openai-compatible\",
    \"base_url\":\"${LLM_BASE_URL}\",
    \"provider_model_name\":\"${LLM_MODEL}\",
    \"api_key\":\"${LLM_API_KEY}\",
    \"is_secure\":true,
    \"is_active\":true,
    \"input_price\":0,
    \"output_price\":0,
    \"currency\":\"USD\"
  }"
```

验证：

```bash
curl -sk "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items | map({display_name,is_secure,is_active,provider_model_name})'
```

预期：至少看到两条 active 记录：

- 一条 `is_secure=false`
- 一条 `is_secure=true`

## 第 9 步：创建 1 个实例

你可以在 `Manager` 界面创建，也可以直接用命令。

为了避免页面里填错，建议直接执行：

```bash
curl -sk -X POST "$CM_API/instances" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"a1-arm64-core",
    "type":"openclaw",
    "cpu_cores":2,
    "memory_gb":3,
    "disk_gb":20,
    "gpu_enabled":false,
    "gpu_count":0,
    "os_type":"openclaw",
    "os_version":"latest",
    "storage_class":""
  }' | tee /tmp/a1-instance-create.json
```

记录实例 ID：

```bash
export A1_INSTANCE_ID="$(jq -r '.data.id' /tmp/a1-instance-create.json)"
echo "$A1_INSTANCE_ID"
```

## 第 10 步：等实例完全 ready

执行轮询：

```bash
for i in $(seq 1 18); do
  echo "==== poll $i ===="
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/status" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data'
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/runtime" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data'
  sleep 10
done
```

你要等到这 4 个状态同时成立：

- `status=running`
- `infra_status=ready`
- `agent_status=online`
- `openclaw_status=running`

没到这一步，不要继续。

## 第 11 步：进入实例聊天

先生成访问链接：

```bash
curl -sk -X POST "$CM_API/instances/$A1_INSTANCE_ID/access" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee /tmp/a1-access.json
```

然后拿到 `access_url`：

```bash
jq -r '.data.access_url' /tmp/a1-access.json
```

浏览器打开这个链接，进入实例内的 OpenClaw 页面。

发一句：

```text
ping
```

如果能收到回复，这条主链就通了。

## 第 12 步：如果你想传 Skill

只在这一种情况下上传 `.zip`：

- 你要验证 Skill 上传和挂载

做法：

1. 进 `User` 界面
2. 打开 `OpenClaw Resource Management`
3. 上传 `skill.zip`
4. 再把它 attach 到目标实例

不要把 `ClawManager-full-workspace-*.tar.gz` 传到任何导入框里。

## 第 13 步：如果你看到 `OpenClaw 导入`

这个位置只接受：

- `*.openclaw`

它的意思是：

- 给实例导入一个 OpenClaw 工作区归档

它不接受：

- `ClawManager-full-workspace-*.tar.gz`
- Skill `.zip`

如果你手里没有 `.openclaw`，这里留空，直接建实例。

## 第 14 步：你最后怎么回报

只回这 3 类结论就够了：

### PASS

- 平台起来了
- 管理员能登录
- 模型配好了
- 1 个实例 ready 了
- 聊天有回复

### BLOCKED_BY_MODEL_BOOTSTRAP

- 平台起来了
- 但模型参数没给全，或 model 没配好

### BLOCKED_BY_INSTANCE_MAINLINE

- 模型已经配好
- 但实例没有到 `running / ready / online / running`

## 你如果只想找更详细的材料

再看这些：

- `docs/k3s-local-setup.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
- `docs/manual-skill-import.md`
