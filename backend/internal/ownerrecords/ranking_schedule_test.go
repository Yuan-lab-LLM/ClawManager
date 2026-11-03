package ownerrecords

import (
	"context"
	"testing"
	"time"
)

func TestRankingRefreshSchedule(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at := func(day, hour, minute, second int) time.Time {
		return time.Date(2026, 9, day, hour, minute, second, 0, loc)
	}
	snapshot := rankingSnapshot{at: at(21, 21, 20, 0), owners: map[string]TokenRanking{}}
	for _, tc := range []struct {
		name     string
		snapshot rankingSnapshot
		now      time.Time
		want     bool
	}{
		{"before expiry", snapshot, at(21, 21, 49, 59), false},
		{"expiry", snapshot, at(21, 21, 50, 0), true},
		{"last second", snapshot, at(21, 21, 59, 59), true},
		{"boundary", snapshot, at(21, 22, 0, 0), false},
		{"midnight", snapshot, at(22, 0, 0, 0), false},
		{"before reopening", snapshot, at(22, 5, 59, 59), false},
		{"reopening", snapshot, at(22, 6, 0, 0), true},
		{"cold deferred", rankingSnapshot{}, at(21, 23, 0, 0), false},
		{"cold available", rankingSnapshot{}, at(22, 6, 0, 0), true},
		{"UTC conversion", snapshot, at(21, 22, 0, 0).UTC(), false},
		{"clock correction", snapshot, at(21, 20, 0, 0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rankingRefreshDue(tc.snapshot, tc.now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestRankingDeferredReadDoesNotQueryDatabase(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 9, 21, 23, 0, 0, 0, loc)
	s := &Store{} // No DB: any accidental query would fail this test.
	result, err := s.TokenRanking(context.Background(), "fixture@example.com", now)
	if err != nil || result != nil || s.perf.rankBuilds != 0 {
		t.Fatal("cold read must not invent a ranking", result, err)
	}
	rank := 2
	s.perf.rank = rankingSnapshot{at: now.Add(-2 * time.Hour), owners: map[string]TokenRanking{
		"fixture@example.com": {Rank: &rank, Tokens: 123},
	}}
	result, err = s.TokenRanking(context.Background(), "fixture@example.com", now)
	if err != nil || result == nil || *result.Rank != 2 || result.Tokens != 123 || !result.End.Equal(s.perf.rank.at) || s.perf.rankBuilds != 0 {
		t.Fatal("cached result changed", result, err)
	}
	*result.Rank = 99
	if rank != 2 {
		t.Fatal("response mutated the shared snapshot")
	}
}
