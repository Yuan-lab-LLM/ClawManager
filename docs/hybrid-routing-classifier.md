# 规则优先 + Laya 补充分档

## 行为与启用

管理端支持选择并启用 `rules` / `hybrid` / `laya`，无需重启。模式持久化在数据库，优先于部署环境变量；尚未保存模式时，才使用 `CLAWMANAGER_ROUTING_CLASSIFIER` 作为兼容默认值（未设置即 rules）。保存连接配置、点击对照测试不会切换模式；只有明确点击启用/停用才会改变它。不修改模型配置、Virtual Key 权限或 Laya 的 v2 提示词。

迁移 `061_add_routing_classifier_mode.sql` 增加 `routing_mode`，默认空字符串以保留升级前行为，不自动启用 Laya。必须先将所有后端副本升级完成，再使用页面切换功能。

| 模式 | 分类流程 |
| --- | --- |
| 未设置 / rules | 原 content-cost-v4 本地规则 |
| laya | Laya 优先，异常回退原规则 |
| hybrid | 规则覆盖明确时直接采用，否则在输入合规时调用 Laya，异常回退原规则 |

分类只决定所需能力档位。之后仍由 Gateway 筛选权限、安全策略、工具/视觉能力、上下文容量、费用、健康与负载。显式选择模型不会触发分类器。

在「系统设置 → 自动路由分类器」操作：

1. 保存完整连接配置和密钥，建议先运行对照测试确认后端连通。
2. 在「启用自动路由模式」选择「规则优先 + Laya（推荐）」。
3. 勾选允许向已保存服务发送符合条件的用户任务文本及必要历史的确认项。
4. 点击「启用规则优先 + Laya」，看到 Hybrid Auto 已启用。
5. 如需关闭，点击「停用 Laya，切回本地规则」。关闭不要求连接配置有效，不需要发送确认。

各副本对每个新分类请求读取数据库中的模式与连接配置快照，没有长期模式缓存；模式读取限制为每副本最多 32 个并发，查询超时为 1 秒，繁忙或失败时本地规则兜底，不使用旧启用状态发送数据。已开始的分类可能按旧快照完成。页面显示实际返回的模式和来源；环境变量不再覆盖已保存的模式，因此不要用修改环境变量的方式关闭已在页面启用的 Laya。

未保存的地址、密钥或超时修改会阻止启用按钮，以免将表单误认为已生效配置。启用模式接口验证已保存凭据可解密、地址及超时有效，不自动进行模型推理，也不保证远端服务当时可用。连接配置加密要求见 [Laya 配置说明](laya-classifier-integration.md)。

## 不把默认档位当成成功识别

原规则总会给出 1～4 档，`score` 仅表示任务难度，不是分类可信度。本版另加 `RuleAssessment`，不会用 score 或 Laya 概率作为“是否相信”的阈值。

| status | 含义 | Hybrid 行为 |
| --- | --- | --- |
| matched | 完整匹配已覆盖的任务模式，且与原规则档位一致 | 读取已保存模式后直接采用原规则，不解密密钥、不占推理槽、不请求 Laya |
| ambiguous | 部分意图、附加任务、续问或输入不完整 | 输入合规时调用 Laya |
| unmatched | 没有适用模式或用户文本 | 输入合规时调用 Laya，否则本地兜底 |

第一版明确命中的模式刻意收窄：常见中英文问候、有限格式的整数二元算式，以及指定语言的排序、去重、求和、闰年判断、字符串反转、二分查找等小函数请求。不是所有带“Python”“函数”的文本都直接放行。

例如：

- `帮我写一个 Python 排序函数`：规则 2 档，`small_function_v1` 明确命中，不调用 Laya。
- `帮我写一个 Python 排序函数，然后证明正确性`：复合任务，需要进一步分类。
- `帮我写一个 Python 排序函数，支持分布式处理和零停机迁移`：未完整匹配简单模式，不直接按 2 档放行。
- `继续`：需要上下文，交给适配器进一步处理。
- 没有匹配意图的文本：原规则即使返回 1 档，仍算未命中，可以触发 Laya。

判断使用最新原始用户文本做完整匹配，不使用已剥离代码/引用后的文本放行，以免漏掉额外要求。历史仍按原有用户轮次预算传递；如包含非文本、截断或超出 Laya 保守输入预算，则不发送。此处保守覆盖不代表已经证明某模式对任意文本均准确；扩大模式必须配套正例和近似反例测试。当前不开放任意正则在线编辑。

安全策略强制本地时，无论覆盖状态如何都不调用 Laya。Laya 配置缺失、401、损坏响应、超时、并发繁忙等沿用原有可审计回退。调用者取消请求直接终止，不继续发起回答。低分布集中度本身不触发回退，也不假定 Laya 必然比规则更准确。

## 管理端与审计

手动对照保持原语义：**仍尝试调用规则和 Laya 两者**，以便观察差异；额外返回 `hybrid` 预览，不新增第二次 Laya 调用。即使 Hybrid 会走规则，这次手动对照也会尝试 Laya。页面会明确说明这一点。

预览显示规则覆盖状态、原因、规则 ID（规则卡片）、是否需要 Laya、最终采用来源和档位、失败回退原因、复用此次测量得到的分类路径耗时。它只模拟这段测试文本的分类，不模拟真实请求安全策略、对话历史或最终模型筛选。`would_call_laya` 表示该文本需要模型且通过输入预检，不是“HTTP 已发送”的计数；缺失配置、限流等仍可能在发包前阻止调用。

新增接口字段：

- `GET/PUT` 设置返回 `mode: hybrid_auto`。
- `PUT /api/v1/system-settings/routing-classifier/mode`（仅管理员）：`{"mode":"hybrid","confirm_external":true}`；停用为 `{"mode":"rules"}`。启用外部分类必须确认，缺少完整已保存连接配置会拒绝。
- 设置返回 `routing_mode`（rules / hybrid / laya）及 `mode_source`（saved / deployment_default）。模式更新与连接更新使用独立字段 SQL，避免跨副本保存覆盖另一类配置。
- `compare.rule.classification.rule_assessment`: `status`、`reason`、`rule_ids`。
- `compare.hybrid`: `rule_assessment`、`would_call_laya`、`source`（rules / laya / rules_fallback）、`required_tier`、可选 `fallback_reason`、`duration_ms`。

真实路由决策审计使用：

- `policy: hybrid-classifier-v1`，`requested_classifier: hybrid`。
- `classifier: heuristic` 表示规则；`classifier: laya` 表示 Laya。
- `rule_assessment` 记录覆盖状态和原因，不记录用户原文。
- `classifier_fallback` 仅在异常或策略回退时出现；规则明确命中不是异常回退。
- `classification_duration_ms` 记录本次分类耗时；它不是聊天模型回答耗时。
- 模式读取失败时 `requested_classifier: dynamic`，`classifier_fallback: storage_error`，表示无法确认当前启用模式而保持本地；数据库中的非法模式同样不外发。

这轮没有调用远端 Laya、修改当前 K8s 或宣称混合方案准确率提高。验收需针对规则拒绝独立判断的子集评测 Laya，同时统计覆盖率、直接命中的错误率、子集增益、回退率及额外延迟，不能直接套用整批数据的一致率。

## 回归

```powershell
go -C backend test ./internal/aigateway -run 'Test(Hybrid|LayaLive|RoutingClassifier)' -count=1
```

前端目录执行 `node --test scripts/routing-classifier-settings.test.mjs`。覆盖明确命中不调用、未命中使用 Laya、手动模式不变、安全/输入限制、缺失配置、并发、无效响应、超时、取消、选模审计与预览等路径。
