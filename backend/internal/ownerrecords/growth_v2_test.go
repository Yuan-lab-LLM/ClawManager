package ownerrecords

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func verifyGrowthV2(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	prior := time.Date(2026, 9, 17, 10, 0, 0, 0, loc)
	effective, err := s.growthEffectiveAt(ctx, prior)
	if err != nil {
		t.Fatal(err)
	}
	if !effective.Equal(time.Date(2026, 9, 18, 0, 0, 0, 0, loc)) {
		t.Fatal("persistent next-midnight boundary", effective)
	}
	again, err := s.growthEffectiveAt(ctx, prior.AddDate(0, 0, 30))
	if err != nil || !again.Equal(effective) {
		t.Fatal("schedule moved on restart", again, err)
	}
	rule, old := growthTasksAt(effective.Add(-time.Nanosecond), effective)
	if rule != growthRule || old[0].XP != 5 {
		t.Fatal("old rule lost before boundary")
	}
	rule, current := growthTasksAt(effective, effective)
	if rule != growthRuleV2 || current[0].XP != 10 {
		t.Fatal("new rule boundary")
	}
	owner := "growth-v2@example.com"
	now := effective.Add(time.Hour)
	if err = s.VisitGrowth(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	identity := Identity{Scope: 51, Owner: owner, Instance: 1500, Runtime: "hermes", Mode: "lite"}
	apply := func(f Fact) {
		t.Helper()
		for repeat := 0; repeat < 2; repeat++ {
			if _, e := s.Apply(ctx, identity, f); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Old facts stay excluded even if inserted after rollout.
	apply(Fact{Type: "invocation", Source: "test", SourceID: "v2-old", Tokens: 100000, At: effective.Add(-time.Second)})
	for n := 1; n <= 5; n++ {
		apply(Fact{Type: "question", Source: "test", SourceID: fmt.Sprint("v2-q", n), QuestionID: fmt.Sprint(n), SessionID: "session", At: now.Add(time.Second)})
	}
	apply(Fact{Type: "invocation", Source: "test", SourceID: "v2-tokens1", Tokens: 20000, At: now.Add(time.Second)})
	identity.Instance++
	apply(Fact{Type: "invocation", Source: "test", SourceID: "v2-tokens2", Tokens: 30000, At: now.Add(time.Second)})
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.ReconcileGrowth(ctx, owner, now.Add(time.Minute)) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	g, err := s.Growth(ctx, owner, now.Add(time.Minute))
	if err != nil || g.XP != 100 || g.TodayXP != 100 {
		t.Fatal("daily cap/multi-instance/dedupe", g, err)
	}
	for _, task := range g.Tasks {
		if task.Period == "day" && !task.Awarded {
			t.Fatal("unawarded node", task)
		}
		if task.Metric == "tokens" && task.Progress != 50000 {
			t.Fatal("old/replayed tokens counted", task)
		}
	}
	// A late next-tier invocation must reward the occurrence day once, not today.
	lateOwner := "growth-v2-late@example.com"
	if err = s.VisitGrowth(ctx, lateOwner, now); err != nil {
		t.Fatal(err)
	}
	identity.Owner = lateOwner
	identity.Instance++
	apply(Fact{Type: "invocation", Source: "test", SourceID: "v2-late", Tokens: 50000, At: now.Add(time.Minute)})
	for i := 0; i < 2; i++ {
		if err = s.ReconcileGrowth(ctx, lateOwner, now.AddDate(0, 0, 1)); err != nil {
			t.Fatal(err)
		}
	}
	g, err = s.Growth(ctx, lateOwner, now.AddDate(0, 0, 1))
	if err != nil || g.XP != 50 || g.TodayXP != 0 {
		t.Fatal("late fact business date", g, err)
	}
	// Seven verified visits unlock a collectible, never a cumulative XP bonus.
	for day := 1; day < 7; day++ {
		if err = s.VisitGrowth(ctx, owner, now.AddDate(0, 0, day)); err != nil {
			t.Fatal(err)
		}
	}
	g, err = s.Growth(ctx, owner, now.AddDate(0, 0, 6))
	if err != nil || g.XP != 160 {
		t.Fatal("cumulative task changed XP", g, err)
	}
	unlocked := false
	for _, task := range g.Tasks {
		if task.Code == "milestone-login-7" {
			unlocked = task.Awarded && task.Reward != ""
		}
	}
	if !unlocked {
		t.Fatal("collectible not persisted")
	}
	other, err := s.Growth(ctx, "different@example.com", now)
	if err != nil || other.XP != 0 {
		t.Fatal("owner isolation", other, err)
	}
}
