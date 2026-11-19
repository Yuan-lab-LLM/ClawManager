package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/upper/db/v4"
)

func RuntimeScaleConfigurationApproved(ctx context.Context, sess db.Session, runtimeType, namespace, name, uid, digest string) (bool, error) {
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT source_pod_uid FROM runtime_scale_approvals
 WHERE configuration_digest=? AND runtime_type=? AND namespace=? AND deployment_name=? AND deployment_uid=?`, digest, runtimeType, namespace, name, uid)
	if err != nil {
		return false, err
	}
	var sourceUID string
	if err := row.Scan(&sourceUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return sourceUID != "", nil
}

func RecordRuntimeScaleApproval(ctx context.Context, sess db.Session, runtimeType, namespace, name, uid, digest, sourceUID string, configuration []byte) error {
	_, err := sess.SQL().ExecContext(ctx, `INSERT INTO runtime_scale_approvals
 (configuration_digest,runtime_type,namespace,deployment_name,deployment_uid,source_pod_uid,configuration_json)
 VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE configuration_digest=configuration_digest`, digest, runtimeType, namespace, name, uid, sourceUID, string(configuration))
	return err
}

// RuntimeScaleStartupBaseline reads only the membership associated with the
// controller's current attempt. A missing journal cannot prove startup.
func RuntimeScaleStartupBaseline(ctx context.Context, sess db.Session, deploymentUID, attemptID string) ([]string, bool, error) {
	if attemptID == "" {
		return nil, false, nil
	}
	row, err := sess.SQL().QueryRowContext(ctx, "SELECT pod_uids FROM runtime_scale_startup_baselines WHERE deployment_uid=? AND attempt_id=?", deploymentUID, attemptID)
	if err != nil {
		return nil, false, err
	}
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var uids []string
	if err := json.Unmarshal([]byte(raw), &uids); err != nil {
		return nil, false, err
	}
	return uids, true, nil
}

// Keep the current attempt and its replacement until the next admission
// observes which patch committed. An uncertain Kubernetes response does not
// erase either membership.
func RecordRuntimeScaleStartupBaseline(ctx context.Context, sess db.Session, deploymentUID, prior, attemptID string, uids []string) error {
	raw, err := json.Marshal(uids)
	if err != nil {
		return err
	}
	if _, err := sess.SQL().ExecContext(ctx, "DELETE FROM runtime_scale_startup_baselines WHERE deployment_uid=? AND attempt_id<>?", deploymentUID, prior); err != nil {
		return err
	}
	_, err = sess.SQL().ExecContext(ctx, "INSERT INTO runtime_scale_startup_baselines (deployment_uid,attempt_id,pod_uids) VALUES (?,?,?)", deploymentUID, attemptID, string(raw))
	return err
}
