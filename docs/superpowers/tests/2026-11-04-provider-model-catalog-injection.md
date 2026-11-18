# 四类 Agent 的供应商模型目录注入修复

范围说明：本文的四类验证是完整模型目录的注入与解析，不是四类原生界面分组名称统一验收。后续 `ClawManager / icompify` 显示名及防合并修复只修改了 Hermes；OpenClaw、OpenCode、DSH 尚未同步统一分组显示名。

2026-11-04，测试节点 10.130.81.54，命名空间 `clawmanager-system`。用户报告 DSH 及其他类型仅显示 auto / icompify，并确认可以更新实例。测试和构建全部在测试节点执行。

## 根因与范围

`llm_models` 中一条启用的 icompify 配置保存了 10 个 `provider_models_json` 子模型。公共注入逻辑直接把配置行当作单个模型，没有展开已保存目录，因此所有共用该逻辑的 Runtime 都只收到 auto 和 icompify。

修复复用 `models.ExpandLLMModelCatalog`：先按实例所有者过滤模型权限，再展开持久化目录；保留父级供应商名称、授权记录 ID、模型组、凭据和传输配置。未启用其他配置、未修改权限、未把上游实时发现结果自动视为授权。

AI Gateway 的显式选模也展开同一份已保存目录；选择具体子模型后，部署/故障转移候选保留该实际模型名，避免悄悄回到供应商默认模型或其他兄弟模型。目录成员被移除、读取失败或没有合法候选时，在进入传统空列表回退前拒绝请求。Auto 路由和原供应商默认别名保持原行为。

## 自动化验证

测试节点运行带 race 的回归，普通构建与分钟级测试构建各通过 18 项，0 失败、0 跳过，覆盖：

- 公共供应商目录展开、正确供应商分组、私有供应商对不可见用户不注入。
- 指定 GLM 时保留实际 GLM 上游名称和父级授权 ID，不改为其他模型。
- 隐藏目录不能被显式选模访问，原 icompify 默认别名仍然可用。
- 模型在解析与分配之间被移出目录、或目录读取失败，不能通过旧选择快照继续调用。
- 现有用户权限、模型目录和 OpenCode 配置回归。

保留首次失败及两个撤销/读取失败边界的 red 日志；修正后完整定向回归通过。

## 集群验证

发布检查时全部 Lite 实例已依照 5 分钟空闲测试规则自然停止，没有强制中断运行任务。更新后临时启动 OpenClaw 5、Hermes 2、OpenCode 26、DSH 4：四类当前代次下发的目录均包含 auto + 10 个已保存模型。读取新代次环境时只输出模型元数据，不输出凭据或对话内容。

OpenClaw、Hermes、OpenCode 的实际生成配置均包含这 11 项；历史自定义条目保留，因此总配置数量可能更大。DSH 额外用安装在 Runtime 内的 llm-config 解析器验证这份目录可解析为 11 项。没有声称执行了完整浏览器点击或发送真实付费模型请求；显式路由通过测试节点的单元回归验证。

临时验证实例在没有新增模型活动的情况下通过原 CM API 恢复为停止状态。随后针对用户报告的 `lite-dsh-010`（45）执行启动，确认 Running，新代次下发如下列表：

- `auto`
- `deepseek-v4.1-flash`
- `glm-5.3`
- `glm-5.3-flash`
- `deepseek-v4-flash-02`
- `qwen3.8-27b`
- `deepseek-v4-pro`
- `deepseek-v4-flash`
- `kimi-k2.6`
- `jev-clef`
- `jev-intern`

构建服务已停止。已停止实例下次启动时读取新目录；没有离线覆盖它们的工作空间配置。保留已有 Runtime 事件流严格 10,000 条上限及实例每分钟扫描 / 空闲 5 分钟的测试规则。

## 镜像和证据

- 已部署测试镜像：`clawmanager.local/clawmanager@sha256:9aa1f46ba8e0e9ab8500373446bab26a74556f8792b3a4ecc8844c7e959ce3b8`。
- 已构建、未切换的普通日规则镜像：`clawmanager.local/clawmanager@sha256:03765053a04d0b2163f9cf854eba8cc16019d58ce8dafadb699750d57dd35fa7`。
- 节点目录 `/root/cm-pod-elastic-20261104`：`model-catalog-red.log`、`model-catalog-removal-red.log`、`model-catalog-failclosed-red.log`、`model-catalog-final-green.log`、`model-catalog-results/`、`model-catalog-cluster-verification.json`、`model-catalog-final-verification.json`。
- 含环境变量的更新前部署快照只保留在节点权限为 0600 的文件中。
