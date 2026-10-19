package services

import (
	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"testing"
	"time"
)

func TestAgentHeartbeatPolicyWindows(t *testing.T) {
	for _, tc := range []struct {
		age    time.Duration
		status string
	}{
		{heartbeat.ReportInterval + time.Second, agentStatusOnline},
		{heartbeat.OnlineWindow + time.Second, agentStatusStale},
		{heartbeat.Timeout + time.Second, agentStatusOffline},
	} {
		last := time.Now().Add(-tc.age)
		if got := deriveAgentStatus(&models.InstanceAgent{LastHeartbeatAt: &last}); got != tc.status {
			t.Fatalf("age=%v got=%s want=%s", tc.age, got, tc.status)
		}
	}
}
