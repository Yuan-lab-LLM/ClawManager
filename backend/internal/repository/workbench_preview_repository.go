package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"clawreef/internal/workbench"
	"github.com/upper/db/v4"
)

var errWorkbenchPreviewStorage = errors.New("preview storage is unavailable")

const maxWorkbenchPreviewsPerInstance = 64

type workbenchPreviewRow struct {
	TokenHash  string    `db:"token_hash,primarykey"`
	InstanceID int       `db:"instance_id"`
	UserID     int       `db:"user_id"`
	Generation int       `db:"generation"`
	GatewayID  string    `db:"gateway_id"`
	TargetURL  string    `db:"target_url"`
	CreatedAt  time.Time `db:"created_at"`
	ExpiresAt  time.Time `db:"expires_at"`
}

type workbenchPreviewStore struct{ session db.Session }

func NewWorkbenchPreviewStore(session db.Session) workbench.PreviewStore {
	return &workbenchPreviewStore{session: session}
}

func validPreviewHash(key string) bool {
	decoded, err := hex.DecodeString(key)
	return err == nil && len(decoded) == 32
}

// Shared records use a versioned envelope in the existing TEXT field. Legacy
// owner records remain plain URLs; old replicas safely reject the new format.
type workbenchPreviewTargetRecord struct {
	Version int                            `json:"version"`
	URL     string                         `json:"url"`
	Shared  *workbench.SharedPreviewAccess `json:"shared"`
}

func encodeWorkbenchPreviewTarget(lease workbench.PreviewLease) (string, error) {
	if lease.Shared == nil {
		return lease.Target.URL(), nil
	}
	if !lease.Shared.Valid() || lease.ExpiresAt.After(lease.Shared.ExpiresAt) {
		return "", workbench.ErrPreviewScope
	}
	raw, err := json.Marshal(workbenchPreviewTargetRecord{Version: 1, URL: lease.Target.URL(), Shared: lease.Shared})
	return string(raw), err
}
func decodeWorkbenchPreviewTarget(raw string) (workbench.PreviewTarget, *workbench.SharedPreviewAccess, error) {
	var shared *workbench.SharedPreviewAccess
	if strings.HasPrefix(raw, "{") {
		var record workbenchPreviewTargetRecord
		if json.Unmarshal([]byte(raw), &record) != nil || record.Version != 1 || record.Shared == nil || !record.Shared.Valid() {
			return workbench.PreviewTarget{}, nil, workbench.ErrPreviewNotFound
		}
		raw, shared = record.URL, record.Shared
	}
	target, err := workbench.ParsePreviewURL(raw)
	return target, shared, err
}

func (s *workbenchPreviewStore) Put(ctx context.Context, key string, lease workbench.PreviewLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.session == nil || !validPreviewHash(key) || lease.ID != "" || !lease.Scope.Valid() || lease.CreatedAt.IsZero() || !lease.ExpiresAt.After(lease.CreatedAt) {
		return workbench.ErrPreviewScope
	}
	if _, err := workbench.ParsePreviewURL(lease.Target.URL()); err != nil {
		return err
	}
	targetURL, err := encodeWorkbenchPreviewTarget(lease)
	if err != nil {
		return err
	}
	row := workbenchPreviewRow{TokenHash: key, InstanceID: lease.Scope.InstanceID, UserID: lease.Scope.UserID, Generation: lease.Scope.Generation, GatewayID: lease.Scope.GatewayID, TargetURL: targetURL, CreatedAt: lease.CreatedAt.UTC(), ExpiresAt: lease.ExpiresAt.UTC().Truncate(time.Microsecond)}
	return s.session.TxContext(ctx, func(tx db.Session) error {
		// Serialize per-instance admission and verify the current ownership and
		// generation again at persistence time, including cross-replica callers.
		if err := lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? AND user_id=? AND runtime_generation=? FOR UPDATE", row.InstanceID, row.UserID, row.Generation); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoMoreRows) {
				return workbench.ErrPreviewScope
			}
			return errWorkbenchPreviewStorage
		}
		collection := tx.Collection("instance_workbench_previews")
		// The expiry index and fixed batch keep one UI action from collecting an
		// unbounded number of expired capabilities for the whole installation.
		if _, err := tx.SQL().ExecContext(ctx, "DELETE FROM instance_workbench_previews WHERE expires_at<=? LIMIT 256", time.Now().UTC()); err != nil {
			return errWorkbenchPreviewStorage
		}
		// Always release this instance's expired slots even if the global
		// expiry batch was filled by other instances.
		if err := collection.Find(db.Cond{"instance_id": row.InstanceID, "expires_at <=": time.Now().UTC()}).Delete(); err != nil {
			return errWorkbenchPreviewStorage
		}
		if err := collection.Find(db.Cond{"instance_id": row.InstanceID, "generation !=": row.Generation}).Delete(); err != nil {
			return errWorkbenchPreviewStorage
		}
		count, err := collection.Find(db.Cond{"instance_id": row.InstanceID}).Count()
		if err != nil {
			return errWorkbenchPreviewStorage
		}
		if count >= maxWorkbenchPreviewsPerInstance {
			return workbench.ErrPreviewLimit
		}
		if _, err := collection.Insert(row); err != nil {
			return errWorkbenchPreviewStorage
		}
		return nil
	}, nil)
}

func (s *workbenchPreviewStore) Get(ctx context.Context, key string) (workbench.PreviewLease, error) {
	if err := ctx.Err(); err != nil {
		return workbench.PreviewLease{}, err
	}
	if s.session == nil || !validPreviewHash(key) {
		return workbench.PreviewLease{}, workbench.ErrPreviewNotFound
	}
	var row workbenchPreviewRow
	if err := s.session.WithContext(ctx).Collection("instance_workbench_previews").Find(db.Cond{"token_hash": key}).One(&row); err != nil {
		if errors.Is(err, db.ErrNoMoreRows) {
			return workbench.PreviewLease{}, workbench.ErrPreviewNotFound
		}
		return workbench.PreviewLease{}, errWorkbenchPreviewStorage
	}
	target, shared, err := decodeWorkbenchPreviewTarget(row.TargetURL)
	if err != nil {
		return workbench.PreviewLease{}, workbench.ErrPreviewNotFound
	}
	return workbench.PreviewLease{Scope: workbench.PreviewScope{InstanceID: row.InstanceID, UserID: row.UserID, Generation: row.Generation, GatewayID: row.GatewayID}, Target: target, Shared: shared, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}, nil
}
