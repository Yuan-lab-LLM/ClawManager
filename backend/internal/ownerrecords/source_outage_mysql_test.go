package ownerrecords

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSyncResilienceMySQLCoverageIncludesOlderAndMissingDays(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	today := sourceLocalDay(time.Now())
	first := today.AddDate(0, 0, -3)
	if _, err := s.prepareAnalyticsEpoch(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, day := range []time.Time{first, today.AddDate(0, 0, -1), today} {
		resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'ready',?)`, Key("source-day-cursor", day.Format("2006-01-02")), day.AddDate(0, 0, 1).UTC())
	}
	now := today.Add(14 * time.Hour)
	covered, err := s.sourceAsOf(ctx, now)
	if err != nil || !covered.Equal(first.AddDate(0, 0, 1)) {
		t.Fatal("missing date older than yesterday was reported as fresh", covered, err)
	}
	gap := first.Add(18*time.Hour + 10*time.Minute)
	resilienceExec(t, s, `UPDATE northbound_owner_records SET interval_end=? WHERE record_type='source_day_cursor' AND record_key=?`, gap.UTC(), Key("source-day-cursor", first.Format("2006-01-02")))
	covered, err = s.sourceAsOf(ctx, now)
	if err != nil || !covered.Equal(gap) {
		t.Fatal("older partial date was skipped", covered, err)
	}
	resilienceExec(t, s, `UPDATE northbound_owner_records SET status='initializing' WHERE record_type='source_day_cursor' AND record_key=?`, Key("source-day-cursor", first.Format("2006-01-02")))
	covered, err = s.sourceAsOf(ctx, now)
	if err != nil || !covered.Equal(first) {
		t.Fatal("unconfirmed cursor was treated as committed coverage", covered, err)
	}
}

func TestSyncResilienceMySQLOutage39HoursFreshLaneAndRestart(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	today := sourceLocalDay(time.Now())
	first := today.AddDate(0, 0, -2)
	gap := first.Add(23 * time.Hour)
	now := today.Add(14 * time.Hour)
	if _, err := s.prepareAnalyticsEpoch(ctx, first); err != nil {
		t.Fatal(err)
	}
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(1,99,'outage@example.test','openclaw','lite')`)
	for n, at := range []time.Time{gap.Add(time.Minute), first.Add(25 * time.Hour), now.Add(-time.Minute)} {
		resilienceExec(t, s, `INSERT INTO model_invocations VALUES(?,1,'same-session','completed',?,0,10,10)`, n+1, at.UTC())
	}
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'ready',?)`, Key("source-day-cursor", first.Format("2006-01-02")), gap.UTC())
	// A failed old bucket must not prevent the latest closed bucket from being
	// collected. No historical content or pending work is removed on failure.
	resilienceExec(t, s, `CREATE TRIGGER fail_old_bucket BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='source_usage' AND NEW.business_date=DATE('`+first.Format("2006-01-02")+`') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic old bucket failure'; END IF; END`)
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := s.refreshSource(withSourceReadBatch(work), now)
	cancel()
	if err == nil {
		t.Fatal("injected old-day error was hidden")
	}
	if n := resilienceInt(t, s, `SELECT COALESCE(SUM(total_tokens),0) FROM northbound_owner_records WHERE record_type='source_usage' AND business_date=?`, today.Format("2006-01-02")); n != 10 {
		t.Fatal("historical backlog starved the current numeric bucket", n, err)
	}
	covered, err := s.sourceAsOf(ctx, now)
	if err != nil || !covered.Equal(gap) {
		t.Fatal("a newer collected bucket concealed the old gap", covered, err)
	}
	resilienceExec(t, s, `DROP TRIGGER fail_old_bucket`)
	for pass := 0; pass < 8; pass++ {
		// Reconstruct the Store on every pass: progress must be in SQL, not in
		// a process-local cursor that disappears on leader/App restart.
		restarted := &Store{Session: s.Session}
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = restarted.refreshSource(withSourceReadBatch(work), now)
		cancel()
		if err != nil && !errors.Is(err, errSyncYield) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("catch-up failed", err)
		}
		covered, err = restarted.sourceAsOf(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if covered.Equal(now) {
			break
		}
	}
	if !covered.Equal(now) {
		t.Fatal("39-hour multi-day outage did not resume from its durable gap", covered)
	}
	for replay := 0; replay < 2; replay++ {
		if err = s.refreshSource(withSourceReadBatch(ctx), now); err != nil {
			t.Fatal("idle replay", err)
		}
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 30 {
		t.Fatal("catch-up omitted or double-counted saved usage", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_session'`); n != 1 {
		t.Fatal("cross-day replay duplicated the same session", n)
	}
}

func TestSyncResilienceMySQLRepairFailureRetainsOtherLanes(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	today := sourceLocalDay(time.Now())
	now := today.Add(12 * time.Hour)
	old := today.AddDate(0, 0, -2).Add(time.Hour)
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(1,99,'repair-lanes@example.test','openclaw','lite')`)
	resilienceExec(t, s, `INSERT INTO model_invocations VALUES(1,1,'old','completed',?,0,7,7),(2,1,'new','completed',?,0,11,11)`, old.Add(time.Minute).UTC(), now.Add(-time.Minute).UTC())
	key := Key("source-window", old.UTC().Format(time.RFC3339))
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start,interval_end,version) VALUES('source_window_dirty',?,0,'pending',?,?,1)`, key, old.UTC(), old.Add(summaryInterval).UTC())
	resilienceExec(t, s, `CREATE TRIGGER fail_discovered_bucket BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='source_usage' AND NEW.business_date=DATE('`+sourceLocalDay(old).Format("2006-01-02")+`') THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic dirty bucket failure'; END IF; END`)
	if err := s.repairSource(withSourceReadBatch(ctx), now); err == nil || errors.Is(err, errSyncYield) {
		t.Fatal("failed dirty window must remain an actual failure", err)
	}
	if n := resilienceInt(t, s, `SELECT COALESCE(SUM(total_tokens),0) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 11 {
		t.Fatal("dirty queue failure starved the latest-bucket repair", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_repair_cursor'`); n != 2 {
		t.Fatal("recent/deeper repair lost their reserved capacity", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_window_dirty' AND record_key=? AND status='pending' AND version=1`, key); n != 1 {
		t.Fatal("failed work was discarded or falsely acknowledged", n)
	}
}
