package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
)

// RuntimeCleanupHistory keeps retired sources visible after their binding is
// released. Absence of a binding is not absence of an old publication intent.
func (r *instanceRepository) RuntimeCleanupHistory(ctx context.Context, want *models.Instance) ([]models.RuntimeCleanupSource, error) {
	if want == nil {
		return nil, fmt.Errorf("cleanup instance is missing")
	}
	sources := []models.RuntimeCleanupSource{}
	err := r.withInstanceRowLock(ctx, want.ID, func(local *instanceRepository) error {
		var user, generation int
		row, err := local.sess.SQL().QueryRowContext(ctx, "SELECT user_id,runtime_generation FROM instances WHERE id=? FOR UPDATE", want.ID)
		if err != nil {
			return err
		}
		if err = row.Scan(&user, &generation); err != nil {
			return err
		}
		if user != want.UserID || generation != want.RuntimeGeneration {
			return fmt.Errorf("cleanup instance owner or generation changed")
		}
		var bindings int
		row, err = local.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=? FOR UPDATE", want.ID)
		if err != nil {
			return err
		}
		if err = row.Scan(&bindings); err != nil {
			return err
		}
		if bindings != 0 {
			return fmt.Errorf("runtime binding appeared during cleanup proof")
		}
		rows, err := local.sess.SQL().QueryContext(ctx, "SELECT id,items_json FROM runtime_management_operations")
		if err != nil {
			return err
		}
		for rows.Next() {
			var operation, raw string
			if err = rows.Scan(&operation, &raw); err != nil {
				rows.Close()
				return err
			}
			var items []recoveredStopJournalItem
			if err = json.Unmarshal([]byte(raw), &items); err != nil {
				rows.Close()
				return fmt.Errorf("malformed management cleanup history: %w", err)
			}
			for _, item := range items {
				if item.InstanceID != want.ID || (item.SourceGeneration == 0 && item.SourcePodID == 0) {
					continue
				}
				source := models.RuntimeCleanupSource{Generation: item.SourceGeneration, RuntimePodID: item.SourcePodID, PodUID: item.SourcePodUID, GatewayID: item.SourceGatewayID}
				if want.WorkspacePath != nil {
					source.WorkspacePath = *want.WorkspacePath
				}
				if source.Generation <= 0 || source.PodUID == "" || source.GatewayID == "" {
					source.Blocker = "management source has incomplete retained publication identity"
				}
				if j := item.Recovery; j != nil {
					source.Endpoint = j.SourceEndpoint
					source.WorkspacePath = j.SourceBinding.WorkspacePath
					if j.Phase == "released" {
						op := models.InstanceIdleStopOperation{InstanceID: want.ID, UserID: want.UserID, Generation: source.Generation, RuntimePodID: source.RuntimePodID, GatewayID: source.GatewayID, PodUID: source.PodUID, Endpoint: source.Endpoint, WorkspacePath: source.WorkspacePath}
						if err = validateRecoveredSourceJournal(operation, item, op); err != nil {
							rows.Close()
							return fmt.Errorf("source generation %d recovery proof invalid: %w", source.Generation, err)
						}
						source.StoppedProof = true
					}
				}
				sources = append(sources, source)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = local.sess.SQL().QueryContext(ctx, "SELECT user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,phase FROM instance_idle_stop_operations WHERE instance_id=?", want.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var owner int
			var phase string
			var source models.RuntimeCleanupSource
			if err = rows.Scan(&owner, &source.Generation, &source.RuntimePodID, &source.GatewayID, &source.PodUID, &source.Endpoint, &source.WorkspacePath, &phase); err != nil {
				rows.Close()
				return err
			}
			if source.RuntimePodID == 0 && source.GatewayID == "" {
				continue
			}
			if owner != want.UserID {
				rows.Close()
				return fmt.Errorf("native stop source owner changed")
			}
			source.StoppedProof = phase == "completed"
			sources = append(sources, source)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = local.sess.SQL().QueryContext(ctx, "SELECT user_id,generation,source_pod_id,source_pod_uid,source_endpoint,gateway_id,workspace_path,source_stopped FROM runtime_failover_intents WHERE instance_id=?", want.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var owner int
			var source models.RuntimeCleanupSource
			if err = rows.Scan(&owner, &source.Generation, &source.RuntimePodID, &source.PodUID, &source.Endpoint, &source.GatewayID, &source.WorkspacePath, &source.StoppedProof); err != nil {
				rows.Close()
				return err
			}
			if owner != want.UserID {
				rows.Close()
				return fmt.Errorf("failover cleanup source owner changed")
			}
			sources = append(sources, source)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = local.sess.SQL().QueryContext(ctx, "SELECT generation,source_pod_id FROM instance_runtime_recovery WHERE instance_id=? AND attempts>0", want.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var source models.RuntimeCleanupSource
			if err = rows.Scan(&source.Generation, &source.RuntimePodID); err != nil {
				rows.Close()
				return err
			}
			source.Blocker = "unresolved runtime recovery lacks an immutable source stop receipt"
			sources = append(sources, source)
		}
		err = rows.Err()
		rows.Close()
		return err
	})
	return sources, err
}
