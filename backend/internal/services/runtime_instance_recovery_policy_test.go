package services

import (
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestRuntimeRecoveryBudgetSurvivesHeartbeatAndManagerRestart(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	state := models.InstanceRuntimeRecovery{}
	for attempt, delay := range []time.Duration{30 * time.Second, 120 * time.Second, 300 * time.Second} {
		var due bool
		state, due = runtimeRecoveryFailure(state, now, "process_exit")
		if due || state.NextRetryAt == nil || !state.NextRetryAt.Equal(now.Add(delay)) {
			t.Fatalf("attempt %d lost backoff: %+v", attempt, state)
		}
		restored := state
		state, due = runtimeRecoveryFailure(restored, now.Add(delay-time.Second), "process_exit")
		if due || !state.NextRetryAt.Equal(now.Add(delay)) {
			t.Fatal("heartbeat reset persisted retry clock")
		}
		state, due = runtimeRecoveryFailure(state, now.Add(delay), "process_exit")
		if !due {
			t.Fatal("persisted deadline did not become due")
		}
		state.Attempts++
		state.NextRetryAt = nil
		now = now.Add(delay)
	}
	state, due := runtimeRecoveryFailure(state, now.Add(time.Hour), "process_exit")
	if due || state.Attempts != 3 || state.NextRetryAt != nil {
		t.Fatalf("exhausted recovery retried: %+v", state)
	}
}

func TestRuntimeRecoveryRequiresFiveMinutesContinuousFreshHealth(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	state := models.InstanceRuntimeRecovery{Attempts: 3, Reason: "process_exit"}
	state = runtimeRecoveryHealthy(state, now, true)
	state = runtimeRecoveryHealthy(state, now.Add(299*time.Second), true)
	if state.Attempts != 3 {
		t.Fatal("budget cleared before stable health")
	}
	state = runtimeRecoveryHealthy(state, now.Add(300*time.Second), false)
	if state.HealthySince != nil || state.Attempts != 3 {
		t.Fatal("stale heartbeat cleared budget")
	}
	state = runtimeRecoveryHealthy(state, now.Add(301*time.Second), true)
	state = runtimeRecoveryHealthy(state, now.Add(601*time.Second), true)
	if state.Attempts != 0 || state.Reason != "" {
		t.Fatal("continuous stable health did not clear budget")
	}
}

func TestRuntimeRecoveryClassificationRequiresReportedFailure(t *testing.T) {
	for _, tt := range []struct {
		status, state, message, code string
		recover                      bool
	}{
		{"error", "error", "gateway process exited unexpectedly: signal: killed", "", true},
		{"running", "failed", "process exited with code 137", "", true},
		{"error", "error", "gateway_process_exited", "", true},
		{"error", "error", "port_conflict", "", true},
		{"error", "unhealthy", "missing_llm_credentials", "", false},
		{"error", "unhealthy", "dashboard_auth_failed", "", false},
		{"running", "unhealthy", "health check failed", "", true},
		{"running", "running", "heartbeat stale", "", false},
		{"stopped", "failed", "process exited with code 137", "", false},
		{"sleeping", "failed", "process exited with code 137", "", false},
		{"deleting", "failed", "process exited with code 137", "", false},
		{"error", "error", "invalid API key", "", false},
		{"error", "failed", "process exited: unauthorized model credentials", "", false},
		{"error", "failed", "process exited with code 1", "MODEL_CONFIGURATION_REQUIRED", false},
		{"error", "error", "arbitrary reported error", "", false},
	} {
		t.Run(tt.status+tt.state+tt.message, func(t *testing.T) {
			i := models.Instance{Status: tt.status, RuntimeErrorMessage: &tt.message, RuntimeErrorCode: &tt.code}
			b := &models.InstanceRuntimeBinding{State: tt.state, ErrorMessage: &tt.message}
			if _, ok := runtimeRecoveryFailureReason(i, b); ok != tt.recover {
				t.Fatalf("recoverable=%v want %v", ok, tt.recover)
			}
		})
	}
}

func TestRuntimeRecoveryFreshDetailsRequireConcreteFailureWithoutExposingErrors(t *testing.T) {
	for _, tt := range []struct {
		code, message string
		blocked       bool
	}{
		{"", "", false},
		{"missing_llm_credentials", "", true},
		{"INVALID_CREDENTIALS", "gateway_process_exited", true},
		{"", "gateway_start_failed", true},
		{"gateway_process_exited", "", false},
		{"", "port_conflict", false},
		{"", "invalid configuration: sensitive value", true},
	} {
		if got := runtimeRecoveryFreshFailureBlocked(tt.code, tt.message); got != tt.blocked {
			t.Fatalf("classification mismatch for %q: %v", tt.code, got)
		}
	}
}
