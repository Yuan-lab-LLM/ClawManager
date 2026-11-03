package aigateway

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Intent and subject must agree: mentioning "distributed" or "proof" is not
// itself a difficult task. These rules are versioned heuristics, not a model
// confidence score. Never use their result for authorization or risk policy.
var (
	routingTechnicalSubject = regexp.MustCompile(`(?i)\b(code|coding|function|sql|python|javascript|typescript|golang|algorithm|api|script|bug|test|tests)\b|代码|编程|函数|算法|脚本|接口|程序|单元测试|报错`)
	routingTechnicalAction  = regexp.MustCompile(`(?i)\b(write|implement|debug|fix|refactor|optimize|review|test|explain)\b|写|编写|实现|调试|修复|重构|优化|审查|测试|解释`)
	routingDebugAction      = regexp.MustCompile(`(?i)\b(debug|refactor)\b|排查|调试|修复.{0,12}(报错|故障|漏洞)`)
	routingComplexSubject   = regexp.MustCompile(`(?i)\b(architecture|distributed|concurrency|deadlock|migration|trade-?offs?|root cause|consistency|multi-step|multi-tenant|tenant isolation)\b|架构|分布式|并发|死锁|迁移|权衡|根因|一致性|多步骤|多步推理|多租户|租户隔离`)
	routingComplexAction    = regexp.MustCompile(`(?i)\b(design|implement|analy[sz]e|diagnose|investigate|evaluate|compare|plan|resolve|debug|fix|why)\b|设计|实现|分析|排查|定位|评估|比较|对比|制定|规划|解决|修复|为什么`)
	routingFormalAction     = regexp.MustCompile(`(?i)\b(prove|derive)\b|证明|推导|严密推理|论证`)
	routingFormalSubject    = regexp.MustCompile(`(?i)\b(theorem|lemma|induction|convergence|correctness|complexity|equation|formula|inequality|optimality|invariant|axioms?)\b|定理|引理|命题|数学归纳|数学论证|公理|不变式|连通图|任意实数|恒成立|收敛|正确性|复杂度|公式|方程|不等式|最优|充分必要|无理数|质数|素数|偶数|奇数`)
	routingFormalRequest    = regexp.MustCompile(`(?i)^(请|请你|帮我|请帮我|严格|严密|please\s+)?(证明|推导|prove\b|derive\b)`)
	routingDocumentProof    = regexp.MustCompile(`(?i)证明(文件|材料|书|信|开具)|开具.{0,8}证明|\bproof of (address|income|employment|identity)\b`)
	routingDefinition       = regexp.MustCompile(`(?i)^(什么是|什么叫|what (is|are)\b|define\b)|是什么[？?。.!\s]*$|什么意思[？?。.!\s]*$`)
	routingCauseQuestion    = regexp.MustCompile(`(?i)\bwhat (is|are) (causing|the (root )?causes?)\b|根因是什么|死锁原因是什么`)
	routingGeneralAction    = regexp.MustCompile(`(?i)\b(write|draft|compare|analy[sz]e|evaluate|plan|design)\b|写|撰写|起草|比较|对比|分析|评估|制定|规划|设计|论证`)
	routingTransform        = regexp.MustCompile(`(?i)^(请你|请|帮我|请帮我|please\s+)?(翻译|翻成|摘要|总结|概括|提取|改写|润色|translate\b|summari[sz]e\b|extract\b|rewrite\b|paraphrase\b)`)
	routingCompoundAction   = regexp.MustCompile(`(?i)(并且|然后|同时|并|再|[，,；;。.!！？?\n]|\band\b|\bthen\b)\s*(请|帮我|please\s+)?(严格|严谨)?(证明|推导|论证|设计|实现|分析|排查|优化|修复|写|编写|比较|拟|草拟|做|弄|整|搞|梳理|prove\b|derive\b|design\b|implement\b|analy[sz]e\b|debug\b|optimize\b|fix\b|write\b|compare\b)`)
	routingContinuation     = regexp.MustCompile(`(?i)^(请你|请|please\s+)?(继续(吧|执行|做|分析|推导|证明|完善|往下讲)?|接着(说|讲|做|来)?|下一步|go on|carry on|continue|keep going)(please|\s+please)?([\s.!?。！？]*$|\s*[，,；;：:\n]\s*)`)
	routingReference        = regexp.MustCompile(`(?i)上面|上述|刚才|这个方案|这个问题|这种情况|按这个|照这个|换一种方法|另一种方法|为什么这么|\b(that approach|that solution|the above|previous (answer|solution)|another approach|another method|why is that)\b`)
	routingNewTopic         = regexp.MustCompile(`(?i)换个(话题|问题)|另一个问题|\b(new topic|different question|unrelated question)\b`)
	routingQuoted           = regexp.MustCompile("(?s)\"[^\"\n]*\"|“[^”]*”|‘[^’]*’|`[^`\n]+`")
	routingNegated          = regexp.MustCompile(`(?i)(无需|不需要|不要|不用)(进行|做)?(严格|严谨)?(证明|推导|论证|设计|实现|分析|排查|编写|写|做|弄|整|搞|拟|草拟|梳理)[^，,。.!！？?；;\n]*|\b(do not|don't|no need to|without)\s+(prove|proving|derive|deriving|design|designing|implement|implementing)\b[^,.;\n]*`)
	// Colloquial creation requires an artifact, not just a verb such as "做".
	routingColloquialTask = regexp.MustCompile(`(弄|整|搞|做|拟|草拟)(一)?(个|份|封|段|套|篇|张).{0,60}(程序|游戏|网页|网站|页面|函数|脚本|接口|代码|邮件|通知|文案|报告|方案|计划|证明文件)|(?:给我|帮我|替我)拟.{0,30}(通知|邮件|文案|报告|方案|计划)`)
	routingPlanRequest    = regexp.MustCompile(`(给|提供|出).{0,12}(整体方案|方案|计划)|梳理.{0,30}(因果|原因|验证)|\b(?i:diagnose|investigate)\b`)
	routingCaseWord       = regexp.MustCompile(`大小写|大写|小写|缩写`)
	// Object-first transforms are intentionally bounded to references/materials;
	// "把系统实现并总结" must not be mistaken for a simple summary.
	routingObjectTransform       = regexp.MustCompile(`^(请你|请|帮我|请帮我)?(把|将)(上面|上述|刚才|之前|前面|这段|这篇|这份|以下|下面)[^，。；;：:\n]{0,60}(总结|概括|翻译|翻成|提取|改写|润色)`)
	routingCaseTransform         = regexp.MustCompile(`^(请你|请|帮我|请帮我)?(把|将)[^，。；;：:\n]{1,100}(变成|转换成|转成|改成)(大写|小写)`)
	routingBeforeTransformAction = regexp.MustCompile(`实现|编写|排查|修复|(设计|推导|论证|证明).{0,12}(后|并|然后)|\b(?i:design|implement|debug|prove|derive)\b`)
	routingWorkflowRequest       = regexp.MustCompile(`整体方案|每个环节.{0,8}(配合|协作)|\b(?i:end-to-end (plan|design))\b`)
)

// Count distinct constraint families, not keyword occurrences or prompt length.
var routingConstraints = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(latency|throughput|performance|qps)\b|延迟|吞吐|性能`),
	regexp.MustCompile(`(?i)\b(budget|cost|memory|cpu|resource limits?)\b|预算|成本|内存|资源限制`),
	regexp.MustCompile(`(?i)\b(availability|reliability|failover|rollback|recovery)\b|可用性|可靠性|容灾|回滚|恢复|不能丢.{0,6}(订单|消息|数据)|不丢.{0,6}(订单|消息|数据)|(坏了|断电|宕机).{0,12}(服务|接管|订单|恢复)`),
	regexp.MustCompile(`(?i)\b(security|privacy|compliance|permission|tenant isolation)\b|安全|隐私|合规|权限|数据.{0,8}(不能|禁止).{0,8}(互传|出境)|租户隔离`),
	regexp.MustCompile(`(?i)\b(compatibility|backward.compatible|zero.downtime)\b|兼容|零停机|不停机|旧系统.{0,12}(接入|接进|迁移)`),
	regexp.MustCompile(`(?i)\b(idempotenc[ey]|consistency)\b|幂等|一致性|不能超卖|不能重复.{0,6}(扣款|支付)|扣款不能重复`),
}

// Two distinct operational symptoms plus a causal/diagnostic request indicate
// cross-component troubleshooting. Symptoms alone do not elevate a fact query.
var routingOperationalSymptoms = []*regexp.Regexp{
	regexp.MustCompile(`(服务|系统).{0,12}(挂|崩溃|宕机)|\b(?i:outage|crash)\b`),
	regexp.MustCompile(`(数据库|连接池|连接).{0,8}(满|耗尽)|\b(?i:connection pool exhausted)\b`),
	regexp.MustCompile(`(队列|消息).{0,8}(堆|积压)|\b(?i:queue backlog)\b`),
	regexp.MustCompile(`扩容.{0,8}(严重|恶化|更慢)|\b(?i:scaling worsens)\b`),
}

const routingHistoryLimit = 8
const routingTextLimit = 16000
const routingReliabilityConstraint = 2
const routingIntegrityConstraint = 5

func classifyRoutingDifficulty(messages []ChatMessage) routingDecision {
	return classifyRoutingTurns(newRoutingClassificationInput(messages))
}

func classifyRoutingTurns(input RoutingClassificationInput) routingDecision {
	d := routingDecision{Policy: "content-cost-v4", Classifier: "heuristic", Signals: []string{}, Warnings: []string{}, Rejections: []routingRejection{}}
	seen, needsContext := 0, false
	for i := len(input.Turns) - 1; i >= 0; i-- {
		seen++
		turn := input.Turns[i]
		text, nonText := turn.Text, turn.HasNonText
		if nonText {
			appendRoutingUnique(&d.Warnings, "non_text_content_not_classified")
		}
		if turn.Truncated {
			appendRoutingUnique(&d.Warnings, "difficulty_text_truncated")
		}
		score, signals := scoreRoutingTask(text)
		if nonText && score < 0.25 {
			score = 0.25
			signals = append(signals, "unclassified_content")
		}
		if score > d.Score || seen == 1 {
			d.Score = score
			d.Signals = signals
		}
		instruction, _ := routingInstruction(text)
		// A summary/translation refers to previous material, not its required
		// capability. Additional work in the same turn is scored independently.
		followup := !routingNewTopic.MatchString(instruction) && !routingIsTransform(instruction) &&
			(routingContinuation.MatchString(instruction) ||
				(utf8.RuneCountInString(instruction) <= 200 && routingReference.MatchString(instruction)))
		if seen > 1 {
			appendRoutingUnique(&d.Signals, "followup_context")
		}
		// Empty text accompanying an image is a new task, not a continuation.
		needsContext = followup || (text == "" && !nonText)
		if !needsContext || seen == routingHistoryLimit {
			break
		}
	}
	if needsContext {
		appendRoutingUnique(&d.Warnings, "followup_context_unresolved")
		if d.Score < 0.25 {
			d.Score = 0.25
		}
	}
	if seen == 0 {
		appendRoutingUnique(&d.Warnings, "no_user_task")
	}
	switch {
	case d.Score >= 0.8:
		d.Difficulty, d.RequiredTier = "reasoning", 4
	case d.Score >= 0.5:
		d.Difficulty, d.RequiredTier = "complex", 3
	case d.Score >= 0.2:
		d.Difficulty, d.RequiredTier = "medium", 2
	default:
		d.Difficulty, d.RequiredTier = "simple", 1
	}
	return d
}

func appendRoutingUnique(items *[]string, item string) {
	for _, existing := range *items {
		if existing == item {
			return
		}
	}
	*items = append(*items, item)
}

// Do not JSON-stringify images/URLs into task text, as flattenMessageContent
// does for non-text content. Their subject and difficulty are unknown here.
func routingUserText(content interface{}) (string, bool) {
	switch value := content.(type) {
	case nil:
		return "", false
	case string:
		return strings.TrimSpace(value), false
	case []interface{}:
		parts, nonText := []string{}, false
		for _, part := range value {
			text, unknown := routingUserText(part)
			parts = append(parts, text)
			nonText = nonText || unknown
		}
		return strings.TrimSpace(strings.Join(parts, "\n")), nonText
	case map[string]interface{}:
		kind, _ := value["type"].(string)
		if kind == "" || kind == "text" || kind == "input_text" {
			if text := flattenStructuredContentPart(value); text != "" {
				return text, false
			}
		}
	}
	return "", true
}

// Treat fenced code, block quotes, and quoted strings as data. A simple
// transformation's colon-delimited payload is also data, not a new command.
func routingInstruction(text string) (string, bool) {
	lines := []string{}
	fence, hasCode := "", false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence, hasCode = trimmed[:3], true
			continue
		}
		if !strings.HasPrefix(trimmed, ">") {
			lines = append(lines, line)
		}
	}
	instruction := strings.TrimSpace(strings.Join(lines, "\n"))
	if routingIsTransform(instruction) {
		// A colon/newline separating "继续" from the task is not the start
		// of the transform payload. Keep the actual task before truncation.
		prefixLength := len(routingContinuation.FindString(instruction))
		if index := strings.IndexAny(instruction[prefixLength:], ":：\n"); index >= 0 {
			instruction = instruction[:prefixLength+index]
		}
	}
	instruction = routingQuoted.ReplaceAllString(instruction, " ")
	instruction = routingNegated.ReplaceAllString(instruction, " ")
	return strings.TrimSpace(instruction), hasCode
}

func scoreRoutingTask(text string) (float64, []string) {
	instruction, hasCode := routingInstruction(text)
	signals := []string{}
	// Translating a theorem is not proving it. Explicit compound instructions
	// ("summarize and prove ...") still go through the higher-tier checks.
	if routingSimpleTransform(instruction) {
		return 0, []string{"text_transformation"}
	}
	if routingDefinition.MatchString(instruction) && !routingCompoundAction.MatchString(instruction) && !routingCauseQuestion.MatchString(instruction) && !hasCode {
		return 0, []string{"definition_or_fact_request"}
	}
	formalText := routingDocumentProof.ReplaceAllString(instruction, " ")
	formal := routingFormalAction.MatchString(formalText) &&
		(routingFormalSubject.MatchString(formalText) || routingFormalRequest.MatchString(formalText))
	if formal {
		return 0.85, []string{"formal_reasoning_intent"}
	}
	// Case-conversion nouns contain 写 but are not requests to compose text.
	instruction = routingCaseWord.ReplaceAllString(instruction, " ")
	score := 0.0
	if routingDebugAction.MatchString(instruction) ||
		(routingTechnicalAction.MatchString(instruction) && (routingTechnicalSubject.MatchString(instruction) || hasCode)) {
		score = 0.25
		signals = append(signals, "technical_task_intent")
	}
	if routingGeneralAction.MatchString(instruction) && score < 0.25 {
		score = 0.25
		signals = append(signals, "composition_or_analysis_intent")
	}
	if (routingColloquialTask.MatchString(instruction) || routingPlanRequest.MatchString(instruction)) && score < 0.25 {
		score = 0.25
		signals = append(signals, "colloquial_task_intent")
	}
	if (routingComplexAction.MatchString(instruction) || routingCauseQuestion.MatchString(instruction)) && routingComplexSubject.MatchString(instruction) {
		score = 0.55
		signals = append(signals, "complex_task_intent")
	}
	constraints := 0
	for _, pattern := range routingConstraints {
		if pattern.MatchString(instruction) {
			constraints++
		}
	}
	if score >= 0.25 && constraints >= 3 {
		score = 0.65
		signals = append(signals, "multiple_constraint_families")
	}
	if score >= 0.25 && score < 0.55 && routingWorkflowRequest.MatchString(instruction) &&
		routingConstraints[routingReliabilityConstraint].MatchString(instruction) && routingConstraints[routingIntegrityConstraint].MatchString(instruction) {
		score = 0.55
		signals = append(signals, "reliable_transaction_workflow")
	}
	symptoms := 0
	for _, pattern := range routingOperationalSymptoms {
		if pattern.MatchString(instruction) {
			symptoms++
		}
	}
	if symptoms >= 2 && (routingComplexAction.MatchString(instruction) || routingPlanRequest.MatchString(instruction)) && score < 0.55 {
		score = 0.55
		signals = append(signals, "multi_component_diagnosis")
	}
	// Long text and repeated keywords do not raise the tier on their own.
	return score, signals
}

func routingIsTransform(instruction string) bool {
	// Strip a continuation marker for task recognition, but not its extra work.
	if prefix := routingContinuation.FindString(instruction); prefix != "" {
		instruction = strings.TrimSpace(strings.TrimPrefix(instruction, prefix))
	}
	if routingTransform.MatchString(instruction) {
		return true
	}
	match := routingObjectTransform.FindString(instruction)
	if match == "" {
		match = routingCaseTransform.FindString(instruction)
	}
	return match != "" && !routingBeforeTransformAction.MatchString(match)
}

func routingSimpleTransform(instruction string) bool {
	return routingIsTransform(instruction) && !routingCompoundAction.MatchString(instruction)
}
