package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/browserorigin"
	"github.com/upper/db/v4"
)

type browserOriginStore struct{ session db.Session }

type browserOriginRow struct {
	Port         int    `db:"port"`
	InstanceID   int    `db:"instance_id"`
	Kind         string `db:"kind"`
	Target       string `db:"target"`
	IdentityHash string `db:"identity_hash"`
}

// Creation and owner identify the security principal independently of mutable
// runtime generations and physical workspace locations.
func browserOriginIdentity(instanceID, userID int, createdAt time.Time) string {
	identity, _ := json.Marshal([]any{instanceID, userID, createdAt.UTC().Format(time.RFC3339Nano)})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:])
}

func NewBrowserOriginStore(session db.Session) browserorigin.Store {
	return &browserOriginStore{session: session}
}
func (s *browserOriginStore) Bind(ctx context.Context, instanceID int, kind, target string, first, last int) (browserorigin.Binding, error) {
	if err := ctx.Err(); err != nil {
		return browserorigin.Binding{}, err
	}
	if s.session == nil || instanceID <= 0 || first < 1 || last > 65535 || first > last || last-first >= 512 {
		return browserorigin.Binding{}, browserorigin.ErrInvalid
	}
	target, err := browserorigin.CanonicalTarget(kind, target)
	if err != nil {
		return browserorigin.Binding{}, err
	}
	var binding browserorigin.Binding
	err = s.session.TxContext(ctx, func(tx db.Session) error {
		// Every replica seeds and locks the same bounded pool in ascending order.
		// The rows are the allocation lock; no connection-scoped advisory locks.
		values := make([]string, 0, last-first+1)
		args := make([]any, 0, last-first+1)
		for port := first; port <= last; port++ {
			values = append(values, "(?)")
			args = append(args, port)
		}
		// A duplicate-key UPDATE takes an exclusive lock immediately. INSERT
		// IGNORE would take shared duplicate locks that can deadlock on upgrade.
		if _, err := tx.SQL().ExecContext(ctx, "INSERT INTO browser_origin_ports (port) VALUES "+strings.Join(values, ",")+" ON DUPLICATE KEY UPDATE port=browser_origin_ports.port", args...); err != nil {
			return err
		}
		rows, err := tx.SQL().QueryContext(ctx, "SELECT port,instance_id FROM browser_origin_ports WHERE port BETWEEN ? AND ? ORDER BY port FOR UPDATE", first, last)
		if err != nil {
			return err
		}
		freePort := 0
		for rows.Next() {
			var port int
			var id sql.NullInt64
			if err = rows.Scan(&port, &id); err != nil {
				rows.Close()
				return err
			}
			if !id.Valid && freePort == 0 {
				freePort = port
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Validate against the live identity even when a permanent binding exists.
		// Retained tombstones must never resurrect an instance after deletion.
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT user_id,created_at FROM instances WHERE id=? FOR UPDATE", instanceID)
		var userID int
		var createdAt time.Time
		if err == nil {
			err = row.Scan(&userID, &createdAt)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoMoreRows) {
				return browserorigin.ErrNotFound
			}
			return err
		}
		if userID <= 0 || createdAt.IsZero() {
			return browserorigin.ErrNotFound
		}
		identityHash := browserOriginIdentity(instanceID, userID, createdAt)
		var existing []browserOriginRow
		if err = tx.Collection("browser_origin_ports").Find(db.Cond{"instance_id": instanceID}).All(&existing); err != nil {
			return err
		}
		previewCount := 0
		var matched browserorigin.Binding
		for _, b := range existing {
			// A reused numeric ID or transferred owner must not inherit any old
			// origin, including a previously unseen target for that stale ID.
			if b.IdentityHash != identityHash {
				return browserorigin.ErrNotFound
			}
			if b.Kind == kind && b.Target == target {
				if b.Port < first || b.Port > last {
					return browserorigin.ErrCapacity
				}
				matched = browserorigin.Binding{Port: b.Port, InstanceID: b.InstanceID, Kind: b.Kind, Target: b.Target}
			}
			if b.Kind == browserorigin.Preview {
				previewCount++
			}
		}
		if matched.Port != 0 {
			binding = matched
			return nil
		}
		if freePort == 0 || (kind == browserorigin.Preview && previewCount >= 32) {
			return browserorigin.ErrCapacity
		}
		result, err := tx.SQL().ExecContext(ctx, "UPDATE browser_origin_ports SET instance_id=?,identity_hash=?,kind=?,target=? WHERE port=? AND instance_id IS NULL", instanceID, identityHash, kind, target, freePort)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return browserorigin.ErrCapacity
		}
		binding = browserorigin.Binding{Port: freePort, InstanceID: instanceID, Kind: kind, Target: target}
		return nil
	}, nil)
	if err != nil {
		return browserorigin.Binding{}, fmt.Errorf("bind browser origin: %w", err)
	}
	return binding, nil
}

func (s *browserOriginStore) ByPort(ctx context.Context, port int) (browserorigin.Binding, error) {
	if err := ctx.Err(); err != nil {
		return browserorigin.Binding{}, err
	}
	if s.session == nil || port < 1 || port > 65535 {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	// Joining the live instance suppresses deleted identities without releasing
	// their assigned browser origins back into the pool.
	row, err := s.session.SQL().QueryRowContext(ctx, "SELECT p.port,p.instance_id,p.kind,p.target,p.identity_hash,i.user_id,i.created_at FROM browser_origin_ports p JOIN instances i ON i.id=p.instance_id WHERE p.port=?", port)
	var binding browserorigin.Binding
	var identityHash sql.NullString
	var userID int
	var createdAt time.Time
	if err == nil {
		err = row.Scan(&binding.Port, &binding.InstanceID, &binding.Kind, &binding.Target, &identityHash, &userID, &createdAt)
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoMoreRows) {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	if err != nil {
		return browserorigin.Binding{}, fmt.Errorf("lookup browser origin: %w", err)
	}
	if userID <= 0 || createdAt.IsZero() || !identityHash.Valid || identityHash.String != browserOriginIdentity(binding.InstanceID, userID, createdAt) {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	return binding, nil
}
