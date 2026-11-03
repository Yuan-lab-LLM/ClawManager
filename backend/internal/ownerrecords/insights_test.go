package ownerrecords

import (
	"testing"
	"time"
)

func TestAdaptiveComparisonWindows(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 9, 17, 14, 0, 0, 0, loc)
	for _, tc := range []struct {
		days int
		kind string
	}{{0, "day"}, {6, "day"}, {7, "week"}, {30, "week"}, {31, "month"}} {
		w := ComparisonWindow(now.AddDate(0, 0, -tc.days), now)
		if w.Kind != tc.kind || w.End.Sub(w.Start) != w.PreviousEnd.Sub(w.PreviousStart) {
			t.Fatalf("%+v: %+v", tc, w)
		}
	}
	march := time.Date(2026, 3, 31, 14, 0, 0, 0, loc)
	w := ComparisonWindow(march.AddDate(0, 0, -60), march)
	if w.PreviousEnd.Month() != time.February || w.PreviousEnd.Day() != 28 || w.End.Day() != 28 {
		t.Fatal(w)
	}
}

func TestMetricComparisonMissingAndZero(t *testing.T) {
	if c := CompareMetric(10, 0, false); c.Status != "insufficient" || c.Percent != nil {
		t.Fatal(c)
	}
	if c := CompareMetric(10, 0, true); c.Status != "new" || c.Percent != nil {
		t.Fatal(c)
	}
	if c := CompareMetric(0, 0, true); c.Status != "flat" {
		t.Fatal(c)
	}
	if c := CompareMetric(120, 100, true); c.Percent == nil || *c.Percent != 20 || c.Delta != 20 {
		t.Fatal(c)
	}
}
