package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Only the dedicated native controller may acknowledge its own reserved start
// while the normal instance mutation fence remains closed.
func CommitOpenCodeUpgradeGatewayStart(ctx context.Context, sess db.Session, rolloutID int64, instanceID, generation int, workspace, status, gatewayID string, pid *int) error {
	if (status != "creating" && status != "running") || gatewayID != fmt.Sprintf("gw-%d-%d", instanceID, generation) {
		return ErrUpgradeReservationDenied
	}
	return sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_rollouts WHERE id=? FOR UPDATE", rolloutID); err != nil {
			return err
		}
		var r models.RuntimeRollout
		if err := tx.Collection("runtime_rollouts").Find("id", rolloutID).One(&r); err != nil {
			return err
		}
		var plan struct {
			Strategy  string `json:"strategy"`
			Operation string `json:"operation"`
		}
		if r.RuntimeType != "opencode" || r.Status != "running" || (r.Phase != "gateway_restart" && r.Phase != "opencode_rollback_restart") || r.PreflightJSON == nil || json.Unmarshal([]byte(*r.PreflightJSON), &plan) != nil || plan.Strategy != "opencode_native_preserve_v1" || !models.ValidRuntimeOperation(plan.Operation) {
			return ErrUpgradeReservationDenied
		}
		var item models.RuntimeUpgradeItem
		if err := tx.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": r.ID, "instance_id": instanceID}).One(&item); err != nil {
			return err
		}
		expectedGeneration := 0
		expectedState := "restart_ready"
		expectedDigest := stringValueOC(r.TargetImageDigest)
		if item.SourceGeneration != nil {
			expectedGeneration = *item.SourceGeneration + 1
		}
		if r.Phase == "opencode_rollback_restart" {
			scope, e := loadOpenCodeRollbackReservation(ctx, tx, r, instanceID)
			if e != nil {
				return e
			}
			expectedGeneration = scope.RollbackGeneration
			expectedState = "rollback_ready"
			expectedDigest = stringValueOC(scope.SourcePod.ImageDigest)
		}
		if item.State != expectedState || item.SourceGeneration == nil || generation != expectedGeneration || item.PostflightJSON == nil || item.SnapshotRef == nil {
			return ErrUpgradeReservationDenied
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", instanceID); err != nil {
			return err
		}
		var current struct {
			Owner, Generation int
			Status, Workspace string
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,status,COALESCE(workspace_path,'') FROM instances WHERE id=? AND type='opencode' AND instance_mode='lite'`, instanceID)
		if err != nil {
			return err
		}
		if err = row.Scan(&current.Owner, &current.Generation, &current.Status, &current.Workspace); err != nil {
			return err
		}
		if current.Generation != generation || current.Status != "creating" && current.Status != "running" || current.Workspace != "" && current.Workspace != workspace {
			return ErrUpgradeReservationDenied
		}
		var count int
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND owner_user_id=? AND generation=? AND operation_id=? AND state='process_group_confirmed'`, instanceID, current.Owner, *item.SourceGeneration, plan.Operation)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return models.ErrRuntimeUpgradeConflict
		}
		var binding models.InstanceRuntimeBinding
		if err = tx.Collection("instance_runtime_bindings").Find("instance_id", instanceID).One(&binding); err != nil {
			return err
		}
		if binding.Generation != generation || binding.WorkspacePath != workspace || binding.RuntimeType != "opencode" || binding.State != "creating" && binding.State != "running" && binding.State != "error" || binding.GatewayID != gatewayID && binding.GatewayID != fmt.Sprintf("pending-%d-%d", instanceID, generation) {
			return ErrUpgradeReservationDenied
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_pods WHERE id=? AND runtime_type='opencode' AND pod_uid=? AND image_digest=?`, binding.RuntimePodID, stringValueOC(item.RestartTargetPodUID), expectedDigest)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrUpgradeReservationDenied
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instance_runtime_bindings SET gateway_id=?,gateway_pid=?,state=?,last_health_at=IF(?='running',UTC_TIMESTAMP(6),last_health_at),error_message=NULL,updated_at=UTC_TIMESTAMP(6) WHERE instance_id=? AND generation=?`, gatewayID, pid, status, status, instanceID, generation); err != nil {
			return err
		}
		if err = checkAutoStopRuntimeState(ctx, tx, instanceID, status, generation); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET status=?,workspace_path=?,runtime_error_message=NULL,runtime_error_code=NULL,started_at=IF(?='running',COALESCE(started_at,?),started_at),updated_at=? WHERE id=? AND runtime_generation=?`, status, workspace, status, time.Now().UTC(), time.Now().UTC(), instanceID, generation)
		return err
	}, nil)
}

// Commit a confirmed source C1 receipt and release only its exact binding in
// one transaction. Ordinary binding release remains forbidden by the fence.
func ReleaseOpenCodeUpgradeSource(ctx context.Context, sess db.Session, rolloutID int64, operation string, instanceID, owner, generation int, pod models.RuntimePod, gateway string, proof json.RawMessage) error {
	var receipt struct {
		PodUID      string    `json:"pod_uid"`
		InstanceID  int       `json:"instance_id"`
		UserID      int       `json:"user_id"`
		Generation  int       `json:"generation"`
		GatewayID   string    `json:"gateway_id"`
		ContainerID string    `json:"container_id"`
		FinishedAt  time.Time `json:"finished_at"`
	}
	if !models.ValidRuntimeOperation(operation) || json.Unmarshal(proof, &receipt) != nil || receipt.PodUID != stringValueOC(pod.PodUID) || receipt.InstanceID != instanceID || receipt.UserID != owner || receipt.Generation != generation || receipt.GatewayID != gateway || receipt.ContainerID == "" || receipt.FinishedAt.IsZero() {
		return ErrUpgradeReservationDenied
	}
	return sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_rollouts WHERE id=? FOR UPDATE", rolloutID); err != nil {
			return err
		}
		var r models.RuntimeRollout
		if err := tx.Collection("runtime_rollouts").Find("id", rolloutID).One(&r); err != nil {
			return err
		}
		var plan struct {
			Strategy  string `json:"strategy"`
			Operation string `json:"operation"`
		}
		if r.RuntimeType != "opencode" || r.Status != "running" || r.PreflightJSON == nil || json.Unmarshal([]byte(*r.PreflightJSON), &plan) != nil || plan.Strategy != "opencode_native_preserve_v1" || plan.Operation != operation {
			return ErrUpgradeReservationDenied
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_upgrade_items WHERE rollout_id=? AND instance_id=? FOR UPDATE", rolloutID, instanceID); err != nil {
			return err
		}
		var item models.RuntimeUpgradeItem
		if err := tx.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": rolloutID, "instance_id": instanceID}).One(&item); err != nil {
			return err
		}
		if item.SourceGeneration == nil || *item.SourceGeneration != generation || item.RuntimePodID == nil || *item.RuntimePodID != pod.ID || stringValueOC(item.SourcePodUID) != receipt.PodUID || stringValueOC(item.SourceGatewayID) != gateway || item.State != "pending" && item.State != "quiesced" {
			return ErrUpgradeReservationDenied
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", instanceID); err != nil {
			return err
		}
		var count int
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances i JOIN gateway_runtime_upgrades u ON u.instance_id=i.id WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND i.type='opencode' AND i.status IN ('running','stopped') AND u.operation_id=? AND u.state IN ('reserved','process_group_confirmed')`, instanceID, owner, generation, operation)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return models.ErrRuntimeUpgradeConflict
		}
		if err = requireNoPlatformWriters(ctx, tx, instanceID); err != nil {
			return err
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM runtime_pods WHERE id=? FOR UPDATE", pod.ID); err != nil {
			return err
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_pods WHERE id=? AND pod_uid=? AND namespace=? AND deployment_name=? AND runtime_type='opencode'`, pod.ID, receipt.PodUID, pod.Namespace, pod.DeploymentName)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return models.ErrRuntimeUpgradeConflict
		}
		var binding models.InstanceRuntimeBinding
		err = tx.Collection("instance_runtime_bindings").Find("instance_id", instanceID).One(&binding)
		if err != nil && err != db.ErrNoMoreRows {
			return err
		}
		if err == nil {
			if binding.RuntimePodID != pod.ID || binding.Generation != generation || binding.GatewayID != gateway || binding.State != "running" {
				return models.ErrRuntimeUpgradeConflict
			}
			if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE instance_id=? AND runtime_pod_id=? AND generation=? AND gateway_id=?`, instanceID, pod.ID, generation, gateway); err != nil {
				return err
			}
			if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(0,used_slots-1),updated_at=UTC_TIMESTAMP(6) WHERE id=?`, pod.ID); err != nil {
				return err
			}
		}
		if item.State == "quiesced" && stringValueOC(item.PreflightJSON) != string(proof) {
			return models.ErrRuntimeUpgradeConflict
		}
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='quiesced',preflight_json=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, string(proof), item.ID); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state='process_group_confirmed',updated_at=UTC_TIMESTAMP(6) WHERE instance_id=? AND operation_id=? AND state IN ('reserved','process_group_confirmed')`, instanceID, operation)
		return err
	}, nil)
}
func stringValueOC(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

type openCodeRollbackReservation struct {
	ID                     int               `json:"id"`
	Generation             int               `json:"generation"`
	SourcePod              models.RuntimePod `json:"source_pod"`
	RollbackGeneration     int               `json:"rollback_generation"`
	RollbackFromGeneration int               `json:"rollback_from_generation"`
	RollbackState          string            `json:"rollback_state"`
	TargetStopProof        json.RawMessage   `json:"target_stop_proof"`
	Restore                *struct {
		RolloutID              string    `json:"rollout_id"`
		Generation             int       `json:"generation"`
		OutputSHA256           string    `json:"output_sha256"`
		CompletedAt            time.Time `json:"completed_at"`
		InstanceID             int       `json:"instance_id"`
		Status                 string    `json:"status"`
		ConfigRestored         bool      `json:"config_restored"`
		StateRestored          bool      `json:"state_restored"`
		PreservedSessionSQLite bool      `json:"preserved_session_sqlite"`
	} `json:"restore"`
}

func loadOpenCodeRollbackReservation(ctx context.Context, tx db.Session, r models.RuntimeRollout, id int) (*openCodeRollbackReservation, error) {
	if r.RuntimeType != "opencode" || r.Status != "running" || r.Phase != "opencode_rollback_restart" {
		return nil, ErrUpgradeReservationDenied
	}
	var raw string
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT state_json FROM runtime_rollout_checkpoints WHERE rollout_id=?`, r.ID)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&raw); err != nil {
		return nil, err
	}
	var p struct {
		Kind      string                        `json:"kind"`
		Strategy  string                        `json:"strategy"`
		Stage     string                        `json:"stage"`
		Instances []openCodeRollbackReservation `json:"instances"`
	}
	if json.Unmarshal([]byte(raw), &p) != nil || p.Kind != "opencode-native-preserve" || p.Strategy != "opencode_native_preserve_v1" || p.Stage != "rollback_restart" {
		return nil, ErrUpgradeReservationDenied
	}
	for _, entry := range p.Instances {
		if entry.ID != id {
			continue
		}
		if entry.RollbackState != "restored" && entry.RollbackState != "restart_ready" || entry.RollbackGeneration <= entry.Generation || entry.RollbackFromGeneration != entry.Generation && entry.RollbackFromGeneration != entry.Generation+1 || entry.RollbackFromGeneration == entry.Generation+1 && len(entry.TargetStopProof) == 0 || entry.Restore == nil || entry.Restore.RolloutID != fmt.Sprintf("%d", r.ID) || entry.Restore.Generation != entry.RollbackFromGeneration || entry.Restore.CompletedAt.IsZero() || len(entry.Restore.OutputSHA256) != 64 || entry.Restore.InstanceID != id || entry.Restore.Status != "restored" || !entry.Restore.ConfigRestored || !entry.Restore.StateRestored || !entry.Restore.PreservedSessionSQLite {
			return nil, ErrUpgradeReservationDenied
		}
		return &entry, nil
	}
	return nil, ErrUpgradeReservationDenied
}

// Rollback target release is restricted to the same rollout's immutable target
// Pod, successor generation and its fresh C1 receipt. Source proof stays intact.
func ReleaseOpenCodeUpgradeRollbackTarget(ctx context.Context, sess db.Session, rolloutID int64, operation string, id, owner, generation int, pod models.RuntimePod, gateway string, proof json.RawMessage) error {
	var receipt struct {
		PodUID      string    `json:"pod_uid"`
		InstanceID  int       `json:"instance_id"`
		UserID      int       `json:"user_id"`
		Generation  int       `json:"generation"`
		GatewayID   string    `json:"gateway_id"`
		ContainerID string    `json:"container_id"`
		FinishedAt  time.Time `json:"finished_at"`
	}
	if json.Unmarshal(proof, &receipt) != nil || receipt.PodUID != stringValueOC(pod.PodUID) || receipt.InstanceID != id || receipt.UserID != owner || receipt.Generation != generation || receipt.GatewayID != gateway || receipt.ContainerID == "" || receipt.FinishedAt.IsZero() {
		return ErrUpgradeReservationDenied
	}
	return sess.TxContext(ctx, func(tx db.Session) error {
		var r models.RuntimeRollout
		if err := tx.Collection("runtime_rollouts").Find("id", rolloutID).One(&r); err != nil {
			return err
		}
		if r.RuntimeType != "opencode" || r.Status != "running" || r.Phase != "opencode_rollback_stop" || r.TargetImageDigest == nil || *r.TargetImageDigest != stringValueOC(pod.ImageDigest) {
			return ErrUpgradeReservationDenied
		}
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", id); err != nil {
			return err
		}
		var count int
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances i JOIN gateway_runtime_upgrades u ON u.instance_id=i.id JOIN runtime_upgrade_items item ON item.instance_id=i.id AND item.rollout_id=? WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND i.type='opencode' AND u.operation_id=? AND u.state='process_group_confirmed' AND item.source_generation+1=?`, rolloutID, id, owner, generation, operation, generation)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return models.ErrRuntimeUpgradeConflict
		}
		var sources map[string]string
		if r.SourceImagesJSON == nil || json.Unmarshal([]byte(*r.SourceImagesJSON), &sources) != nil {
			return ErrUpgradeReservationDenied
		}
		owned := false
		for key := range sources {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) == 2 && parts[0] == pod.Namespace && upgradeTargetDeploymentName(parts[1], r.ID) == pod.DeploymentName {
				owned = true
			}
		}
		if !owned {
			return ErrUpgradeReservationDenied
		}
		if err = requireNoPlatformWriters(ctx, tx, id); err != nil {
			return err
		}
		var binding models.InstanceRuntimeBinding
		err = tx.Collection("instance_runtime_bindings").Find("instance_id", id).One(&binding)
		if err != nil && err != db.ErrNoMoreRows {
			return err
		}
		if err == nil {
			if binding.RuntimePodID != pod.ID || binding.Generation != generation || binding.GatewayID != gateway && binding.GatewayID != fmt.Sprintf("pending-%d-%d", id, generation) {
				return models.ErrRuntimeUpgradeConflict
			}
			if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE id=?`, binding.ID); err != nil {
				return err
			}
			if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=GREATEST(0,used_slots-1),updated_at=UTC_TIMESTAMP(6) WHERE id=?`, pod.ID); err != nil {
				return err
			}
		}
		detail, _ := json.Marshal(map[string]any{"instance_id": id, "generation": generation, "pod_uid": receipt.PodUID, "container_id": receipt.ContainerID, "c1": json.RawMessage(proof)})
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO runtime_upgrade_audits(rollout_id,action,phase,outcome,detail_json,created_at) VALUES(?,'opencode_rollback_target_stopped','opencode_rollback_stop','confirmed',?,UTC_TIMESTAMP(6))`, rolloutID, string(detail))
		return err
	}, nil)
}

// Only OpenCode advances serialize here. Keep the connection-scoped mutex
// outside transactions, as lifecycle does; every state transition still CASes
// its durable checkpoint. Losing a coordinator connection releases ownership.
func LockOpenCodeUpgradeController(ctx context.Context, sess db.Session) (func(), error) {
	database, ok := sess.Driver().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("OpenCode controller requires SQL advisory lock support")
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(sess.Name() + ":opencode-native-preserve"))
	name := fmt.Sprintf("oc-upgrade:%x", digest[:24])
	var acquired sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&acquired); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return nil, ErrUpgradeCapacityPending
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}, nil
}
