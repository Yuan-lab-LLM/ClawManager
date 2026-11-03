# 可替换路由分类器

## 本次范围

Gateway 的 Auto 内容路由通过 `RoutingClassifier` 接收能力档位建议。默认运行本地 `content-cost-v4` 规则（已补充口语、续问和业务约束识别）；模型权限、安全策略、工具与视觉能力、上下文容量、费用和健康/负载选择仍由 Gateway 负责。管理员可通过页面明确启用 Laya，默认关闭。

生产路由在服务初始化时注册适配器及动态模式解析器。已有 Laya HTTP 适配器、管理员连接配置、模式启停及手动分类对照入口；环境变量仅作为尚未保存模式时的默认值，见 [Laya 分类器配置与 Auto 试用](laya-classifier-integration.md)。保存连接配置和点击对照测试不会切换模式，也不是动态插件加载器。

## 接入方式

管理端现可启用 hybrid，由完整任务模式评估规则覆盖，只将未明确解决且输入合规的任务交给 Laya。规则难度评分不充当可信度；默认档位不视为明确命中。已保存模式每请求读取、优先于环境变量，无需重启；未保存模式时仍兼容原部署默认值。保留 rules / laya 算法行为，详情见 [混合分类器](hybrid-routing-classifier.md)。

接口在 `backend/internal/aigateway/routing_classifier.go`：

```go
type RoutingClassifier interface {
    Classify(context.Context, RoutingClassificationInput) (RoutingClassification, error)
}
```

在 `aigateway.NewService` 的可选参数中注册：

```go
aigateway.WithRoutingClassifier("my-classifier-v1", adapter, 500*time.Millisecond)
```

`adapter` 必须实现该接口。省略参数或传入 nil 使用本地默认规则。超时非正数默认 2 秒；注册名称应使用至多 96 字符的 ASCII 标识符（字母、数字、下划线、连字符、点或冒号），不合规名称使用 `custom`。名称 `heuristic` 保留给默认规则。

已有规则也有公开实现 `RuleRoutingClassifier`，可供组合式分类器调用。它是同步本地计算，不发网络请求。

## 输入和输出

- 输入只有最近至多 8 条用户消息的文本，按时间顺序排列；最新任务在末尾。关联判断由分类器负责，默认规则仍只在识别到续问时向前回看。
- 每条文字超过 16,000 个 Unicode 字符时保留前 12,000 和后 4,000 个字符，中间用换行隔开，并设置 `Truncated`。这是字符上限，不是 token 上限；小模型适配器还需自行预算 tokenizer 长度及分类问题的开销。
- 系统/助手消息、独立工具消息不传入；非文本块只保留 `HasNonText` 标志，不传原始图片。不会传入请求头、网关 Key、供应商凭证、模型记录或权限字段。用户主动粘贴在文字里的秘密仍属于文字，本接口不提供通用脱敏保证。
- 输入是脱离原请求的值结构；适配器修改它不能修改待转发请求或本地兜底结果。
- 输出 `RequiredTier` 必须是 1–4。`Score` 是可选评分，`Confidence` 是可选置信度，两者不能混为一谈；提供时必须是有限的 0–1 值。未提供 Score 时，为兼容旧审计结构，审计 `score` 为 0，不代表模型判断“零难度”。
- `Signals` 和 `Warnings` 每组最多 32 个，单项为至多 96 字符的 ASCII 标识符；不要返回原始提问、模型解释、凭证或任意错误文本。适配器是受信任代码，标识符校验不是秘密扫描。
- 当前契约只影响能力档位，没有新增“编程/写作”等类别到模型用途组的自动映射。

## 失败、安全和并发

1. 自定义分类器出错、panic、超过时间预算或输出不合法时，退回本地规则；不会因为分类服务不可用而直接拒绝正常对话。
2. 每个 Gateway service 最多 32 个未完成分类调用，达到上限立即回退，不排队。即使适配器忽略 context，外层等待仍受超时控制，未退出的 worker 继续占用槽位，避免无限创建 goroutine。
3. Go 不能强制终止任意 goroutine；适配器必须尊重 context，尤其 HTTP 请求要绑定调用 context，避免超时后仍消耗资源。极端情况下 32 个永久挂起的适配器调用会使该 service 持续使用规则兜底。
4. 用户请求自身取消或截止时，返回 context 错误，不把它当作分类失败继续选模型。
5. 显式指定模型不调用分类器；风险阻断/审批也不调用。被安全策略强制要求安全模型的请求，仅使用本地规则，并记录 `secure_policy_local_only`。即便将来接入内网分类器，本版也不会自动放开这一限制。
6. 分类器只能建议档位，不能授予模型访问权限、更改安全属性或放开工具/上下文限制。后续“没有足够档位时使用最高可用档位”的原有行为保持不变。
7. 注册只在 service 初始化时进行；适配器必须支持并发调用，并在返回后不再修改返回值里的切片和指针。不支持运行中修改注册或自动关闭适配器资源。

## 审计

已有 `gateway.routing.selected` / `gateway.routing.rejected` 决策详情增加：

- `classifier`：实际使用的分类器，默认 `heuristic`。
- `requested_classifier`：注册的分类器名称（如有）。
- `classifier_fallback`：`error`、`panic`、`timeout`、`busy`、`invalid_result` 或 `secure_policy_local_only`。
- Laya 额外记录白名单内的故障码，如 `configuration_missing`、`http_401`、`connection_failed`、`input_too_long`；其他未知错误仍只记 `error`，不透传原文。
- `classifier_confidence`：适配器提供且验证通过的置信度（可选）。

默认规则使用 `policy: content-cost-v4`（旧记录仍可能是 v3）；自定义成功使用 `pluggable-classifier-v1`。失败时不把适配器原始错误写入路由审计，避免 URL、凭证或提问意外进入错误详情。这些新增字段暂未制作单独的管理端展示。

## Laya：默认关闭，可显式试用

研究对象是 [NandhaKishorM/laya](https://github.com/NandhaKishorM/laya)。它提供类型化决策和自托管 HTTP 服务，可作为独立进程或 K8s 内网 Service，再由 Go 适配器调用；不必把 Python 推理依赖装进 ClawManager 主镜像。项目提供多语言权重，但在本项目中文任务上的效果仍需评测，不能将项目宣传的延迟和其他任务准确率当作我们的保证。

独立服务打包、手动适配和部署级 Auto 试用开关已实现，具体边界见上述专门文档。正式路由的评估与接入要点如下（不是准确率或生产可用性保证）：

1. 固定服务代码和权重版本，使用独立 Deployment，限制网络访问和资源，预加载权重。
2. 将精简后的当前任务及必要历史作为 `state`，定义一个能力档位的 `choice` 问题，选项为 simple/medium/complex/reasoning，并提供具体判断标准。
3. 验证返回结构，把 choice 映射到 1–4；概率若存在则映射到 Confidence。未知 choice、损坏返回或低于经过评测确定的阈值时，适配器返回错误，交给规则兜底。
4. 为服务做鉴权、请求/响应体上限、连接与调用超时；不得通过请求参数任意指定服务 URL，也不能递归调用 Gateway 的 auto 路由。
5. 按所选版本和 checkpoint 的实际 tokenizer/上下文限制压缩输入，避免静默截断当前问题；非文本任务不能仅凭文本分类声称理解了图片。
6. 先用中文和多轮任务标注集离线评测，再做不影响正式选模的对照评估；必要时微调。单个高置信度不等于分类正确。

默认模式不启动 Laya 服务、不发送真实会话，也不自动修改 K8s。仅点击手动测试时发送填写的文本；明确启用模式后，符合条件的 Auto 用户任务文本和必要历史也会发送给已保存的 Laya 服务。用户文本不提供通用脱敏保证，启用前需确认数据传输范围。

后续已单独完成一轮本地合成中文对照评测，见 [Laya 中文路由对照测试](laya-routing-evaluation-20260924.md)。该实验下载了多语言权重，但没有安装 Gateway 适配器或启用正式路由。

## 回归测试

```text
cd backend
go test ./internal/aigateway -run 'Test(RoutingClassifier|RoutingDifficulty|ClassifyRouting|RoutingIgnores|ContentRouting|PrepareContent|PrepareExplicit|ProviderPermission)' -count=1
```

覆盖默认规则保持一致、自定义选模与审计、输入隔离、异常/非法值/超时/并发占满兜底、请求取消，以及显式模型、安全策略、Key 权限、工具能力和上下文限制。
