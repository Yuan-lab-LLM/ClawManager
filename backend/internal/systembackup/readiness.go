package systembackup

import (
	"context"
	"database/sql"
	"errors"
)

var RequiredDependencies = []string{"controller", "schema_registry", "writer_registry", "runtime_quiesce", "mysql_checkpoint", "redis_checkpoint", "object_checkpoint", "workspace_checkpoint", "deployment_inventory", "secret_capture", "external_target", "encryption_key", "signing_key", "catalog", "isolation", "permissions", "capacity"}

type Readiness struct {
	ContractVersion    string  `json:"contract_version"`
	Enabled            bool    `json:"enabled"`
	CanCreate          bool    `json:"can_create"`
	Checks             []Check `json:"checks"`
	RecoveryPointCount int     `json:"recovery_point_count"`
	ConfigVersion      uint64  `json:"config_version"`
}

func readiness(ctx context.Context, q SQLQueryer, c ConfigVersion) (Readiness, error) {
	out := Readiness{ContractVersion: ContractVersion, Enabled: c.Config.Enabled, ConfigVersion: c.Version, Checks: []Check{}, CanCreate: c.Config.Enabled}
	for _, dep := range RequiredDependencies {
		var state, reason string
		var fresh bool
		e := q.QueryRowContext(ctx, "SELECT status,reason_code,expires_at>UTC_TIMESTAMP(6) FROM system_backup_dependency_health WHERE config_version=? AND dependency=?", c.Version, dep).Scan(&state, &reason, &fresh)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if errors.Is(e, sql.ErrNoRows) {
			state = "unknown"
			reason = "not_checked"
		} else if !fresh {
			state = "stale"
			reason = "check_expired"
		}
		passed := state == "healthy" && fresh
		out.Checks = append(out.Checks, Check{Name: dep, Passed: passed, Code: reason, Detail: state})
		if !passed {
			out.CanCreate = false
		}
	}
	if !c.Config.Enabled {
		out.Checks = append(out.Checks, Check{Name: "feature", Code: "disabled", Detail: "disabled"})
	}
	if e := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_tasks WHERE task_type='backup' AND status='succeeded' AND acceptance_eligible=1 AND artifact_registered=1").Scan(&out.RecoveryPointCount); e != nil {
		return out, e
	}
	return out, nil
}
func (s Store) Readiness(ctx context.Context) (Readiness, error) {
	c, e := s.Config(ctx)
	if e != nil {
		return Readiness{}, e
	}
	return readiness(ctx, s.DB, c)
}
func AdmitReady(ctx context.Context, tx *sql.Tx, c ConfigVersion, r CreateTaskRequest) error {
	health, e := readiness(ctx, tx, c)
	if e != nil {
		return e
	}
	if !health.CanCreate {
		return errors.New("not_ready")
	}
	// Functional tests never bypass missing capture/isolation prerequisites.
	if r.Type != Backup {
		var eligible bool
		if e = tx.QueryRowContext(ctx, "SELECT acceptance_eligible FROM system_backup_tasks WHERE public_id=? AND task_type='backup' AND status='succeeded' AND artifact_registered=1", r.SourceID).Scan(&eligible); e != nil {
			return errors.New("artifact_unavailable")
		}
		if r.Purpose == "acceptance" && !eligible {
			return errors.New("validation_failed")
		}
	}
	return nil
}

// RecordHealth is a controller-only capability. API code has no route for
// asserting health, so checking a UI box cannot create acceptance evidence.
func (s Store) RecordHealth(ctx context.Context, version uint64, dependency, status, reason, hash string, validSeconds int) error {
	known := false
	for _, d := range RequiredDependencies {
		if d == dependency {
			known = true
		}
	}
	if !known || validSeconds < 1 || validSeconds > 3600 || len(hash) != 64 || (status != "healthy" && status != "degraded" && status != "unknown") || (status == "healthy") != (reason == "none") {
		return errors.New("validation_failed")
	}
	_, e := s.DB.ExecContext(ctx, `INSERT INTO system_backup_dependency_health(config_version,dependency,status,reason_code,evidence_hash,checked_at,expires_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6),TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6))) ON DUPLICATE KEY UPDATE status=VALUES(status),reason_code=VALUES(reason_code),evidence_hash=VALUES(evidence_hash),checked_at=VALUES(checked_at),expires_at=VALUES(expires_at),row_version=row_version+1`, version, dependency, status, reason, hash, validSeconds)
	return e
}
