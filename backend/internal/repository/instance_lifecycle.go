package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"sync"
	"time"

	"clawreef/internal/models"
)

// MySQL named locks are connection scoped. Keep this connection checked out
// until the entire operation (including stop/wait/start) has finished.
func (r *instanceRepository) AcquireLifecycleLock(ctx context.Context, id int) (func(), error) {
	release, err := r.acquireLifecycleLock(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := checkRuntimeUpgrade(ctx, r.sess, id); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (r *instanceRepository) acquireLifecycleLock(ctx context.Context, id int) (func(), error) {
	db, ok := r.sess.Driver().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("instance lifecycle lock requires a SQL connection")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire instance lifecycle connection: %w", err)
	}
	name := fmt.Sprintf("instance:%x:%d", sha256.Sum256([]byte(r.sess.Name())), id)
	// Keep the name below MySQL's 64-character limit, including large IDs.
	name = fmt.Sprintf("instance:%x", sha256.Sum256([]byte(name)))[:63]
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&acquired); err != nil {
		// A canceled query may have acquired the lock without delivering a
		// response. Discard the physical connection rather than pooling it.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, fmt.Errorf("acquire instance lifecycle lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		if acquired.Valid && acquired.Int64 == 0 {
			return nil, models.ErrInstanceOperationInProgress
		}
		return nil, fmt.Errorf("instance lifecycle lock returned no result")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			defer conn.Close()
			releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var released sql.NullInt64
			if err := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
				log.Printf("discarding instance %d lifecycle lock connection after release failure", id)
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
		})
	}, nil
}
