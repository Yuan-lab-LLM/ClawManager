package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func verifySourceRollup(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := time.Date(2041, 1, 2, 0, 0, 0, 0, loc)
	if _, err := s.aggregateSourceDay(ctx, day.Add(time.Hour), day.Add(2*time.Hour)); err == nil {
		t.Fatal("non-day start accepted")
	}
	if _, err := s.Session.SQL().Exec(`INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(990001,88,'rollup@example.com','hermes','lite')`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id      int
		at      time.Time
		session string
		tokens  int
	}{{990001, day.Add(time.Hour), "session-a", 10}, {990002, day.Add(2 * time.Hour), "session-a", 20}, {990003, day.Add(25 * time.Hour), "session-a", 30}, {990004, day.Add(26 * time.Hour), "sess_trc_fake", 40}} {
		if _, err := s.Session.SQL().Exec(`INSERT INTO model_invocations(id,instance_id,session_id,status,created_at,prompt_tokens,completion_tokens,total_tokens) VALUES(?,990001,?,'completed',?,0,?,?)`, v.id, v.session, v.at.UTC(), v.tokens, v.tokens); err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range []time.Time{day.AddDate(0, 0, 1), day} {
		if _, err := s.aggregateSourceDay(ctx, at, at.AddDate(0, 0, 1)); err != nil {
			t.Fatal(err)
		}
		if n, err := s.aggregateSourceDay(ctx, at, at.AddDate(0, 0, 1)); err != nil || n != 0 {
			t.Fatal("unchanged replay wrote owner rows", n, err)
		}
	}
	row, err := s.Session.SQL().QueryRow(`SELECT SUM(total_tokens),SUM(invocation_count) FROM northbound_owner_records WHERE record_type='source_usage' AND owner_normalized='rollup@example.com'`)
	if err != nil {
		t.Fatal(err)
	}
	var tokens, calls int
	if err = row.Scan(&tokens, &calls); err != nil || tokens != 100 || calls != 4 {
		t.Fatal("absolute usage", tokens, calls, err)
	}
	row, err = s.Session.SQL().QueryRow(`SELECT COUNT(*),MIN(occurred_at) FROM northbound_owner_records WHERE record_type='source_session' AND owner_normalized='rollup@example.com'`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var first time.Time
	if err = row.Scan(&count, &first); err != nil || count != 1 || !first.Equal(day.Add(time.Hour)) {
		t.Fatal("cross-day session dedupe", count, first, err)
	}
	// A stale bootstrap covering less of the day must not roll totals backwards.
	if n, err := s.aggregateSourceDay(ctx, day, day.Add(90*time.Minute)); err != nil || n != 0 {
		t.Fatal("stale bootstrap overwrote live interval", n, err)
	}
	if _, err = s.Session.SQL().Exec(`UPDATE model_invocations SET total_tokens=25,completion_tokens=25 WHERE id=990002`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.aggregateSourceDay(ctx, day, day.AddDate(0, 0, 1)); err != nil || n != 1 {
		t.Fatal("late correction", n, err)
	}
	row, err = s.Session.SQL().QueryRow(`SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage' AND owner_normalized='rollup@example.com'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&tokens); err != nil || tokens != 105 {
		t.Fatal("correction must replace, not add", tokens, err)
	}
	if _, err = s.Session.SQL().Exec(`UPDATE model_invocations SET status='pending' WHERE id IN (990001,990002)`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.aggregateSourceDay(ctx, day, day.AddDate(0, 0, 1)); err != nil || n != 2 {
		t.Fatal("removed terminal group must become zero", n, err)
	}
	row, err = s.Session.SQL().QueryRow(`SELECT SUM(total_tokens),SUM(invocation_count) FROM northbound_owner_records WHERE record_type='source_usage' AND owner_normalized='rollup@example.com' AND business_date='2041-01-02'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&tokens, &calls); err != nil || tokens != 0 || calls != 0 {
		t.Fatal("stale aggregate after source correction", tokens, calls, err)
	}
}
