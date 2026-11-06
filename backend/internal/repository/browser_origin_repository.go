package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/browserorigin"
	"github.com/upper/db/v4"
)

type browserOriginStore struct{ session db.Session }

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
		if err = lockUpgradeRow(ctx, tx, "SELECT id FROM instances WHERE id=? FOR UPDATE", instanceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoMoreRows) {
				return browserorigin.ErrNotFound
			}
			return err
		}
		var existing []browserorigin.Binding
		if err = tx.Collection("browser_origin_ports").Find(db.Cond{"instance_id": instanceID}).All(&existing); err != nil {
			return err
		}
		previewCount := 0
		for _, b := range existing {
			if b.Kind == kind && b.Target == target {
				if b.Port < first || b.Port > last {
					return browserorigin.ErrCapacity
				}
				binding = b
				return nil
			}
			if b.Kind == browserorigin.Preview {
				previewCount++
			}
		}
		if freePort == 0 || (kind == browserorigin.Preview && previewCount >= 32) {
			return browserorigin.ErrCapacity
		}
		result, err := tx.SQL().ExecContext(ctx, "UPDATE browser_origin_ports SET instance_id=?,kind=?,target=? WHERE port=? AND instance_id IS NULL", instanceID, kind, target, freePort)
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
	row, err := s.session.SQL().QueryRowContext(ctx, "SELECT p.port,p.instance_id,p.kind,p.target FROM browser_origin_ports p JOIN instances i ON i.id=p.instance_id WHERE p.port=?", port)
	var binding browserorigin.Binding
	if err == nil {
		err = row.Scan(&binding.Port, &binding.InstanceID, &binding.Kind, &binding.Target)
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, db.ErrNoMoreRows) {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	if err != nil {
		return browserorigin.Binding{}, fmt.Errorf("lookup browser origin: %w", err)
	}
	return binding, nil
}
