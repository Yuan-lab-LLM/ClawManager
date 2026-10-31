package systembackup

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"time"
)

// Recover invokes the same restore implementation against an explicitly
// configured disaster-side control DB. No source job record is required.
func (s *TeamDataService) Recover(ctx context.Context, origin, id, commit string) (TeamDataJob, error) {
	return s.runRecoveryJob(ctx, "restore", origin, id, commit)
}
func (s *TeamDataService) VerifyRecovery(ctx context.Context, origin, id, commit string) (TeamDataJob, error) {
	return s.runRecoveryJob(ctx, "verify", origin, id, commit)
}
func (s *TeamDataService) runRecoveryJob(ctx context.Context, operation, origin, id, commit string) (TeamDataJob, error) {
	if !s.recoveryOnly || origin != s.cfg.InstallationID {
		return TeamDataJob{}, errors.New("validation_failed")
	}
	lock, e := s.recoveryLock(ctx)
	if e != nil {
		return TeamDataJob{}, e
	}
	defer releaseRecoveryLock(lock)
	a, e := OpenPublishedArtifact(ctx, s.store, origin, id, s.catalog().PublicKey, s.cfg.KeyVersion)
	if e != nil {
		return TeamDataJob{}, e
	}
	if a.CommitReference.SHA256 != commit {
		return TeamDataJob{}, errors.New("artifact_conflict")
	}
	if e = s.catalog().Import(ctx, a, 1, false); e != nil {
		return TeamDataJob{}, e
	}
	key, e := RandomID("offline_")
	if e != nil {
		return TeamDataJob{}, e
	}
	j, e := s.Create(ctx, 1, key, operation, id)
	if e != nil {
		return j, e
	}
	claimed, e := s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='running',owner_id=?,started_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status='pending'", s.owner, j.ID)
	if e = requireRow(claimed, e); e != nil {
		return j, e
	}
	// Reuse the worker's lock-loss/cancellation watch and conditional final
	// write. A late old process cannot overwrite a cleaned/newer job result.
	s.execute(ctx, lock, j)
	finish, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	j, e = s.Get(finish, j.ID)
	if e == nil && j.Status != "succeeded" {
		return j, errors.New(j.Error)
	}
	return j, e
}
func (s *TeamDataService) CleanupRecovery(ctx context.Context, id string) error {
	if !s.recoveryOnly {
		return errors.New("validation_failed")
	}
	lock, e := s.recoveryLock(ctx)
	if e != nil {
		return e
	}
	defer releaseRecoveryLock(lock)
	j, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if j.ConfigHash != s.configHash {
		return errors.New("configuration_changed")
	}
	if (j.Status == "running" || j.Status == "canceling") && strings.HasPrefix(j.Owner, "recovery_") {
		// Only a process using this same DB-owned mutex can have this owner
		// prefix. Holding it proves that process no longer owns the recovery.
		_, e = s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET status='failed',phase='interrupted',error_code='process_interrupted',owner_id=NULL,finished_at=UTC_TIMESTAMP(6) WHERE public_id=? AND owner_id=? AND status IN ('running','canceling')", id, j.Owner)
		if e != nil {
			return e
		}
	} else if j.Status == "pending" {
		if e = s.Cancel(ctx, id); e != nil {
			return e
		}
	}
	return s.cleanup(ctx, id)
}

func (s *TeamDataService) recoveryLock(ctx context.Context) (*sql.Conn, error) {
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return nil, e
	}
	hash, e := RequestHash(map[string]string{"installation_id": s.cfg.InstallationID})
	var held sql.NullInt64
	if e == nil {
		e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", "team-data-"+hash[:40]).Scan(&held)
	}
	if e != nil || !held.Valid || held.Int64 != 1 {
		conn.Close()
		if e != nil {
			return nil, e
		}
		return nil, errors.New("conflict_active_job")
	}
	return conn, nil
}

func releaseRecoveryLock(conn *sql.Conn) {
	// The connection is dedicated to this one mutex. Closing a pooled SQL
	// connection alone does not release MySQL advisory locks.
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	_, e := conn.ExecContext(ctx, "DO RELEASE_ALL_LOCKS()")
	if e != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	conn.Close()
}
