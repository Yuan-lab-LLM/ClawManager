package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/upper/db/v4"
)

// CheckRuntimeOrphanSource runs under the shared publication admission lock.
// Cached used_slots/native reports are deliberately not stop or owner authority.
func CheckRuntimeOrphanSource(ctx context.Context, tx db.Session, podID int64, uid, namespace, deployment string) error {
	var actualUID, actualNamespace, actualDeployment string
	row, e := tx.SQL().QueryRowContext(ctx, "SELECT pod_uid,namespace,deployment_name FROM runtime_pods WHERE id=? FOR UPDATE", podID)
	if e != nil {
		return e
	}
	if e = row.Scan(&actualUID, &actualNamespace, &actualDeployment); e != nil {
		return e
	}
	if uid == "" || actualUID != uid || actualNamespace != namespace || actualDeployment != deployment {
		return fmt.Errorf("orphan SQL source identity changed")
	}
	var bound int
	row, e = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE p.pod_uid=? OR (p.namespace=? AND p.deployment_name=?)`, uid, namespace, deployment)
	if e != nil {
		return e
	}
	if e = row.Scan(&bound); e != nil {
		return e
	}
	if bound > 0 {
		return fmt.Errorf("source controller has a current SQL binding; use instance recovery")
	}
	return nil
}
func FenceRuntimeOrphanSource(ctx context.Context, tx db.Session, podID int64, uid, namespace, deployment, controllerUID string) error {
	if e := CheckRuntimeOrphanSource(ctx, tx, podID, uid, namespace, deployment); e != nil {
		return e
	}
	var existing string
	row, e := tx.SQL().QueryRowContext(ctx, "SELECT deployment_uid FROM runtime_pod_deletion_intents WHERE namespace=? AND deployment_name=?", namespace, deployment)
	if e != nil {
		return e
	}
	e = row.Scan(&existing)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if existing != "" && existing != controllerUID {
		return fmt.Errorf("another controller UID owns the retirement fence")
	}
	if existing == "" {
		if _, e = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES(?,?,?)", namespace, deployment, controllerUID); e != nil {
			return e
		}
	}
	_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=1,updated_at=UTC_TIMESTAMP() WHERE namespace=? AND deployment_name=?", namespace, deployment)
	return e
}
