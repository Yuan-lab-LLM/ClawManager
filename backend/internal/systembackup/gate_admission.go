package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Close only the short admission row before waiting for complete-request
// locks. Existing units keep an idle gate and can finish their nested SQL;
// newly arriving units cannot keep extending the drain indefinitely.
func (g Gate) closeAdmission(ctx context.Context, scope, owner string, acquire time.Duration) error {
	tx, err := g.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prior sql.NullString
	var until sql.NullTime
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT owner,close_until,UTC_TIMESTAMP(6) FROM system_backup_gate_admissions WHERE scope=? FOR UPDATE NOWAIT", scope).Scan(&prior, &until, &now); err != nil {
		var busy *mysql.MySQLError
		if errors.As(err, &busy) && busy.Number == 3572 {
			return ErrMaintenance
		}
		return err
	}
	if prior.Valid && prior.String != owner && until.Valid && until.Time.After(now) {
		return ErrMaintenance
	}
	var phase string
	if err = tx.QueryRowContext(ctx, "SELECT phase FROM system_backup_gate WHERE scope=?", scope).Scan(&phase); err != nil {
		return err
	}
	if phase != "idle" {
		return ErrMaintenance
	}
	deadline := now.Add(acquire)
	if prior.String == owner && until.Valid && until.Time.After(now) && until.Time.Before(deadline) {
		deadline = until.Time
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_gate_admissions SET owner=?,close_until=?,updated_at=UTC_TIMESTAMP(6) WHERE scope=?", owner, deadline, scope); err != nil {
		return err
	}
	return tx.Commit()
}
func (g Gate) CancelAdmission(ctx context.Context, scope, owner string) error {
	_, err := g.DB.ExecContext(ctx, "UPDATE system_backup_gate_admissions SET owner=NULL,close_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE scope=? AND owner=?", scope, owner)
	return err
}
