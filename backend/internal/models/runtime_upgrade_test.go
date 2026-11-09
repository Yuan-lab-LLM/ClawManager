package models

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimeUpgradeUnknownStatesFailClosed(t *testing.T) {
	for _, state := range []string{"reserved", "stop_intent", "recovery_required", "", "future_state"} {
		if !(RuntimeUpgrade{State: state}).Active() {
			t.Fatal(state)
		}
	}
	for _, state := range []string{"cancelled", "completed"} {
		if (RuntimeUpgrade{State: state}).Active() {
			t.Fatal(state)
		}
	}
	if !ValidRuntimeOperation(strings.Repeat("a", 32)) || ValidRuntimeOperation(strings.Repeat("A", 32)) || ValidRuntimeOperation("invalid") {
		t.Fatal("operation token validation")
	}
}

func TestUpgradeFencePinsAdmissionButStillRequiresFreshDrainProof(t *testing.T) {
	now := time.Now().UTC()
	state := RuntimeMaintenance{UpgradeFenced: true, Deadline: now.Add(-time.Hour), GateClosed: true, ReportedAt: &now}
	if !state.Drained(now) {
		t.Fatal("fence expired")
	}
	if state.Drained(now.Add(6 * time.Second)) {
		t.Fatal("stale report certified drain")
	}
	state.Cancelled = true
	if state.Draining(now) {
		t.Fatal("corrupt cancelled row considered valid")
	}
}
