package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func TestOwnerInvalidationIsScoped(t *testing.T) {
	s := &Store{}
	now := time.Now()
	for _, owner := range []string{"a@example.com", "b@example.com"} {
		key := Key(owner, "day")
		s.registerRead(owner, key)
		if s.perf.low.growth == nil {
			s.perf.low.growth = map[string]growthRead{}
		}
		s.perf.low.growth[key] = growthRead{now, &Growth{XP: 10}}
		if s.perf.reads == nil {
			s.perf.reads = map[string]readEntry{}
		}
		s.perf.reads[Key(owner, "7d")] = readEntry{now, &Dashboard{}}
	}
	s.InvalidateOwner("A@example.com")
	if len(s.perf.low.growth) != 1 || s.perf.low.growth[Key("b@example.com", "day")].data == nil || len(s.perf.reads) != 1 || s.perf.cacheRevision.Load() != 1 {
		t.Fatal("invalidation must preserve unrelated owner")
	}
}

func verifyImmediateVisits(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2037, 2, 18, 4, 0, 0, 0, time.UTC)
	owner := "immediate@example.com"
	before, err := s.CachedGrowth(ctx, owner, now)
	if err != nil || before.XP != 0 {
		t.Fatal(before, err)
	}
	failed, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.VisitToday(failed, owner, now); err == nil {
		t.Fatal("cancelled visit succeeded")
	}
	if err = s.VisitToday(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	after, err := s.CachedGrowth(ctx, owner, now.Add(time.Second))
	if err != nil || after == before || after.XP != 10 {
		t.Fatal("stale cache or missing visit reward", after, err)
	}
	for _, task := range after.Tasks {
		if task.Metric == "login" && task.Period == "day" && (!task.Awarded || task.Progress != 1) {
			t.Fatal(task)
		}
	}
	// On another replica the existing login must still repair a missing reward.
	for n := 0; n < 3; n++ {
		replica := &Store{Session: s.Session}
		if err = replica.VisitToday(ctx, owner, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(xp_value),0) FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=?`, owner)
	if err != nil {
		t.Fatal(err)
	}
	var count, xp int
	if err = row.Scan(&count, &xp); err != nil || count != 1 || xp != 10 {
		t.Fatal("replay reward", count, xp, err)
	}
	// A completed visit has an in-memory fast path, without a database at all.
	fast := &Store{}
	fast.perf.low.visits = s.perf.low.visits
	if err = fast.VisitToday(ctx, owner, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	repaired := "visit-repair@example.com"
	day := now.In(mustShanghai()).Format("2006-01-02")
	_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,occurred_at) VALUES ('login_day',?,0,?,'confirmed',?,?)`, Key("login", repaired, day), repaired, day, now.UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.VisitToday(ctx, repaired, now); err != nil {
		t.Fatal(err)
	}
	fixed, err := s.Growth(ctx, repaired, now.Add(time.Second))
	if err != nil || fixed.XP != 10 {
		t.Fatal("existing fact must not suppress reward retry", fixed, err)
	}
	// Collection started before yesterday: no collected tokens yesterday is zero,
	// while the independent question stream remains unknown.
	_, err = s.Apply(ctx, Identity{Scope: 1, Owner: owner, Instance: 777}, Fact{Type: "invocation", Source: "refresh-test", SourceID: "old", Tokens: 20, At: now.AddDate(0, 0, -3)})
	if err != nil {
		t.Fatal(err)
	}
	insights, err := s.Insights(ctx, owner, nil, now)
	if err != nil || insights.Comparison["tokens"].Status != "flat" || insights.Comparison["questions"].Status != "insufficient" {
		t.Fatal(insights, err)
	}
}
func mustShanghai() *time.Location { loc, _ := time.LoadLocation("Asia/Shanghai"); return loc }
