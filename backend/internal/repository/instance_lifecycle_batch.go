package repository

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

const lifecycleGlobalLimit = 10
const lifecycleActionLimit = 5
const lifecyclePodLimit = 5

// New batches admit five lifecycles per source pod. Old batches retain their
// exclusions, including when sharing a pod with a new batch. Unknown bindings
// use a per-runtime bucket rather than silently bypassing the limit.
func lifecyclePodHasCapacity(item models.InstanceLifecycleBatchItem, batch models.InstanceLifecycleBatch, active []models.InstanceLifecycleBatchItem, batches map[string]models.InstanceLifecycleBatch) bool {
	count := 0
	for _, other := range active {
		if item.SourceID == other.SourceID {
			return false
		}
		b, ok := batches[other.BatchID]
		if !ok {
			return false
		}
		samePod := item.RuntimePodID != 0 && item.RuntimePodID == other.RuntimePodID
		unknownSameType := item.RuntimeType == other.RuntimeType && (item.RuntimePodID == 0 || other.RuntimePodID == 0)
		if batch.PolicyVersion < 2 || b.PolicyVersion < 2 {
			if samePod || (item.RuntimeType == other.RuntimeType && (batch.Action == "reset" || b.Action == "reset" || unknownSameType)) {
				return false
			}
		} else if samePod || unknownSameType || (item.RuntimeType == other.RuntimeType && (batch.Action == "reset" || b.Action == "reset")) {
			count++
		}
	}
	return count < lifecyclePodLimit
}

func lifecycleHasCapacity(action string, active []models.InstanceLifecycleBatchItem, batches map[string]models.InstanceLifecycleBatch) bool {
	if len(active) >= lifecycleGlobalLimit || (action != "restart" && action != "reset") {
		return false
	}
	count := 0
	for _, item := range active {
		batch, ok := batches[item.BatchID]
		if !ok { // An unresolved reservation must never free capacity.
			return false
		}
		if batch.Action == action {
			count++
		}
	}
	return count < lifecycleActionLimit
}

// The singleton is locked only during short DB transactions, never while
// contacting Kubernetes/agents. All replicas share admission and reservations.
func lockLifecycleScheduler(ctx context.Context, tx db.Session) error {
	_, err := tx.SQL().ExecContext(ctx, "UPDATE instance_lifecycle_scheduler SET revision=revision+1 WHERE id=1")
	return err
}

func (r *NorthboundRepository) HasBatchReservation(id int) (bool, error) {
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_lifecycle_batch_items i JOIN northbound_operations o ON o.operation_id=i.operation_id COLLATE utf8mb4_unicode_ci JOIN instance_lifecycle_batches b ON b.batch_id=i.batch_id WHERE (i.state IN ('pending','active') OR (b.policy_version=2 AND b.status IN ('running','paused') AND b.retry_round=0 AND i.state IN ('failed','warning'))) AND (i.source_id=? OR o.instance_id=?)`, id, id)
	if err != nil {
		return false, err
	}
	var count int
	err = row.Scan(&count)
	return count > 0, err
}

// Close the check-then-act race without holding DB locks over runtime calls.
// A crashed direct mutation leaves a guard for inspection, never an unsafe retry.
func (r *NorthboundRepository) ReserveInstanceMutations(ids []int) (func(), error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := fmt.Sprintf("%x", b)
	err := r.sess.TxContext(context.Background(), func(tx db.Session) error {
		if err := lockLifecycleScheduler(context.Background(), tx); err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			reserved, err := lifecycleRetryReservation(tx, id)
			if err != nil {
				return err
			}
			if reserved {
				return fmt.Errorf("instance %d is awaiting batch retry", id)
			}
			count, err := tx.Collection("northbound_operations").Find(db.And(db.Cond{"status IN": []string{"queued", "processing", "batch_pending"}}, db.Or(db.Cond{"instance_id": id}, db.Cond{"request_payload": fmt.Sprintf(`{"instance_id":%d}`, id)}))).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("instance %d has an unfinished lifecycle operation", id)
			}
			if _, err := tx.SQL().Exec(`INSERT INTO instance_lifecycle_manual_guards (instance_id,token,created_at) VALUES (?,?,?)`, id, token, time.Now().UTC()); err != nil {
				return fmt.Errorf("instance %d is reserved by another mutation", id)
			}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return func() { _, _ = r.sess.SQL().Exec(`DELETE FROM instance_lifecycle_manual_guards WHERE token=?`, token) }, nil
}

func (r *NorthboundRepository) CreateLifecycleBatch(ctx context.Context, batch *models.InstanceLifecycleBatch, items []models.InstanceLifecycleBatchItem, ops []*models.NorthboundOperation, admin bool) error {
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockLifecycleScheduler(ctx, tx); err != nil {
			return err
		}
		var existing models.InstanceLifecycleBatch
		err := tx.Collection("instance_lifecycle_batches").Find(db.Cond{"batch_id": batch.BatchID}).One(&existing)
		if err == nil {
			if existing.UserID != batch.UserID || existing.RequestHash != batch.RequestHash {
				return fmt.Errorf("batch request key conflict")
			}
			return nil
		}
		if err != db.ErrNoMoreRows {
			return err
		}
		for i := range items {
			retryReserved, err := lifecycleRetryReservation(tx, items[i].SourceID)
			if err != nil {
				return err
			}
			if retryReserved {
				return fmt.Errorf("instance %d is awaiting batch retry", items[i].SourceID)
			}
			reserved, err := tx.Collection("instance_lifecycle_manual_guards").Find(db.Cond{"instance_id": items[i].SourceID}).Count()
			if err != nil {
				return err
			}
			if reserved > 0 {
				return fmt.Errorf("instance %d has a direct mutation in progress", items[i].SourceID)
			}
			var instance models.Instance
			if err := tx.Collection("instances").Find(db.Cond{"id": items[i].SourceID}).One(&instance); err != nil {
				return err
			}
			if !admin && instance.UserID != batch.UserID {
				return fmt.Errorf("access denied")
			}
			if instance.InstanceMode != "lite" || (instance.Status != "running" && instance.Status != "error" && !(batch.Action == "reset" && instance.Status == "stopped")) || strings.HasPrefix(instance.Name, "cleanup-pending-") || strings.HasPrefix(instance.Name, "reset-") {
				return fmt.Errorf("instance %d cannot be operated in this batch", instance.ID)
			}
			count, err := tx.Collection("team_members").Find(db.Cond{"instance_id": instance.ID}).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("team instance %d is excluded", instance.ID)
			}
			count, err = tx.Collection("northbound_operations").Find(db.And(db.Cond{"status IN": []string{"queued", "processing", "batch_pending"}}, db.Or(db.Cond{"instance_id": instance.ID}, db.Cond{"request_payload": fmt.Sprintf(`{"instance_id":%d}`, instance.ID)}))).Count()
			if err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("instance %d already has an operation", instance.ID)
			}
			ops[i].UserID = instance.UserID
			if _, err := tx.Collection("northbound_operations").Insert(ops[i]); err != nil {
				return err
			}
			if _, err := tx.Collection("instance_lifecycle_batch_items").Insert(&items[i]); err != nil {
				return err
			}
		}
		_, err = tx.Collection("instance_lifecycle_batches").Insert(batch)
		return err
	}, nil)
}

func (r *NorthboundRepository) ListLifecycleBatches(userID int) ([]models.InstanceLifecycleBatch, error) {
	items := []models.InstanceLifecycleBatch{}
	err := r.sess.Collection("instance_lifecycle_batches").Find(db.Cond{"user_id": userID}).OrderBy("-created_at").Limit(20).All(&items)
	return items, err
}

func (r *NorthboundRepository) GetLifecycleBatch(id string, userID int) (*models.InstanceLifecycleBatch, error) {
	var item models.InstanceLifecycleBatch
	err := r.sess.Collection("instance_lifecycle_batches").Find(db.Cond{"batch_id": id, "user_id": userID}).One(&item)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *NorthboundRepository) LifecycleBatchItems(id string) ([]models.InstanceLifecycleBatchItem, error) {
	items := []models.InstanceLifecycleBatchItem{}
	err := r.sess.Collection("instance_lifecycle_batch_items").Find(db.Cond{"batch_id": id}).OrderBy("id").All(&items)
	return items, err
}

func (r *NorthboundRepository) LifecycleBatchOperations(ids []string) ([]models.NorthboundOperation, error) {
	ops := []models.NorthboundOperation{}
	if len(ids) == 0 {
		return ops, nil
	}
	err := r.sess.Collection("northbound_operations").Find(db.Cond{"operation_id IN": ids}).All(&ops)
	return ops, err
}

func (r *NorthboundRepository) ControlLifecycleBatch(ctx context.Context, id string, userID int, action string) error {
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockLifecycleScheduler(ctx, tx); err != nil {
			return err
		}
		var b models.InstanceLifecycleBatch
		if err := tx.Collection("instance_lifecycle_batches").Find(db.Cond{"batch_id": id, "user_id": userID}).One(&b); err != nil {
			return err
		}
		if b.Status != "running" && b.Status != "paused" {
			return fmt.Errorf("batch is no longer controllable")
		}
		status := "paused"
		switch action {
		case "resume":
			status = "running"
		case "pause":
		case "cancel":
			status = "cancelling"
		default:
			return fmt.Errorf("invalid batch control")
		}
		if action == "cancel" {
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE northbound_operations o JOIN instance_lifecycle_batch_items i ON i.operation_id COLLATE utf8mb4_unicode_ci=o.operation_id SET o.status='failed',o.error_code='BATCH_CANCELLED',o.error_message='Cancelled before execution',o.finished_at=?,o.updated_at=? WHERE i.batch_id=? AND i.state='pending' AND o.status='batch_pending'`, time.Now().UTC(), time.Now().UTC(), id); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE instance_lifecycle_batch_items SET state='cancelled' WHERE batch_id=? AND state='pending'`, id); err != nil {
				return err
			}
		}
		return tx.Collection("instance_lifecycle_batches").Find(db.Cond{"batch_id": id}).Update(map[string]any{"status": status, "updated_at": time.Now().UTC()})
	}, nil)
}

// AdvanceLifecycleBatches reserves entire lifecycles, including health waits
// and cleanup. Terminal warnings pause remaining work instead of hiding them.
func (r *NorthboundRepository) AdvanceLifecycleBatches(ctx context.Context) error {
	count, err := r.sess.Collection("instance_lifecycle_batches").Find(db.Cond{"status IN": []string{"running", "paused", "cancelling"}}).Count()
	if err != nil || count == 0 {
		return err
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := lockLifecycleScheduler(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE instance_lifecycle_batches b JOIN instance_lifecycle_batch_items i ON i.batch_id=b.batch_id JOIN northbound_operations o ON o.operation_id=i.operation_id COLLATE utf8mb4_unicode_ci SET b.status='paused',b.updated_at=? WHERE b.status='running' AND b.policy_version < 2 AND i.state='active' AND (o.status='failed' OR (o.status='succeeded' AND o.error_code IS NOT NULL))`, time.Now().UTC()); err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE instance_lifecycle_batch_items i JOIN northbound_operations o ON o.operation_id=i.operation_id COLLATE utf8mb4_unicode_ci SET i.state=CASE WHEN o.status='succeeded' AND o.error_code IS NOT NULL THEN 'warning' ELSE o.status END WHERE i.state='active' AND o.status IN ('succeeded','failed')`); err != nil {
			return err
		}
		if err := prepareLifecycleSecondRound(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE instance_lifecycle_batches b SET b.status=CASE WHEN b.status='cancelling' THEN 'cancelled' WHEN EXISTS (SELECT 1 FROM instance_lifecycle_batch_items i WHERE i.batch_id=b.batch_id AND i.state IN ('failed','warning')) THEN 'completed_with_errors' ELSE 'completed' END,b.updated_at=? WHERE b.status IN ('running','paused','cancelling') AND NOT (b.policy_version=2 AND b.status='paused') AND NOT EXISTS (SELECT 1 FROM instance_lifecycle_batch_items i WHERE i.batch_id=b.batch_id AND i.state IN ('pending','active'))`, time.Now().UTC()); err != nil {
			return err
		}
		var batches []models.InstanceLifecycleBatch
		if err := tx.Collection("instance_lifecycle_batches").Find(db.Cond{"status IN": []string{"running", "paused", "cancelling"}}).All(&batches); err != nil {
			return err
		}
		byID := map[string]models.InstanceLifecycleBatch{}
		for _, b := range batches {
			byID[b.BatchID] = b
		}
		var active, pending []models.InstanceLifecycleBatchItem
		if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"state": "active"}).All(&active); err != nil {
			return err
		}
		if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"state": "pending"}).OrderBy("id").All(&pending); err != nil {
			return err
		}
		if len(pending) == 0 {
			return nil
		}
		ids := make([]int, 0, len(pending))
		for _, i := range pending {
			ids = append(ids, i.SourceID)
		}
		var bindings []models.InstanceRuntimeBinding
		if err := tx.Collection("instance_runtime_bindings").Find(db.Cond{"instance_id IN": ids}).All(&bindings); err != nil {
			return err
		}
		pods := map[int]int64{}
		for _, b := range bindings {
			pods[b.InstanceID] = b.RuntimePodID
		}
		batchIDs := []string{}
		for id := range byID {
			batchIDs = append(batchIDs, id)
		}
		var successes []models.InstanceLifecycleBatchItem
		if len(batchIDs) > 0 {
			if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"batch_id IN": batchIDs, "state": "succeeded"}).All(&successes); err != nil {
				return err
			}
		}
		passedTypes := map[string]bool{}
		for _, i := range successes {
			passedTypes[i.BatchID+":"+i.RuntimeType] = true
		}
		for _, item := range pending {
			b, ok := byID[item.BatchID]
			if !ok || b.Status != "running" {
				continue
			}
			if len(active) >= lifecycleGlobalLimit {
				break
			}
			if !lifecycleHasCapacity(b.Action, active, byID) {
				continue
			}
			item.RuntimePodID = pods[item.SourceID]
			if !lifecyclePodHasCapacity(item, b, active, byID) {
				continue
			}
			blocked := false
			// One canary per runtime/action in a batch before widening.
			if b.PolicyVersion < 2 && !passedTypes[b.BatchID+":"+item.RuntimeType] {
				for _, a := range active {
					if a.BatchID == b.BatchID && a.RuntimeType == item.RuntimeType {
						blocked = true
					}
				}
			}
			if blocked {
				continue
			}
			if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"id": item.ID}).Update(map[string]any{"state": "active", "runtime_pod_id": item.RuntimePodID}); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE northbound_operations SET status='queued',available_at=?,updated_at=? WHERE operation_id=? AND status='batch_pending'`, time.Now().UTC(), time.Now().UTC(), item.OperationID); err != nil {
				return err
			}
			active = append(active, item)
		}
		return nil
	}, nil)
}
