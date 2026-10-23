package models

import (
	"testing"
	"time"
)

func TestRuntimeMaintenanceRequiresFreshCompleteDrainEvidence(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-time.Second)
	base := RuntimeMaintenance{Deadline: now.Add(time.Minute), GateClosed: true, ReportedAt: &fresh}
	if !base.Drained(now) {
		t.Fatal("fresh empty runtime must be drained")
	}
	for _, field := range []string{"active", "jobs", "admissions", "queue", "gate", "report", "stale", "future", "expired", "cancelled"} {
		t.Run(field, func(t *testing.T) {
			state := base
			switch field {
			case "active":
				state.Active = 1
			case "jobs":
				state.Jobs = 1
			case "admissions":
				state.Admissions = 1
			case "queue":
				state.Queued = 1
			case "gate":
				state.GateClosed = false
			case "report":
				state.ReportedAt = nil
			case "stale":
				v := now.Add(-5 * time.Second)
				state.ReportedAt = &v
			case "future":
				v := now.Add(time.Second)
				state.ReportedAt = &v
			case "expired":
				state.Deadline = now
			case "cancelled":
				state.Cancelled = true
			}
			if state.Drained(now) {
				t.Fatal("must not report drained")
			}
		})
	}
	if (RuntimeMaintenanceReport{Jobs: -1}).Valid() || (RuntimeMaintenanceReport{Operation: "wrong"}).Valid() {
		t.Fatal("invalid counter/identity accepted")
	}
}
