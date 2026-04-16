# A1 Live 演示记录

这份记录只描述当前机器上已经观察到的 live 事实，给外部测试方一个“现在这包是怎么被跑通的”参考基线。

## Live 基线

- 日期：`2026-04-16`
- 基础地址：`https://localhost:30443`
- 管理员：`admin / admin123`
- 当前实例：`id=1`
- 当前实例 Pod：`clawreef-1-a1-arm64-132858`
- 当前实例命名空间：`clawmanager-user-1`

## 已观察到的主链状态

### 1. control plane 健康

当前机器上：

```text
GET /healthz -> ok
```

对应归档证据：

- `evidence/live/a1-package-healthz.txt`

### 2. 模型 bootstrap 已到位

当前 live 管理端里已经存在两条 active 记录：

- `A1 normal model`
- `A1 secure model`

两条记录当前都指向同一个 operator-configured OpenAI-compatible provider model：

- `gpt-5-mini`

对应归档证据：

- `evidence/live/a1-package-models.json`

### 3. 单实例主链已到位

当前 live 实例 `id=1` 已达到：

- `status=running`
- `infra_status=ready`
- `agent_status=online`
- `openclaw_status=running`

对应归档证据：

- `evidence/live/a1-package-instances-raw.json`
- `evidence/live/a1-package-runtime-raw.json`

### 4. direct QA 已在实例内成功过

当前实例内的 OpenClaw 聊天页已经能够返回回复。  
这说明 A1 的 control plane -> gateway -> model bootstrap -> instance chat 这条最小链路已经成立。

补充说明：

- 实例 UI 显示的模型通常是别名 `auto/auto`
- 真正的上游模型配置在 Manager 的 `/admin/models`
- 实例只是通过 gateway 使用这条配置，不在实例 UI 中直接暴露 provider 细节

## 手动 Skill 链路的 live 观察

这次 live 环境里，手动 Skill 路径也已经被证明能走通：

- 手动上传的 Skill 会进入平台，`source_type=uploaded`
- 在 scanner 默认关闭时，`scan_status=pending`、`risk_level=unknown` 属于预期行为
- attach 到实例后，平台会下发 `install_skill` 命令
- 当前实例里至少有 1 条 `install_skill` 成功记录

从当前 runtime 汇总里还能看到：

- `skill_count=1`

对应归档证据：

- `evidence/live/a1-package-skills-raw.json`
- `evidence/live/a1-package-runtime-raw.json`

## 对测试方最重要的口径

这份交付包可以诚实宣称的只有两件事：

1. A1 core-chain 已可复现
2. 手动 Skill 上传/挂载链路可作为可选扩展验证

这份交付包不宣称：

- scanner 默认可用
- FastSkill 自动发现可用
- frozen `U3` 已签收
- `/api/v1/admin/skills` 必然非空
