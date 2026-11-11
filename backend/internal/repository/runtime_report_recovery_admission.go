package repository

import (
	"context"
	"encoding/json"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// ReportedGatewayRecoveryBlocked preserves the terminal authority of a recovery
// journal even after its short-lived lifecycle guard has been released. A native
// report cannot reconstruct a generation whose writer was deliberately retired.
func (r *instanceRuntimeBindingRepository) ReportedGatewayRecoveryBlocked(ctx context.Context, binding *models.InstanceRuntimeBinding) (bool, error) {
	return reportedGatewayRecoveryBlocked(ctx, r.sess, binding)
}

func reportedGatewayRecoveryBlocked(ctx context.Context, sess db.Session, binding *models.InstanceRuntimeBinding) (bool, error) {
	if binding == nil {
		return true, nil
	}
	rows, err := sess.SQL().QueryContext(ctx, `SELECT status,items_json FROM runtime_management_operations WHERE kind='eviction' AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.mode'))='recovery'`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, raw string
		if err = rows.Scan(&status, &raw); err != nil {
			return false, err
		}
		var items []struct {
			InstanceID       int `json:"instance_id"`
			SourceGeneration int `json:"source_generation"`
			Recovery         *struct {
				Phase string `json:"phase"`
			} `json:"recovery"`
		}
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return false, err
		}
		for _, item := range items {
			if item.InstanceID != binding.InstanceID || item.SourceGeneration != binding.Generation || item.Recovery == nil {
				continue
			}
			switch item.Recovery.Phase {
			case "fenced", "stopping", "source_stopped", "released":
				return true, nil
			}
			switch status {
			case "pending", "processing", "needs_attention":
				return true, nil
			}
		}
	}
	return false, rows.Err()
}
