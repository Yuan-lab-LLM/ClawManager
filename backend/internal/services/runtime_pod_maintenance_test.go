package services

import (
	"clawreef/internal/models"
	"testing"
	"time"
)

func TestRuntimeMaintenanceRejectsOccupiedAndStaleReports(t *testing.T) {
	now := time.Now()
	uid := "uid"
	pod := models.RuntimePod{PodUID: &uid, State: "ready", LastSeenAt: &now}
	if err := runtimeMaintenanceEmptyReport(&pod, "uid", now); err != nil {
		t.Fatal(err)
	}
	pod.UsedSlots = 1
	if runtimeMaintenanceEmptyReport(&pod, "uid", now) == nil {
		t.Fatal("accepted in-flight slot claim")
	}
	pod.UsedSlots = 0
	old := now.Add(-3 * time.Minute)
	pod.LastSeenAt = &old
	if runtimeMaintenanceEmptyReport(&pod, "uid", now) == nil {
		t.Fatal("accepted stale occupancy")
	}
	pod.LastSeenAt = &now
	if runtimeMaintenanceEmptyReport(&pod, "other", now) == nil {
		t.Fatal("accepted changed UID")
	}
}
