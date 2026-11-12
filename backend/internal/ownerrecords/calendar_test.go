package ownerrecords

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func calendarFixture() CalendarDocument {
	return CalendarDocument{1, "CN", "Asia/Shanghai", 2026, "official_full_year", []CalendarSource{{"gov-2026", "测试通知", "https://www.gov.cn/test", "2025-11-01"}}, []CalendarDay{{"2026-09-25", "holiday", "中秋节", "gov-2026"}, {"2026-09-26", "holiday", "中秋节", "gov-2026"}, {"2026-09-27", "holiday", "中秋节", "gov-2026"}, {"2026-10-10", "workday", "补班", "gov-2026"}}}
}
func TestCalendarValidationAndStreak(t *testing.T) {
	d := calendarFixture()
	raw, _ := json.Marshal(d)
	if _, e := ParseCalendar(raw); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(raw), "www.gov.cn", "example.com", 1), strings.Replace(string(raw), `"year":2026`, `"year":2027`, 1), strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"unknown":1`, 1)} {
		if _, e := ParseCalendar([]byte(bad)); e == nil {
			t.Fatal("invalid accepted", bad)
		}
	}
	d.Days = append(d.Days, d.Days[0])
	raw, _ = json.Marshal(d)
	if _, e := ParseCalendar(raw); e == nil {
		t.Fatal("duplicate day accepted")
	}
	d = calendarFixture()
	heads := map[int]CalendarVersion{2026: {Document: d, Version: 1}}
	now := time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC)
	v := calculateVisits([]string{"2026-09-23", "2026-09-24", "2026-09-26"}, heads, now)
	if v.Streak != 3 || v.Total != 3 || v.Provisional {
		t.Fatal("holiday visit/protection", v)
	}
	v = calculateVisits([]string{"2026-09-23", "2026-09-24"}, heads, now.AddDate(0, 0, 1))
	if v.Streak != 0 || v.Longest != 2 {
		t.Fatal("ended workday must break")
	}
	v = calculateVisits([]string{"2026-10-09"}, heads, time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC))
	if v.Streak != 0 {
		t.Fatal("Saturday makeup must break")
	}
	dates := []string{}
	for x := now.AddDate(0, 0, -120); !x.After(now); x = x.AddDate(0, 0, 1) {
		dates = append(dates, x.Format("2006-01-02"))
	}
	v = calculateVisits(dates, heads, now)
	if v.Streak != 121 || v.Longest != 121 {
		t.Fatal("90 day clipping")
	}
	v = calculateVisits([]string{"2025-12-31", "2026-01-01"}, heads, time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC))
	if v.Streak != 2 || !v.Provisional {
		t.Fatal("cross year provisional")
	}
	p := calendarFixture()
	p.Coverage = "partial"
	p.Days = p.Days[3:]
	merged, e := mergeCalendar(d, p)
	if e != nil || len(merged.Days) != 4 || merged.Coverage != "official_full_year" {
		t.Fatal("partial removed dates", e)
	}
}
func verifyCalendarAndReportIsolation(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	d := calendarFixture()
	head, e := s.CalendarHead(ctx, 2026)
	if e != nil || !head.Builtin || head.Version != 0 || len(head.Document.Days) != 39 {
		t.Fatal("read-only builtin fallback", head, e)
	}
	history, e := s.CalendarVersions(ctx, 2026)
	if e != nil || len(history) != 0 {
		t.Fatal("builtin must not create revisions", e)
	}
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='calendar_head'`)
	if e != nil {
		t.Fatal(e)
	}
	var heads int
	if e = row.Scan(&heads); e != nil || heads != 0 {
		t.Fatal("read seeded database", heads, e)
	}
	partial := calendarFixture()
	partial.Coverage = "partial"
	partial.Days = partial.Days[:1]
	partial.Sources[0].ID = "correction"
	partial.Days[0].SourceID = "correction"
	preview, e := s.PreviewCalendar(ctx, partial, now)
	if e != nil || preview.ExpectedVersion != 0 || len(preview.Document.Days) != 39 {
		t.Fatal("preview must merge builtin", e)
	}
	first, e := s.PublishCalendar(ctx, partial, 0, 1, now)
	if e != nil || first.Version != 1 || first.Builtin || len(first.Document.Days) != 39 {
		t.Fatal("publish", e)
	}
	if _, e = s.PublishCalendar(ctx, d, 0, 2, now); !errors.Is(e, ErrCalendarConflict) {
		t.Fatal("lost update accepted", e)
	}
	p, e := s.PreviewCalendar(ctx, d, now)
	if e != nil || p.ExpectedVersion != 1 {
		t.Fatal("preview", e)
	}
	count := func() int {
		r, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('login_day','xp_ledger')`)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = r.Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	before := count()
	if _, e = s.PortalUsers(ctx, "", "streak", 1, now); e != nil {
		t.Fatal("admin", e)
	}
	if _, e = s.OwnerVisitCalendar(ctx, "native@example.com", now); e != nil {
		t.Fatal("visits", e)
	}
	if count() != before {
		t.Fatal("admin signed in or rewarded user")
	}
	d.Coverage = "partial"
	d.Days = d.Days[:1]
	second, e := s.PublishCalendar(ctx, d, 1, 1, now, true)
	if e != nil || len(second.Document.Days) != 1 {
		t.Fatal("restore replacement", e)
	}
	head, e = s.CalendarHead(ctx, 2026)
	if e != nil || head.Builtin || head.Version != 2 || len(head.Document.Days) != 1 {
		t.Fatal("administrator revision must override builtin, not silently merge", e)
	}
	history, e = s.CalendarVersions(ctx, 2026)
	if e != nil || len(history) != 2 {
		t.Fatal("immutable audit", e)
	}
	// A failed report is a separate job; it cannot change a published core snapshot.
	owner := "native@example.com"
	if e = s.queueReportPreviews(ctx, owner, now); e != nil {
		t.Fatal(e)
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	e = s.processReports(bounded, now)
	cancel()
	if e != nil {
		t.Fatal("independent report phase", e)
	}
	r, e := s.reportPreview(ctx, owner, "week", "2026-09-28", now)
	if e != nil || r == nil {
		t.Fatal("preview saved", e)
	}
	if _, e = s.reportPreview(ctx, owner, "week", "2026-09-28", now.AddDate(0, 0, 7)); e == nil {
		t.Fatal("closed period reused ongoing preview")
	}
	// Concentrated report work stays bounded and advances without holding core state.
	if _, e = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET expires_at=? WHERE record_type='report_dirty'`, now.AddDate(1, 0, 0)); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 355; n++ {
		o := fmt.Sprintf("report-burst-%d@example.test", n)
		if _, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,period_type,period_start,expires_at,version) VALUES('report_dirty',?,0,?,'pending','week','2026-09-21',?,1)`, Key("report-dirty", o, "week", "2026-09-21"), o, now.Add(-time.Hour)); e != nil {
			t.Fatal(e)
		}
	}
	remaining := func() int {
		r, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='report_dirty' AND owner_normalized LIKE 'report-burst-%' AND status='pending'`)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = r.Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	previous := remaining()
	for pass := 0; pass < 2; pass++ {
		work, cancel := context.WithTimeout(ctx, 3*time.Second)
		e = s.processDueReports(work, now)
		cancel()
		if e != nil {
			t.Fatal("burst report pass", e)
		}
		left := remaining()
		if left >= previous || previous-left > 5 {
			t.Fatal("bounded report queue failed", previous, left)
		}
		previous = left
	}
}
