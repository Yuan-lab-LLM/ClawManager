package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"clawreef/internal/runtimeauth"
	"github.com/upper/db/v4"
)

// Measure only explicitly traced control requests; never mutate the shared SQL
// session or emit SQL/arguments. The query retains its existing WithContext.
func runtimeQueryTrace(ctx context.Context, sess db.Session, name string) func(error) {
	if !runtimeauth.DiagnosticsEnabled(ctx) {
		return func(error) {}
	}
	started := time.Now()
	pool, ok := sess.Driver().(*sql.DB)
	var before sql.DBStats
	if ok {
		before = pool.Stats()
	}
	return func(err error) {
		if errors.Is(err, db.ErrNoMoreRows) {
			err = nil
		}
		if ok {
			runtimeauth.RecordPoolStage(ctx, name, started, before, pool.Stats(), err)
		} else {
			runtimeauth.RecordStage(ctx, name, started, err)
		}
	}
}
