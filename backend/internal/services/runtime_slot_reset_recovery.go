package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"errors"
)

// Ordinary crash recovery still requires its existing explicit authority.
// Only an exact, committed administrator slot-reset receipt authorizes this
// automatic continuation, called under the scheduler's instance lifecycle lock.
func (s *RuntimeScheduler) resumeSlotResetRecoveryLocked(ctx context.Context, instance models.Instance) (bool, []error) {
	if s.management == nil || s.management.sess == nil {
		return false, nil
	}
	store, ok := s.instanceRepo.(repository.InstanceRuntimeRecoveryRepository)
	if !ok {
		return false, nil
	}
	state, err := store.GetRuntimeRecovery(ctx, instance.ID)
	if err != nil {
		return true, []error{err}
	}
	if state == nil || state.Generation != instance.RuntimeGeneration || state.Reason != "manual slot reset" || state.Phase != "source_stopped" && state.Phase != "placing" {
		return false, nil
	}
	desired, err := s.management.slotResetDesiredPower(ctx, s.management.sess, instance.ID, &instance)
	if err != nil {
		return true, []error{err}
	}
	if desired != "running" {
		return true, nil
	}
	recoveryGeneration := instance.RuntimeGeneration
	if state.Phase == "placing" {
		recoveryGeneration--
	}
	rows, err := s.management.sess.SQL().QueryContext(ctx, `SELECT o.items_json FROM runtime_slot_reset_requests r JOIN runtime_management_operations o ON o.id=r.operation_id WHERE r.instance_id=? AND r.recovery_generation=? AND r.source_pod_id=? AND r.gateway_port=? AND o.kind='slot_reset' AND JSON_EXTRACT(o.items_json,'$[0].slot_reset.cleanup_committed')=true AND JSON_UNQUOTE(JSON_EXTRACT(o.items_json,'$[0].slot_reset.recovery_action'))='reschedule' ORDER BY r.created_at DESC LIMIT 1`, instance.ID, recoveryGeneration, state.SourcePodID, state.SourceGatewayPort)
	if err != nil {
		return true, []error{err}
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return true, []error{err}
		}
		var items []RuntimeSchedulingItem
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return true, []error{err}
		}
		if len(items) != 1 || items[0].SlotReset == nil {
			continue
		}
		j := items[0].SlotReset
		if !j.CleanupCommitted || j.InstanceID != instance.ID || j.RecoveryAction != "reschedule" || j.Source.ID != state.SourcePodID || j.GatewayPort != state.SourceGatewayPort || runtimeeviction.ValidateSlotReceipt(j.Scope, j.Receipt) != nil {
			continue
		}
		sourceGeneration := 1
		if j.Owner != nil {
			if j.Owner.UserID != instance.UserID || j.WriterScope == nil || j.WriterScope.Generation != j.Owner.Generation || j.WriterScope.InstanceID != instance.ID || j.WriterScope.PodUID != j.PodUID || runtimeeviction.ValidateReceipt(*j.WriterScope, j.WriterReceipt) != nil {
				continue
			}
			sourceGeneration = j.Owner.Generation
		} else if !validUnpublishedSlotReset(j, instance.UserID) || j.RecoveryGeneration != 1 {
			continue
		}
		expected := j.RecoveryGeneration
		if expected < sourceGeneration || expected != recoveryGeneration {
			continue
		}
		if state.Phase == "placing" {
			expected++
		}
		if expected != state.Generation {
			continue
		}
		// Release the cursor before starting placement; fixture and production
		// connection pools must not be needed to keep this proof read alive.
		if err = rows.Close(); err != nil {
			return true, []error{err}
		}
		return s.resumeRuntimeRecovery(ctx, store, instance, *state)
	}
	if err = rows.Err(); err != nil {
		return true, []error{err}
	}
	return true, []error{errors.New("manual slot reset recovery is waiting for its exact committed source proof")}
}
