package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"testing"
	"time"
)

func TestRuntimeMaintenanceRejectsOccupiedAndStaleReports(t *testing.T) {
	now := time.Now().UTC()
	uid := "uid"
	raw, _ := json.Marshal(models.RuntimePodObservation{PodUID: uid, ObservedAt: now, Slots: []models.RuntimeSlotObservation{{ContainerName: "slot-0", GatewayPort: 20000, State: "free"}}})
	text := string(raw)
	pod := models.RuntimePod{PodUID: &uid, Capacity: 1, State: "ready", LastSeenAt: &now, ObservationJSON: &text, ObservationAt: &now}
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

func TestRuntimeMaintenanceRequiresPhysicalEmptyProof(t *testing.T) {
	now := time.Now().UTC().UTC()
	uid := "uid"
	pod := models.RuntimePod{PodUID: &uid, Capacity: 1, State: "ready", LastSeenAt: &now}
	for _, state := range []string{"missing", "unknown", "occupied", "free"} {
		t.Run(state, func(t *testing.T) {
			candidate := pod
			if state != "missing" {
				observation := models.RuntimePodObservation{PodUID: uid, ObservedAt: now, Slots: []models.RuntimeSlotObservation{{ContainerName: "slot-0", GatewayPort: 20000, State: state}}}
				raw, _ := json.Marshal(observation)
				text := string(raw)
				candidate.ObservationJSON = &text
				candidate.ObservationAt = &now
			}
			err := runtimeMaintenanceEmptyReport(&candidate, uid, now)
			if state == "free" && err != nil {
				t.Fatal(err)
			}
			if state != "free" && err == nil {
				t.Fatalf("accepted %s physical proof with zero SQL usage", state)
			}
		})
	}
}
