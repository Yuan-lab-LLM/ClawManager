package models

import (
	"encoding/json"
	"testing"
	"time"
)

func observationViewFixture(t *testing.T) (RuntimePod, RuntimePodObservation, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	uid := "pod-uid"
	p := RuntimePod{PodUID: &uid, Capacity: 2, State: "ready", ObservationAt: &now}
	o := RuntimePodObservation{PodUID: uid, ObservedAt: now, Slots: []RuntimeSlotObservation{}}
	for index, port := range []int{20000, 20001} {
		p.Slots = append(p.Slots, RuntimeSlot{Index: index + 1, ContainerName: "slot-" + string(rune('0'+index)), GatewayPort: &port, Ready: true})
		o.Slots = append(o.Slots, RuntimeSlotObservation{ContainerName: p.Slots[index].ContainerName, GatewayPort: port, State: "free"})
	}
	return p, o, now
}

func setViewObservation(t *testing.T, p *RuntimePod, o RuntimePodObservation) {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	p.ObservationJSON = &s
}

func TestRuntimeInventoryObservationDoesNotInventIdle(t *testing.T) {
	for _, scenario := range []string{"missing", "stale", "wrong UID", "duplicate port", "read failure", "SQL unavailable", "unattributed count"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, now := observationViewFixture(t)
			switch scenario {
			case "stale":
				old := now.Add(-3 * time.Minute)
				p.ObservationAt = &old
				o.ObservedAt = old
			case "wrong UID":
				o.PodUID = "another-pod"
			case "duplicate port":
				o.Slots[1].GatewayPort = o.Slots[0].GatewayPort
			case "read failure":
				o.Error = "inspection unavailable"
				o.Slots = nil
			case "unattributed count":
				p.UsedSlots = 1
			}
			if scenario != "missing" {
				setViewObservation(t, &p, o)
			}
			ReconcileRuntimePodObservation(&p, nil, scenario != "SQL unavailable", now)
			if p.ReconciliationState == "consistent" || p.UnknownSlots != 2 {
				t.Fatalf("uncertain evidence looked free: %+v", p)
			}
			for _, slot := range p.Slots {
				if slot.ReconciliationState != "unknown" || slot.BindingConfirmed {
					t.Fatalf("unsafe slot interpretation: %+v", slot)
				}
			}
		})
	}
}

func TestRuntimeInventoryReportedOwnerWithoutBindingIsVisible(t *testing.T) {
	p, o, now := observationViewFixture(t)
	o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "occupied", InstanceID: 25, UserID: 1, GatewayID: "gw-25-14", Generation: 14, InstanceState: "starting", GatewayState: "error", Error: "legacy owner proof unavailable"}
	setViewObservation(t, &p, o)
	ReconcileRuntimePodObservation(&p, nil, true, now)
	slot := p.Slots[0]
	if slot.InstanceID == nil || *slot.InstanceID != 25 || slot.InstanceState != "error" || slot.ReconciliationState != "residual" || slot.BindingConfirmed || p.UsedSlots != 1 {
		t.Fatalf("Hermes orphan disappeared: %+v / %+v", slot, p)
	}
	if p.Slots[1].ReconciliationState != "consistent" {
		t.Fatal("one orphan hid the confirmed empty sibling")
	}
}

func TestRuntimeInventoryBindingAndObservedOwnership(t *testing.T) {
	for _, scenario := range []string{"matching", "different owner", "different generation", "empty with binding", "weak identity"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, now := observationViewFixture(t)
			binding := InstanceRuntimeBinding{InstanceID: 7, GatewayID: "gw-7-1", GatewayPort: 20000, Generation: 1, State: "running"}
			o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "occupied", InstanceID: 7, UserID: 2, GatewayID: "gw-7-1", Generation: 1, InstanceState: "running", GatewayState: "running", IdentityVerified: true}
			switch scenario {
			case "different owner":
				o.Slots[0].InstanceID = 9
				o.Slots[0].GatewayID = "gw-9-1"
			case "different generation":
				o.Slots[0].Generation = 2
				o.Slots[0].GatewayID = "gw-7-2"
			case "empty with binding":
				o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "free"}
			case "weak identity":
				o.Slots[0].IdentityVerified = false
			}
			setViewObservation(t, &p, o)
			ReconcileRuntimePodObservation(&p, []InstanceRuntimeBinding{binding}, true, now)
			slot := p.Slots[0]
			if scenario == "matching" {
				if !slot.BindingConfirmed || slot.ReconciliationState != "consistent" {
					t.Fatalf("matching facts diverged: %+v", slot)
				}
			} else if slot.ReconciliationState == "consistent" || slot.BindingConfirmed || slot.ExpectedInstanceID == nil || *slot.ExpectedInstanceID != 7 {
				t.Fatalf("conflict hidden: %+v", slot)
			}
			if scenario == "empty with binding" && slot.InstanceID != nil {
				t.Fatalf("database reservation was presented as observed owner: %+v", slot)
			}
			if p.UsedSlots < 1 {
				t.Fatal("lost a binding/owner reservation")
			}
		})
	}
}

func TestRuntimeInventoryConfirmedEmptyIsConsistent(t *testing.T) {
	p, o, now := observationViewFixture(t)
	setViewObservation(t, &p, o)
	ReconcileRuntimePodObservation(&p, nil, true, now)
	if p.ReconciliationState != "consistent" || p.UnknownSlots != 0 || p.UsedSlots != 0 {
		t.Fatalf("confirmed emptiness was lost: %+v", p)
	}
}

func TestRuntimeInventoryKeepsUnverifiedReportedIDForDiagnosis(t *testing.T) {
	p, o, now := observationViewFixture(t)
	o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "occupied", InstanceID: 25, GatewayState: "error"}
	setViewObservation(t, &p, o)
	ReconcileRuntimePodObservation(&p, nil, true, now)
	if p.Slots[0].InstanceID == nil || *p.Slots[0].InstanceID != 25 || p.Slots[0].ReconciliationState != "unknown" || p.Slots[0].BindingConfirmed {
		t.Fatalf("unverified report was hidden or granted authority: %+v", p.Slots[0])
	}
}

func TestRuntimeInventoryEmptyAdmissionRequiresCurrentCompleteFacts(t *testing.T) {
	for _, scenario := range []string{"fresh", "stale", "occupied", "unknown", "incomplete"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, now := observationViewFixture(t)
			p.Slots = nil // Locked SQL admission has no Kubernetes slot enrichment.
			switch scenario {
			case "stale":
				old := now.Add(-3 * time.Minute)
				o.ObservedAt = old
				p.ObservationAt = &old
			case "occupied":
				o.Slots[0].State = "occupied"
				o.Slots[0].InstanceID = 25
			case "unknown":
				o.Slots[0].State = "unknown"
			case "incomplete":
				o.Slots = o.Slots[:1]
			}
			setViewObservation(t, &p, o)
			err := ConfirmedEmptyRuntimeObservation(p, now)
			if (err == nil) != (scenario == "fresh") {
				t.Fatalf("%s: %v", scenario, err)
			}
		})
	}
}

func TestRuntimeInventoryPartialOwnerDoesNotInheritDatabaseIdentity(t *testing.T) {
	p, o, now := observationViewFixture(t)
	o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "occupied", InstanceID: 25, GatewayState: "error"}
	setViewObservation(t, &p, o)
	ReconcileRuntimePodObservation(&p, []InstanceRuntimeBinding{{InstanceID: 7, GatewayID: "gw-7-1", GatewayPort: 20000, Generation: 1, State: "running"}}, true, now)
	slot := p.Slots[0]
	if slot.InstanceID == nil || *slot.InstanceID != 25 || slot.Generation != nil || slot.GatewayID != "" || slot.ExpectedInstanceID == nil || *slot.ExpectedInstanceID != 7 || slot.BindingConfirmed {
		t.Fatalf("mixed observed/database identities: %+v", slot)
	}
}

func TestRuntimeInventoryUnknownReportedOwnerRemainsDisplayOnly(t *testing.T) {
	for _, scenario := range []string{"unbound", "bound", "incomplete-generation", "incomplete-user", "incomplete-gateway"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, now := observationViewFixture(t)
			o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "unknown", InstanceID: 25, UserID: 1, Generation: 14, GatewayID: "gw-25-14", InstanceState: "starting", GatewayState: "error", Error: "gateway ownership ambiguous"}
			bindings := []InstanceRuntimeBinding{}
			if scenario != "unbound" {
				bindings = append(bindings, InstanceRuntimeBinding{InstanceID: 7, Generation: 3, GatewayID: "gw-7-3", GatewayPort: 20000, State: "running"})
			}
			switch scenario {
			case "incomplete-generation":
				o.Slots[0].Generation = 0
			case "incomplete-user":
				o.Slots[0].UserID = 0
			case "incomplete-gateway":
				o.Slots[0].GatewayID = ""
			}
			setViewObservation(t, &p, o)
			ReconcileRuntimePodObservation(&p, bindings, true, now)
			slot := p.Slots[0]
			if slot.InstanceID == nil || *slot.InstanceID != 25 || slot.InstanceState != "error" || slot.ReconciliationState != "unknown" || slot.BindingConfirmed || p.UnknownSlots != 1 {
				t.Fatalf("unknown owner was hidden or granted authority: %+v / %+v", slot, p)
			}
			if scenario != "unbound" && (slot.ExpectedInstanceID == nil || *slot.ExpectedInstanceID != 7) {
				t.Fatalf("expected SQL owner was lost: %+v", slot)
			}
			if scenario == "unbound" || scenario == "bound" {
				if slot.Generation == nil || *slot.Generation != 14 || slot.GatewayID != "gw-25-14" {
					t.Fatalf("complete reported tuple was lost: %+v", slot)
				}
			} else if slot.Generation != nil || slot.GatewayID != "" {
				t.Fatalf("incomplete report inherited SQL generation or gateway: %+v", slot)
			}
			if err := ConfirmedEmptyRuntimeObservation(p, now); err == nil {
				t.Fatal("unknown reported owner enabled empty admission")
			}
		})
	}
}

func TestRuntimeInventoryGlobalFailureRetainsOnlyCurrentMappedReportedOwner(t *testing.T) {
	for _, scenario := range []string{"fresh", "stale", "wrong UID", "unreadable", "mismatched port", "mismatched name"} {
		t.Run(scenario, func(t *testing.T) {
			p, o, now := observationViewFixture(t)
			o.Error = "assignment inventory unavailable"
			o.Slots[0] = RuntimeSlotObservation{ContainerName: "slot-0", GatewayPort: 20000, State: "occupied", InstanceID: 25, UserID: 1, Generation: 14, GatewayID: "gw-25-14", InstanceState: "starting", GatewayState: "error", IdentityVerified: true}
			switch scenario {
			case "stale":
				old := now.Add(-3 * time.Minute)
				p.ObservationAt = &old
				o.ObservedAt = old
			case "wrong UID":
				o.PodUID = "replacement"
			case "mismatched port":
				o.Slots[1].GatewayPort = 20999
			case "mismatched name":
				o.Slots[1].ContainerName = "slot-9"
			}
			setViewObservation(t, &p, o)
			if scenario == "unreadable" {
				raw := "{"
				p.ObservationJSON = &raw
			}
			bindings := []InstanceRuntimeBinding{{InstanceID: 7, Generation: 3, GatewayID: "gw-7-3", GatewayPort: 20000, State: "running"}}
			ReconcileRuntimePodObservation(&p, bindings, true, now)
			slot := p.Slots[0]
			if p.UnknownSlots != 2 || slot.ReconciliationState != "unknown" || slot.BindingConfirmed || slot.ExpectedInstanceID == nil || *slot.ExpectedInstanceID != 7 {
				t.Fatalf("global failure enabled a slot or hid SQL reservation: %+v", p)
			}
			if scenario == "fresh" {
				if slot.InstanceID == nil || *slot.InstanceID != 25 || slot.Generation == nil || *slot.Generation != 14 || slot.GatewayID != "gw-25-14" || slot.InstanceState != "error" {
					t.Fatalf("fresh scoped report disappeared behind lookup failure: %+v", slot)
				}
			} else if slot.InstanceID != nil && *slot.InstanceID == 25 {
				t.Fatalf("invalid envelope exposed retained owner as current: %+v", slot)
			}
			if err := ConfirmedEmptyRuntimeObservation(p, now); err == nil {
				t.Fatal("global failure enabled empty admission")
			}
		})
	}
}
