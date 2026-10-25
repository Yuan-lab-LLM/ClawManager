package ownerrecords

import "fmt"

const growthRuleV4 = "growth-v4"

// Keep historic definitions immutable. Selection/activation is versioned separately.
func growthTasksV4() []GrowthTask {
	tasks := []GrowthTask{
		{Code: "login-day", Name: "今日启航", Period: "day", Metric: "login", Target: 1, XP: 10},
		{Code: "question-3", Name: "深入交流", Period: "day", Metric: "questions", Target: 3, XP: 20},
		{Code: "question-5", Name: "持续推进", Period: "day", Metric: "questions", Target: 5, XP: 35},
		{Code: "tokens-100000", Name: "轻量协作", Period: "day", Metric: "tokens", Target: 100000, XP: 10},
		{Code: "tokens-500000", Name: "深度协作", Period: "day", Metric: "tokens", Target: 500000, XP: 10},
		{Code: "tokens-2000000", Name: "充分协作", Period: "day", Metric: "tokens", Target: 2000000, XP: 20},
	}
	for _, group := range []struct {
		metric, name string
		targets      []uint64
		rewards      []int64
	}{
		{"login", "累计访问", []uint64{7, 30, 60, 90, 180}, []int64{150, 300, 450, 700, 1100}},
		{"questions", "累计有效提问", []uint64{50, 200, 500, 900}, []int64{300, 500, 800, 1100}},
		{"tokens", "累计 Token", []uint64{10000000, 50000000, 100000000, 300000000}, []int64{300, 500, 800, 1100}},
	} {
		for i, target := range group.targets {
			tasks = append(tasks, GrowthTask{Code: fmt.Sprintf("milestone-%s-%d", group.metric, target), Name: group.name, Period: "all", Metric: group.metric, Target: target, XP: group.rewards[i]})
		}
	}
	return tasks
}

func levelThresholdV4(level int) int64 {
	n := int64(level - 1)
	// Integer nearest-10 rounding, identical to the client; no float drift.
	return ((120*n+5*n*(n-1))*2700 + 8820) / 17640 * 10
}
