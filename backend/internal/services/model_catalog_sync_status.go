package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type RuntimeModelCatalogSyncReport struct {
	State       string    `json:"state"`
	OwnerUserID int       `json:"owner_user_id"`
	Generation  int       `json:"generation"`
	Revision    string    `json:"revision,omitempty"`
	ModelCount  int       `json:"model_count"`
	ObservedAt  time.Time `json:"observed_at"`
}

type InstanceModelCatalogSyncStatus struct {
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	Generation int        `json:"generation"`
	Revision   string     `json:"revision,omitempty"`
	ModelCount int        `json:"model_count"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

// Gateway-state inspection is reused outside the model-status endpoint. Never
// retain arbitrary strings from an agent as public revision/state metadata.
func sanitizeModelCatalogSyncReport(report *RuntimeModelCatalogSyncReport) *RuntimeModelCatalogSyncReport {
	if report == nil {
		return nil
	}
	copy := *report
	switch copy.State {
	case "applied", "empty":
		if revision, err := hex.DecodeString(copy.Revision); err != nil || len(revision) != 32 {
			return nil
		}
	case "syncing", "failed":
		copy.Revision = ""
		copy.ModelCount = 0
	default:
		return nil
	}
	return &copy
}

func validatedModelCatalogSync(instance *models.Instance, state *RuntimeAgentGatewayState, gatewayID string) *InstanceModelCatalogSyncStatus {
	unknown := func(reason string) *InstanceModelCatalogSyncStatus {
		return &InstanceModelCatalogSyncStatus{State: "unknown", Reason: reason, Generation: instance.RuntimeGeneration}
	}
	if state == nil || state.GatewayID != gatewayID || state.InstanceID != instance.ID || state.Generation != instance.RuntimeGeneration {
		return unknown("identity_mismatch")
	}
	report := state.ModelCatalogSync
	if report == nil {
		return unknown("not_reported")
	}
	if report.OwnerUserID != instance.UserID || report.Generation != instance.RuntimeGeneration {
		return unknown("identity_mismatch")
	}
	if report.ModelCount < 0 || report.ModelCount > 100000 || report.ObservedAt.IsZero() || report.ObservedAt.After(time.Now().Add(2*time.Minute)) {
		return unknown("invalid_report")
	}
	revision, count := "", 0
	switch report.State {
	case "applied", "empty":
		if revision, err := hex.DecodeString(report.Revision); err != nil || len(revision) != 32 {
			return unknown("invalid_report")
		}
		if report.State == "empty" && report.ModelCount != 0 {
			return unknown("invalid_report")
		}
		revision, count = strings.ToLower(report.Revision), report.ModelCount
	case "syncing", "failed":
	default:
		return unknown("invalid_report")
	}
	return &InstanceModelCatalogSyncStatus{State: report.State, Generation: report.Generation, Revision: revision, ModelCount: count, ObservedAt: &report.ObservedAt}
}

// Observation only. Never infer an applied write from desired configuration.
func (s *instanceService) ModelCatalogSyncStatus(ctx context.Context, instance *models.Instance) *InstanceModelCatalogSyncStatus {
	status := &InstanceModelCatalogSyncStatus{State: "unknown", Reason: "not_reported"}
	if instance == nil {
		return status
	}
	status.Generation = instance.RuntimeGeneration
	if instance.Status != "running" && instance.Status != "creating" {
		status.State = "not_running"
		status.Reason = ""
		return status
	}
	if s.bindingRepo == nil || s.runtimePodRepo == nil || s.agentClient == nil {
		return status
	}
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	binding, err := s.bindingRepo.GetByInstanceID(check, instance.ID)
	if err != nil {
		status.Reason = "runtime_unavailable"
		return status
	}
	if binding == nil {
		return status
	}
	if binding.InstanceID != instance.ID || binding.Generation != instance.RuntimeGeneration || binding.GatewayID == "" {
		status.Reason = "identity_mismatch"
		return status
	}
	pod, err := s.runtimePodRepo.GetByID(check, binding.RuntimePodID)
	if err != nil || pod == nil || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
		status.Reason = "runtime_unavailable"
		return status
	}
	reader, ok := s.agentClient.(interface {
		GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
	})
	if !ok {
		return status
	}
	state, err := reader.GatewayState(check, *pod.AgentEndpoint, binding.GatewayID)
	if err != nil {
		if errors.Is(err, ErrRuntimeAgentUnsupported) {
			return status
		}
		status.Reason = "runtime_unavailable"
		return status
	}
	return validatedModelCatalogSync(instance, state, binding.GatewayID)
}
