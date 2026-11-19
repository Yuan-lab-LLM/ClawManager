package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

func (s *RuntimeManagementService) commitSlotResetRecovery(ctx context.Context, op *RuntimeManagementOperation) error {
	if op == nil || len(op.Items) != 1 || op.Items[0].SlotReset == nil {
		return errors.New("single fixed slot reset required")
	}
	j := op.Items[0].SlotReset
	if j.Scope.OperationID != op.ID || j.Scope.PodUID != j.PodUID || j.Scope.ContainerName != j.Slot || j.Scope.ContainerID != j.ContainerID {
		return errors.New("slot reset source checkpoint identity changed")
	}
	if err := runtimeeviction.ValidateSlotReceipt(j.Scope, j.Receipt); err != nil {
		return err
	}
	var committedItems []RuntimeSchedulingItem
	callbackCompleted := false
	err := s.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
			return e
		}
		var owner, status, raw string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status,items_json FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
		if err != nil {
			return err
		}
		if err = row.Scan(&owner, &status, &raw); err != nil {
			return err
		}
		if owner != s.token || status != "processing" {
			return errors.New("slot reset operation ownership changed")
		}
		var stored []RuntimeSchedulingItem
		if json.Unmarshal([]byte(raw), &stored) != nil || len(stored) != 1 || stored[0].SlotReset == nil {
			return errors.New("slot reset durable checkpoint missing")
		}
		if stored[0].SlotReset.Scope != j.Scope {
			return errors.New("slot reset durable scope changed")
		}
		if stored[0].SlotReset.CleanupCommitted {
			committedItems = stored
			callbackCompleted = true
			return nil
		}
		action := "none"
		recoveryGeneration := 0
		if j.InstanceID > 0 {
			var userID, generation int
			var instanceStatus, instanceType, instanceBackend, instanceMode string
			row, err = tx.SQL().QueryRowContext(ctx, "SELECT user_id,runtime_generation,status,COALESCE(type,''),COALESCE(runtime_type,''),COALESCE(instance_mode,'') FROM instances WHERE id=? FOR UPDATE", j.InstanceID)
			if err != nil {
				return err
			}
			err = row.Scan(&userID, &generation, &instanceStatus, &instanceType, &instanceBackend, &instanceMode)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				unpublished := validUnpublishedSlotReset(j, userID) && generation == 1
				var current models.InstanceRuntimeBinding
				row, err = tx.SQL().QueryRowContext(ctx, "SELECT id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,generation,state FROM instance_runtime_bindings WHERE instance_id=? FOR UPDATE", j.InstanceID)
				if err != nil {
					return err
				}
				err = row.Scan(&current.ID, &current.InstanceID, &current.RuntimePodID, &current.RuntimeType, &current.GatewayID, &current.GatewayPort, &current.WorkspacePath, &current.Generation, &current.State)
				var binding *models.InstanceRuntimeBinding
				if err == nil {
					binding = &current
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				old := models.InstanceRuntimeBinding{InstanceID: j.InstanceID, RuntimePodID: j.Source.ID, RuntimeType: j.Source.RuntimeType, GatewayPort: j.GatewayPort}
				if j.Owner != nil {
					old.Generation = j.Owner.Generation
					old.GatewayID = j.Owner.GatewayID
				}
				if j.SourceBinding != nil {
					old = *j.SourceBinding
				}
				desired := "running"
				row, err = tx.SQL().QueryRowContext(ctx, "SELECT desired_power_state FROM instance_desired_state WHERE instance_id=? FOR UPDATE", j.InstanceID)
				if err != nil {
					return err
				}
				if err = row.Scan(&desired); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				action = slotResetRecoveryAction(&models.Instance{ID: j.InstanceID, UserID: userID, Status: instanceStatus, RuntimeGeneration: generation, Type: instanceType, RuntimeType: instanceBackend, InstanceMode: instanceMode}, binding, old, desired)
				var stopOperation string
				row, err = tx.SQL().QueryRowContext(ctx, "SELECT stop_operation_id FROM instance_model_activity WHERE instance_id=? FOR UPDATE", j.InstanceID)
				if err != nil {
					return err
				}
				if err = row.Scan(&stopOperation); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if stopOperation != "" && action == "reschedule" {
					action = "none"
				}
				if binding == nil || sameSlotResetBinding(*binding, old) {
					if (j.Owner == nil || j.Owner.UserID != userID) && !unpublished {
						return errors.New("old slot business ownership is not confirmed")
					}
					if !unpublished && (j.WriterScope == nil || j.WriterReceipt == nil || runtimeeviction.ValidateReceipt(*j.WriterScope, j.WriterReceipt) != nil || j.WriterScope.PodUID != j.PodUID || j.WriterScope.ContainerID != j.ContainerID || j.WriterScope.ContainerName != j.Slot || j.WriterScope.InstanceID != j.InstanceID || j.WriterScope.UserID != userID || j.WriterScope.Generation != old.Generation) {
						return errors.New("exact old workspace writer retirement is not confirmed")
					}
					if binding != nil {
						result, e := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND generation=? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ?`, old.ID, old.InstanceID, old.RuntimePodID, old.Generation, old.GatewayID, old.GatewayPort, old.RuntimeType, old.WorkspacePath)
						if e != nil {
							return e
						}
						n, e := result.RowsAffected()
						if e != nil {
							return e
						}
						if n != 1 {
							return errors.New("old binding changed during slot reset")
						}
						if _, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,0),updated_at=? WHERE id=?", time.Now().UTC(), old.RuntimePodID); e != nil {
							return e
						}
					}
					if action == "reschedule" {
						recoveryGeneration = generation
						var existingGeneration int
						var existingPod int64
						var existingPort int
						var existingPhase string
						row, e := tx.SQL().QueryRowContext(ctx, "SELECT generation,phase,source_pod_id,source_gateway_port FROM instance_runtime_recovery WHERE instance_id=? FOR UPDATE", j.InstanceID)
						if e != nil {
							return e
						}
						e = row.Scan(&existingGeneration, &existingPhase, &existingPod, &existingPort)
						if e != nil && !errors.Is(e, sql.ErrNoRows) {
							return e
						}
						if e == nil && (existingGeneration != generation || existingPhase != "" && existingPhase != "source_stopped" && existingPhase != "placing" || existingPod != old.RuntimePodID || existingPort != old.GatewayPort) {
							return errors.New("another generation already owns instance recovery")
						}
						_, e = tx.SQL().ExecContext(ctx, `INSERT INTO instance_runtime_recovery(instance_id,attempts,reason,source_pod_id,source_gateway_port,generation,phase,updated_at) VALUES(?,1,'manual slot reset',?,?,?,'source_stopped',?) ON DUPLICATE KEY UPDATE attempts=1,next_retry_at=NULL,healthy_since=NULL,reason='manual slot reset',source_pod_id=VALUES(source_pod_id),source_gateway_port=VALUES(source_gateway_port),phase='source_stopped',updated_at=VALUES(updated_at)`, j.InstanceID, old.RuntimePodID, old.GatewayPort, generation, time.Now().UTC())
						if e != nil {
							return e
						}
						if _, e = tx.SQL().ExecContext(ctx, "UPDATE instances SET status='creating',runtime_error_code=NULL,runtime_error_message=NULL,updated_at=? WHERE id=? AND runtime_generation=?", time.Now().UTC(), j.InstanceID, generation); e != nil {
							return e
						}
					}
				}
			}
		}
		copy := *j
		copy.CleanupCommitted = true
		copy.Phase = "committed"
		copy.RecoveryAction = action
		copy.RecoveryGeneration = recoveryGeneration
		copy.RecoveryState = "none"
		if action == "reschedule" {
			copy.RecoveryState = "pending"
		} else if action == "preserve_current" {
			copy.RecoveryState = "preserve_current"
		}
		if j.Owner != nil {
			if _, e := tx.SQL().ExecContext(ctx, "UPDATE runtime_slot_reset_requests SET instance_id=?,source_generation=?,source_gateway_id=?,recovery_generation=? WHERE operation_id=?", j.InstanceID, j.Owner.Generation, j.Owner.GatewayID, recoveryGeneration, op.ID); e != nil {
				return e
			}
		} else if j.UnpublishedReservation != nil && j.SourceBinding != nil {
			if _, e := tx.SQL().ExecContext(ctx, "UPDATE runtime_slot_reset_requests SET recovery_generation=? WHERE operation_id=?", recoveryGeneration, op.ID); e != nil {
				return e
			}
		}
		items := append([]RuntimeSchedulingItem(nil), op.Items...)
		items[0].SlotReset = &copy
		encoded, err := json.Marshal(items)
		if err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET items_json=?,updated_at=? WHERE id=? AND owner_token=? AND status='processing'", string(encoded), time.Now().UTC(), op.ID, s.token)
		if err != nil {
			return err
		}
		if n, e := result.RowsAffected(); e != nil {
			return e
		} else if n != 1 {
			return fmt.Errorf("slot reset checkpoint lost")
		}
		committedItems = items
		callbackCompleted = true
		return nil
	}, nil)
	if err != nil && callbackCompleted {
		return errors.Join(ErrRuntimeSlotResetCommitUncertain, err)
	}
	if err == nil {
		op.Items = committedItems
	}
	return err
}

func publicSlotResetOperation(op RuntimeManagementOperation) RuntimeManagementOperation {
	items := append([]RuntimeSchedulingItem(nil), op.Items...)
	for i := range items {
		if j := items[i].SlotReset; j != nil {
			items[i].SlotReset = &RuntimeSlotResetJournal{Phase: j.Phase, Slot: j.Slot, PodUID: j.PodUID, ContainerID: j.ContainerID, InstanceID: j.InstanceID, RecoveryAction: j.RecoveryAction, RecoveryState: j.RecoveryState, ErrorMessage: j.ErrorMessage, CleanupCommitted: j.CleanupCommitted, ScopeFingerprint: j.ScopeFingerprint}
		}
	}
	op.Items = items
	return op
}

func (s *RuntimeManagementService) refreshSlotResetProjection(ctx context.Context, op *RuntimeManagementOperation) {
	for index := range op.Items {
		old := op.Items[index].SlotReset
		if old == nil {
			continue
		}
		copy := *old
		op.Items[index].SlotReset = &copy
		j := &copy
		op.Phase = j.Phase
		op.RuntimeType = j.Source.RuntimeType
		if !j.CleanupCommitted || j.InstanceID == 0 || j.RecoveryAction != "reschedule" {
			continue
		}
		instance, err := s.scheduler.instanceRepo.GetByID(j.InstanceID)
		if err != nil {
			j.RecoveryState = "unknown"
			continue
		}
		if instance == nil {
			j.RecoveryState = "none"
			continue
		}
		if instance.Status != "creating" && instance.Status != "running" {
			j.RecoveryState = instance.Status
			continue
		}
		binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, j.InstanceID)
		if err != nil {
			j.RecoveryState = "unknown"
			continue
		}
		j.RecoveryState = "pending"
		if binding != nil && binding.Generation == instance.RuntimeGeneration {
			j.RecoveryState = "starting"
			if instance.Status == "running" && binding.State == RuntimeGatewayBindingRunning {
				j.RecoveryState = "running"
			}
		}
	}
}

func (s *RuntimeManagementService) finishSlotReset(ctx context.Context, op *RuntimeManagementOperation) error {
	encoded, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		result, e := tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='completed',items_json=?,updated_at=? WHERE id=? AND owner_token=? AND status='processing'", string(encoded), time.Now().UTC(), op.ID, s.token)
		if e != nil {
			return e
		}
		if n, e := result.RowsAffected(); e != nil {
			return e
		} else if n != 1 {
			return errors.New("slot reset completion owner changed")
		}
		_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_slot_reset_requests SET active=NULL WHERE operation_id=?", op.ID)
		return e
	}, nil)
	if err != nil {
		return errors.Join(ErrRuntimeSlotResetCommitUncertain, err)
	}
	return nil
}
