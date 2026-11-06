package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"clawreef/internal/models"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
)

// Older database fixtures may omit the additive metadata table. Production
// Gateway admission and the controller require it and never use this fallback.
func missingAutoStopTable(err error) bool {
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1146
}
func checkInstanceAutoStopMutation(ctx context.Context, s db.Session, id int) error {

	row, err := s.SQL().QueryRowContext(ctx, `SELECT instance_mode,status FROM instances WHERE id=?`, id)
	if err != nil {
		return err
	}
	var mode, status string
	if err = row.Scan(&mode, &status); err != nil {
		return err
	}
	if mode != "lite" {
		return nil
	}
	if status != "running" && status != "creating" {
		return models.ErrInstanceStopping
	}
	row, err = s.SQL().QueryRowContext(ctx, `SELECT stop_operation_id,start_deadline_at FROM instance_model_activity WHERE instance_id=?`, id)
	if err != nil {
		return err
	}
	var operation string
	var deadline sql.NullTime
	if err = row.Scan(&operation, &deadline); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if operation != "" || (deadline.Valid && !deadline.Time.After(time.Now().UTC())) {
		return models.ErrInstanceStopping
	}
	return nil
}

func checkAutoStopRuntimeState(ctx context.Context, s db.Session, id int, target string, generation int) error {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT i.status,i.runtime_generation,COALESCE(a.stop_operation_id,''),COALESCE(a.stopped_reason,''),a.start_deadline_at FROM instances i LEFT JOIN instance_model_activity a ON a.instance_id=i.id WHERE i.id=? AND i.instance_mode='lite'`, id)
	if missingAutoStopTable(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var current, operation, reason string
	var gen int
	var deadline sql.NullTime
	err = row.Scan(&current, &gen, &operation, &reason, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if generation < gen {
		return ErrStaleRuntimeGeneration
	}
	if target == "stopped" || target == "deleting" {
		return nil
	}
	if operation != "" {
		return models.ErrInstanceStopping
	}
	if current == "stopped" && (!deadline.Valid || target != "creating" || generation <= gen) {
		return models.ErrInstanceStopping
	}
	if deadline.Valid && !deadline.Time.After(time.Now().UTC()) && (target == "creating" || target == "running" || target == "error") {
		return models.ErrInstanceStopping
	}
	return nil
}

// The caller holds the instance row lock. Readiness and the complete idle
// window are committed atomically, and only the current generation can finish Start.
func confirmAutoStopRunning(ctx context.Context, s db.Session, id, generation int) error {
	now := time.Now().UTC()
	_, err := s.SQL().ExecContext(ctx, `UPDATE instance_model_activity a JOIN instances i ON i.id=a.instance_id
 SET a.generation=?,a.monitoring_since=?,a.activity_version=a.activity_version+1,
 a.start_deadline_at=NULL,a.stopped_reason='',a.stop_phase='',a.blocker='',a.updated_at=?,i.started_at=?
 WHERE i.id=? AND i.instance_mode='lite' AND i.status='running' AND i.runtime_generation=?
 AND a.stop_operation_id='' AND a.start_deadline_at IS NOT NULL`, generation, now, now, now, id, generation)
	if missingAutoStopTable(err) {
		return nil
	}
	return err
}
