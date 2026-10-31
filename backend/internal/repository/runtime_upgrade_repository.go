package repository

import (
	"context"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Intentionally not exposed as an HTTP upgrade API until migration/restore
// coordination exists. Only a reservation (before any stop intent) can cancel.
type RuntimeUpgradeRepository interface {
	Get(context.Context, int) (*models.RuntimeUpgrade, error)
	Reserve(context.Context, int, int, int, string) error
	MarkStopIntent(context.Context, int, int, int, string) error
	MarkRecoveryRequired(context.Context, int, int, int, string) error
	CancelReservation(context.Context, int, int, int, string) error
}

type runtimeUpgradeRepository struct{ sess db.Session }

func NewRuntimeUpgradeRepository(sess db.Session) RuntimeUpgradeRepository {
	return &runtimeUpgradeRepository{sess}
}

func (r *runtimeUpgradeRepository) Get(ctx context.Context, id int) (*models.RuntimeUpgrade, error) {
	var state models.RuntimeUpgrade
	err := r.sess.WithContext(ctx).Collection("gateway_runtime_upgrades").Find(db.Cond{"instance_id": id}).One(&state)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func checkRuntimeUpgrade(ctx context.Context, sess db.Session, id int) error {
	state, err := NewRuntimeUpgradeRepository(sess).Get(ctx, id)
	if err != nil {
		return err
	}
	if state != nil && state.Active() {
		return models.ErrRuntimeUpgradeInProgress
	}
	return nil
}

func (r *runtimeUpgradeRepository) Reserve(ctx context.Context, id, owner, generation int, operation string) error {
	return r.change(ctx, id, owner, generation, operation, "reserved")
}
func (r *runtimeUpgradeRepository) MarkStopIntent(ctx context.Context, id, owner, generation int, operation string) error {
	return r.change(ctx, id, owner, generation, operation, "stop_intent")
}
func (r *runtimeUpgradeRepository) MarkRecoveryRequired(ctx context.Context, id, owner, generation int, operation string) error {
	return r.change(ctx, id, owner, generation, operation, "recovery_required")
}
func (r *runtimeUpgradeRepository) CancelReservation(ctx context.Context, id, owner, generation int, operation string) error {
	return r.change(ctx, id, owner, generation, operation, "cancelled")
}

func (r *runtimeUpgradeRepository) change(ctx context.Context, id, owner, generation int, operation, next string) error {
	if id <= 0 || owner <= 0 || generation < 0 || !models.ValidRuntimeOperation(operation) {
		return models.ErrRuntimeUpgradeConflict
	}
	// Same connection-scoped lock as ordinary lifecycle and scheduler actions.
	// Raw acquisition is private; only this state machine bypasses the fence.
	release, err := (&instanceRepository{r.sess}).acquireLifecycleLock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,type,status FROM instances WHERE id=? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		var actualOwner, actualGeneration int
		var kind, status string
		if err := row.Scan(&actualOwner, &actualGeneration, &kind, &status); err != nil {
			return err
		}
		if owner != actualOwner || generation != actualGeneration || kind != "opencode" {
			return models.ErrRuntimeUpgradeConflict
		}
		store := &runtimeUpgradeRepository{sess}
		prior, err := store.Get(ctx, id)
		if err != nil {
			return err
		}
		matching := prior != nil && prior.Operation == operation && prior.OwnerID == owner && prior.Generation == generation
		if prior != nil && prior.Active() && !matching {
			return models.ErrRuntimeUpgradeConflict
		}
		if next == "reserved" {
			if prior != nil && prior.Active() {
				if matching && prior.State == next {
					return nil
				}
				return models.ErrRuntimeUpgradeConflict
			}
			// Do not revive the same previously cancelled operation.
			if matching {
				return models.ErrRuntimeUpgradeConflict
			}
		} else {
			if !matching {
				return models.ErrRuntimeUpgradeConflict
			}
			if prior.State == next {
				return nil
			}
			if (next == "stop_intent" || next == "cancelled") && prior.State != "reserved" {
				return models.ErrRuntimeUpgradeConflict
			}
			if next == "recovery_required" && !prior.Active() {
				return models.ErrRuntimeUpgradeConflict
			}
		}
		if next == "reserved" || next == "stop_intent" {
			if err := requireNoPlatformWriters(ctx, sess, id); err != nil {
				return err
			}
			drain, err := NewRuntimeMaintenanceRepository(sess).Get(ctx, id)
			if err != nil {
				return err
			}
			if status != "running" || drain == nil || drain.Operation != operation || drain.OwnerID != owner || drain.Generation != generation || !drain.Drained(time.Now().UTC()) {
				return models.ErrRuntimeUpgradeConflict
			}
		}
		now := time.Now().UTC()
		if next == "reserved" {
			_, err = sess.SQL().ExecContext(ctx, `INSERT INTO gateway_runtime_upgrades(instance_id,owner_user_id,generation,operation_id,state,created_at,updated_at)
 VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE owner_user_id=VALUES(owner_user_id),generation=VALUES(generation),operation_id=VALUES(operation_id),state=VALUES(state),created_at=VALUES(created_at),updated_at=VALUES(updated_at)`, id, owner, generation, operation, next, now, now)
		} else {
			_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state=?,updated_at=? WHERE instance_id=? AND operation_id=?`, next, now, id, operation)
		}
		if err != nil {
			return err
		}
		if next == "cancelled" {
			_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_maintenance SET cancelled=TRUE WHERE instance_id=? AND operation_id=?`, id, operation)
		}
		return err
	}, nil)
}
