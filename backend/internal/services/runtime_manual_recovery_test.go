package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManualRuntimeRecoveryRejectsStaleStoppedAndHealthySources(t *testing.T) {
	for _, scenario := range []string{"stale", "stopped", "healthy", "actor"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, b, a, i, _ := recoveryFixture()
			gen, actor := i.RuntimeGeneration, 1
			switch scenario {
			case "stale":
				gen++
			case "stopped":
				copy := i
				copy.Status = "stopped"
				r.byID[i.ID] = &copy
			case "healthy":
				b.bindings[i.ID].State = "running"
			case "actor":
				actor = 0
			}
			if _, err := s.RetryInstanceRecovery(context.Background(), i.ID, gen, actor); err == nil {
				t.Fatal("manual recovery accepted invalid or changed authority")
			}
			if len(a.deleteRequests) != 0 || b.bindings[i.ID] == nil || r.state != nil {
				t.Fatal("rejected manual retry mutated source")
			}
		})
	}
}
func TestManualRuntimeRecoveryRearmsBudgetButRetainsUnconfirmedWriter(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, Reason: "gateway_process_exited", UpdatedAt: now}
	a.deleteErr = errors.New("worker unreachable")
	if _, err := s.RetryInstanceRecovery(context.Background(), i.ID, i.RuntimeGeneration, 1); err == nil {
		t.Fatal("unconfirmed stop was reported as recovered")
	}
	current, _ := r.GetByID(i.ID)
	if len(a.deleteRequests) != 1 || b.bindings[i.ID] == nil || current.RuntimeGeneration != i.RuntimeGeneration || r.state.Attempts != 1 {
		t.Fatalf("manual rearm bypassed fencing or did not rearm: %+v", r.state)
	}
}
func TestManualRuntimeRecoveryInspectionIsReadOnly(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, UpdatedAt: now}
	info, err := s.InspectInstanceRecovery(context.Background(), i.ID)
	if err != nil || info.Generation != i.RuntimeGeneration || info.Recovery.Attempts != 3 || info.Binding == nil {
		t.Fatalf("inspection %+v %v", info, err)
	}
	if len(a.deleteRequests) != 0 || b.bindings[i.ID] == nil || r.state.UpdatedAt != now {
		t.Fatal("inspection performed recovery")
	}
}
func TestManualRuntimeRecoveryContinuesCompletedStopWithoutRearming(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	delete(b.bindings, i.ID)
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, Phase: "source_stopped", Reason: "gateway_process_exited", UpdatedAt: now.Add(-time.Hour)}
	_, err := s.RetryInstanceRecovery(context.Background(), i.ID, i.RuntimeGeneration, 1)
	current, _ := r.GetByID(i.ID)
	if (err != nil && !strings.Contains(err.Error(), "no schedulable")) || current.RuntimeGeneration != i.RuntimeGeneration+1 || r.state.Attempts != 3 || len(a.deleteRequests) != 0 {
		t.Fatalf("continuation repeated stop/reset budget: %+v %+v %v", current, r.state, err)
	}
}

func TestManualRuntimeRecoveryRetriesFailedPlacementWithoutAdvancingAgain(t *testing.T) {
	s, r, b, a, i, now := recoveryFixture()
	delete(b.bindings, i.ID)
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, Phase: "placing", Reason: "gateway_process_exited", UpdatedAt: now}
	_, err := s.RetryInstanceRecovery(context.Background(), i.ID, i.RuntimeGeneration, 1)
	current, _ := r.GetByID(i.ID)
	if (err != nil && !strings.Contains(err.Error(), "no schedulable")) || current.RuntimeGeneration != i.RuntimeGeneration || r.state.Phase != "placing" || r.state.Attempts != 3 || len(a.deleteRequests) != 0 {
		t.Fatalf("manual placement retry changed generation/source: %+v %+v %v", current, r.state, err)
	}
}
