# 模型思考开关

管理端的“模型思考”控制 Gateway 发给上游的请求参数，不是隐藏前端思考文本，也不赋予模型新的能力。

## 配置

在模型路由规划表中，逐模型选择“思考参数格式”，再勾选开启或取消勾选关闭，点击底部保存。

| 格式 | 开启 / 关闭的上游参数 | 使用条件 |
| --- | --- | --- |
| 自动（保持原有行为） | 保留旧逻辑，仅自动识别 DeepSeek 官方 OpenAI-compatible 接口 | 不改变已有模型配置 |
| 跟随供应商 / 客户端 | 不强制注入开关，保留原始请求参数 | 不确定接口格式、或模型仅支持思考时使用 |
| thinking.type · Kimi / GLM | `thinking.type = enabled / disabled` | 上游明确支持此开关的混合思考模型 |
| thinking.type · DeepSeek | 同上，另归一化 DeepSeek 的输出上限和思考强度参数 | 官方或确认兼容的聚合接口 |
| enable_thinking · Qwen | 顶层 `enable_thinking = true / false` | 支持此扩展的 Qwen 托管接口 |
| chat_template_kwargs · vLLM | `chat_template_kwargs.enable_thinking = true / false` | 使用支持此参数的聊天模板和推理服务版本 |

聚合站不保证遵循模型原厂参数。请按站点文档选择格式，不要仅根据模型名称判断。新增格式仅适用于 OpenAI / OpenAI-compatible **上游**；管理端选用 Anthropic 上游时不提供这些格式。

Gateway 的 Chat Completions、Responses、Messages 入口统一在解析出实际部署后应用其配置。Messages 的 `thinking` 和 Responses 的 `reasoning.effort` 会保留用户的关闭意图。开启是允许使用思考的上限，客户端仍可关闭；托管 OpenClaw 沿用旧约束，需在请求中显式开启。多个参数冲突时以关闭为准。

“仅思考”型号无法通过通用开关变成非思考模型。GLM-5.3 等仅思考型号，以及 Qwen 的仅思考版本，不应配置为可关闭。适配器不发起付费探测，也不声称已验证聚合站真实响应。

## 兼容与验证

- 新字段 `reasoning_control_mode` 由迁移 059 增加，默认空字符串；不会把旧聚合模型自动关闭。
- API 省略字段保留已有设置；传空字符串恢复自动，传 `none` 跟随上游。
- 开关及格式逐模型保存；刷新发现结果保留手动选择，取消编辑恢复快照。
- 布尔参数发送在 HTTP JSON 顶层，不发送 Python SDK 的 `extra_body` 包装。
- 测试覆盖参数映射、管理员关闭优先、客户端关闭、混合参数冲突、默认透传、保存与批量保存、迁移和协议入口转换。未对用户真实 API Key 发起测试。

## 流式思考与故障切换（2026-09-23）

- 当前 OpenCode 使用的 OpenAI-compatible 流：非空字符串 `delta.reasoning_content` / `delta.reasoning` 与正文、工具调用一样视为有效输出，立即释放前置缓冲。`reasoning` 会补充为客户端可消费的 `reasoning_content`，不混入最终回答正文。
- 首个有效输出耗时 TTFT 包含思考；修复前的历史指标不回算。供应商不返回思考内容时，不会构造或伪造思考文本；客户端是否展开仍由其自身界面控制。本次没有修改 OpenCode UI，也未新增 Responses/原生 Anthropic 的思考输出协议映射。
- 256 KiB 上限仍保护纯心跳/空前奏；有效内容在写入前释放缓冲，长思考以及单个大片段不会误触发该上限。内容已输出后，断流向客户端报错，不把另一个模型的内容拼入当前回答。
- 模型提示区将失败尝试与请求最终失败分开。结构化 failover 事件按实例、request ID 和尝试序号关联，显示“已切换模型，等待结果”，不套用失败模型的耗时。终止事件结束重试状态；超过 10 分钟未见结果时显示“结果暂未确认”，该时间仅是展示防陈旧阈值，不是供应商请求超时。
- 使用本地模拟 SSE 测试，不调用用户付费模型。修复减少不必要的缓冲和重试，不保证模型本身生成速度变快。

## 官方参考资料

- [DeepSeek 思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)
- [Qwen / 百炼深度思考](https://www.alibabacloud.com/help/tc/model-studio/deep-thinking)
- [Qwen vLLM 部署](https://qwen.readthedocs.io/en/latest/deployment/vllm.html)
- [Kimi K2.6](https://platform.kimi.ai/docs/guide/kimi-k2-6-quickstart)
- [GLM 思考模式](https://docs.z.ai/guides/capabilities/thinking)
