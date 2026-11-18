package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/go-sql-driver/mysql"
)

// ForceReleaseExpired invalidates capture before reopening admission. It can
// resume application admission only after every enrolled participant has
// explicitly resumed; it never fabricates a Runtime/tool resume ACK.
func (g Gate) ForceReleaseExpired(ctx context.Context, scope string) (bool, error) {
	tx, err := g.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var admission string
	if err = tx.QueryRowContext(ctx, "SELECT scope FROM system_backup_gate_admissions WHERE scope=? FOR UPDATE NOWAIT", scope).Scan(&admission); err != nil {
		return false, watchdogLockError(err)
	}
	var phase, owner string
	var generation uint64
	var expired bool
	if err = tx.QueryRowContext(ctx, "SELECT phase,COALESCE(owner,''),generation,COALESCE(UTC_TIMESTAMP(6)>=hold_deadline OR (phase='acquiring' AND UTC_TIMESTAMP(6)>=acquire_deadline),0) FROM system_backup_gate WHERE scope=? FOR UPDATE NOWAIT", scope).Scan(&phase, &owner, &generation, &expired); err != nil {
		return false, watchdogLockError(err)
	}
	if phase == "idle" || !expired {
		return false, tx.Commit()
	}
	if owner == "" {
		return false, errors.New("coordination_invalid")
	}
	// This historical fact survives a subsequent participant generation reset.
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_capture_plans SET forced_resume_at=COALESCE(forced_resume_at,UTC_TIMESTAMP(6)) WHERE scope=? AND generation=?", scope, generation); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_gate SET phase='releasing',updated_at=UTC_TIMESTAMP(6) WHERE scope=?", scope); err != nil {
		return false, err
	}
	var waiting int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_participants WHERE scope=? AND (generation<>? OR state<>'running' OR resume_result NOT IN ('succeeded','forced'))", scope, generation).Scan(&waiting); err != nil {
		return false, err
	}
	if waiting != 0 {
		return false, tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_capture_plans SET source_resumed_at=COALESCE(source_resumed_at,UTC_TIMESTAMP(6)) WHERE scope=? AND generation=?", scope, generation); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_gate SET phase='idle',owner=NULL,acquire_deadline=NULL,hold_deadline=NULL,updated_at=UTC_TIMESTAMP(6) WHERE scope=?", scope); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_gate_admissions SET owner=NULL,close_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE scope=? AND owner=?", scope, owner); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
func watchdogLockError(err error) error {
	var busy *mysql.MySQLError
	if errors.As(err, &busy) && busy.Number == 3572 {
		return ErrMaintenance
	}
	return err
}

func (g Gate) RunExpiryWatchdog(ctx context.Context, scope string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastLog time.Time
	for {
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := g.ForceReleaseExpired(step, scope)
		cancel()
		if err != nil && !errors.Is(err, ErrMaintenance) && ctx.Err() == nil && time.Since(lastLog) >= 30*time.Second {
			log.Print("backup gate safety recovery deferred: ", PublicError(err))
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
