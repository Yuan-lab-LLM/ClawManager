package repository

import (
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func validRuntimeObservation() models.RuntimePodObservation {
	return models.RuntimePodObservation{PodUID: "pod-uid", ObservedAt: time.Now().UTC().Add(-time.Second), Slots: []models.RuntimeSlotObservation{{ContainerName: "runtime-1", GatewayPort: 20000, State: "occupied", InstanceID: 25, UserID: 7, GatewayID: "gw-25-2", Generation: 2, InstanceState: "running"}}}
}

func TestSaveRuntimeObservationRejectsInvalidFactsBeforeSQL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.RuntimePodObservation)
	}{
		{"source", func(o *models.RuntimePodObservation) { o.PodUID = "different" }},
		{"empty source", func(o *models.RuntimePodObservation) { o.PodUID = "" }},
		{"long source", func(o *models.RuntimePodObservation) { o.PodUID = strings.Repeat("x", 129) }},
		{"zero time", func(o *models.RuntimePodObservation) { o.ObservedAt = time.Time{} }},
		{"future time", func(o *models.RuntimePodObservation) { o.ObservedAt = time.Now().Add(time.Minute) }},
		{"empty scan", func(o *models.RuntimePodObservation) { o.Slots = nil }},
		{"oversized scan", func(o *models.RuntimePodObservation) { o.Slots = make([]models.RuntimeSlotObservation, 257) }},
		{"invalid port", func(o *models.RuntimePodObservation) { o.Slots[0].GatewayPort = 0 }},
		{"port overflow", func(o *models.RuntimePodObservation) { o.Slots[0].GatewayPort = 65536 }},
		{"duplicate port", func(o *models.RuntimePodObservation) {
			o.Slots = append(o.Slots, o.Slots[0])
			o.Slots[1].ContainerName = "runtime-2"
		}},
		{"duplicate container", func(o *models.RuntimePodObservation) {
			o.Slots = append(o.Slots, o.Slots[0])
			o.Slots[1].GatewayPort = 20001
		}},
		{"unknown state", func(o *models.RuntimePodObservation) { o.Slots[0].State = "idle" }},
		{"negative owner", func(o *models.RuntimePodObservation) { o.Slots[0].InstanceID = -1 }},
		{"free owner", func(o *models.RuntimePodObservation) { o.Slots[0].State = "free" }},
		{"oversized error", func(o *models.RuntimePodObservation) { o.Error = strings.Repeat("x", 513) }},
		{"control error", func(o *models.RuntimePodObservation) { o.Error = "bad\nraw payload" }},
		{"oversized identity", func(o *models.RuntimePodObservation) { o.Slots[0].GatewayID = strings.Repeat("x", 129) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validRuntimeObservation()
			tc.change(&o)
			r := &runtimePodRepository{}
			saved, err := r.SaveRuntimeObservation(t.Context(), 1, "pod-uid", o)
			if err == nil || saved {
				t.Fatalf("invalid observation saved: %v %v", saved, err)
			}
		})
	}
}

func TestRuntimeObservationFreeRequiresConfirmedEmptiness(t *testing.T) {
	for _, tc := range []struct{ name, gatewayState, errorMessage string }{
		{name: "read error", errorMessage: "runtime inspection unavailable"},
		{name: "live gateway", gatewayState: "running"},
		{name: "unknown gateway", gatewayState: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validRuntimeObservation()
			o.Slots = []models.RuntimeSlotObservation{{ContainerName: "runtime-1", GatewayPort: 20000, State: "free", GatewayState: tc.gatewayState, Error: tc.errorMessage}}
			if err := validateRuntimeObservation(1, "pod-uid", o); err == nil {
				t.Fatal("free observation accepted without confirmed emptiness")
			}
		})
	}
}

func TestRuntimeObservationAcceptsLegacyOwnershipAndExplicitUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.RuntimePodObservation)
	}{
		{"legacy owner", func(o *models.RuntimePodObservation) {}},
		{"unknown owner", func(o *models.RuntimePodObservation) {
			o.Slots[0].State = "unknown"
			o.Slots[0].Error = "identity unavailable"
		}},
		{"read failure", func(o *models.RuntimePodObservation) { o.Slots = nil; o.Error = "runtime inspection unavailable" }},
		{"verified owner", func(o *models.RuntimePodObservation) { o.Slots[0].IdentityVerified = true }},
		{"free slot", func(o *models.RuntimePodObservation) {
			o.Slots = []models.RuntimeSlotObservation{{ContainerName: "runtime-1", GatewayPort: 20000, State: "free", GatewayState: "stopped"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validRuntimeObservation()
			tc.change(&o)
			if err := validateRuntimeObservation(1, "pod-uid", o); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeObservationMySQLCASAndCounters(t *testing.T) {
	s := upgradeMySQL(t)
	r := NewRuntimePodRepository(s)
	store := r.(RuntimePodObservationStore)
	uid := "pod-uid"
	pod := &models.RuntimePod{RuntimeType: "hermes", Namespace: "runtime", PodName: "runtime-1", PodUID: &uid, DeploymentName: "pool", ImageRef: "runtime:test", State: "ready", Capacity: 2, UsedSlots: 1}
	if err := r.UpsertFromAgent(t.Context(), pod); err != nil {
		t.Fatal(err)
	}
	o := validRuntimeObservation()
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, uid, o); err != nil || !saved {
		t.Fatalf("first save: %v %v", saved, err)
	}
	initial, err := r.GetByID(t.Context(), pod.ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.ObservationJSON == nil || initial.ObservationAt == nil || !initial.ObservationAt.Equal(o.ObservedAt.Truncate(time.Microsecond)) || initial.UsedSlots != 1 {
		t.Fatalf("facts or counters lost: %+v", initial)
	}
	for _, delta := range []time.Duration{0, -time.Second} {
		old := o
		old.ObservedAt = old.ObservedAt.Add(delta)
		if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, uid, old); err != nil || saved {
			t.Fatalf("nonmonotonic save: %v %v", saved, err)
		}
	}
	samePrecision := o
	samePrecision.ObservedAt = o.ObservedAt.Truncate(time.Microsecond).Add(999 * time.Nanosecond)
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, uid, samePrecision); err != nil || saved {
		t.Fatalf("same SQL timestamp precision accepted: %v %v", saved, err)
	}
	caseChanged := o
	caseChanged.PodUID = strings.ToUpper(uid)
	caseChanged.ObservedAt = caseChanged.ObservedAt.Add(time.Millisecond)
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, caseChanged.PodUID, caseChanged); err != nil || saved {
		t.Fatalf("case-changed UID accepted: %v %v", saved, err)
	}
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID+1, uid, o); err != nil || saved {
		t.Fatalf("missing row saved: %v %v", saved, err)
	}
	if err := r.UpdateHeartbeat(t.Context(), pod.ID, "ready", 1, 2, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	pod.UsedSlots = 0
	if err := r.UpsertFromAgent(t.Context(), pod); err != nil {
		t.Fatal(err)
	}
	same, err := r.GetByID(t.Context(), pod.ID)
	if err != nil {
		t.Fatal(err)
	}
	if same.ObservationJSON == nil || *same.ObservationJSON != *initial.ObservationJSON || same.UsedSlots != 1 {
		t.Fatalf("same-UID registration erased facts or counters: %+v", same)
	}
	replacement := "replacement-uid"
	pod.PodUID = &replacement
	if err := r.UpsertFromAgent(t.Context(), pod); err != nil {
		t.Fatal(err)
	}
	changed, err := r.GetByID(t.Context(), pod.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ObservationJSON != nil || changed.ObservationAt != nil || changed.UsedSlots != 1 {
		t.Fatalf("replacement reused facts or changed counters: %+v", changed)
	}
	o.ObservedAt = o.ObservedAt.Add(time.Second)
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, uid, o); err != nil || saved {
		t.Fatalf("old source saved: %v %v", saved, err)
	}
	o.PodUID = replacement
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, replacement, o); err != nil || !saved {
		t.Fatalf("replacement save: %v %v", saved, err)
	}
	o.ObservedAt = o.ObservedAt.Add(time.Second)
	o.Slots = nil
	o.Error = "runtime inspection unavailable"
	if saved, err := store.SaveRuntimeObservation(t.Context(), pod.ID, replacement, o); err != nil || !saved {
		t.Fatalf("failed inspection not persisted: %v %v", saved, err)
	}
}
