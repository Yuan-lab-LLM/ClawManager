# A1 外测说明

这份说明给测试方直接使用，目标只有一个：按 A1 core-chain 的已验证边界，把 ClawManager 在 macOS + k3d 上拉起，并完成一条最小成功链。

如果你不想在多份文档之间来回跳，先直接看仓库根目录的：

- `00-START-HERE.md`

它是单文件傻瓜版入口。

## 先说清楚这次要测什么

本次外测范围只包含：

1. control plane 启动
2. admin 登录
3. OpenClaw ARM runtime bootstrap
4. normal + secure model bootstrap
5. 创建 1 个 OpenClaw 实例
6. 等到 runtime ready
7. 做 1 次 direct QA

本次外测不包含：

- `skill-scanner`
- FastSkill 自动发现
- frozen `U3`
- `/api/v1/admin/skills` 非空承诺

## 包里会拿到什么

请以打包产物里的这些文件为准：

- `deployments/k3s/clawmanager.yaml`
- `docs/k3s-local-setup.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
- `docs/superpowers/releases/2026-04-16-a1-core-release/06-live-demo-record.md`
- `docs/manual-skill-import.md`（可选扩展验证，不属于 A1 默认 gate）

## 测试前提

测试方需要自己准备：

- 一台 macOS 机器
- Docker Desktop
- `kubectl`
- `k3d`
- 一个可用的 OpenAI-compatible 模型供应商输入：
  - `base_url`
  - `provider_model_name`
  - `api_key`

注意：这三个模型输入不在 repo 里，不在 manifest 里，也不在交付包里。

## 测试链路

### 第 1 步：启动 K3D 集群并部署

按 `docs/k3s-local-setup.md` 执行：

1. 启动 Docker Desktop
2. 创建 `clawmanager` k3d 集群
3. 执行：

```bash
kubectl apply -f deployments/k3s/clawmanager.yaml
```

4. 等待 `clawmanager-system` 命名空间里核心 Pod 进入 `Running`

第一条检查：

```bash
curl -sk https://localhost:30443/healthz
```

预期输出：`ok`

### 第 2 步：登录管理端

浏览器访问：

- `https://localhost:30443/login`

默认管理员：

- username: `admin`
- password: `admin123`

UI 分工：

- `Manager` 界面：系统设置、模型配置、发布前 bootstrap
- `User` 界面：实例进入、Skill 上传、Skill 挂载

### 第 3 步：做 ARM runtime bootstrap

不要猜默认镜像。直接按 `03-bootstrap-contract.md` 的 `PUT /api/v1/system-settings/images` 把 OpenClaw runtime image 指到冻结过的 ARM image reference。

这一步完成后，再用：

```bash
curl -sk https://localhost:30443/api/v1/system-settings/images \
  -H "Authorization: Bearer <admin-token>"
```

确认 `openclaw` 对应的 image 已经是文档里那条 ARM bootstrap image。

### 第 4 步：做模型 bootstrap

仍然按 `03-bootstrap-contract.md`：

1. upsert 一个 active normal model
2. upsert 一个 active secure model
3. `GET /api/v1/admin/models` 复核两条记录都 active

如果这一步没做完，会出现两个已知边界：

- 创建实例时报：`no active models are configured`
- direct QA 时报：`403 sensitive content requires an active secure model`

这不是 ARM 问题，是 operator input 缺失。

### 第 5 步：创建 1 个验证实例

严格按 `04-acceptance-path.md` 使用这组参数：

- `type=openclaw`
- `cpu_cores=2`
- `memory_gb=3`
- `disk_gb=20`
- `gpu_enabled=false`
- `gpu_count=0`
- `os_type=openclaw`
- `os_version=latest`

不要在这一步扩成多实例。

### 第 6 步：等到 runtime ready

继续按 `04-acceptance-path.md` 轮询，直到同时看到：

- `status=running`
- `infra_status=ready`
- `agent_status=online`
- `openclaw_status=running`

如果没同时到达这四个状态，就停在这里，不要往外扩 scope。

### 第 7 步：做 direct QA

为实例生成 access link 后，进入实例内的 OpenClaw 页面。

推荐最小验证：

1. 发送一个短 prompt，例如 `ping`
2. 收到正常回复

补充说明：

- 实例里的 OpenClaw UI 通常显示模型别名 `auto/auto`
- 真正的模型配置不在实例 UI 里改，而是在 Manager 的 `/admin/models`

## 可选扩展：手动 Skill 上传与挂载

这条链路已经打通，但它不属于 A1 默认 gate。

如果测试方要顺手验证手动 Skill：

1. 进入 `User` 界面
2. 打开 `OpenClaw Resource Management`
3. 上传一个符合要求的 Skill `.zip`
4. 再到实例页执行 attach

完整说明见：

- `docs/manual-skill-import.md`

这里要注意两点：

- scanner 默认没开，所以导入后 `scan_status=pending`、`risk_level=unknown` 是正常的
- 如果 attach 时看到 `skill md5 mismatch`，先按 Skill 包完整性问题处理，重新导出并重新上传 zip

## 测试结束后怎么回报

建议测试方只回 3 类结果：

1. `PASS`
   - deploy 成功
   - admin 可登录
   - 1 个实例达到 ready
   - direct QA 成功
2. `BLOCKED_BY_MODEL_BOOTSTRAP`
   - 模型输入没给全或模型记录没配好
3. `BLOCKED_BY_INSTANCE_MAINLINE`
   - 实例状态链没有到达 `running/ready/online/running`

不要把 scanner/FastSkill/U3 的结果混进这次 A1 签收结论。
