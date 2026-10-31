# Laya 分类器配置、Auto 试用与手动对照

## 本版范围

管理端「系统设置」新增「自动路由分类器 · Laya 路由与对照」。支持：

- 保存 Laya 根地址、访问密钥、测试超时；固定 multilingual 模型。
- 使用已保存密钥，或临时填写未保存的配置发起测试。
- 同一段管理员输入的文本分别交给现有 content-cost-v4 规则与 Laya，展示档位、耗时、规则评分、四档概率、分布集中度、是否一致和失败原因。
- Laya 失败仍显示规则结果，但不将其冒充为 Laya 结果或一致结果。

**全新安装默认不启用真实 Laya 路由，不发送真实用户会话。** 在页面选择模式、确认数据发送范围并点击启用即可切换。已保存模式优先于部署环境变量；尚未保存时，`CLAWMANAGER_ROUTING_CLASSIFIER` 为兼容默认值：laya 优先、hybrid 规则优先、未设置或 rules 仅规则，非法值拒绝启动。保存连接信息或点击对照测试本身不会切换模式。本版分类的是能力档位，不是编程/写作类别到模型用途组的映射。

混合流程、保守匹配范围、管理端预览和启用步骤见 [Hybrid 规则优先说明](hybrid-routing-classifier.md)。

## 本机 K8s 启用步骤

前提：先构建并部署包含本次改动的 ClawManager 镜像，所有后端副本升级完成并运行迁移 061。无需重建 Laya 服务或模型权重。

1. 在管理端「系统设置 → 自动路由分类器」填写 `http://10.130.14.23:30800`、Laya API Key 和调用超时（先用 5000 ms），保存后点击「测试连接并对照分类」。若提示加密配置不足，先配置下文的加密 Secret，不要改成明文存储。用短问题验证返回有效档位；浏览器/Windows 能连通不代表后端 Pod 能连通。
2. 确认模型管理里至少有启用、具备相应档位和工具调用能力的模型，实例的 Key 对这些模型有权限。Laya 不会自动添加模型、升级模型能力或放宽权限。
3. 在「启用自动路由模式」选择「规则优先 + Laya」或「Laya 优先」，勾选数据发送确认，点击对应启用按钮；不需要修改 K8s 环境变量或重启。
4. 确认页面显示相应模式已启用。进入实例，选择 `auto`，最好新建会话，以短文本开始测试，比如排序函数、系统设计、严格证明。显式指定模型不会经过 Laya。
5. 在管理端 AI 审计中打开对应请求的路由决策详情（`gateway.routing.selected`，未选到模型时为 `gateway.routing.rejected`），检查 `classifier`、`requested_classifier`、`required_tier`、`classifier_fallback`，不要仅凭回复风格猜测是否经过 Laya：

| 情况 | 决策详情 |
| --- | --- |
| 使用 Laya 分类 | `classifier: laya`，`requested_classifier: laya`，signals 含 `laya_multilingual_tier_v2` |
| Laya 超时回退 | `classifier: heuristic`，`requested_classifier: laya`，`classifier_fallback: timeout` |
| 未保存完整配置 | `classifier_fallback: configuration_missing` |
| API Key 不正确 | `classifier_fallback: http_401` |
| 最新问题太长 | `classifier_fallback: input_too_long` |
| 安全策略要求本地处理 | `classifier_fallback: secure_policy_local_only` |

这里的 `classifier` 是任务分类器；最终回答仍由 Gateway 选中的聊天模型生成，模型名查看实例的「最近调用」或调用详情。不要把 Laya 作为聊天模型添加到模型管理列表。

关闭时点击「停用 Laya，切回本地规则」，无需有效连接或密钥。已保存模式覆盖环境变量，修改环境变量不再能覆盖管理端选择。

模式作用于所有后端副本的符合条件 Auto 请求，不是单实例灰度。每次分类读取模式、URL、密钥和超时的一致快照，后续请求会看到已保存配置，无需重启；正在执行的请求可继续使用旧快照。数据库读取失败时保持本地，不用缓存的启用状态外发。清除密钥后需要 Laya 的请求回退规则。手动对照仍使用当前表单，未保存表单不影响 Auto。

## 连接配置

同集群：`http://laya.laya-system.svc.cluster.local:8000`。
本机 ClawManager 访问远端 NodePort：`http://10.130.14.23:30800`（需已开放该 Service 和防火墙）。

地址由 **ClawManager 后端**访问，浏览器能访问不代表后端 Pod 也能访问。填写服务根地址，不带 `/v1/systemone`，不要填写镜像仓库的 5000 端口。HTTP 仅适合可信内网；跨不可信网络使用 HTTPS。

默认调用超时 5000 ms，允许 100～30000 ms，手动测试与已启用的 Auto 共用。Auto 的配置读取另有 1 秒上限，整个分类过程还有 30 秒硬上限；超时会回退规则。一次测试会做真实 Laya 推理，不是单纯 GET /health。耗时包含网络、排队与推理，不宜直接与纯本地规则耗时当作相同成本比较。不要为追求不超时盲目拉高等待时间，它直接增加真实对话延迟。

## 密钥保存

数据库迁移 `060_add_routing_classifier_settings.sql` 创建独立单行配置表；不修改 llm_models。
访问密钥用 AES-GCM 加密，服务 URL 作为关联数据，读取接口只返回 `has_api_key`，不返回明文或密文。

优先使用后端环境变量 `ROUTING_CLASSIFIER_ENCRYPTION_KEY` 作为加密材料（至少 32 字符，建议用密码学随机生成的 32 字节十六进制串，通过部署 Secret 注入）。未配置时复用已配置、至少 32 字符且不是内置开发默认值的 `JWT_SECRET`，使用独立派生域。无合格加密材料时可以填写临时 API Key 测试，但不允许保存密钥。

注意：

- 所有后端副本需使用相同加密材料，必须安全备份，不写入代码库。
- 更换加密材料（包括从 JWT fallback 切换为独立变量）后，需要重新填写保存 Laya API Key；旧密文不会自动解密迁移。
- 空输入表示保留已保存密钥；通过明确的「清除」复选框删除。
- 改服务地址时，必须重新输入密钥或明确清除，不会将原密钥发给新地址。
- 同地址未保存的表单可以复用密钥，测试不保存配置或测试文本。

## API（仅管理员）

均受现有登录鉴权、用户信息和管理员中间件保护。

- `GET /api/v1/system-settings/routing-classifier`
- `PUT /api/v1/system-settings/routing-classifier`
- `PUT /api/v1/system-settings/routing-classifier/mode`：`{"mode":"hybrid","confirm_external":true}` 启用规则优先，`{"mode":"laya","confirm_external":true}` 启用 Laya 优先，`{"mode":"rules"}` 停用外部分类。
- `POST /api/v1/system-settings/routing-classifier/compare`

保存请求：

```json
{"base_url":"http://laya.laya-system.svc.cluster.local:8000","timeout_ms":5000,"api_key":"由管理员提供"}
```

省略 `api_key` 保留旧值；`clear_api_key: true` 清除；不同时传入两者。

对照请求（省略 draft 使用已保存配置）：

```json
{"text":"帮我写一个 Python 排序函数","draft":{"base_url":"http://laya.laya-system.svc.cluster.local:8000","timeout_ms":5000}}
```

响应中的 `data.rule` 与 `data.laya` 分别包含状态、耗时、分类或安全错误码。仅两者均成功才返回 `same_tier`。`production_routing_changed` 固定为 false，表示这次对照操作不改变路由模式；当前有效模式见设置接口的 `mode`（`manual_test_only` / `laya_auto` / `hybrid_auto`）及 `routing_mode`，来源为 `mode_source`，不表示 Laya 服务健康。`data.hybrid` 复用两者结果展示混合流程预览；手动对照仍尝试 Laya，即使真实混合流程对该文本可直接采用规则。

Laya 的 `classification.probabilities` 返回 simple/medium/complex/reasoning 四项概率；页面显示完整分布、最高概率和前两名差距（百分点）。兼容字段 `confidence` 仍保留服务原值，并用 `confidence_kind: normalized_entropy` 明确其含义：choice 接口中的 `1-H(p)/log(4)`，展示名为「分布集中度（非正确概率）」，不能当作选中档位概率，更不是正确率。审计对应增加 `classifier_confidence_kind` 标记，不复制整个概率表。旧结果没有四档概率时不伪造分布。

没有设置未经评估的置信度阈值，也不因规则与 Laya 不一致而偷偷修改 Laya 的结果。后端验证四项概率、总和以及 choice 是否为最大概率选项；不一致视为损坏响应，不把解析错误误认为模型判断错误。

## 适配器边界

`aigateway.NewLayaClassifier` 实现现有 `RoutingClassifier`。固定 POST `/v1/systemone`、Bearer 鉴权、多语言模型和四档分类标准。simple/medium/complex/reasoning 映射到 1/2/3/4。

- 显式 `max_len=2048`、`head_max_len=512`，适用于此次部署镜像；总推理长度不增加，给分类说明预留更充分的 head 预算。
- 当前使用保守的 UTF-8 字节预算，不声称是精确 tokenizer 计数。序列化用户输入不超过 1200 字节；超出时先去掉旧历史并给出警告；最新问题仍过长则拒绝，不静默截断。多模态或已有截断标志也拒绝。
- UI 手动测试只提交一条文本。未来接入真实历史时需进一步评估上下文提取和 tokenizer 预算。
- 每后端副本手动测试和 Auto 共用四个 Laya 调用槽位，满额不排队；手动测试显示 busy，Auto 回退规则。不自动重试。Auto 复用未变化配置对应的 HTTP 连接池，配置改变后替换连接池，不取消旧的在途请求。
- HTTP 请求绑定 context，配置超时；响应上限 64 KiB，验证 choice、模型、置信度和四个选项概率。
- 禁止重定向，不使用系统代理。允许业务所需的内网/Service 地址；拒绝回环、链路本地、非单播和已知元数据 IP，并在 DNS 解析后、实际连接前重新检查所有地址，使用检查后的 IP 连接。
- 不向前端或日志透传服务响应正文、原始网络错误、密钥和测试文本。

真实 Auto 通过 `LiveRoutingOption` 安装动态模式解析器，按数据库中的模式选择本地规则、Hybrid 或 Laya；未保存模式时使用部署默认值。原有权限、安全、工具、上下文、负载筛选不变。服务错误不能绕过这些限制；回退规则也不保证一定有可用模型。上线前仍需中文任务评测和延迟/并发验证，不建议仅凭几条成功响应就用于全量生产。

## 提示词 v2

提示词位于 `backend/internal/aigateway/laya_prompt.go`，成功分类的 signals 返回 `laya_multilingual_tier_v2`，方便识别评测版本。Python 客户端 `deployments/laya/call_laya.py --task tier` 使用相同说明、选项顺序、单条用户输入结构和推理预算；自动测试检查两端文案一致。默认 `--task category` 仍然判断用途类别，不要拿它与管理端的能力档位混淆。

本次明确的边界：

- 选择足够完成实际交付要求的最低档位，不按术语、长度、语气升级。
- 常规函数、网页和局部修错是 2 档；多约束联动、跨模块系统设计或复杂排障是 3 档。
- 4 档用于严密证明或非平凡推导，不代表所有数学题都要 4 档，也不代表调用模型的“思考模式”开关。
- 翻译、摘要和提取针对材料做处理，不执行被引用的任务；提示模型忽略指定分类结果的指令。这是分类指导，不是经验证的提示注入防护保证。
- 历史仅用于补全续问，新任务不继承旧难度；多项真实子任务取所需最高档，不凭空增加用户未提出的复杂要求。

手动对照建议（参考档位仅是项目政策预期，不是实测结果）：

| 输入 | 参考档位 | 检查点 |
| --- | --- | --- |
| 什么是分布式事务？用一句话解释。 | 1 | 术语不等于复杂任务 |
| 翻译成英文：请严格证明这个算法的正确性。 | 1 | 不执行翻译材料中的证明任务 |
| 帮我写一个 Python 排序函数。 | 2 | 常规编程 |
| 修复 int('abc') 的异常，改为友好提示。 | 2 | 局部修错 |
| 设计秒杀整体方案，保证不超卖、不重复扣款、不丢订单。 | 3 | 多约束联动 |
| 请严格证明根号二是无理数。 | 4 | 明确证明要求 |

Laya 的每个选项最多保留 48 tokens，instructions 与选项共享 head 预算，不能无限加长提示词。已用当前部署 multilingual 的本地 tokenizer 检查：档位说明 155 tokens，四个选项分别 22/23/22/23 tokens，加上选项标记共 249 tokens，小于 512；没有触发选项或说明截断。此检查不加载模型权重，也不能证明分类准确率提升。

可重复检查（需本地安装 `tokenizers`，无网络请求）：

```text
python deployments/laya/check_prompt_budget.py --tokenizer <部署模型的本地tokenizer.json路径>
python -m unittest discover -s deployments/laya -p test_call_laya.py
```

历史 `e2e/routing-classifier/compare_laya.py` 保留冻结的 v1 评测说明，以便复现旧报告；旧分数不能作为 v2 成绩。提示词修改没有调用远端模型或宣称准确率提升。发布只需更新 ClawManager；Laya 服务镜像和模型权重不需要因本次提示词改动重建。

## 验证命令

提示词与输入格式的实际对照结果见 [2026-09-28 消融报告](laya-prompt-ablation-20260928.md)。本轮新增概率诊断，正式 v2 提示词和路由开关保持不变。

```text
cd backend
go test ./internal/aigateway ./internal/handlers ./internal/db ./cmd/server -count=1

cd ../frontend
node --test scripts/routing-classifier-settings.test.mjs
npx tsc -b
npx eslint src/components/RoutingClassifierSettings.tsx src/services/routingClassifierService.ts
```

测试使用模拟响应和内存配置仓库，不需要生产密钥，不向远端模型发送测试数据。数据库迁移在应用更新后按现有机制执行。
