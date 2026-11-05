package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"time"
)

// Separate from desired configuration and Kubernetes resources. No deletion or TTL.
type BrowserWriterRegistry interface {
	LoadBrowserWriter(context.Context, int) (string, int, error)
	StoreBrowserWriter(context.Context, int, int, string) error
}

func (r *browserWorkerRepository) LoadBrowserWriter(ctx context.Context, id int) (string, int, error) {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT evidence,revision FROM browser_writer_registry WHERE instance_id=?`, id)
	if err != nil {
		return "", 0, err
	}
	var raw string
	var revision int
	err = row.Scan(&raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return raw, revision, err
}
func (r *browserWorkerRepository) StoreBrowserWriter(ctx context.Context, id, revision int, raw string) error {
	if id <= 0 || revision < 0 || len(raw) == 0 || len(raw) > 64<<10 {
		return models.ErrRuntimeUpgradeConflict
	}
	if revision == 0 {
		_, err := r.sess.SQL().ExecContext(ctx, `INSERT INTO browser_writer_registry(instance_id,revision,evidence,updated_at)VALUES(?,1,?,?)`, id, raw, time.Now().UTC())
		return err
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE browser_writer_registry SET revision=revision+1,evidence=?,updated_at=? WHERE instance_id=? AND revision=?`, raw, time.Now().UTC(), id, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return models.ErrRuntimeUpgradeConflict
	}
	return nil
}
