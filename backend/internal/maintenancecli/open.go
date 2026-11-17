// Package maintenancecli gives finite offline commands the serving writer gate.
package maintenancecli

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"clawreef/internal/backupgate"
	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/systembackup"
	"github.com/upper/db/v4"
)

func Open(ctx context.Context, cfg config.DatabaseConfig) (db.Session, func(), error) {
	scope := appdb.SQLGateScope()
	if scope == "" {
		return nil, nil, errors.New("maintenance writes require SYSTEM_BACKUP_SQL_GATE_SCOPE and independent SYSTEM_BACKUP_CONTROL_DSN_FILE")
	}
	sess, err := appdb.Connect(cfg)
	if err != nil {
		return nil, nil, err
	}
	control, err := systembackup.OpenControlDatabase(ctx, os.Getenv("SYSTEM_BACKUP_CONTROL_DSN_FILE"))
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	pool, ok := sess.Driver().(*sql.DB)
	if !ok {
		control.Close()
		sess.Close()
		return nil, nil, errors.New("SQL maintenance session required")
	}
	if err = backupgate.RequireSameDatabase(ctx, pool, control); err != nil {
		control.Close()
		sess.Close()
		return nil, nil, err
	}
	undo := backupgate.ConfigureWorkBoundary(func(ctx context.Context) (func(), error) { return backupgate.HoldRequest(ctx, control, scope) })
	release, err := backupgate.HoldWorkUnit(ctx, "maintenance.cli")
	if err != nil {
		undo()
		control.Close()
		sess.Close()
		return nil, nil, err
	}
	return sess, func() { release(); undo(); sess.Close(); control.Close() }, nil
}
