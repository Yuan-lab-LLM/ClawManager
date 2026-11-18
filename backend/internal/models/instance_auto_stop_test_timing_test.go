//go:build cm_idle_stop_test_timing

package models

import (
	"testing"
	"time"
)

func TestAutoStopTestTimingScanEveryMinute(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 30, 15, 0, time.UTC)
	if got := NextInstanceAutoStopCheck(now); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("test build next check = %v; want %v", got, now.Add(time.Minute))
	}
	for _, days := range []int{1, 7} {
		due := now.Add(-3*time.Minute - 15*time.Second)
		if got, want := AdvanceInstanceAutoStopCheck(due, now, days), due.Add(4*time.Minute); !got.Equal(want) {
			t.Fatalf("test build catch-up = %v; want %v", got, want)
		}
	}
}

func TestAutoStopTestTimingFiveMinuteIdleWindow(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 30, 0, 0, time.UTC)
	anchor := now.Add(-5 * time.Minute)
	for _, days := range []int{1, 7} {
		state := InstanceModelActivity{MonitoringSince: anchor}
		if !state.IdleAt(now, days) || state.IdleAt(now.Add(-time.Nanosecond), days) {
			t.Fatalf("test build must use an exact five-minute window (days=%d)", days)
		}
		activity := now.Add(-4 * time.Minute)
		state.LastGatewayActivityAt = &activity
		if state.IdleAt(now, days) {
			t.Fatal("recent model activity must restart the idle window")
		}
		state.LastGatewayActivityAt = nil
		state.MonitoringSince = now.Add(-time.Minute)
		if state.IdleAt(now, days) {
			t.Fatal("a new monitoring window must receive five full minutes")
		}
		state.MonitoringSince = anchor
		state.ActiveRequests = 1
		if state.IdleAt(now, days) {
			t.Fatal("an active request must prevent stopping")
		}
		state.ActiveRequests, state.AutoStopDisabled = 0, true
		if state.IdleAt(now, days) {
			t.Fatal("keep-running exemption must remain effective")
		}
		state.AutoStopDisabled, state.StopOperationID = false, "pending"
		if state.IdleAt(now, days) {
			t.Fatal("an existing stop operation must prevent another stop")
		}
	}
}
