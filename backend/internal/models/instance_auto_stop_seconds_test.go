package models

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestAutoStopExplicitSecondsAcceptsSubDayDurations(t *testing.T) {
	for _, seconds := range []int64{1, 60, 3600, 86400, 9223372036} {
		var cfg InstanceAutoStopSettings
		raw, _ := json.Marshal(map[string]any{"check_interval_seconds": seconds, "idle_timeout_seconds": seconds})
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("explicit %d seconds rejected: %v", seconds, err)
		}
	}
}

func TestAutoStopSecondsValidationAndLegacyTiming(t *testing.T) {
	now := time.Date(2026, 11, 9, 1, 2, 3, 0, time.UTC)
	legacy := InstanceAutoStopSettings{CheckIntervalDays: 2, IdleTimeoutDays: 7}
	interval, idle := 2*AutoStopDay, 7*AutoStopDay
	if AutoStopTestTiming {
		interval, idle = time.Minute, 5*time.Minute
	}
	if legacy.CheckInterval() != interval || legacy.IdleTimeout() != idle {
		t.Fatal("legacy effective durations changed")
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response["check_interval_seconds"] != interval.Seconds() || response["idle_timeout_seconds"] != idle.Seconds() || response["check_interval_days"] != float64(2) || response["idle_timeout_days"] != float64(7) {
		t.Fatalf("API effective durations: %s", raw)
	}
	for _, seconds := range []int64{0, -1, 9223372037, math.MaxInt64} {
		cfg := legacy
		cfg.CheckIntervalSeconds = &seconds
		if cfg.Validate() == nil {
			t.Fatalf("accepted interval %d", seconds)
		}
		cfg.CheckIntervalSeconds, cfg.IdleTimeoutSeconds = nil, &seconds
		if cfg.Validate() == nil {
			t.Fatalf("accepted timeout %d", seconds)
		}
	}
	intervalSeconds, idleSeconds := int64(2), int64(7)
	cfg := InstanceAutoStopSettings{CheckIntervalSeconds: &intervalSeconds, IdleTimeoutSeconds: &idleSeconds}
	if cfg.CheckInterval() != 2*time.Second || cfg.IdleTimeout() != 7*time.Second {
		t.Fatal("test build or day fields override explicit seconds")
	}
	if got := cfg.NextCheck(now); !got.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("next check: %v", got)
	}
	due := now.Add(-6500 * time.Millisecond)
	if got := cfg.AdvanceCheck(due, now); !got.Equal(due.Add(8 * time.Second)) {
		t.Fatalf("missed second checks: %v", got)
	}
	maximum := int64(math.MaxInt64 / int64(time.Second))
	cfg.CheckIntervalSeconds = &maximum
	if got := cfg.AdvanceCheck(now.Add(-time.Second), now); !got.After(now) {
		t.Fatalf("maximum interval overflowed: %v", got)
	}
}

func TestAutoStopExplicitIdleWindowBoundariesAndProtections(t *testing.T) {
	now := time.Date(2026, 11, 9, 1, 2, 3, 0, time.UTC)
	anchor := now.Add(-7 * time.Second)
	activity := InstanceModelActivity{MonitoringSince: anchor}
	if !activity.IdleAtDuration(now, 7*time.Second) || activity.IdleAtDuration(now.Add(-time.Nanosecond), 7*time.Second) {
		t.Fatal("sub-day idle boundary lost precision")
	}
	recent := now.Add(-6 * time.Second)
	activity.LastGatewayActivityAt = &recent
	if activity.IdleAtDuration(now, 7*time.Second) {
		t.Fatal("recent model activity ignored")
	}
	activity.LastGatewayActivityAt, activity.MonitoringSince = nil, recent
	if activity.IdleAtDuration(now, 7*time.Second) {
		t.Fatal("new monitoring window ignored")
	}
	activity.MonitoringSince, activity.ActiveRequests = anchor, 1
	if activity.IdleAtDuration(now, 7*time.Second) {
		t.Fatal("active request ignored")
	}
	activity.ActiveRequests, activity.AutoStopDisabled = 0, true
	if activity.IdleAtDuration(now, 7*time.Second) {
		t.Fatal("keep running ignored")
	}
	activity.AutoStopDisabled, activity.StopOperationID = false, "pending"
	if activity.IdleAtDuration(now, 7*time.Second) {
		t.Fatal("stop fence ignored")
	}
	activity.StopOperationID = ""
	if activity.IdleAtDuration(now, time.Nanosecond) {
		t.Fatal("subsecond timeout accepted")
	}
}
