[← 返回 README](../README.zh-CN.md)

# AI Gateway 使用指南

AI Gateway 是 OpenClaw、Hermes、OpenCode、DeepSeek Harness、Team 与平台功能统一访问模型的受管入口。

## 使用前配置

在 **管理后台 → AI 网关 → 模型** 添加并启用至少一个普通模型。普通模型即可创建实例和生成自定义 Team；安全模型是可选项，只在风控规则需要敏感请求改道时使用。

## 五个模块

- **模型**：Provider、地址、协议、凭据、价格、启用状态、健康和 Thinking。
- **AI 审计**：请求、响应、路由、风控命中、延迟和错误。
- **成本**：Token 与配置价格形成的费用估算。
- **会话用量**：按 Runtime、实例和会话比较用量。
- **风控规则**：按顺序执行放行、阻断或路由到安全模型。

Thinking 是持久化模型设置，仅在可可靠控制的模型/协议上开放。开启后可能增加延迟和推理 Token，但不会展示私有思维链。

Runtime 侧支持 OpenAI Chat Completions、OpenAI Responses 和 Anthropic Messages。应选择与上游 Provider 匹配的协议，并在生产使用前验证流式输出和工具调用。

## 会话用量怎么使用

按时间、用户、Runtime、实例或会话筛选，在 Provider 有上报时比较输入、输出、缓存和推理 Token，并根据模型配置价格查看估算费用。找到异常会话后，进入对应实例查看用户可见上下文，再到 **AI 审计**核对请求级路由、错误和策略证据。

会话用量只是观测视图，不会修改会话或 Team 状态，也不是 Provider 最终账单。旧会话、中断会话或未实现用量上报的 Runtime 可能缺少字段；重试和不同 Token 分类方式也会让汇总值与 Provider 账单不同。

详见 [用户手册](./use_guide_cn.md) 与 [安全防护平台指南](./security-platform_cn.md)。
