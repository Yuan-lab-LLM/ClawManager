package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"github.com/upper/db/v4"
)

// commitRecoveredEviction consumes only durable stop authority. Releasing the
// exact old slot and recording its lifecycle continuation are one transaction.
func (s *RuntimeManagementService) commitRecoveredEviction(ctx context.Context, item RuntimeSchedulingItem, ownerID string) (string, error) {
	var resultState string
	err := s.sess.TxContext(ctx, func(tx db.Session) error {
		var owner, status, raw string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status,items_json FROM runtime_management_operations WHERE id=? FOR UPDATE", ownerID)
		if err != nil {
			return err
		}
		if err = row.Scan(&owner, &status, &raw); err != nil {
			return err
		}
		if owner != s.token || status != "processing" {
			return errors.New("recovery execution ownership lost; source held")
		}
		var items []RuntimeSchedulingItem
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return err
		}
		index := -1
		for i := range items {
			if items[i].InstanceID == item.InstanceID {
				if index != -1 {
					return errors.New("duplicate recovery item")
				}
				index = i
			}
		}
		if index < 0 {
			return errors.New("durable recovery item missing")
		}
		current := &items[index]
		journal := current.Recovery
		if journal == nil {
			return errors.New("durable recovery checkpoint missing")
		}
		if journal.Phase == "released" {
			resultState = journal.ResultState
			if resultState == "" {
				return errors.New("released recovery result missing")
			}
			return nil
		}
		if journal.Phase != "source_stopped" {
			return errors.New("source stop is not confirmed")
		}
		if journal.Receipt != nil {
			if err = runtimeeviction.ValidateReceipt(journal.Scope, journal.Receipt); err != nil {
				return err
			}
		} else if !journal.SourceStopConfirmed {
			return errors.New("source stop authority missing")
		}
		binding := journal.SourceBinding
		if binding.InstanceID != current.InstanceID || binding.RuntimePodID != current.SourcePodID || binding.GatewayID != current.SourceGatewayID || binding.Generation != current.SourceGeneration || binding.GatewayPort <= 0 || journal.SourceUserID <= 0 || current.SourcePodUID == "" {
			return errors.New("recovery source identity incomplete")
		}
		receiptGateway := binding.GatewayID
		if strings.HasPrefix(receiptGateway, "pending-") {
			receiptGateway = "gw-" + strings.TrimPrefix(receiptGateway, "pending-")
		}
		if journal.Receipt != nil && (journal.Scope.OperationID != ownerID || journal.Scope.InstanceID != current.InstanceID || journal.Scope.UserID != journal.SourceUserID || journal.Scope.PodUID != current.SourcePodUID || journal.Scope.Generation != binding.Generation || journal.Scope.GatewayID != receiptGateway || journal.Scope.RuntimeType != binding.RuntimeType) {
			return errors.New("stop receipt scope differs from source binding")
		}
		var userID, generation int
		var instanceStatus string
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT user_id,runtime_generation,status FROM instances WHERE id=? FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&userID, &generation, &instanceStatus); err != nil {
			return err
		}
		if userID != journal.SourceUserID || (generation != binding.Generation && generation != binding.Generation+1) {
			return errors.New("instance identity changed after source stop")
		}
		var guard string
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT token FROM instance_lifecycle_manual_guards WHERE instance_id=? FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&guard); err != nil {
			return err
		}
		if guard != ownerID {
			return errors.New("recovery mutation guard lost")
		}
		// Consume only journals describing the exact terminal source. A newer
		// intent is a conflict, never collateral cleanup.
		var failover models.RuntimeFailoverIntent
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT user_id,generation,source_pod_id,source_pod_uid,source_endpoint,gateway_id,gateway_port,runtime_type,workspace_path FROM runtime_failover_intents WHERE instance_id=? FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		err = row.Scan(&failover.UserID, &failover.Generation, &failover.SourcePodID, &failover.SourcePodUID, &failover.SourceEndpoint, &failover.GatewayID, &failover.GatewayPort, &failover.RuntimeType, &failover.WorkspacePath)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if failover.UserID != journal.SourceUserID || failover.Generation != binding.Generation || failover.SourcePodID != binding.RuntimePodID || failover.SourcePodUID != current.SourcePodUID || failover.SourceEndpoint != journal.SourceEndpoint || (failover.GatewayID != binding.GatewayID && !sameFaultGatewayAlias(current.InstanceID, binding.Generation, failover.GatewayID, binding.GatewayID)) || failover.GatewayPort != binding.GatewayPort || failover.RuntimeType != binding.RuntimeType || failover.WorkspacePath != binding.WorkspacePath {
				return errors.New("conflicting failover source; recovery held")
			}
			deleted, e := tx.SQL().ExecContext(ctx, `DELETE FROM runtime_failover_intents WHERE instance_id=? AND user_id=? AND generation=? AND source_pod_id=? AND BINARY source_pod_uid=BINARY ? AND BINARY source_endpoint=BINARY ? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ?`, current.InstanceID, failover.UserID, failover.Generation, failover.SourcePodID, failover.SourcePodUID, failover.SourceEndpoint, failover.GatewayID, failover.GatewayPort, failover.RuntimeType, failover.WorkspacePath)
			if e != nil {
				return e
			}
			n, e := deleted.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("failover source changed")
			}
		}
		var recovery models.InstanceRuntimeRecovery
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT generation,source_pod_id,source_gateway_port,phase FROM instance_runtime_recovery WHERE instance_id=? FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		err = row.Scan(&recovery.Generation, &recovery.SourcePodID, &recovery.SourceGatewayPort, &recovery.Phase)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if recovery.Generation != binding.Generation || (recovery.Phase != "" && recovery.Phase != "source_stopped" && recovery.Phase != "placing") {
				return errors.New("conflicting runtime recovery source; recovery held")
			}
			if recovery.Phase == "placing" {
				// This row names the previous generation's source. Even reuse of
				// the same Pod/port cannot substitute the current stop for its proof.
				proofCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				e := s.verifyPreviousRecoveryPlacement(proofCtx, tx, *current, recovery, generation)
				cancel()
				if e != nil {
					return e
				}
			} else if recovery.SourcePodID != binding.RuntimePodID || recovery.SourceGatewayPort != binding.GatewayPort {
				return errors.New("conflicting runtime recovery source; recovery held")
			}
			deleted, e := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_recovery WHERE instance_id=? AND generation=? AND source_pod_id=? AND source_gateway_port=? AND BINARY phase=BINARY ?`, current.InstanceID, recovery.Generation, recovery.SourcePodID, recovery.SourceGatewayPort, recovery.Phase)
			if e != nil {
				return e
			}
			n, e := deleted.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("runtime recovery source changed")
			}
		}
		var upgrades int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND state NOT IN ('cancelled','completed') FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&upgrades); err != nil {
			return err
		}
		if upgrades > 0 {
			return errors.New("protected runtime upgrade is active")
		}
		var desired string
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT desired_power_state FROM instance_desired_state WHERE instance_id=? FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&desired); err != nil && err != sql.ErrNoRows {
			return err
		}
		var stopIntent int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_model_activity WHERE instance_id=? AND (stop_operation_id<>'' OR stopped_reason<>'') FOR UPDATE", current.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&stopIntent); err != nil {
			return err
		}
		nextStatus := instanceStatus
		switch {
		case instanceStatus == "deleting":
			resultState = "deleting"
		case instanceStatus == "stopped" || desired == "stopped" || stopIntent > 0 || journal.DesiredStatus == "stopped":
			nextStatus = "stopped"
			resultState = "stopped"
		case (instanceStatus == "running" || instanceStatus == "error" || instanceStatus == "creating") && (journal.DesiredStatus == "running" || journal.DesiredStatus == "error" || journal.DesiredStatus == "creating"):
			nextStatus = "creating"
			resultState = "waiting_capacity"
			if generation == binding.Generation {
				generation++
			}
		default:
			return fmt.Errorf("instance lifecycle status %q cannot continue recovery", instanceStatus)
		}
		// Bindings lock before the Pod, matching the ordinary release lock order.
		deleted, err := tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id=? AND instance_id=? AND runtime_pod_id=? AND generation=? AND BINARY gateway_id=BINARY ? AND gateway_port=? AND BINARY runtime_type=BINARY ? AND BINARY workspace_path=BINARY ?`, binding.ID, current.InstanceID, binding.RuntimePodID, binding.Generation, binding.GatewayID, binding.GatewayPort, binding.RuntimeType, binding.WorkspacePath)
		if err != nil {
			return err
		}
		n, err := deleted.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("source binding changed after confirmed stop")
		}
		var uid string
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT pod_uid FROM runtime_pods WHERE id=? FOR UPDATE", binding.RuntimePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&uid); err != nil {
			return err
		}
		if uid != current.SourcePodUID {
			return errors.New("source Pod UID changed after confirmed stop")
		}
		now := time.Now().UTC()
		if _, err = tx.SQL().ExecContext(ctx, "UPDATE instances SET status=?,runtime_generation=?,updated_at=? WHERE id=?", nextStatus, generation, now, current.InstanceID); err != nil {
			return err
		}
		if nextStatus == "creating" {
			if _, err = tx.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET generation=?,monitoring_since=?,activity_version=activity_version+1,blocker='',start_deadline_at=NULL,updated_at=? WHERE instance_id=?`, generation, now, now, current.InstanceID); err != nil {
				return err
			}
		}
		var remaining int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?", binding.RuntimePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&remaining); err != nil {
			return err
		}
		if _, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET used_slots=?,updated_at=? WHERE id=?", remaining, now, binding.RuntimePodID); err != nil {
			return err
		}
		journal.Phase = "released"
		journal.ResultState = resultState
		current.State = resultState
		current.ErrorMessage = ""
		encoded, err := json.Marshal(items)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET items_json=?,updated_at=? WHERE id=?", string(encoded), now, ownerID)
		return err
	}, nil)
	return resultState, err
}

// The caller holds the checkpoint row lock through proof verification and its
// exact conditional deletion. Failed/unavailable evidence rolls back the whole
// source release; this read-only check never grants a new stopped-source receipt.
func (s *RuntimeManagementService) verifyPreviousRecoveryPlacement(ctx context.Context, tx db.Session, item RuntimeSchedulingItem, checkpoint models.InstanceRuntimeRecovery, generation int) error {
	if checkpoint.Phase != "placing" || checkpoint.Generation != item.SourceGeneration || checkpoint.Generation < 2 || checkpoint.SourcePodID < 1 || checkpoint.SourceGatewayPort < 1 || s.scheduler == nil {
		return errors.New("prior recovery placement identity is incomplete")
	}
	client, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok || client.assignments == nil {
		return errors.New("prior recovery placement requires retained publication verification")
	}
	var uid, endpoint, kind string
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT COALESCE(pod_uid,''),COALESCE(agent_endpoint,''),runtime_type FROM runtime_pods WHERE id=?", checkpoint.SourcePodID)
	if err != nil {
		return err
	}
	if err = row.Scan(&uid, &endpoint, &kind); err != nil {
		return err
	}
	j := item.Recovery
	if uid == "" || endpoint == "" || kind != j.SourceBinding.RuntimeType {
		return errors.New("prior recovery placement source identity is unavailable")
	}
	sources := []models.RuntimeCleanupSource{
		{Generation: checkpoint.Generation - 1, RuntimePodID: checkpoint.SourcePodID, PodUID: uid, Endpoint: endpoint, GatewayID: fmt.Sprintf("gw-%d-%d", item.InstanceID, checkpoint.Generation-1), WorkspacePath: j.SourceBinding.WorkspacePath},
		{Generation: item.SourceGeneration, RuntimePodID: item.SourcePodID, PodUID: item.SourcePodUID, Endpoint: j.SourceEndpoint, GatewayID: fmt.Sprintf("gw-%d-%d", item.InstanceID, item.SourceGeneration), WorkspacePath: j.SourceBinding.WorkspacePath},
	}
	if err = client.assignments.CheckUnboundInstanceStopped(ctx, item.InstanceID, j.SourceUserID, generation, kind, sources); err != nil {
		return fmt.Errorf("prior recovery placement stop proof: %w", err)
	}
	return nil
}

func sameFaultGatewayAlias(id, generation int, a, b string) bool {
	canonical := fmt.Sprintf("gw-%d-%d", id, generation)
	pending := fmt.Sprintf("pending-%d-%d", id, generation)
	return (a == canonical && b == pending) || (a == pending && b == canonical)
}
