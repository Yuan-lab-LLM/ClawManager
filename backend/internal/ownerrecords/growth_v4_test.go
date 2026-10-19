package ownerrecords

import "testing"

func TestV4RewardsAndCurve(t *testing.T) {
	var daily, all int64
	groups := map[string]int64{}
	nodes := 0
	for _, task := range growthTasksV4() {
		if task.Period == "day" {
			daily += task.XP
			nodes++
		} else {
			all += task.XP
			groups[task.Metric] += task.XP
		}
		if task.Metric == "full_days" {
			t.Fatal("unexpected fourth category")
		}
	}
	if daily != 105 || all != 8100 || nodes != 6 || len(groups) != 3 {
		t.Fatal(daily, all, nodes, groups)
	}
	for _, xp := range groups {
		if xp != 2700 {
			t.Fatal(groups)
		}
	}
	if levelThresholdV4(1) != 0 || levelThresholdV4(50) != 27000 {
		t.Fatal("curve endpoints")
	}
	for l := 2; l <= 50; l++ {
		if levelThresholdV4(l) <= levelThresholdV4(l-1) {
			t.Fatal("non-increasing curve", l)
		}
	}
	if 179*daily+all >= levelThresholdV4(50) || 180*daily+all != levelThresholdV4(50) {
		t.Fatal("180-day bound")
	}
	if 180*2000000 < 300000000 {
		t.Fatal("daily token threshold cannot cover milestone")
	}
	for level := 1; level <= 50; level++ {
		xp := levelThresholdV4(level)
		actual, start, next := levelForRuleXP(xp, growthRuleV4)
		if actual != level || start != xp || next < start {
			t.Fatal("threshold/level mismatch", level, actual, start, next)
		}
		if level > 1 {
			previous, _, _ := levelForRuleXP(xp-1, growthRuleV4)
			if previous != level-1 {
				t.Fatal("boundary off by one", level, previous)
			}
		}
	}
	level, start, next := levelForRuleXP(90000, growthRuleV4)
	if level != 50 || start != 27000 || next != 27000 {
		t.Fatal("open cap", level, start, next)
	}
}
