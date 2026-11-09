package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func TestActivityBufferNoDatabase(t *testing.T) {
	s := &Store{}
	now := time.Date(2036, 2, 18, 12, 0, 0, 0, time.UTC)
	pulse := func(stream string, seq uint64, at time.Time, active bool) {
		t.Helper()
		if err := s.BufferActivity("fixture@example.com", 1, ActivityPulse{Stream: stream, Sequence: seq, Active: active}, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, stream := range []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"} {
		for n := 0; n <= 5; n++ {
			pulse(stream, uint64(n+1), now.Add(time.Duration(n)*time.Minute), true)
		}
	}
	if len(s.perf.low.windows) != 1 {
		t.Fatal("not a single merged window", len(s.perf.low.windows))
	}
	for _, w := range s.perf.low.windows {
		if w.mask != 31 {
			t.Fatal("overlapping tabs counted incorrectly", w.mask)
		}
	}
	pulse("aaaaaaaaaaaaaaaa", 6, now.Add(5*time.Minute), true)
	pulse("aaaaaaaaaaaaaaaa", 7, now.Add(60*time.Minute), true)
	if len(s.perf.low.windows) != 1 {
		t.Fatal("offline time counted")
	}
	pulse("aaaaaaaaaaaaaaaa", 8, now.Add(61*time.Minute), false)
	pulse("aaaaaaaaaaaaaaaa", 9, now.Add(62*time.Minute), true)
	if len(s.perf.low.windows) != 1 {
		t.Fatal("idle time counted")
	}
}
func TestLowCostQueuesAndCache(t *testing.T) {
	s := &Store{}
	now := time.Now()
	for n := 0; n < 5000; n++ {
		s.QueueBinding(uint64(n + 1))
		s.QueueOpenClaw(1, OpenClawEvent{Kind: "submitted"}, now)
	}
	if len(s.perf.low.bindings) != 2048 || len(s.perf.low.events) != 2048 {
		t.Fatal("queue bound")
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now = time.Date(2036, 2, 18, 12, 0, 0, 0, loc)
	s.perf.low.growth = map[string]growthRead{Key("fixture@example.com", now.Format("2006-01-02")): {now, &Growth{XP: 123}}}
	g, err := s.CachedGrowth(context.Background(), "fixture@example.com", now.Add(9*time.Minute+59*time.Second))
	if err != nil || g.XP != 123 {
		t.Fatal("cache hit queried DB", err)
	}
}

func verifyBufferedActivity(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	owner := "activity-buffer@example.com"
	now := time.Date(2036, 2, 18, 12, 0, 0, 0, time.UTC)
	if err := s.VisitGrowth(ctx, owner, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		replica := &Store{Session: s.Session}
		for n := 0; n <= 5; n++ {
			if err := replica.BufferActivity(owner, 1, ActivityPulse{Stream: "aaaaaaaaaaaaaaaa", Sequence: uint64(n + 1), Active: true}, now.Add(time.Duration(n)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		if err := replica.flushActivity(ctx); err != nil {
			t.Fatal(err)
		}
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*),SUM(active_work_seconds) FROM northbound_owner_records WHERE record_type='activity_window' AND owner_normalized=?`, owner)
	if err != nil {
		t.Fatal(err)
	}
	var count, seconds int
	if err = row.Scan(&count, &seconds); err != nil || count != 1 || seconds != 300 {
		t.Fatal("replica flush replay", count, seconds, err)
	}
	cached := &Store{Session: s.Session}
	g, err := cached.CachedGrowth(ctx, owner, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 20; n++ {
		again, e := cached.CachedGrowth(ctx, owner, now.Add(7*time.Minute))
		if e != nil || g != again {
			t.Fatal("not a shared cache hit", e)
		}
	}
}
