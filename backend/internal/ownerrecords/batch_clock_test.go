package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func TestSummaryBatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitSummaryBatch(ctx) {
		t.Fatal("cancelled worker must stop without another pass")
	}
}

func TestSummaryBatchClock(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"2026-09-22T10:00:00+08:00", "2026-09-22T02:10:00Z"},
		{"2026-09-22T10:09:59+08:00", "2026-09-22T02:10:00Z"},
		{"2026-09-22T10:10:00+08:00", "2026-09-22T02:20:00Z"},
		{"2026-09-22T23:59:59+08:00", "2026-09-22T16:00:00Z"},
	} {
		at, _ := time.Parse(time.RFC3339, tc.input)
		if nextSummaryBatch(at).Format(time.RFC3339) != tc.want {
			t.Fatal(tc)
		}
	}
}
