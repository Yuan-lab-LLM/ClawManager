//go:build !cm_idle_stop_test_timing

package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAutoStopSettingsDaysAndBoundary(t *testing.T) {
	now := time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC)
	settings := DefaultInstanceAutoStopSettings(now)
	if !settings.Enabled || settings.CheckIntervalDays != 1 || settings.IdleTimeoutDays != 1 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	if want := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC); !settings.NextCheckAt.Equal(want) {
		t.Fatalf("next check %v, want %v", settings.NextCheckAt, want)
	}
	for _, days := range []int{1, 2, 7} {
		settings.CheckIntervalDays, settings.IdleTimeoutDays = days, days
		if err := settings.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, days := range []int{0, -1, 1 << 30} {
		settings.CheckIntervalDays, settings.IdleTimeoutDays = days, 1
		if settings.Validate() == nil {
			t.Fatalf("accepted check interval %d", days)
		}
		settings.CheckIntervalDays, settings.IdleTimeoutDays = 1, days
		if settings.Validate() == nil {
			t.Fatalf("accepted idle timeout %d", days)
		}
	}
	var decoded InstanceAutoStopSettings
	if json.Unmarshal([]byte(`{"check_interval_days":1.5,"idle_timeout_days":1}`), &decoded) == nil {
		t.Fatal("accepted fractional days")
	}
}

func TestAutoStopIdleRequiresContinuousWindowAndNoWork(t *testing.T) {
	now := time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC)
	anchor := now.Add(-24 * time.Hour)
	state := InstanceModelActivity{InstanceID: 1, Generation: 2, MonitoringSince: anchor, LastGatewayActivityAt: &anchor}
	if !state.IdleAt(now, 1) || state.IdleAt(now.Add(-time.Second), 1) {
		t.Fatal("24-hour idle boundary is incorrect")
	}
	state.MonitoringSince = now.Add(-time.Hour)
	if state.IdleAt(now, 1) {
		t.Fatal("missing history treated as idle")
	}
	state.MonitoringSince = anchor
	state.ActiveRequests = 1
	if state.IdleAt(now, 1) {
		t.Fatal("in-flight request treated as idle")
	}
	state.ActiveRequests, state.AutoStopDisabled = 0, true
	if state.IdleAt(now, 1) {
		t.Fatal("keep-running ignored")
	}
	state.AutoStopDisabled, state.StopOperationID = false, "pending"
	if state.IdleAt(now, 1) {
		t.Fatal("existing stop operation ignored")
	}
}

func TestAdvanceAutoStopCheckSkipsMissedTicks(t *testing.T) {
	due := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	now := due.Add(5 * 24 * time.Hour)
	if got, want := AdvanceInstanceAutoStopCheck(due, now, 2), due.Add(6*24*time.Hour); !got.Equal(want) {
		t.Fatalf("next check %v, want %v", got, want)
	}
}
