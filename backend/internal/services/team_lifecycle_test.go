package services

import (
	"context"
	"fmt"
	"testing"
)

func TestTeamManualLegacyStopDoesNotWeakenAutomaticPreflight(t *testing.T) {
	if !legacyTeamManualStop("manual", fmt.Errorf("missing route: %w", ErrRuntimeAgentNotFound)) {
		t.Fatal("legacy confirmed manual stop blocked")
	}
	for _, reason := range []string{"manual", "idle_timeout"} {
		for _, err := range []error{context.DeadlineExceeded, fmt.Errorf("authorization failed"), nil} {
			if legacyTeamManualStop(reason, err) {
				t.Fatalf("uncertain stop admitted: %s %v", reason, err)
			}
		}
	}
	if legacyTeamManualStop("idle_timeout", ErrRuntimeAgentNotFound) {
		t.Fatal("automatic stop bypassed missing execution proof")
	}
}

func TestTeamStopFailedStartRecoveryCannotWeakenIdleOrRunningAdmission(t *testing.T) {
	cases := []struct {
		name, reason, state, execution string
		supported, want                bool
	}{
		{"automatic idle", "idle_timeout", "running", "idle", true, true},
		{"automatic unknown failed source", "idle_timeout", "error", "unknown", true, false},
		{"automatic unsupported", "idle_timeout", "creating", "unsupported", false, false},
		{"manual failed start", "manual", "error", "unknown", true, true},
		{"manual pending start", "manual", "creating", "unsupported", false, true},
		{"manual live unknown", "manual", "running", "unknown", true, false},
		{"manual active tool", "manual", "error", "busy", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if teamStopPreflightAllows(c.reason, c.state, &RuntimeIdlePreflight{ExecutionProofSupported: c.supported, ExecutionState: c.execution}) != c.want {
				t.Fatal("unsafe lifecycle admission")
			}
		})
	}
	if teamStopPreflightAllows("manual", "error", nil) {
		t.Fatal("missing native observation admitted")
	}
}
