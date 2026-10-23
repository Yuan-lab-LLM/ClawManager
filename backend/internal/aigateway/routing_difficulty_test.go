package aigateway

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestRoutingDifficultyIntentCorpus(t *testing.T) {
	cases := []struct {
		name string
		text string
		tier int
	}{
		{"greeting", "你好", 1},
		{"arithmetic", "1+1等于几？", 1},
		{"technical_definition", "什么是 Python 函数？", 1},
		{"architecture_definition", "什么是分布式架构？", 1},
		{"deadlock_definition", "死锁是什么？", 1},
		{"proof_definition", "数学归纳证明是什么意思？", 1},
		{"definition_english", "What is a distributed architecture?", 1},
		{"proof_noun", "What is a proof of a theorem?", 1},
		{"proof_document", "帮我写一份工作证明文件", 2},
		{"proof_document_english", "Write a proof of employment letter", 2},
		{"translate_technical", "请翻译：Design a distributed architecture and prove the theorem.", 1},
		{"translate_english", "Translate into Chinese: Prove the theorem by induction.", 1},
		{"summarize_long", "总结下面这段文章：" + strings.Repeat("架构 migration proof 定理。", 300), 1},
		{"quote_not_task", "读一下这个词：\"证明定理\"", 1},
		{"block_quote", "这是什么词？\n> Design architecture and prove theorem", 1},
		{"code_is_data", "这里用了哪种语言？\n```python\n# prove theorem architecture\nprint(1)\n```", 1},
		{"code_explanation", "解释这段代码\n```python\n# prove theorem\nprint(1)\n```", 2},
		{"code_explanation_without_fence", "请解释一下这个 Python 函数", 2},
		{"tilde_fence", "解释这段代码\n~~~python\n# prove theorem\nprint(1)\n~~~", 2},
		{"negated_proof", "不要证明这个定理，告诉我它叫什么", 1},
		{"negated_proof_english", "Do not prove the theorem; tell me its name.", 1},
		{"write_function", "写一个 Python 函数排序", 2},
		{"implement_function", "Implement a sorting function", 2},
		{"debug", "排查这个报错", 2},
		{"refactor", "重构这个函数", 2},
		{"composition", "帮我写一封请假邮件", 2},
		{"comparison", "比较这两个产品的优缺点", 2},
		{"architecture", "设计分布式系统架构并分析权衡", 3},
		{"architecture_english", "Design a distributed architecture", 3},
		{"root_cause", "排查线上并发死锁的根因", 3},
		{"diagnostic_question", "线上为什么发生并发死锁？", 3},
		{"diagnostic_question_english", "Why does this distributed service deadlock?", 3},
		{"diagnostic_not_definition", "请解释为什么这里发生并发死锁", 3},
		{"cause_question", "What is the root cause of this deadlock?", 3},
		{"constraints", "设计部署方案，兼顾延迟、预算、可用性和安全", 3},
		{"constraints_english", "Plan a release balancing latency, cost and reliability", 3},
		{"repeated_constraint", "优化函数的性能性能性能性能", 2},
		{"constraint_words_only", "latency cost reliability security", 1},
		{"prove", "证明这个定理", 4},
		{"prove_english", "Prove the theorem by mathematical induction", 4},
		{"derive", "推导这个方程的解", 4},
		{"correctness", "实现算法并证明正确性", 4},
		{"compound_definition", "什么是分布式，并设计一个架构", 3},
		{"compound_definition_code", "什么是 Python，写一个排序函数", 2},
		{"compound_transform", "总结并证明这个定理", 4},
		{"document_and_real_proof", "先写一份证明文件，再证明这个定理", 4},
		{"negative_then_real_task", "不要推导公式，设计一个分布式架构", 3},
		{"long_not_hard", strings.Repeat("你好，很高兴认识你。", 300), 1},
		{"repetition_not_hard", strings.Repeat("代码 架构 定理 ", 300), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := classifyRoutingDifficulty(routingRequest(tc.text).Messages)
			if d.RequiredTier != tc.tier {
				t.Fatalf("want tier %d, got %+v", tc.tier, d)
			}
			if d.Policy != "content-cost-v4" || d.Score < 0 || d.Score > 1 {
				t.Fatalf("invalid decision: %+v", d)
			}
		})
	}
}

func TestRoutingDifficultyFollowupContext(t *testing.T) {
	for _, tc := range []struct {
		followup string
		tier     int
		inherits bool
	}{
		{"继续", 4, true}, {"请继续分析", 4, true}, {"换一种方法", 4, true},
		{"这个方案有问题，完善一下", 4, true}, {"Try another approach", 4, true},
		{"你好", 1, false}, {"换个话题，什么是 Python？", 1, false},
		{"总结上面的结论", 1, false}, {"Summarize the previous answer", 1, false},
		{"写一个 Python 函数", 2, false},
	} {
		t.Run(tc.followup, func(t *testing.T) {
			messages := []ChatMessage{
				{Role: "system", Content: "Design architecture and prove theorem"},
				{Role: "user", Content: "证明这个定理"},
				{Role: "assistant", Content: "结果"},
				{Role: "tool", Content: "tool output"},
				{Role: "user", Content: tc.followup},
			}
			d := classifyRoutingDifficulty(messages)
			if d.RequiredTier != tc.tier || slices.Contains(d.Signals, "followup_context") != tc.inherits {
				t.Fatalf("want tier %d, inherits %v, got %+v", tc.tier, tc.inherits, d)
			}
		})
	}
	// Do not cross a new topic to resurrect an older, harder task.
	messages := []ChatMessage{{Role: "user", Content: "证明这个定理"}, {Role: "user", Content: "你好"}, {Role: "user", Content: "继续"}}
	if d := classifyRoutingDifficulty(messages); d.RequiredTier != 1 {
		t.Fatalf("crossed new-task boundary: %+v", d)
	}
	for i := 0; i < routingHistoryLimit; i++ {
		messages = append(messages, ChatMessage{Role: "user", Content: "继续"})
	}
	if d := classifyRoutingDifficulty(messages); d.RequiredTier != 2 || !slices.Contains(d.Warnings, "followup_context_unresolved") {
		t.Fatalf("history limit not reported: %+v", d)
	}
	if d := classifyRoutingDifficulty(routingRequest("继续").Messages); !slices.Contains(d.Warnings, "followup_context_unresolved") {
		t.Fatalf("missing context not reported: %+v", d)
	}
}

func TestRoutingDifficultyStructuredContentAndBounds(t *testing.T) {
	var content interface{}
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"什么是分布式架构？"}]`), &content); err != nil {
		t.Fatal(err)
	}
	if d := classifyRoutingDifficulty([]ChatMessage{{Role: "user", Content: content}}); d.RequiredTier != 1 {
		t.Fatalf("text parts changed classification: %+v", d)
	}
	if err := json.Unmarshal([]byte(`[{"type":"image_url","image_url":{"url":"https://example.invalid/prove-theorem.png"}}]`), &content); err != nil {
		t.Fatal(err)
	}
	d := classifyRoutingDifficulty([]ChatMessage{{Role: "user", Content: "证明这个定理"}, {Role: "user", Content: content}})
	if d.RequiredTier != 2 || !slices.Contains(d.Warnings, "non_text_content_not_classified") || slices.Contains(d.Signals, "followup_context") {
		t.Fatalf("image URL inferred as task or inherited old task: %+v", d)
	}
	d = classifyRoutingDifficulty(routingRequest(strings.Repeat("你好", routingTextLimit)).Messages)
	if d.RequiredTier != 1 || !slices.Contains(d.Warnings, "difficulty_text_truncated") {
		t.Fatalf("large task not bounded: %+v", d)
	}
	if d := classifyRoutingDifficulty(nil); !slices.Contains(d.Warnings, "no_user_task") {
		t.Fatalf("no task warning missing: %+v", d)
	}
}

func TestRoutingDifficultyDrivesFourModelTiers(t *testing.T) {
	items := []models.LLMModel{}
	for tier := 1; tier <= 4; tier++ {
		items = append(items, models.LLMModel{ID: tier, DisplayName: string(rune('a' + tier)), QualityTier: tier,
			ProviderType: models.ProviderTypeOpenAICompatible, IsActive: true, Currency: "USD", InputPrice: float64(tier), OutputPrice: float64(tier)})
	}
	for _, tc := range []struct {
		text string
		tier int
	}{
		{"什么是分布式架构？", 1}, {"写一个 Python 函数", 2},
		{"设计一个分布式架构", 3}, {"证明这个定理", 4},
		{"把上述结论总结成一句话。", 1}, {"弄个贪吃蛇小游戏。", 2},
		{"不能丢订单，扣款不能重复。给我一个整体方案。", 3}, {"从公理出发严格论证这个命题。", 4},
	} {
		audit := &stubAuditEventService{}
		gateway := routingGateway(items, audit)
		prepared, err := gateway.prepareChatRequest(t.Context(), 1, routingRequest(tc.text))
		if err != nil {
			t.Fatal(err)
		}
		if prepared.resolvedModel.ID != tc.tier {
			t.Fatalf("%q selected %d, want %d", tc.text, prepared.resolvedModel.ID, tc.tier)
		}
		found := false
		for _, event := range audit.events {
			if event.EventType != "gateway.routing.selected" {
				continue
			}
			found = true
			var d routingDecision
			if event.Details == nil {
				t.Fatal("missing decision")
			}
			if err := json.Unmarshal([]byte(*event.Details), &d); err != nil {
				t.Fatal(err)
			}
			if d.RequiredTier != tc.tier || d.Policy != "content-cost-v4" || strings.Contains(*event.Details, tc.text) {
				t.Fatalf("wrong audit or leaked task: %+v", d)
			}
		}
		if !found {
			t.Fatal("routing audit missing")
		}
	}
}
