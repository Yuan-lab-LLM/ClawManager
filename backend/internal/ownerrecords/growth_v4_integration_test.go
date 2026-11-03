package ownerrecords

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func verifyGrowthV4(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	initial := time.Date(2040, 1, 3, 4, 0, 0, 0, time.UTC)
	epoch, err := s.prepareAnalyticsEpoch(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.prepareAnalyticsEpoch(ctx, initial.Add(time.Hour))
	if err != nil || !epoch.Start.Equal(replayed.Start) || !epoch.RuleStart.Equal(replayed.RuleStart) {
		t.Fatal("epoch must survive initialization retries", epoch, replayed, err)
	}
	if !epoch.RuleStart.Equal(initial) {
		t.Fatal("new rules must activate immediately")
	}
	now := epoch.RuleStart.Add(time.Hour)
	owner := "v4-new-owner@example.com"
	if err = s.VisitToday(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	identity := Identity{Scope: 77, Owner: owner, Instance: 980001, Runtime: "hermes", Mode: "lite"}
	if applied, e := s.Apply(ctx, identity, Fact{Type: "question", Source: "v4-fixture", SourceID: "old", SessionID: "s", QuestionID: "old", SubmittedAt: epoch.Start.Add(-time.Second), At: now}); e != nil || applied {
		t.Fatal("pre-epoch question with late reply counted", applied, e)
	}
	if _, err = s.Session.SQL().Exec(`INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(980001,77,?,'hermes','lite')`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session.SQL().Exec(`INSERT INTO model_invocations(id,instance_id,session_id,status,created_at,prompt_tokens,completion_tokens,total_tokens) VALUES(980001,980001,'v4-session','completed',?,0,300000000,300000000)`, now.Add(time.Minute).UTC()); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if _, err = s.aggregateSourceDay(ctx, day, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 50; n++ {
		id := fmt.Sprint(n)
		if _, err = s.Apply(ctx, identity, Fact{Type: "question", Source: "v4-fixture", SourceID: id, SessionID: "s", QuestionID: id, At: now.Add(time.Minute), SubmittedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for n := 0; n < 3; n++ {
		if err = s.ReconcileGrowth(ctx, owner, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		g, e := s.Growth(ctx, owner, now.Add(2*time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		if g.Rule != growthRuleV4 || g.DailyLimit != 105 || g.XP != 3105 || len(g.LevelThresholds) != 50 || g.LevelThresholds[49] != 27000 {
			t.Fatalf("new rule or once-only rewards mismatch: rule=%s limit=%d xp=%d thresholds=%v", g.Rule, g.DailyLimit, g.XP, g.LevelThresholds)
		}
		var daily, cumulative int64
		for _, task := range g.Tasks {
			if !task.Awarded {
				continue
			}
			if task.Period == "day" {
				daily += task.XP
			} else {
				cumulative += task.XP
			}
		}
		if daily != 105 || cumulative != 3000 {
			t.Fatal("ledger/tasks disagree", daily, cumulative)
		}
	}
	if _, e := s.buildOwnerSnapshot(ctx, owner, now.Add(12*time.Minute), true); e != nil {
		t.Fatal(e)
	}
	replica := &Store{Session: s.Session}
	d, e := replica.CachedDashboard(ctx, owner, nil, "7d", now.Add(12*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if d.Summary.TotalTokens != 300000000 || d.Summary.Questions == nil || *d.Summary.Questions != 50 || d.Growth == nil || d.Growth.XP != 3105 {
		t.Fatal("published replica snapshot disagrees", d)
	}
	version := d.Snapshot.Version
	if _, e := s.buildOwnerSnapshot(ctx, owner, now.Add(12*time.Minute+time.Second), true); e != nil {
		t.Fatal(e)
	}
	d, e = replica.CachedDashboard(ctx, owner, nil, "7d", now.Add(12*time.Minute+time.Second))
	if e != nil || d.Snapshot.Version != version {
		t.Fatal("unchanged snapshot rewritten", e)
	}
	r, e := replica.CachedReport(ctx, owner, "month", now.Format("2006-01-02"), now.Add(12*time.Minute))
	if e != nil || r.Counts["tokens"] != 300000000 || r.UsedSessions != 1 {
		t.Fatal("published report mismatch", r, e)
	}
	// An old daily award is credited on switching rules, not paid twice.
	legacy := "v4-credited@example.com"
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,occurred_at) VALUES('growth_profile',?,0,?,'active',10,?)`, []any{Key("growth", legacy), legacy, initial}},
		{`INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,task_code,business_date,occurred_at,definition_version) VALUES('xp_event',?,0,?,'awarded',10,'login-day',?,?,'growth-v3')`, []any{Key("old-award", legacy), legacy, now.In(loc).Format("2006-01-02"), now}},
	} {
		if _, e = s.Session.SQL().Exec(q.sql, q.args...); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.VisitToday(ctx, legacy, now); e != nil {
		t.Fatal(e)
	}
	g, e := s.CachedGrowth(ctx, legacy, now)
	if e != nil || g.XP != 10 || g.TodayXP != 10 || !g.Tasks[0].Awarded {
		t.Fatal("legacy daily credit", g, e)
	}
	if e = s.publishRanking(ctx, now.Add(12*time.Minute)); e != nil {
		t.Fatal(e)
	}
	rank, e := replica.TokenRanking(ctx, owner, now.Add(13*time.Minute))
	if e != nil || rank == nil || rank.Tokens != 300000000 {
		t.Fatal("shared ranking", rank, e)
	}
	if e = s.batchState(ctx, now, "updating"); e != nil {
		t.Fatal(e)
	}
	d, e = replica.CachedDashboard(ctx, owner, nil, "7d", now.Add(13*time.Minute))
	if e != nil || d.Snapshot.State != "initializing" {
		t.Fatal("initialization status lost", e)
	}
	if _, e = s.Session.SQL().Exec(`UPDATE northbound_owner_records SET status='ready' WHERE record_type='analytics_epoch'`); e != nil {
		t.Fatal(e)
	}
	d, e = replica.CachedDashboard(ctx, owner, nil, "7d", now.Add(13*time.Minute))
	if e != nil || d.Snapshot.State != "updating" {
		t.Fatal("partial batch falsely ready", e)
	}
	verifyV4SourceCapacity(t, s, day.AddDate(0, 0, 1))
}

func verifyV4SourceCapacity(t *testing.T, s *Store, day time.Time) {
	t.Helper()
	ctx := context.Background()
	var iv, cv []string
	var ia, ca []any
	for n := 0; n < 500; n++ {
		id := 970000 + n
		owner := fmt.Sprintf("v4-capacity-%d@example.com", n)
		iv = append(iv, "(?,88,?,'hermes','lite')")
		ia = append(ia, id, owner)
		cv = append(cv, "(?,?,'capacity','completed',?,10,10,20)")
		ca = append(ca, id, id, day.Add(time.Hour).UTC())
	}
	if _, e := s.Session.SQL().Exec(`INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES `+strings.Join(iv, ","), ia...); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Session.SQL().Exec(`INSERT INTO model_invocations(id,instance_id,session_id,status,created_at,prompt_tokens,completion_tokens,total_tokens) VALUES `+strings.Join(cv, ","), ca...); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	count, e := s.aggregateSourceDay(ctx, day, day.AddDate(0, 0, 1))
	if e != nil || count != 1000 {
		t.Fatal("500-owner aggregate", count, e)
	}
	t.Logf("V4 numeric aggregation: 500 owners, 500 calls, elapsed=%s", time.Since(start))
	count, e = s.aggregateSourceDay(ctx, day, day.AddDate(0, 0, 1))
	if e != nil || count != 0 {
		t.Fatal("unchanged aggregate rewrote owner facts", count, e)
	}
}
