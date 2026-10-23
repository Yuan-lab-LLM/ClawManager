package repository

import (
	"context"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Kept separate from ordinary lifecycle operations; not exposed by an HTTP API.
type RuntimeUpgradeStopRepository interface {
	RuntimeUpgradeRepository
	GetStop(context.Context, int) (*models.RuntimeUpgradeStop, error)
	PrepareStop(context.Context, models.RuntimeUpgradeStop) error
	RecordStopConfirmation(context.Context, models.RuntimeUpgradeStop) error
}

func NewRuntimeUpgradeStopRepository(sess db.Session) RuntimeUpgradeStopRepository {
	return &runtimeUpgradeRepository{sess}
}

func (r *runtimeUpgradeRepository) GetStop(ctx context.Context, id int) (*models.RuntimeUpgradeStop, error) {
	var stop models.RuntimeUpgradeStop
	err := r.sess.WithContext(ctx).Collection("gateway_runtime_upgrade_stops").Find(db.Cond{"instance_id": id}).One(&stop)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &stop, nil
}

// Persist the target and stop intent in ONE transaction, before any stop HTTP.
func (r *runtimeUpgradeRepository) PrepareStop(ctx context.Context, target models.RuntimeUpgradeStop) error {
	if target.ConfirmedAt != nil || target.ConfirmationScope != "" {
		return models.ErrRuntimeUpgradeConflict
	}
	return r.withStopTarget(ctx, target, func(sess db.Session, state string, prior *models.RuntimeUpgradeStop) error {
		if prior != nil {
			if (state == "stop_intent" || state == "process_group_confirmed") && prior.SameTarget(target) {
				return nil
			}
			return models.ErrRuntimeUpgradeConflict
		}
		// A legacy intent without a pinned target cannot be adopted automatically.
		if state != "reserved" {
			return models.ErrRuntimeUpgradeConflict
		}
		drain, err := NewRuntimeMaintenanceRepository(sess).Get(ctx, target.InstanceID)
		if err != nil {
			return err
		}
		if drain == nil || drain.Operation != target.Operation || drain.OwnerID != target.OwnerID || drain.Generation != target.Generation || !drain.Drained(time.Now().UTC()) {
			return models.ErrRuntimeUpgradeConflict
		}
		target.CreatedAt = time.Now().UTC()
		if _, err := sess.Collection("gateway_runtime_upgrade_stops").Insert(target); err != nil {
			return err
		}
		_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state='stop_intent',updated_at=? WHERE instance_id=?`, target.CreatedAt, target.InstanceID)
		return err
	})
}

func (r *runtimeUpgradeRepository) RecordStopConfirmation(ctx context.Context, receipt models.RuntimeUpgradeStop) error {
	if receipt.ConfirmationScope != "process_group" || receipt.ConfirmedAt == nil || receipt.ConfirmedAt.IsZero() {
		return models.ErrRuntimeUpgradeConflict
	}
	return r.withStopTarget(ctx, receipt, func(sess db.Session, state string, prior *models.RuntimeUpgradeStop) error {
		if prior == nil || !prior.SameTarget(receipt) {
			return models.ErrRuntimeUpgradeConflict
		}
		if state == "process_group_confirmed" {
			// Agent may return a newer timestamp for an idempotent retry.
			// Preserve the FIRST durable confirmation, not a new backup permit.
			if prior.ConfirmationScope == receipt.ConfirmationScope && prior.ConfirmedAt != nil {
				return nil
			}
			return models.ErrRuntimeUpgradeConflict
		}
		if state != "stop_intent" {
			return models.ErrRuntimeUpgradeConflict
		}
		if _, err := sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrade_stops SET confirmation_scope=?,confirmed_at=? WHERE instance_id=?`, receipt.ConfirmationScope, receipt.ConfirmedAt.UTC(), receipt.InstanceID); err != nil {
			return err
		}
		_, err := sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state='process_group_confirmed',updated_at=? WHERE instance_id=?`, time.Now().UTC(), receipt.InstanceID)
		return err
	})
}

func (r *runtimeUpgradeRepository) withStopTarget(ctx context.Context, target models.RuntimeUpgradeStop, apply func(db.Session, string, *models.RuntimeUpgradeStop) error) error {
	if !target.ValidTarget() {
		return models.ErrRuntimeUpgradeConflict
	}
	release, err := (&instanceRepository{r.sess}).acquireLifecycleLock(ctx, target.InstanceID)
	if err != nil {
		return err
	}
	defer release()
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,type,status FROM instances WHERE id=? FOR UPDATE`, target.InstanceID)
		if err != nil {
			return err
		}
		var owner, generation int
		var kind, status string
		if err := row.Scan(&owner, &generation, &kind, &status); err != nil {
			return err
		}
		if owner != target.OwnerID || generation != target.Generation || kind != "opencode" || status != "running" {
			return models.ErrRuntimeUpgradeConflict
		}
		store := &runtimeUpgradeRepository{sess}
		upgrade, err := store.Get(ctx, target.InstanceID)
		if err != nil {
			return err
		}
		if upgrade == nil || upgrade.OwnerID != owner || upgrade.Generation != generation || upgrade.Operation != target.Operation || !upgrade.Active() {
			return models.ErrRuntimeUpgradeConflict
		}
		// Lock the placement snapshot too: a changed Pod/endpoint/PID cannot
		// retarget an old operation, even if the caller has a stale snapshot.
		row, err = sess.SQL().QueryRowContext(ctx, `SELECT b.runtime_pod_id,b.generation,b.gateway_id,b.gateway_pid,b.state,p.pod_uid,p.agent_endpoint
 FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE b.instance_id=? FOR UPDATE`, target.InstanceID)
		if err != nil {
			return err
		}
		var podID int64
		var bindingGen, pid int
		var gateway, bindingState, uid, endpoint string
		if err := row.Scan(&podID, &bindingGen, &gateway, &pid, &bindingState, &uid, &endpoint); err != nil {
			return err
		}
		if podID != target.RuntimePodID || bindingGen != generation || gateway != target.GatewayID || pid != target.PID || bindingState != "running" || uid != target.PodUID || endpoint != target.AgentEndpoint {
			return models.ErrRuntimeUpgradeConflict
		}
		prior, err := store.GetStop(ctx, target.InstanceID)
		if err != nil {
			return err
		}
		if err := requireNoPlatformWriters(ctx, sess, target.InstanceID); err != nil {
			return err
		}
		return apply(sess, upgrade.State, prior)
	}, nil)
}
