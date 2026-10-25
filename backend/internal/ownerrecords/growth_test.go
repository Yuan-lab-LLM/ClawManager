package ownerrecords

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGrowthDoesNotExposePlannedLevel(t *testing.T) {
	encoded, err := json.Marshal(Growth{PlannedLevel: 100})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "planned_level") {
		t.Fatal("internal roadmap must not be exposed", string(encoded))
	}
}

func TestLevelThresholds(t *testing.T) {
	for _, tc := range []struct {
		xp    int64
		level int
	}{{0, 1}, {119, 1}, {120, 2}, {249, 2}, {250, 3}, {17639, 49}, {17640, 50}, {1000000, 50}} {
		l, _, _ := levelForXP(tc.xp)
		if l != tc.level {
			t.Fatal(tc, l)
		}
	}
}

func TestGrowthV2CurveAndRewards(t *testing.T) {
	var daily int64
	nodes := 0
	for _, task := range growthTasksV2() {
		if task.Period == "day" {
			daily += task.XP
			nodes++
		} else if task.XP != 0 || task.Reward == "" {
			t.Fatal("milestones must unlock collectibles without XP", task)
		}
	}
	if daily != 100 || nodes != 7 {
		t.Fatal(daily, nodes)
	}
	for level := 1; level < plannedGrowthLevel; level++ {
		if delta := levelThreshold(level+1) - levelThreshold(level); delta != 120+10*int64(level-1) {
			t.Fatal(level, delta)
		}
	}
	if levelThreshold(50) != 17640 {
		t.Fatal("177 full days must unlock current maximum")
	}
	for _, days := range []int64{176, 177, 180} {
		level, _, next := levelForXP(days * daily)
		if (days == 176 && level != 49) || (days >= 177 && (level != 50 || next != 17640)) {
			t.Fatal(days, level, next)
		}
	}
}
func verifyGrowthAndReports(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	owner := "growth@example.com"
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, loc)
	if err := s.VisitGrowth(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	identity := Identity{Scope: 31, Owner: owner, Instance: 1100, Runtime: "hermes", Mode: "lite"}
	for _, q := range []struct {
		id string
		at time.Time
	}{{"old", now.Add(-time.Hour)}, {"new", now.Add(time.Second)}} {
		if _, err := s.Apply(ctx, identity, Fact{Type: "question", Source: "test", SourceID: q.id, QuestionID: q.id, SessionID: "s", At: q.at}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- s.ReconcileGrowth(ctx, owner, now.Add(time.Minute)) }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	g, err := s.Growth(ctx, owner, now.Add(time.Minute))
	if err != nil || g.XP != 7 {
		t.Fatal("reward replay/history exclusion", g, err)
	}
	for _, stream := range []string{"test-stream-000001", "test-stream-000002"} {
		for seq := uint64(1); seq <= 3; seq++ {
			if err = s.ActivityPulse(ctx, owner, 1100, ActivityPulse{Stream: stream, Sequence: seq, Active: true}, now.Add(time.Duration(seq)*10*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var seconds uint64
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(active_work_seconds),0) FROM northbound_owner_records WHERE record_type='activity_tick' AND owner_normalized=?`, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&seconds); err != nil || seconds != 20 {
		t.Fatal("overlapping activity", seconds, err)
	}
	if err = s.ActivityPulse(ctx, owner, 1100, ActivityPulse{Stream: "test-stream-000001", Sequence: 4, Active: true}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Report(ctx, owner, "week", "2026-09-14", now.AddDate(0, 0, 10), true)
	if err != nil || r.Revision != 1 {
		t.Fatal("report snapshot", r, err)
	}
	r, err = s.Report(ctx, owner, "week", "2026-09-14", now.AddDate(0, 0, 11), true)
	if err != nil || r.Revision != 1 {
		t.Fatal("report replay", r, err)
	}
	if _, err = s.Apply(ctx, identity, Fact{Type: "question", Source: "test", SourceID: "late", QuestionID: "late", SessionID: "s", At: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Report(ctx, owner, "week", "2026-09-14", now.AddDate(0, 0, 12), true)
	if err != nil || r.Revision != 2 {
		t.Fatal("report late revision", r, err)
	}
	insights, err := s.Insights(ctx, owner, nil, now.Add(time.Hour))
	if err != nil || len(insights.Days) != 90 || insights.Window.Kind != "day" {
		t.Fatal(insights, err)
	}
	for n := 0; n < 2; n++ {
		if err = s.RefreshDaily(ctx, owner, now.Add(time.Hour)); err != nil {
			t.Fatal("daily projection", err)
		}
	}
	var questionCount uint64
	row, err = s.Session.SQL().QueryRowContext(ctx, `SELECT SUM(question_count) FROM northbound_owner_records WHERE record_type='daily_usage' AND owner_normalized=?`, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&questionCount); err != nil || questionCount != 3 {
		t.Fatal("projection replay", questionCount, err)
	}
	if _, err = s.Apply(ctx, identity, Fact{Type: "question", Source: "test", SourceID: "late-next-day", QuestionID: "late-next-day", SessionID: "s", At: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileGrowth(ctx, owner, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal("historical daily reconciliation", err)
	}
	g, err = s.Growth(ctx, owner, now.AddDate(0, 0, 1))
	if err != nil || g.XP != 11 {
		t.Fatal("missed past-day reward", g, err)
	}
	if _, err = s.RefreshOwnerProjections(ctx, 0); err != nil {
		t.Fatal("projection worker", err)
	}
	saved, err := s.SavedReport(ctx, owner, "week", "2026-09-14", 1, now.AddDate(0, 0, 12))
	if err != nil || saved.Revision != 1 || saved.Counts["questions"] != 2 {
		t.Fatal("old report was overwritten", saved, err)
	}
	if _, err = s.SavedReport(ctx, "other@example.com", "week", "2026-09-14", 1, now.AddDate(0, 0, 12)); err == nil {
		t.Fatal("cross-owner report exposed")
	}
	// Controlled disposable-DB fixture, never fabricate live user activity.
	workOwner := "work-thresholds@example.com"
	if err = s.VisitGrowth(ctx, workOwner, now); err != nil {
		t.Fatal(err)
	}
	for _, threshold := range []struct {
		seconds uint64
		xp      int64
	}{{899, 5}, {900, 10}, {1799, 10}, {1800, 20}, {3599, 20}, {3600, 55}} {
		_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,active_work_seconds,business_date,occurred_at) VALUES ('activity_tick',?,0,?,'confirmed',?,?,?) ON DUPLICATE KEY UPDATE active_work_seconds=VALUES(active_work_seconds)`, Key("test-work", workOwner), workOwner, threshold.seconds, now.Format("2006-01-02"), now.Add(time.Second).UTC())
		if err != nil {
			t.Fatal(err)
		}
		for replay := 0; replay < 2; replay++ {
			if err = s.ReconcileGrowth(ctx, workOwner, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			result, e := s.Growth(ctx, workOwner, now.Add(time.Hour))
			if e != nil || result.XP != threshold.xp {
				t.Fatal("work threshold/replay", threshold, result, e)
			}
		}
	}
}
