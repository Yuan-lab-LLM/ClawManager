package ownerrecords

import (
	"context"
	"fmt"
	"time"
)

const growthRuleV2 = "growth-v2"
const openGrowthLevel = 50
const plannedGrowthLevel = 100

func levelThreshold(level int) int64 {
	n := int64(level - 1)
	return 120*n + 5*n*(n-1)
}

// A single persistent rollout boundary is shared by every owner and App replica.
// Never reinitialize on restart; old daily awards cannot overlap new daily awards.
func (s *Store) growthEffectiveAt(ctx context.Context, now time.Time) (time.Time, error) {
	return s.policy(ctx, growthRuleV2, now, true)
}

func growthTasksAt(now, effective time.Time, v3 ...time.Time) (string, []GrowthTask) {
	if len(v3) > 1 && !v3[1].IsZero() && !now.Before(v3[1]) {
		return growthRuleV4, growthTasksV4()
	}
	if len(v3) > 0 && !now.Before(v3[0]) && !now.Before(effective) {
		return growthRuleV3, growthTasksV3()
	}
	if now.Before(effective) {
		return growthRule, growthTasks()
	}
	return growthRuleV2, growthTasksV2()
}

func growthTasksV2() []GrowthTask {
	tasks := []GrowthTask{
		{Code: "login-day", Name: "今日启航", Period: "day", Metric: "login", Target: 1, XP: 10},
		{Code: "question-1", Name: "开启探索", Period: "day", Metric: "questions", Target: 1, XP: 10},
		{Code: "question-3", Name: "深入交流", Period: "day", Metric: "questions", Target: 3, XP: 15},
		{Code: "question-5", Name: "持续推进", Period: "day", Metric: "questions", Target: 5, XP: 25},
		{Code: "tokens-5000", Name: "轻量协作", Period: "day", Metric: "tokens", Target: 5000, XP: 10},
		{Code: "tokens-20000", Name: "深度协作", Period: "day", Metric: "tokens", Target: 20000, XP: 10},
		{Code: "tokens-50000", Name: "充分协作", Period: "day", Metric: "tokens", Target: 50000, XP: 20},
	}
	for _, group := range []struct {
		metric, name, reward string
		targets              []uint64
	}{
		{"login", "累计访问", "足迹收藏章", []uint64{7, 30, 60, 90, 180}},
		{"questions", "累计有效提问", "探索收藏章", []uint64{50, 200, 500, 1000}},
		{"tokens", "累计 Token 用量", "协作收藏章", []uint64{100000, 1000000, 5000000, 10000000}},
		{"full_days", "每日全任务完成", "坚持收藏章", []uint64{7, 30, 60, 90, 180}},
	} {
		for i, target := range group.targets {
			tasks = append(tasks, GrowthTask{Code: fmt.Sprintf("milestone-%s-%d", group.metric, target), Name: group.name, Period: "all", Metric: group.metric, Target: target, Reward: fmt.Sprintf("%s · %d 阶", group.reward, i+1)})
		}
	}
	return tasks
}
