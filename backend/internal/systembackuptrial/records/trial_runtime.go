package records

import (
	"context"
	"database/sql"
	"strings"
)

// VerifyRestoredTrial checks the requested artifact against the current
// stopped target before an isolated sample deployment can start.
func VerifyRestoredTrial(ctx context.Context, reader *sql.DB, target DrillTarget, source Snapshot, placement Placement) error {
	if reader == nil || !strings.HasPrefix(target.Database, "cm_drill_trial_") {
		return ErrIsolation
	}
	conn, err := reader.Conn(ctx)
	if err != nil {
		return ErrDatabase
	}
	defer conn.Close()
	if err := target.check(ctx, conn); err != nil {
		return err
	}
	if err := checkSelectOnly(ctx, conn, target.Database); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ErrDatabase
	}
	defer tx.Rollback()
	if err := checkDrillExecutableSchema(ctx, tx); err != nil {
		return err
	}
	catalog, err := Catalog(ctx, tx)
	if err != nil {
		return err
	}
	plan, err := normalizeTrial(ctx, source, catalog, placement)
	if err != nil {
		return err
	}
	plan.trialSeeds, err = captureTrialSeeds(ctx, tx, catalog)
	if err != nil {
		return err
	}
	if err := canonicalizeTargetJSON(ctx, tx, &plan); err != nil {
		return err
	}
	return verifyRowsInTransaction(ctx, tx, plan)
}

// CheckTrialRuntimeWriter pins the application's DML-only account to the same
// verified sample schema. Ordinary and formal drill startup do not call it.
func CheckTrialRuntimeWriter(ctx context.Context, pool *sql.DB, target DrillTarget) error {
	if pool == nil || !strings.HasPrefix(target.Database, "cm_drill_trial_") {
		return ErrIsolation
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return ErrDatabase
	}
	defer conn.Close()
	if err := target.check(ctx, conn); err != nil {
		return err
	}
	database, err := QuotedGrantDatabase(ctx, conn, target.Database)
	if err != nil {
		return err
	}
	var role string
	if conn.QueryRowContext(ctx, "SELECT CURRENT_ROLE()").Scan(&role) != nil || role != "NONE" {
		return ErrIsolation
	}
	rows, err := conn.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER")
	if err != nil {
		return ErrDatabase
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var grant string
		if rows.Scan(&grant) != nil {
			return ErrDatabase
		}
		if strings.Contains(grant, " WITH GRANT OPTION") {
			return ErrIsolation
		}
		if strings.HasPrefix(grant, "GRANT USAGE ON *.* TO ") {
			continue
		}
		if found || !strings.HasPrefix(grant, "GRANT SELECT, INSERT, UPDATE, DELETE ON "+database+".* TO ") {
			return ErrIsolation
		}
		found = true
	}
	if rows.Err() != nil {
		return ErrDatabase
	}
	if !found {
		return ErrIsolation
	}
	return nil
}
