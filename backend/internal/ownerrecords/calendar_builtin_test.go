package ownerrecords

import (
	"testing"
	"time"
)

func TestBuiltinCalendar2026(t *testing.T) {
	v := builtinCalendar(2026)
	if !v.Builtin || v.Version != 0 || v.Document.Coverage != "official_full_year" || len(v.Document.Days) != 39 {
		t.Fatalf("missing complete builtin: %+v", v)
	}
	if builtinCalendar(2027).Document.Year != 0 {
		t.Fatal("unpublished year must not be guessed")
	}
	holidays, workdays := 0, 0
	for _, d := range v.Document.Days {
		if d.Kind == "holiday" {
			holidays++
		} else {
			workdays++
		}
	}
	if holidays != 33 || workdays != 6 {
		t.Fatal(holidays, workdays)
	}
	heads := builtinCalendars()
	for date, rest := range map[string]bool{"2026-01-04": false, "2026-02-14": false, "2026-02-28": false, "2026-05-09": false, "2026-09-20": false, "2026-10-10": false, "2026-09-25": true, "2026-10-07": true} {
		d, _ := time.Parse("2006-01-02", date)
		if got := calendarDay(d, heads); got.Rest != rest || !got.Known {
			t.Fatal(date, got)
		}
	}
	visits := calculateVisits([]string{"2026-09-24", "2026-09-28"}, heads, time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC))
	if visits.Streak != 2 || visits.Provisional {
		t.Fatal(visits)
	}
	// Callers cannot mutate the shared embedded document or another request.
	v.Document.Days[0].Kind = "workday"
	if builtinCalendar(2026).Document.Days[0].Kind != "holiday" {
		t.Fatal("shared mutable defaults")
	}
	p := calendarFixture()
	p.Coverage = "partial"
	p.Days = p.Days[:1]
	p.Sources[0].ID = "correction"
	p.Days[0].SourceID = "correction"
	merged, err := mergeCalendar(builtinCalendar(2026).Document, p)
	if err != nil || len(merged.Days) != 39 || merged.Coverage != "official_full_year" {
		t.Fatal("partial must preserve builtin year", err)
	}
}
