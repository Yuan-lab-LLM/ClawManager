package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
)

// CompleteStopFromRecovery consumes independent, verified writer-stop authority.
// It does not manufacture an idle/Agent acknowledgement. The released journal
// remains the audit proof after the old native stop fence has been retired.
func (r *InstanceAutoStopRepository) CompleteStopFromRecovery(ctx context.Context, want models.InstanceIdleStopOperation) (bool, error) {
	completed := false
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		instance, err := lockActivityInstance(ctx, tx, want.InstanceID)
		if err != nil {
			return err
		}
		activity, err := readActivity(ctx, tx, want.InstanceID, true)
		if err != nil {
			return err
		}
		if activity == nil || activity.StopOperationID != want.ID || activity.Generation != want.Generation || instance.userID != want.UserID || instance.generation != want.Generation || (instance.status != "stopped" && instance.status != "deleting") || instance.mode != "lite" {
			return nil
		}
		var op models.InstanceIdleStopOperation
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind FROM instance_idle_stop_operations WHERE id=? FOR UPDATE`, want.ID)
		if err != nil {
			return err
		}
		if err = row.Scan(&op.ID, &op.InstanceID, &op.UserID, &op.Generation, &op.RuntimePodID, &op.GatewayID, &op.PodUID, &op.Endpoint, &op.WorkspacePath, &op.StorageIdentity, &op.Phase, &op.Kind); err != nil {
			return err
		}
		if op.InstanceID != want.InstanceID || op.UserID != want.UserID || op.Generation != want.Generation || op.RuntimePodID != want.RuntimePodID || op.GatewayID != want.GatewayID || op.PodUID != want.PodUID || op.Endpoint != want.Endpoint || op.WorkspacePath != want.WorkspacePath || op.StorageIdentity != want.StorageIdentity || op.Kind != want.Kind {
			return models.ErrAutoStopConflict
		}
		if (op.Kind != "idle" && op.Kind != "manual") || (op.Phase != "stopping" && op.Phase != "error" && op.Phase != "teardown_confirmed") || op.RuntimePodID <= 0 || op.PodUID == "" || op.Endpoint == "" {
			return nil
		}
		var bound int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT generation FROM instance_runtime_bindings WHERE instance_id=? FOR UPDATE", op.InstanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&bound); err == nil {
			return nil
		} else if err != sql.ErrNoRows {
			return err
		}
		// Instance locking precedes this first consistent read. Never lock management
		// rows here: recovery commits acquire operation -> instance in that order.
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id,items_json FROM runtime_management_operations WHERE kind='eviction' AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.mode'))='recovery'`)
		if err != nil {
			return err
		}
		proven := false
		for rows.Next() {
			var operation, raw string
			if err = rows.Scan(&operation, &raw); err != nil {
				rows.Close()
				return err
			}
			var items []recoveredStopJournalItem
			if err = json.Unmarshal([]byte(raw), &items); err != nil {
				rows.Close()
				return fmt.Errorf("malformed fault recovery journal: %w", err)
			}
			for _, item := range items {
				if item.InstanceID != op.InstanceID || item.SourceGeneration != op.Generation || item.Recovery == nil || item.Recovery.Phase != "released" {
					continue
				}
				if err = validateRecoveredStopJournal(operation, item, op); err != nil {
					rows.Close()
					return err
				}
				proven = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !proven {
			proven, err = orphanRecoveryStopProof(ctx, tx, op)
			if err != nil {
				return err
			}
		}
		if !proven {
			return nil
		}
		now := time.Now().UTC()
		reason := "idle_timeout"
		if op.Kind == "manual" {
			reason = "manual"
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instance_idle_stop_operations SET phase='completed',error_message='',updated_at=? WHERE id=? AND generation=?`, now, op.ID, op.Generation); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET stop_operation_id='',stop_phase='completed',stopped_reason=IF(stopped_reason='',?,stopped_reason),stopped_at=COALESCE(stopped_at,?),blocker='',updated_at=? WHERE instance_id=? AND generation=? AND stop_operation_id=?`, reason, now, now, op.InstanceID, op.Generation, op.ID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return models.ErrAutoStopConflict
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET stopped_at=COALESCE(stopped_at,?),updated_at=? WHERE id=?`, now, now, op.InstanceID); err != nil {
			return err
		}
		completed = true
		return nil
	}, nil)
	return completed, err
}

type recoveredStopJournalItem struct {
	InstanceID       int    `json:"instance_id"`
	SourceGeneration int    `json:"source_generation"`
	SourcePodID      int64  `json:"source_pod_id"`
	SourcePodUID     string `json:"source_pod_uid"`
	SourceGatewayID  string `json:"source_gateway_id"`
	Recovery         *struct {
		Phase               string                        `json:"phase"`
		ResultState         string                        `json:"result_state"`
		Scope               runtimeeviction.Scope         `json:"scope"`
		Receipt             *runtimeeviction.Receipt      `json:"receipt"`
		SourceStopConfirmed bool                          `json:"source_stop_confirmed"`
		SourceUserID        int                           `json:"source_user_id"`
		SourceEndpoint      string                        `json:"source_endpoint"`
		SourceBinding       models.InstanceRuntimeBinding `json:"source_binding"`
		SourcePod           *corev1.Pod                   `json:"source_pod"`
	} `json:"recovery"`
}

func validateRecoveredStopJournal(operation string, item recoveredStopJournalItem, op models.InstanceIdleStopOperation) error {
	if item.Recovery.ResultState != "stopped" && item.Recovery.ResultState != "deleting" {
		return fmt.Errorf("released recovery continuation is not a stopped source")
	}
	return validateRecoveredSourceJournal(operation, item, op)
}

func validateRecoveredSourceJournal(operation string, item recoveredStopJournalItem, op models.InstanceIdleStopOperation) error {

	j := item.Recovery
	b := j.SourceBinding
	s := j.Scope
	alias := func(gateway string) bool {
		return gateway == fmt.Sprintf("gw-%d-%d", op.InstanceID, op.Generation) || gateway == fmt.Sprintf("pending-%d-%d", op.InstanceID, op.Generation)
	}
	if item.SourcePodID != op.RuntimePodID || item.SourcePodUID != op.PodUID || !alias(item.SourceGatewayID) || j.SourceUserID != op.UserID || j.SourceEndpoint != op.Endpoint || b.InstanceID != op.InstanceID || b.RuntimePodID != op.RuntimePodID || b.Generation != op.Generation || !alias(op.GatewayID) || !alias(b.GatewayID) || b.WorkspacePath != op.WorkspacePath || b.GatewayPort <= 0 {
		return fmt.Errorf("released recovery source differs from native stop")
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if s.OperationID != operation || s.InstanceID != op.InstanceID || s.UserID != op.UserID || s.Generation != op.Generation || s.PodUID != op.PodUID || !alias(s.GatewayID) || s.RuntimeType != b.RuntimeType || path.Join(s.WorkspaceRoot, s.RuntimeType, fmt.Sprintf("user-%d", op.UserID), fmt.Sprintf("instance-%d", op.InstanceID)) != op.WorkspacePath {
		return fmt.Errorf("released recovery scope differs from native stop")
	}
	if j.Receipt != nil {
		if err := runtimeeviction.ValidateReceipt(s, j.Receipt); err != nil {
			return err
		}
	} else if !j.SourceStopConfirmed {
		return fmt.Errorf("released recovery stop authority missing")
	}
	p := j.SourcePod
	if p == nil || string(p.UID) != op.PodUID || p.Namespace != s.Namespace || p.Name != s.PodName || p.Spec.NodeName != s.NodeName {
		return fmt.Errorf("released recovery storage source is missing")
	}
	storage := p.Annotations["clawmanager.io/fault-eviction-storage"]
	digest, err := hex.DecodeString(storage)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != storage {
		return fmt.Errorf("released recovery storage proof is missing")
	}
	var mountName string
	for _, container := range p.Spec.Containers {
		if container.Name != s.ContainerName {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.MountPath != s.WorkspaceRoot {
				continue
			}
			if mountName != "" || mount.ReadOnly || mount.SubPathExpr != "" || path.IsAbs(mount.SubPath) || (mount.SubPath != "" && (path.Clean(mount.SubPath) != mount.SubPath || mount.SubPath == ".." || strings.HasPrefix(mount.SubPath, "../"))) {
				return fmt.Errorf("released recovery workspace mount is ambiguous")
			}
			mountName = mount.Name
		}
	}
	if mountName != "" {
		for _, v := range p.Spec.Volumes {
			if v.Name == mountName && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly && v.PersistentVolumeClaim.ClaimName != "" {
				return nil
			}
		}
	}

	return fmt.Errorf("released recovery workspace PVC proof is missing")
}
