package aigateway

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The original synthetic evaluation set is now a development regression set,
// not an independent measure of real-world classification accuracy.
func TestRoutingDifficultyEvaluationRegressions(t *testing.T) {
	raw, err := os.ReadFile("../../../e2e/routing-classifier/cases.zh.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		ID    string            `json:"id"`
		Tier  int               `json:"expected_tier"`
		Turns []RoutingTaskTurn `json:"turns"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 40 {
		t.Fatalf("unexpected regression corpus size: %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			d := classifyRoutingTurns(RoutingClassificationInput{Turns: tc.Turns})
			if d.RequiredTier != tc.Tier {
				t.Fatalf("want tier %d, got %+v", tc.Tier, d)
			}
		})
	}
}

func TestRoutingDifficultyV4Counterexamples(t *testing.T) {
	for _, tc := range []struct {
		text string
		tier int
	}{
		{"帮我弄一张活动报名网页，支持提交姓名。", 2},
		{"做一个待办事项小程序。", 2},
		{"给我拟一份停水通知。", 2},
		{"弄个煎蛋，做一杯咖啡。", 1},
		{"写一个把英文转成大写的函数。", 2},
		{"把 abc 改成小写。", 1},
		{"请将上述说明翻译成法语。", 1},
		{"把上面的证明总结成两句话。", 1},
		{"把上面的架构实现后总结。", 3},
		{"将这段话总结，并设计一个分布式方案。", 3},
		{"把上述内容概括，然后严格论证这个不等式。", 4},
		{"请将下面文字翻译成英文：严格论证所有实数满足这个不等式。", 1},
		{"什么是循环不变式？", 1},
		{"什么是多租户？", 1},
		{"帮我论证这个促销方案的优缺点。", 2},
		{"严格论证这个品牌很受欢迎。", 2},
		{"不需要严格论证这个不等式，只告诉我它的名字。", 1},
		{"不要论证任意实数的性质，写一个排序函数。", 2},
		{"从公理出发论证这个命题。", 4},
		{"给出循环不变式并证明其保持性。", 4},
		{"Prove the loop invariant holds at termination.", 4},
		{"请设计多租户接口的权限隔离方案。", 3},
		{"数据不能出境、机房断电要继续服务、旧系统要逐步接入，给我一个方案。", 3},
		{"不能超卖，扣款不能重复。", 1},
		{"不能丢订单，扣款不能重复。给我一个整体方案。", 3},
		{"给我一个防止重复扣款的方案。", 2},
		{"数据库连接池耗尽，消息队列积压，帮我排查原因。", 3},
		{"数据库连接池耗尽，消息队列积压，这句话是什么意思？", 1},
		{"总结这份日志：数据库连接池耗尽，队列积压，请排查原因。", 1},
		{"排查队列积压问题。", 2},
		{"排查队列积压，队列积压，队列积压。", 2},
		{"改写下面文字：给我一个整体方案，不能超卖，不能丢订单。", 1},
		{"帮我拟一份工作证明文件。", 2},
		{"不要做一个程序，只告诉我它叫什么。", 1},
		{"不要写代码, 设计一个分布式系统。", 3},
		{"什么是循环不变式？请严格论证算法正确性。", 4},
		{"将上述内容总结，并拟一份上线通知。", 2},
		{"继续，把上面的结论概括成一句话。", 1},
		{"继续：总结下面材料：Design architecture and prove theorem.", 1},
		{"继续\n翻译：Prove the theorem.", 1},
		{"把上面的设计总结成一句话。", 1},
		{"把上述数学论证概括成一句话。", 1},
		{"把上面的公式推导后总结。", 4},
	} {
		t.Run(tc.text, func(t *testing.T) {
			d := classifyRoutingDifficulty(routingRequest(tc.text).Messages)
			if d.RequiredTier != tc.tier {
				t.Fatalf("want tier %d, got %+v", tc.tier, d)
			}
		})
	}
}

func TestRoutingDifficultyV4Followups(t *testing.T) {
	for _, tc := range []struct {
		previous string
		current  string
		tier     int
		inherits bool
	}{
		{"实现一个日期转换函数。", "继续，把闰年的情况补上。", 2, true},
		{"设计一个分布式订单系统。", "接着来，讨论节点掉线怎么接管。", 3, true},
		{"证明这个定理。", "继续，严格论证边界情况。", 4, true},
		{"证明这个定理。", "继续，把上面的证明概括成一句话。", 1, false},
		{"证明这个定理。", "把上述结论翻成英文。", 1, false},
		{"证明这个定理。", "继续，换个话题，帮我拟一封邀请邮件。", 2, false},
		{"证明这个定理。", "继续教育是什么意思？", 1, false},
		{"证明这个定理。", "接着说的英文是什么？", 1, false},
		{"证明这个定理。", "Continue, summarize the previous answer.", 1, false},
		{"Design a distributed architecture.", "Continue, cover failover too.", 3, true},
		{"证明这个定理。", "把上述内容总结，然后写一个 Python 函数。", 2, false},
	} {
		t.Run(tc.current, func(t *testing.T) {
			d := classifyRoutingDifficulty([]ChatMessage{{Role: "user", Content: tc.previous}, {Role: "user", Content: tc.current}})
			if d.RequiredTier != tc.tier || slices.Contains(d.Signals, "followup_context") != tc.inherits {
				t.Fatalf("want tier %d inherits=%v, got %+v", tc.tier, tc.inherits, d)
			}
		})
	}
}
