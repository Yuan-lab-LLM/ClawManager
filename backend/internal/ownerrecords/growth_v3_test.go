package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func TestSixDailyRewards(t *testing.T) {
	var total int64
	count := 0
	for _, task := range growthTasksV3() {
		if task.Period == "day" {
			count++
			total += task.XP
			if task.Code == "question-1" {
				t.Fatal("removed node")
			}
		}
	}
	if count != 6 || total != 100 {
		t.Fatal(count, total)
	}
	v2 := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	v3 := v2.Add(24 * time.Hour)
	for _, tc := range []struct {
		at   time.Time
		rule string
	}{{v2.Add(-time.Second), growthRule}, {v2, growthRuleV2}, {v3.Add(-time.Second), growthRuleV2}, {v3, growthRuleV3}} {
		rule, _ := growthTasksAt(tc.at, v2, v3)
		if rule != tc.rule {
			t.Fatal(tc, rule)
		}
	}
}

func verifyRankingAndSixTasks(t *testing.T, s *Store) {
	ctx := context.Background()
	now := time.Date(2035, 1, 15, 12, 0, 0, 0, time.UTC)
	apply := func(owner, source string, tokens uint64, at time.Time) {
		t.Helper()
		for i := 0; i < 2; i++ {
			_, err := s.Apply(ctx, Identity{Scope: 99, Owner: owner, Instance: 90001, Runtime: "hermes", Mode: "lite"}, Fact{Type: "invocation", Source: "rank-test", SourceID: source, Tokens: tokens, At: at})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	apply("rank-a@example.com", "a", 100, now.Add(-time.Hour))
	apply("rank-b@example.com", "b", 100, now.Add(-time.Hour))
	apply("rank-c@example.com", "c", 50, now.Add(-time.Hour))
	apply("rank-c@example.com", "old", 10000, now.Add(-168*time.Hour-time.Second))
	apply("rank-c@example.com", "future", 10000, now.Add(time.Hour))
	for _, tc := range []struct {
		owner  string
		rank   int
		tokens uint64
	}{{"rank-a@example.com", 1, 100}, {"rank-b@example.com", 1, 100}, {"rank-c@example.com", 3, 50}} {
		r, err := s.TokenRanking(ctx, tc.owner, now)
		if err != nil || r.Rank == nil || *r.Rank != tc.rank || r.Tokens != tc.tokens || r.Participants != 3 {
			t.Fatal(r, err)
		}
	}
	r, err := s.TokenRanking(ctx, "no-tokens@example.com", now)
	if err != nil || r.Rank != nil {
		t.Fatal(r, err)
	}
	// Start precedes the requested 90-day viewport: empty dates are still in range.
	loc, _ := time.LoadLocation("Asia/Shanghai")
	start := now.In(loc).AddDate(0, 0, 10)
	days, _, err := s.UsageDays(ctx, "rank-a@example.com", nil, start, start.AddDate(0, 0, 2))
	if err != nil || len(days) != 2 || !days[0].InRange || days[0].Observed {
		t.Fatal(days, err)
	}
	owner := "six-nodes@example.com"
	if err = s.VisitGrowth(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	apply(owner, "six-tokens", 50000, now.Add(time.Minute))
	for i := 0; i < 5; i++ {
		_, err = s.Apply(ctx, Identity{Scope: 99, Owner: owner, Instance: 90002}, Fact{Type: "question", Source: "six-test", SourceID: string(rune('a' + i)), SessionID: "s", QuestionID: string(rune('a' + i)), At: now.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err = s.ReconcileGrowth(ctx, owner, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	g, err := s.Growth(ctx, owner, now.Add(2*time.Minute))
	if err != nil || g.Rule != growthRuleV3 || g.XP != 100 {
		t.Fatal(g, err)
	}
	n := 0
	for _, task := range g.Tasks {
		if task.Period == "day" {
			n++
			if !task.Awarded {
				t.Fatal(task)
			}
		}
	}
	if n != 6 {
		t.Fatal(n)
	}
}
