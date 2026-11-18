package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type WorkspaceWriteFenceRepository struct{ sess db.Session }

func NewWorkspaceWriteFenceRepository(sess db.Session) *WorkspaceWriteFenceRepository {
	return &WorkspaceWriteFenceRepository{sess: sess}
}

type workspaceWriteFence struct {
	owner, generation int
	pod, state        string
}

func validWorkspaceFenceIdentity(instanceID, ownerID, generation int, podUID string) bool {
	return instanceID > 0 && ownerID > 0 && generation > 0 && podUID != "" && len(podUID) <= 128 && strings.TrimSpace(podUID) == podUID && !strings.ContainsAny(podUID, "\x00\r\n")
}
func readWorkspaceWriteFence(ctx context.Context, s db.Session, id int) (*workspaceWriteFence, error) {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT owner_user_id,generation,pod_uid,state FROM instance_workspace_write_fences WHERE instance_id=?`, id)
	if err != nil {
		return nil, err
	}
	var fence workspaceWriteFence
	if err = row.Scan(&fence.owner, &fence.generation, &fence.pod, &fence.state); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if fence.state != "open" && fence.state != "closed" {
		return nil, models.ErrRuntimeUpgradeConflict
	}
	return &fence, nil
}
func checkWorkspaceWriteAdmission(ctx context.Context, s db.Session, w models.WorkspaceWriteIntent) error {
	fence, err := readWorkspaceWriteFence(ctx, s, w.InstanceID)
	if err != nil {
		return err
	}
	if fence == nil {
		return nil
	}
	if fence.state != "open" {
		return models.ErrInstanceStopping
	}
	if fence.owner != w.OwnerID || fence.generation != w.Generation {
		return models.ErrRuntimeUpgradeConflict
	}
	return nil
}
func pendingWorkspaceWrites(ctx context.Context, s db.Session, id int) (int, error) {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?`, id)
	if err != nil {
		return 0, err
	}
	var count int
	err = row.Scan(&count)
	return count, err
}

// Fence commits closed admission under the same instance row lock as Begin.
// Return pending-write errors after committing: rollback would admit another
// writer while the old helper remains unresolved.
func (r *WorkspaceWriteFenceRepository) Fence(ctx context.Context, instanceID, ownerID, generation int, podUID string) error {
	if !validWorkspaceFenceIdentity(instanceID, ownerID, generation, podUID) {
		return models.ErrRuntimeUpgradeConflict
	}
	pending := 0
	err := (&instanceRepository{r.sess}).withInstanceRowLock(ctx, instanceID, func(tx *instanceRepository) error {
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation FROM instances WHERE id=?`, instanceID)
		if err != nil {
			return err
		}
		var owner, currentGeneration int
		if err = row.Scan(&owner, &currentGeneration); err != nil {
			return err
		}
		if owner != ownerID || generation > currentGeneration {
			return models.ErrRuntimeUpgradeConflict
		}
		fence, err := readWorkspaceWriteFence(ctx, tx.sess, instanceID)
		if err != nil {
			return err
		}
		if fence != nil && (fence.owner != ownerID || fence.generation > generation || fence.generation == generation && fence.state == "open" && fence.pod != podUID) {
			return models.ErrRuntimeUpgradeConflict
		}
		// Maintenance can recheck a closed source from another Pod. Preserve
		// its old binding and admit no writes for that stopped generation.
		if fence == nil || fence.generation != generation || fence.state != "closed" {
			_, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO instance_workspace_write_fences(instance_id,owner_user_id,generation,pod_uid,state,updated_at) VALUES(?,?,?,?,'closed',?) ON DUPLICATE KEY UPDATE generation=VALUES(generation),pod_uid=VALUES(pod_uid),state='closed',updated_at=VALUES(updated_at)`, instanceID, ownerID, generation, podUID, time.Now().UTC())
			if err != nil {
				return err
			}
		}
		pending, err = pendingWorkspaceWrites(ctx, tx.sess, instanceID)
		return err
	})
	if err != nil {
		return err
	}
	if pending != 0 {
		return models.ErrRuntimeUpgradeWritersActive
	}
	return nil
}

// Activate requires new assignment authority from the caller. Closed generations
// and unresolved old write intents cannot be reopened by delayed requests.
func (r *WorkspaceWriteFenceRepository) Activate(ctx context.Context, instanceID, ownerID, generation int, podUID string) error {
	if !validWorkspaceFenceIdentity(instanceID, ownerID, generation, podUID) {
		return models.ErrRuntimeUpgradeConflict
	}
	return (&instanceRepository{r.sess}).withInstanceRowLock(ctx, instanceID, func(tx *instanceRepository) error {
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,status FROM instances WHERE id=?`, instanceID)
		if err != nil {
			return err
		}
		var owner, currentGeneration int
		var status string
		if err = row.Scan(&owner, &currentGeneration, &status); err != nil {
			return err
		}
		if owner != ownerID || currentGeneration != generation || status != "creating" && status != "running" {
			return models.ErrRuntimeUpgradeConflict
		}
		if err := requireNoInstanceTeamWorkspaceWrites(ctx, tx.sess, instanceID); err != nil {
			return err
		}
		fence, err := readWorkspaceWriteFence(ctx, tx.sess, instanceID)
		if err != nil {
			return err
		}
		if fence != nil && (fence.owner != ownerID || fence.generation > generation || fence.generation == generation && (fence.state != "open" || fence.pod != podUID)) {
			return models.ErrRuntimeUpgradeConflict
		}
		if fence != nil && fence.generation == generation {
			return nil
		}
		count, err := pendingWorkspaceWrites(ctx, tx.sess, instanceID)
		if err != nil {
			return err
		}
		if count != 0 {
			return models.ErrRuntimeUpgradeWritersActive
		}
		_, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO instance_workspace_write_fences(instance_id,owner_user_id,generation,pod_uid,state,updated_at) VALUES(?,?,?,?,'open',?) ON DUPLICATE KEY UPDATE generation=VALUES(generation),pod_uid=VALUES(pod_uid),state='open',updated_at=VALUES(updated_at)`, instanceID, ownerID, generation, podUID, time.Now().UTC())
		return err
	})
}

// Only the trusted instance delete/reset path uses erase, after confirmed
// Runtime teardown. Public file deletion remains behind ordinary admission.
func (r *workspaceWriteRepository) beginLifecycleErase(ctx context.Context, w models.WorkspaceWriteIntent) error {
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, w.InstanceID, func(tx *instanceRepository) error {
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,status FROM instances WHERE id=?`, w.InstanceID)
		if err != nil {
			return err
		}
		var owner, generation int
		var status string
		if err = row.Scan(&owner, &generation, &status); err != nil {
			return err
		}
		if owner != w.OwnerID || generation != w.Generation || status != "deleting" && status != "stopped" && status != "error" {
			return models.ErrRuntimeUpgradeConflict
		}
		fence, err := readWorkspaceWriteFence(ctx, tx.sess, w.InstanceID)
		if err != nil {
			return err
		}
		if fence == nil && generation != 0 || fence != nil && (fence.owner != w.OwnerID || fence.generation != generation || fence.state != "closed") {
			return models.ErrInstanceStopping
		}
		row, err = tx.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?`, w.InstanceID)
		if err != nil {
			return err
		}
		var bindings int
		if err = row.Scan(&bindings); err != nil {
			return err
		}
		if bindings != 0 {
			return models.ErrInstanceStopping
		}
		pending, err := pendingWorkspaceWrites(ctx, tx.sess, w.InstanceID)
		if err != nil {
			return err
		}
		if pending != 0 {
			return models.ErrRuntimeUpgradeWritersActive
		}
		return insertWorkspaceWriteIntent(ctx, tx.sess, w)
	})
}
