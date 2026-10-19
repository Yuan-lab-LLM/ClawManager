package ownerrecords

import (
	"testing"
	"time"
)

func TestKeysAreUnambiguousAndScoped(t *testing.T) {
	if Key("a", "bc") == Key("ab", "c") {
		t.Fatal("ambiguous encoding")
	}
	if Key("1", "owner") == Key("2", "owner") {
		t.Fatal("cross-scope collision")
	}
	if Key("session", "1", "same") == Key("session", "2", "same") {
		t.Fatal("different instances merged")
	}
	if Key("invocation", "db-1", "42") != Key("invocation", "db-1", "42") {
		t.Fatal("unstable dedupe")
	}
}

func TestTaskDefinitionValidation(t *testing.T) {
	valid := TaskDefinition{Code: "daily_active_work", Period: "day", Metric: "active_work_seconds", Timezone: "Asia/Shanghai", ClaimMode: "automatic", RewardMode: "incremental", Version: "v1", Tiers: []TaskTier{{Code: "15m", Target: 900, RewardXP: 5}, {Code: "30m", Target: 1800, RewardXP: 10}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*TaskDefinition){
		func(d *TaskDefinition) { d.Tiers[1].Code = "15m" },
		func(d *TaskDefinition) { d.Tiers[1].Target = 800 },
		func(d *TaskDefinition) { d.Tiers[0].RewardXP = -1 },
		func(d *TaskDefinition) { d.Metric = "instance_uptime" },
		func(d *TaskDefinition) { d.Timezone = "missing/zone" },
		func(d *TaskDefinition) { d.RewardMode = "unspecified" },
	} {
		next := valid
		next.Tiers = append([]TaskTier(nil), valid.Tiers...)
		mutate(&next)
		if next.Validate() == nil {
			t.Fatal("invalid task accepted", next)
		}
	}
}

func TestUsageIntervalsUnion(t *testing.T) {
	start := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	input := []Interval{{Start: start.Add(5 * time.Minute), End: start.Add(15 * time.Minute)}, {Start: start, End: start.Add(10 * time.Minute)}, {Start: start, End: start.Add(10 * time.Minute)}}
	seconds, err := UnionSeconds(input)
	if err != nil || seconds != 900 {
		t.Fatal(seconds, err)
	}
	if !input[0].Start.Equal(start.Add(5 * time.Minute)) {
		t.Fatal("mutated caller input")
	}
	if _, err = UnionSeconds([]Interval{{Start: start, End: start.Add(-time.Second)}}); err == nil {
		t.Fatal("negative interval accepted")
	}
}
