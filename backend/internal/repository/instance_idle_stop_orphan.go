package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"github.com/upper/db/v4"
)

// Called only inside the existing instance/activity/stop/binding transaction.
// sources_retired means the whole-Pod receipt was verified and every pinned
// publication was retired; the helper finishing alone is never sufficient.
func orphanRecoveryStopProof(ctx context.Context, tx db.Session, op models.InstanceIdleStopOperation) (bool, error) {
	rows, err := tx.SQL().QueryContext(ctx, "SELECT id,items_json FROM runtime_management_operations WHERE kind='orphan_recovery'")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	proven := false
	for rows.Next() {
		var operation, raw string
		if err = rows.Scan(&operation, &raw); err != nil {
			return false, err
		}
		var items []struct {
			Orphan *nativeStopOrphanJournal `json:"orphan_recovery"`
		}
		if err = json.Unmarshal([]byte(raw), &items); err != nil {
			return false, fmt.Errorf("malformed orphan stop journal: %w", err)
		}
		for _, item := range items {
			j := item.Orphan
			if j == nil || (j.Phase != "sources_retired" && j.Phase != "completed") {
				continue
			}
			matches := 0
			for _, publication := range j.Publications {
				a := publication.Audit
				if a.InstanceID != op.InstanceID || a.Generation != op.Generation {
					continue
				}
				matches++
				if matches > 1 {
					return false, fmt.Errorf("orphan stop journal contains duplicate source generations")
				}
				if err = runtimeeviction.ValidatePodReceipt(j.Scope, j.Receipt); err != nil {
					return false, err
				}
				if j.Source == nil || j.Source.scope(operation) != j.Scope {
					return false, fmt.Errorf("orphan stop source differs from verified Pod scope")
				}
				if !a.Stopped || a.UserID != op.UserID || a.PodUID != op.PodUID || a.PodUID != j.Scope.PodUID || a.Endpoint != op.Endpoint || (op.GatewayID != fmt.Sprintf("gw-%d-%d", op.InstanceID, op.Generation) && op.GatewayID != fmt.Sprintf("pending-%d-%d", op.InstanceID, op.Generation)) || a.GatewayID != fmt.Sprintf("gw-%d-%d", op.InstanceID, op.Generation) || a.RuntimeType != j.Scope.RuntimeType || a.WorkspacePath != op.WorkspacePath || a.WorkspacePath != path.Join(j.Scope.WorkspaceRoot, a.RuntimeType, fmt.Sprintf("user-%d", op.UserID), fmt.Sprintf("instance-%d", op.InstanceID)) || publication.SecretUID == "" || publication.SecretVersion == "" {
					return false, fmt.Errorf("retired orphan publication differs from native stop source")
				}
				proven = true
			}
		}
	}
	return proven, rows.Err()
}

type nativeStopOrphanJournal struct {
	Phase        string                      `json:"phase"`
	Scope        runtimeeviction.PodScope    `json:"scope"`
	Receipt      *runtimeeviction.PodReceipt `json:"receipt"`
	Source       *nativeStopOrphanSource     `json:"source"`
	Publications []struct {
		Audit struct {
			InstanceID    int    `json:"instance_id"`
			UserID        int    `json:"user_id"`
			Generation    int    `json:"generation"`
			RuntimeType   string `json:"runtime_type"`
			PodUID        string `json:"pod_uid"`
			Endpoint      string `json:"endpoint"`
			GatewayID     string `json:"gateway_id"`
			WorkspacePath string `json:"workspace_path"`
			Stopped       bool   `json:"stopped"`
		} `json:"audit"`
		SecretUID     string `json:"secret_uid"`
		SecretVersion string `json:"secret_version"`
	} `json:"publications"`
}

type nativeStopOrphanSource struct {
	Namespace               string `json:"namespace"`
	PodName                 string `json:"pod_name"`
	PodUID                  string `json:"pod_uid"`
	NodeName                string `json:"node_name"`
	NodeUID                 string `json:"node_uid"`
	NodeBootID              string `json:"node_boot_id"`
	RuntimeType             string `json:"runtime_type"`
	WorkspaceRoot           string `json:"workspace_root"`
	DeploymentUID           string `json:"deployment_uid"`
	StorageDigest           string `json:"storage_digest"`
	TerminalResourceVersion string `json:"terminal_resource_version"`
}

func (s nativeStopOrphanSource) scope(operation string) runtimeeviction.PodScope {
	return runtimeeviction.PodScope{Version: 1, OperationID: operation, Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, NodeName: s.NodeName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, RuntimeType: s.RuntimeType, WorkspaceRoot: s.WorkspaceRoot, ControllerUID: s.DeploymentUID, StorageDigest: s.StorageDigest, TerminalResourceVersion: s.TerminalResourceVersion}
}
