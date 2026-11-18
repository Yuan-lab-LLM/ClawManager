package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

var errFaultRecoveryCancelled = errors.New("fault recovery was cancelled before fencing")

// Cancellation and the first noncancellable phase serialize on the operation
// row. A successful Cancel can never be followed by an old worker stopping it.
func (s *RuntimeManagementService) beginRecoveryFence(ctx context.Context, operation string, item RuntimeSchedulingItem) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var owner, status, raw string
		var cancelled bool
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status,items_json,cancel_requested FROM runtime_management_operations WHERE id=? FOR UPDATE", operation)
		if e != nil {
			return e
		}
		if e = row.Scan(&owner, &status, &raw, &cancelled); e != nil {
			return e
		}
		if owner != s.token || status != "processing" {
			return fmt.Errorf("fault recovery execution owner changed")
		}
		if cancelled {
			return errFaultRecoveryCancelled
		}
		var items []RuntimeSchedulingItem
		if e = json.Unmarshal([]byte(raw), &items); e != nil {
			return e
		}
		for i := range items {
			if items[i].InstanceID != item.InstanceID {
				continue
			}
			if items[i].Recovery == nil || items[i].Recovery.Phase != "planned" {
				return fmt.Errorf("fault recovery phase changed")
			}
			items[i].Recovery.Phase = "fencing"
			encoded, e := json.Marshal(items)
			if e != nil {
				return e
			}
			_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET items_json=?,updated_at=? WHERE id=?", string(encoded), time.Now().UTC(), operation)
			return e
		}
		return fmt.Errorf("fault recovery item missing")
	}, nil)
}

func (s *RuntimeManagementService) cancelRecoveryEviction(ctx context.Context, id string) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var status, raw string
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT status,items_json FROM runtime_management_operations WHERE id=? FOR UPDATE", id)
		if e != nil {
			return e
		}
		if e = row.Scan(&status, &raw); e != nil {
			return e
		}
		if status != "pending" && status != "processing" && status != "needs_attention" {
			return fmt.Errorf("fault recovery cannot be cancelled in its current state")
		}
		var items []RuntimeSchedulingItem
		if e = json.Unmarshal([]byte(raw), &items); e != nil {
			return e
		}
		for _, item := range items {
			if item.Recovery == nil || item.Recovery.Phase != "planned" {
				return fmt.Errorf("fault recovery has already fenced execution; continue the same operation instead of cancelling")
			}
		}
		_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET cancel_requested=1,status=CASE WHEN status='needs_attention' THEN 'pending' ELSE status END,updated_at=? WHERE id=?", time.Now().UTC(), id)
		return e
	}, nil)
}

// Complete journaling and release scheduler admission in one commit. A worker
// crash must leave either a resumable processing operation or no guards.
func (s *RuntimeManagementService) completeRecoveryEviction(ctx context.Context, op *RuntimeManagementOperation) error {
	data, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var owner, status string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
		if err != nil {
			return err
		}
		if err = row.Scan(&owner, &status); err != nil {
			return err
		}
		if owner != s.token || status != "processing" {
			return fmt.Errorf("fault recovery execution owner changed")
		}
		if _, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status=?,items_json=?,updated_at=? WHERE id=?", op.Status, string(data), time.Now().UTC(), op.ID); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, "DELETE FROM instance_lifecycle_manual_guards WHERE token=?", op.ID)
		return err
	}, nil)
}
