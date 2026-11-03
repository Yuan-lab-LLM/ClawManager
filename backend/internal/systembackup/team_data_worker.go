package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Run uses a connection-owned mutex only for the backup worker. It does not
// lock business rows, stop source tasks or enable the strong backup gate.
func (s *TeamDataService) Run(ctx context.Context) {
	s.wg.Add(1)
	defer s.wg.Done()
	for ctx.Err() == nil {
		if e := s.runOwned(ctx); e != nil && ctx.Err() == nil {
			log.Printf("Team data backup worker: %s", PublicError(e))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *TeamDataService) runOwned(ctx context.Context) error {
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	var acquired sql.NullInt64
	installationHash, hashErr := RequestHash(map[string]string{"installation_id": s.cfg.InstallationID})
	if hashErr != nil {
		return hashErr
	}
	name := "team-data-" + installationHash[:40]
	if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&acquired); e != nil {
		return e
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, "DO RELEASE_LOCK(?)", name)
	}()
	// An old process never causes a second capture/restore of the same job.
	// Keep partial targets and immutable parts for inspection and explicit cleanup.
	if _, e = s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='failed',phase='interrupted',error_code='process_interrupted',finished_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6),owner_id=NULL WHERE status IN ('running','canceling') AND (owner_id IS NULL OR owner_id<>?)", s.owner); e != nil {
		return e
	}
	for ctx.Err() == nil {
		if e = conn.PingContext(ctx); e != nil {
			return e
		}
		j, e := scanTeamDataJob(s.db.QueryRowContext(ctx, "SELECT "+teamDataColumns+" FROM system_team_data_backup_jobs WHERE status='pending' ORDER BY created_at LIMIT 1"))
		if e != nil && e.Error() != "not_found" {
			return e
		}
		if e == nil {
			r, e := s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='running',owner_id=?,started_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status='pending'", s.owner, j.ID)
			if e != nil {
				return e
			}
			n, _ := r.RowsAffected()
			if n == 1 {
				s.execute(ctx, conn, j)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return ctx.Err()
}

func (s *TeamDataService) execute(parent context.Context, lock *sql.Conn, j TeamDataJob) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(s.cfg.DeadlineSeconds)*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe, stop := context.WithTimeout(ctx, 5*time.Second)
				var canceled bool
				var owner, status string
				e := s.db.QueryRowContext(probe, "SELECT cancel_requested,COALESCE(owner_id,''),status FROM system_team_data_backup_jobs WHERE public_id=?", j.ID).Scan(&canceled, &owner, &status)
				if e == nil {
					e = lock.PingContext(probe)
				}
				stop()
				if e != nil || canceled || owner != s.owner || status != "running" {
					cancel()
					return
				}
			}
		}
	}()
	var report TeamDataReport
	var err error
	if !publicIDPattern.MatchString(j.ID) || len(j.ID) != 36 {
		err = errors.New("validation_failed")
	} else if j.ConfigHash != s.configHash {
		err = errors.New("configuration_changed")
	} else if s.cfg.Disabled && j.Operation != "cleanup" {
		err = errors.New("feature_disabled")
	} else {
		switch j.Operation {
		case "backup":
			report, err = s.backup(ctx, j)
		case "restore":
			report, err = s.restore(ctx, j)
		case "cleanup":
			err = s.cleanup(ctx, j.SourceID)
		case "verify":
			report, err = s.verifyBackup(ctx, j)
		case "delete":
			var actor int
			err = s.db.QueryRowContext(ctx, "SELECT actor_id FROM system_team_data_backup_jobs WHERE public_id=?", j.ID).Scan(&actor)
			if err == nil {
				err = s.catalog().ContinueDelete(ctx, j.SourceID, actor, func(_ int) error { return s.phase(ctx, j, "deleting_versions") })
			}
		default:
			err = errors.New("validation_failed")
		}
	}
	close(stopped)
	cancel()
	<-watchDone
	finish, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	actual, e := s.Get(finish, j.ID)
	if e != nil || actual.Owner != s.owner {
		return
	}
	status, phase, code := "succeeded", "complete", ""
	if err != nil {
		status, phase, code = "failed", actual.Phase, teamDataFailure(err)
		if errors.Is(err, context.DeadlineExceeded) {
			code = "deadline_exceeded"
		}
		if actual.CancelRequested {
			status, code = "canceled", "canceled_by_actor"
		}
		log.Printf("Team data operation failed: id=%s phase=%s code=%s", j.ID, actual.Phase, code)
		// These errors contain registered table/column names, never cell values.
		if strings.HasPrefix(err.Error(), "value mismatch: ") || strings.HasPrefix(err.Error(), "row count mismatch: ") {
			log.Printf("Team data SQL readback: id=%s detail=%s", j.ID, err.Error())
		}
	}
	raw, _ := json.Marshal(report)
	_, _ = s.db.ExecContext(finish, "UPDATE system_team_data_backup_jobs SET status=?,phase=?,error_code=?,report_json=?,finished_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND owner_id=? AND status IN ('running','canceling')", status, phase, code, raw, j.ID, s.owner)
}

func teamDataFailure(err error) string {
	if err == nil {
		return ""
	}
	var sqlError *mysql.MySQLError
	if errors.As(err, &sqlError) {
		return fmt.Sprintf("database_error_%d", sqlError.Number)
	}
	if apierrors.IsForbidden(err) {
		return "cluster_permission_denied"
	}
	if apierrors.IsNotFound(err) {
		return "cluster_resource_missing"
	}
	switch err.Error() {
	case "restore_ownership_unproven":
		return "restore_ownership_unproven"
	case "unsupported_schema":
		return "schema_changed"
	case "source object changed":
		return "source_object_changed"
	case "source object versioning required", "source object version unavailable", "source object identity unavailable":
		return "source_object_identity_unavailable"
	case "file changed during capture", "file identity changed during capture":
		return "source_file_changed"
	case "symlink escapes managed file roots":
		return "source_link_outside_scope"
	case "special file requires explicit capture policy":
		return "source_special_file"
	case "unregistered runtime state":
		return "restore_state_unsupported"
	case "object verifier is not read only", "Redis verifier is not read only":
		return "verifier_not_read_only"
	case "restore Redis readiness deadline exceeded":
		return "restore_redis_not_ready"
	case "restore Redis pod failed":
		return "restore_redis_failed"
	case "restore Redis service unavailable", "Redis unavailable":
		return "restore_redis_connection_unavailable"
	case "Redis authentication failed":
		return "restore_redis_authentication_failed"
	case "Redis database unavailable":
		return "restore_redis_database_unavailable"
	case "configuration_changed":
		return "configuration_changed"
	case "feature_disabled":
		return "feature_disabled"
	}
	if strings.Contains(err.Error(), "schema differs") || strings.Contains(err.Error(), "schema changed") {
		return "schema_changed"
	}
	if strings.HasPrefix(err.Error(), "value mismatch: ") {
		return "restore_sql_value_mismatch"
	}
	if strings.HasPrefix(err.Error(), "row count mismatch: ") {
		return "restore_sql_row_count_mismatch"
	}
	return PublicError(err)
}
