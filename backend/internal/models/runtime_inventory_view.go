package models

import (
	"encoding/json"
	"fmt"
	"time"
)

const RuntimeObservationFreshness = 2 * time.Minute

const RuntimeSlotReleasePendingMessage = "runtime slot quarantine release pending"

// ReconcileRuntimePodObservation interprets persisted facts without granting
// binding, allocation, or cleanup authority. Callers retain their exact fences.
func ReconcileRuntimePodObservation(p *RuntimePod, bindings []InstanceRuntimeBinding, bindingsKnown bool, now time.Time) {
	p.ReconciliationState, p.ReconciliationMessage, p.UnknownSlots = "consistent", "", 0
	byPort := make(map[int]InstanceRuntimeBinding, len(bindings))
	physical := make(map[int]string, len(p.Slots))
	for _, b := range bindings {
		byPort[b.GatewayPort] = b
	}
	for _, slot := range p.Slots {
		if slot.GatewayPort != nil {
			physical[*slot.GatewayPort] = slot.ContainerName
		}
	}
	var observation RuntimePodObservation
	problem := ""
	switch {
	case p.ObservationJSON == nil || p.ObservationAt == nil:
		problem = "Runtime observation has not been recorded"
	case json.Unmarshal([]byte(*p.ObservationJSON), &observation) != nil:
		problem = "Runtime observation is unreadable"
	case p.PodUID == nil || observation.PodUID != *p.PodUID:
		problem = "Runtime observation belongs to another Pod identity"
	case observation.ObservedAt.IsZero() || observation.ObservedAt.Truncate(time.Microsecond) != p.ObservationAt.UTC().Truncate(time.Microsecond):
		problem = "Runtime observation timestamp does not match its database record"
	case now.Sub(observation.ObservedAt) > RuntimeObservationFreshness || observation.ObservedAt.After(now.Add(5*time.Second)):
		problem = "Runtime observation is stale"
	}
	observed := make(map[int]RuntimeSlotObservation, len(observation.Slots))
	if problem == "" {
		for _, slot := range observation.Slots {
			_, duplicate := observed[slot.GatewayPort]
			name, exists := physical[slot.GatewayPort]
			if duplicate || !exists || slot.ContainerName != name {
				problem = "Runtime observation does not match the physical slot inventory"
				break
			}
			observed[slot.GatewayPort] = slot
		}
	}
	// Lookup failures with a current, physically mapped snapshot disable actions
	// without discarding the reported owner needed to explain the divergence.
	envelopeValid := problem == ""
	if envelopeValid && observation.Error != "" {
		problem = observation.Error
	}
	occupied := 0
	for index := range p.Slots {
		slot := &p.Slots[index]
		port := 0
		if slot.GatewayPort != nil {
			port = *slot.GatewayPort
		}
		binding, bound := byPort[port]
		priorID := slot.InstanceID
		slot.InstanceID, slot.Generation, slot.ExpectedInstanceID = nil, nil, nil
		slot.BindingConfirmed, slot.ReconciliationState, slot.ObservationMessage = false, "unknown", ""
		slot.InstanceState, slot.GatewayID = "", ""
		if bound {
			slot.InstanceID, slot.Generation, slot.ExpectedInstanceID = inventoryInt(binding.InstanceID), inventoryInt(binding.Generation), inventoryInt(binding.InstanceID)
			slot.InstanceState, slot.GatewayID = binding.State, binding.GatewayID
		} else {
			slot.ErrorMessage = nil
		}
		if !bound || priorID == nil || *priorID != binding.InstanceID {
			slot.InstanceName, slot.Owner = "", nil
		}
		fact, present := observed[port]
		if envelopeValid && present && (fact.State == "occupied" || fact.State == "unknown") && fact.InstanceID > 0 {
			inventoryReportedOwner(slot, fact)
		}
		switch {
		case problem != "":
			slot.ObservationMessage = problem
		case !present:
			slot.ObservationMessage = "Runtime slot was not observed"
		case fact.State == "unknown" && fact.Error == RuntimeSlotReleasePendingMessage && bindingsKnown && !bound && p.UsedSlots <= len(bindings) &&
			(fact.GatewayState == "" || fact.GatewayState == "stopped") &&
			((fact.InstanceID == 0 && fact.UserID == 0 && fact.Generation == 0 && fact.GatewayID == "" && fact.InstanceState == "") ||
				(fact.InstanceID > 0 && fact.UserID > 0 && fact.Generation > 0 && fact.GatewayID == fmt.Sprintf("gw-%d-%d", fact.InstanceID, fact.Generation) && fact.InstanceState == "stopped")):
			// The audit verified exit and is clearing its prior exclusion. This
			// is display-only progress, never empty-Pod or allocation authority.
			slot.ReconciliationState, slot.ObservationMessage = "releasing", RuntimeSlotReleasePendingMessage
			slot.InstanceID, slot.Generation, slot.ExpectedInstanceID = nil, nil, nil
			slot.GatewayID, slot.InstanceState, slot.InstanceName, slot.Owner = "", "", "", nil
		case fact.State == "free" && fact.Error == "" && (fact.GatewayState == "" || fact.GatewayState == "stopped"):
			if bound {
				slot.InstanceID, slot.Generation, slot.GatewayID, slot.InstanceState, slot.InstanceName, slot.Owner = nil, nil, "", "", "", nil
				slot.ReconciliationState = "conflict"
				slot.ObservationMessage = "Runtime reports an empty slot but the database still reserves it"
			} else {
				slot.ReconciliationState = "consistent"
			}
		case fact.State == "occupied":
			occupied++
			if fact.InstanceID <= 0 || fact.UserID <= 0 || fact.Generation <= 0 || fact.GatewayID != fmt.Sprintf("gw-%d-%d", fact.InstanceID, fact.Generation) {
				slot.ObservationMessage = "Runtime reports occupancy without a usable owner identity"
				break
			}
			switch {
			case !bindingsKnown:
				slot.ObservationMessage = "Database bindings could not be verified"
			case !bound:
				slot.ReconciliationState = "residual"
				slot.ObservationMessage = "Runtime reports an owner without a database binding"
			case binding.InstanceID != fact.InstanceID || binding.Generation != fact.Generation || binding.GatewayID != fact.GatewayID:
				slot.ReconciliationState = "conflict"
				slot.ObservationMessage = "Database and Runtime disagree about the slot owner or generation"
			case !fact.IdentityVerified || fact.Error != "":
				slot.ObservationMessage = "Runtime owner proof is incomplete"
			case inventoryState(binding.State) != inventoryState(slot.InstanceState):
				slot.ReconciliationState = "conflict"
				slot.ObservationMessage = "Database and Runtime disagree about the gateway state"
			default:
				slot.ReconciliationState, slot.BindingConfirmed, slot.ExpectedInstanceID = "consistent", true, nil
			}
			if fact.Error != "" {
				slot.ObservationMessage += ": " + fact.Error
			}
		default:
			slot.ObservationMessage = fact.Error
			if slot.ObservationMessage == "" {
				slot.ObservationMessage = "Runtime slot state could not be verified"
			}
		}
		if !bindingsKnown && slot.ReconciliationState == "consistent" {
			slot.ReconciliationState, slot.BindingConfirmed = "unknown", false
			slot.ObservationMessage = "Database bindings could not be verified"
		}
	}
	// An unattributed count may be an unpublished reservation. Preserve it rather
	// than manufacturing a free slot or resetting a concurrent claim.
	if p.UsedSlots > max(occupied, len(bindings)) {
		for index := range p.Slots {
			slot := &p.Slots[index]
			if slot.InstanceID == nil && slot.ReconciliationState == "consistent" {
				slot.ReconciliationState = "unknown"
				slot.ObservationMessage = "Runtime occupancy count has an unresolved owner or allocation reservation"
			}
		}
	}
	p.UsedSlots = max(p.UsedSlots, occupied, len(bindings))
	for _, slot := range p.Slots {
		if slot.ReconciliationState == "unknown" {
			p.UnknownSlots++
		}
		if inventoryPriority(slot.ReconciliationState) > inventoryPriority(p.ReconciliationState) {
			p.ReconciliationState, p.ReconciliationMessage = slot.ReconciliationState, slot.ObservationMessage
		}
	}
	if len(p.Slots) == 0 {
		p.ReconciliationState, p.ReconciliationMessage = "unknown", "Physical slot inventory is unavailable"
	}
}

// Reported identity is display data. Only the reconciliation branch above may
// confirm a binding; incomplete tuples cannot inherit SQL writer identifiers.
func inventoryReportedOwner(slot *RuntimeSlot, fact RuntimeSlotObservation) {
	if slot.InstanceID == nil || *slot.InstanceID != fact.InstanceID {
		slot.InstanceName, slot.Owner = "", nil
	}
	slot.InstanceID, slot.Generation, slot.GatewayID = inventoryInt(fact.InstanceID), nil, ""
	slot.InstanceState = fact.GatewayState
	if slot.InstanceState == "" {
		slot.InstanceState = fact.InstanceState
	}
	if fact.UserID > 0 && fact.Generation > 0 && fact.GatewayID == fmt.Sprintf("gw-%d-%d", fact.InstanceID, fact.Generation) {
		slot.Generation, slot.GatewayID = inventoryInt(fact.Generation), fact.GatewayID
	}
}

func inventoryInt(value int) *int { return &value }

// ConfirmedEmptyRuntimeObservation is also used under the source row lock at
// publication and replica mutation, so a later SQL observation can veto either.
func ConfirmedEmptyRuntimeObservation(p RuntimePod, now time.Time) error {
	var observation RuntimePodObservation
	if p.ObservationJSON == nil || json.Unmarshal([]byte(*p.ObservationJSON), &observation) != nil || p.Capacity < 1 || len(observation.Slots) != p.Capacity {
		return fmt.Errorf("complete Runtime empty observation is required")
	}
	if len(p.Slots) == 0 {
		for _, slot := range observation.Slots {
			port := slot.GatewayPort
			p.Slots = append(p.Slots, RuntimeSlot{ContainerName: slot.ContainerName, GatewayPort: &port, Ready: true})
		}
	}
	ReconcileRuntimePodObservation(&p, nil, true, now)
	if p.ReconciliationState != "consistent" || p.UsedSlots != 0 {
		return fmt.Errorf("Runtime emptiness is not reconciled: %s", p.ReconciliationMessage)
	}
	return nil
}
func inventoryPriority(state string) int {
	switch state {
	case "conflict":
		return 4
	case "residual":
		return 3
	case "unknown":
		return 2
	case "releasing":
		return 1
	default:
		return 0
	}
}
func inventoryState(state string) string {
	switch state {
	case "ready", "active", "running":
		return "running"
	case "starting", "creating", "pending", "reserved":
		return "starting"
	case "failed", "unhealthy", "error":
		return "error"
	case "stopping", "draining", "migrating":
		return "stopping"
	default:
		return state
	}
}
