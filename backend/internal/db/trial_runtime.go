package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	upper "github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// InitializeVerifiedTrialRuntime adopts a dedicated pool after checking the
// installed schema and obtaining the trial's independent admission proof.
// It never applies migrations. Ordinary business connections reject drill schemas.
func InitializeVerifiedTrialRuntime(ctx context.Context, pool *sql.DB, database, serverUUID string, admit func() error) (upper.Session, error) {
	if pool == nil || admit == nil || !strings.HasPrefix(database, "cm_drill_trial_") || serverUUID == "" || SQLGateScope() != "" {
		return nil, errors.New("verified trial runtime database required")
	}
	var actualDB, actualUUID string
	if pool.QueryRowContext(ctx, "SELECT DATABASE(), @@server_uuid").Scan(&actualDB, &actualUUID) != nil ||
		actualDB != database || actualUUID != serverUUID {
		return nil, errors.New("trial runtime database identity mismatch")
	}
	session, err := mysql.New(pool)
	if err != nil {
		return nil, err
	}
	if err := verifyEmbeddedMigrationsApplied(session); err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := admit(); err != nil {
		_ = session.Close()
		return nil, err
	}
	Session = session
	return session, nil
}
