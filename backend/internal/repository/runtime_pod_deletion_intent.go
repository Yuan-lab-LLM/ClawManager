package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
)

// A controller-name tombstone also fences Pods that register after deletion
// admission. Keep it after an ambiguous Kubernetes response and after success:
// name reuse requires a separate, confirmed controller cleanup lifecycle.
func RuntimePodDeletionIntentUID(ctx context.Context, sess db.Session, namespace, name string) (string, error) {
	var uid string
	row, err := sess.SQL().QueryRowContext(ctx, "SELECT deployment_uid FROM runtime_pod_deletion_intents WHERE namespace=? AND deployment_name=?", namespace, name)
	if err == nil {
		err = row.Scan(&uid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return uid, err
}
func CheckRuntimePodDeletionIntent(ctx context.Context, sess db.Session, namespace, name string) error {
	uid, err := RuntimePodDeletionIntentUID(ctx, sess, namespace, name)
	if err != nil {
		return err
	}
	if uid == "" {
		return nil
	}
	return fmt.Errorf("Runtime pool deletion is fenced; retry deletion instead of resuming or reusing its name")
}

// Caller holds LockRuntimeManagementAdmission. Occupancy and bindings are
// checked in this same transaction before any intent or pause can persist.
func FenceRuntimePodDeletion(ctx context.Context, tx db.Session, namespace, name, uid string) error {
	if namespace == "" || name == "" || uid == "" {
		return fmt.Errorf("verified Runtime controller identity required")
	}
	var count int
	for _, query := range []string{
		"SELECT COUNT(*) FROM runtime_pods WHERE namespace=? AND deployment_name=? AND used_slots<>0",
		"SELECT COUNT(*) FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE p.namespace=? AND p.deployment_name=?",
	} {
		row, err := tx.SQL().QueryRowContext(ctx, query, namespace, name)
		if err != nil {
			return err
		}
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("Runtime pool has occupied slots, admission claims or bindings")
		}
	}
	var existing string
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT deployment_uid FROM runtime_pod_deletion_intents WHERE namespace=? AND deployment_name=?", namespace, name)
	if err == nil {
		err = row.Scan(&existing)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existing != "" && existing != uid {
		return fmt.Errorf("Runtime pool name is fenced for a different controller UID")
	}
	if existing == "" {
		if _, err = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES (?,?,?)", namespace, name, uid); err != nil {
			return err
		}
	}
	_, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=1,updated_at=UTC_TIMESTAMP() WHERE namespace=? AND deployment_name=?", namespace, name)
	return err
}

// Serializes publication with deletion admission. Every real publication needs
// its existing SQL generation binding (the durable reservation); an unregistered Pod cannot
// publish a writer while a pending controller is being removed.
func WithRuntimePodAssignmentAdmission(ctx context.Context, sess db.Session, actual models.RuntimePod, instanceID, generation int, publish func() error) error {
	if actual.PodUID == nil || *actual.PodUID == "" || actual.Namespace == "" || actual.PodName == "" || actual.DeploymentName == "" || actual.RuntimeType == "" {
		return fmt.Errorf("verified native controller identity required")
	}
	return sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := CheckRuntimePodDeletionIntent(ctx, tx, actual.Namespace, actual.DeploymentName); err != nil {
			return err
		}
		var copies int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_pods WHERE pod_uid=?", *actual.PodUID)
		if err == nil {
			err = row.Scan(&copies)
		}
		if err != nil {
			return err
		}
		if copies != 1 {
			return fmt.Errorf("native Pod UID must have one unambiguous SQL identity")
		}
		var id int64
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT id FROM runtime_pods WHERE pod_uid=? AND namespace=? AND pod_name=? AND deployment_name=? AND runtime_type=?", *actual.PodUID, actual.Namespace, actual.PodName, actual.DeploymentName, actual.RuntimeType)
		if err == nil {
			err = row.Scan(&id)
		}
		if err != nil {
			return fmt.Errorf("Runtime SQL identity differs from verified Kubernetes controller: %w", err)
		}
		if err := CheckRuntimePodScaleInAdmission(ctx, tx, id); err != nil {
			return err
		}
		var bound int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=? AND instance_id=? AND generation=?", id, instanceID, generation)
		if err == nil {
			err = row.Scan(&bound)
		}
		if err != nil {
			return err
		}
		if bound != 1 {
			return fmt.Errorf("Runtime assignment publication requires its current durable generation binding")
		}
		return publish()
	}, nil)
}

// Caller holds the global admission row before any binding reservation/replay.
func CheckRuntimePodBindingAdmission(ctx context.Context, tx db.Session, podID int64) error {
	var namespace, name string
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT namespace,deployment_name FROM runtime_pods WHERE id=?", podID)
	if err == nil {
		err = row.Scan(&namespace, &name)
	}
	if err != nil {
		return err
	}
	if err := CheckRuntimePodScaleInAdmission(ctx, tx, podID); err != nil {
		return err
	}
	return CheckRuntimePodDeletionIntent(ctx, tx, namespace, name)
}
