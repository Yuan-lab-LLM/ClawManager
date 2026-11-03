package ownerrecords

import (
	"testing"
	"time"
)

func TestWorkdayStreak(t *testing.T) {
	for _, tc := range []struct {
		name, now string
		dates     []string
		want      int
	}{
		{"empty", "2026-09-21T04:00:00Z", nil, 0},
		{"Monday not used yet retains Friday", "2026-09-21T04:00:00Z", []string{"2026-09-18", "2026-09-17"}, 2},
		{"Monday used skips weekend", "2026-09-21T04:00:00Z", []string{"2026-09-21", "2026-09-18", "2026-09-17", "2026-09-21"}, 3},
		{"weekend not extra workday", "2026-09-20T04:00:00Z", []string{"2026-09-20", "2026-09-18"}, 1},
		{"missed previous workday", "2026-09-22T04:00:00Z", []string{"2026-09-18"}, 0},
		{"future and invalid ignored", "2026-09-21T04:00:00Z", []string{"2026-09-22", "garbage"}, 0},
		{"Shanghai midnight", "2026-09-20T16:01:00Z", []string{"2026-09-21", "2026-09-18"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, tc.now)
			if got := WorkdayStreak(tc.dates, at); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
