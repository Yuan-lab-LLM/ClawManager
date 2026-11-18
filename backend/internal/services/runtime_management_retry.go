package services

import (
	"context"
	"encoding/json"
	"errors"

	"clawreef/internal/models"
)

// An explicit history retry may resume a failed source stop, but cannot revive
// an arbitrary stopped instance. The original journal and binding must still
// identify the same writer, and the restart fence must have advanced only once.
// DeleteGateway revalidates persisted container evidence before any new launch.
func (s *RuntimeManagementService) validateMoveRetry(ctx context.Context, operationID string, instance *models.Instance, binding *models.InstanceRuntimeBinding) error {
	invalid := errors.New("failed move retry identity changed or a later stop intent exists")
	if s.sess == nil || instance == nil || binding == nil || !runtimeManagementSourceState(binding.State) {
		return invalid
	}
	expectedGeneration := binding.Generation
	if instance.Status == "stopped" {
		expectedGeneration++
	} else if instance.Status != "running" && instance.Status != "error" {
		return invalid
	}
	if instance.RuntimeGeneration != expectedGeneration {
		return invalid
	}
	var status, kind, raw string
	row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT status,kind,items_json FROM runtime_management_operations WHERE id=?", operationID)
	if err != nil {
		return err
	}
	if err = row.Scan(&status, &kind, &raw); err != nil {
		return invalid
	}
	if (status != "failed" && status != "partial_failure") || (kind != "migration" && kind != "balance" && kind != "eviction") {
		return invalid
	}
	var items []RuntimeSchedulingItem
	if json.Unmarshal([]byte(raw), &items) != nil {
		return invalid
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return err
	}
	matched := false
	for _, item := range items {
		if item.State == "failed" && item.InstanceID == instance.ID && item.SourcePodID == binding.RuntimePodID && item.SourceGeneration == binding.Generation && item.SourceGatewayID == binding.GatewayID && source != nil && item.SourcePodUID == stringValue(source.PodUID) {
			matched = true
		}
	}
	if !matched {
		return invalid
	}
	var stopped int
	row, err = s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_model_activity WHERE instance_id=? AND (stop_operation_id<>'' OR stopped_reason<>'' OR start_deadline_at IS NOT NULL)", instance.ID)
	if err != nil {
		return err
	}
	if err = row.Scan(&stopped); err != nil {
		return err
	}
	if stopped != 0 {
		return invalid
	}
	return nil
}
