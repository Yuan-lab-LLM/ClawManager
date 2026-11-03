package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func fmtLifecycleRetryHash(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func (r *NorthboundRepository) ReadyBatchOperationCount(now time.Time) (int, error) {
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*) FROM northbound_operations o JOIN instance_lifecycle_batch_items i ON i.operation_id COLLATE utf8mb4_unicode_ci=o.operation_id JOIN instance_lifecycle_batches b ON b.batch_id=i.batch_id WHERE b.policy_version=2 AND i.state='active' AND ((o.status='queued' AND o.available_at<=?) OR (o.status='processing' AND o.lease_expires_at<?))`, now, now)
	if err != nil {
		return 0, err
	}
	var n int
	err = row.Scan(&n)
	return n, err
}

func lifecycleRetryReservation(tx db.Session, id int) (bool, error) {
	row, err := tx.SQL().QueryRow(`SELECT COUNT(*) FROM instance_lifecycle_batch_items i JOIN instance_lifecycle_batches b ON b.batch_id=i.batch_id JOIN northbound_operations o ON o.operation_id=i.operation_id COLLATE utf8mb4_unicode_ci WHERE b.policy_version=2 AND b.status IN ('running','paused') AND b.retry_round=0 AND i.state IN ('failed','warning') AND (i.source_id=? OR o.instance_id=?)`, id, id)
	if err != nil {
		return false, err
	}
	var n int
	err = row.Scan(&n)
	return n > 0, err
}

func (r *NorthboundRepository) LifecycleAttemptHistory(itemID int64) ([]models.NorthboundOperation, error) {
	all, err := r.LifecycleAttemptHistories([]int64{itemID})
	return all[itemID], err
}

func (r *NorthboundRepository) LifecycleAttemptHistories(itemIDs []int64) (map[int64][]models.NorthboundOperation, error) {
	result := map[int64][]models.NorthboundOperation{}
	if len(itemIDs) == 0 {
		return result, nil
	}
	var ids []struct {
		ItemID      int64  `db:"item_id"`
		OperationID string `db:"operation_id"`
	}
	if err := r.sess.Collection("instance_lifecycle_batch_attempts").Find(db.Cond{"item_id IN": itemIDs}).OrderBy("attempt").All(&ids); err != nil {
		return nil, err
	}
	keys := []string{}
	for _, id := range ids {
		keys = append(keys, id.OperationID)
	}
	ops, err := r.LifecycleBatchOperations(keys)
	if err != nil {
		return nil, err
	}
	byID := map[string]models.NorthboundOperation{}
	for _, op := range ops {
		byID[op.OperationID] = op
	}
	for _, id := range ids {
		if op, ok := byID[id.OperationID]; ok {
			result[id.ItemID] = append(result[id.ItemID], op)
		}
	}
	return result, nil
}

// Only transient lifecycle failures are candidates. The worker reconciles the
// actual instance/staging state before performing any second destructive step.
func lifecycleRetryCandidate(action string, op models.NorthboundOperation) bool {
	if op.ErrorCode == nil {
		return false
	}
	code := *op.ErrorCode
	if action == "restart" {
		return code == "LIFECYCLE_FAILED" || code == "LIFECYCLE_TIMEOUT" || code == "DEPENDENCY_UNAVAILABLE"
	}
	switch code {
	case "RESET_REPLACEMENT_CREATE_FAILED", "RESET_REPLACEMENT_TRACKING_FAILED", "RESET_REPLACEMENT_FAILED", "RESET_REPLACEMENT_TIMEOUT", "RESET_CUTOVER_FAILED", "OLD_INSTANCE_CLEANUP_PENDING", "DEPENDENCY_UNAVAILABLE":
		return true
	}
	return false
}

func (r *NorthboundRepository) ValidLifecycleRetry(operationID, previousID string) (bool, error) {
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_lifecycle_batch_attempts a JOIN instance_lifecycle_batch_attempts p ON p.item_id=a.item_id AND p.attempt=1 WHERE a.attempt=2 AND a.operation_id=? AND p.operation_id=?`, operationID, previousID)
	if err != nil {
		return false, err
	}
	var count int
	err = row.Scan(&count)
	return count == 1, err
}

func (r *NorthboundRepository) PreviousLifecycleAttempt(operationID string) (string, error) {
	row, err := r.sess.SQL().QueryRow(`SELECT p.operation_id FROM instance_lifecycle_batch_attempts a JOIN instance_lifecycle_batch_attempts p ON p.item_id=a.item_id AND p.attempt=1 WHERE a.attempt=2 AND a.operation_id=?`, operationID)
	if err != nil {
		return "", err
	}
	var previous string
	err = row.Scan(&previous)
	return previous, err
}

// Called under the scheduler transaction/lock, after terminal state collection
// and before batch completion. Pause/cancel therefore cannot race retry dispatch.
func prepareLifecycleSecondRound(ctx context.Context, tx db.Session) error {
	var batches []models.InstanceLifecycleBatch
	if err := tx.Collection("instance_lifecycle_batches").Find(db.Cond{"status": "running", "policy_version": 2, "retry_round": 0}).All(&batches); err != nil {
		return err
	}
	for _, b := range batches {
		count, err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"batch_id": b.BatchID, "state IN": []string{"pending", "active"}}).Count()
		if err != nil {
			return err
		}
		if count != 0 {
			continue
		}
		var items []models.InstanceLifecycleBatchItem
		if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"batch_id": b.BatchID, "state IN": []string{"failed", "warning"}}).All(&items); err != nil {
			return err
		}
		retried := false
		for _, item := range items {
			var op models.NorthboundOperation
			if err := tx.Collection("northbound_operations").Find(db.Cond{"operation_id": item.OperationID}).One(&op); err != nil {
				return err
			}
			if !lifecycleRetryCandidate(b.Action, op) {
				continue
			}
			retried = true
			if _, err := tx.SQL().ExecContext(ctx, `INSERT INTO instance_lifecycle_batch_attempts (item_id,attempt,operation_id) VALUES (?,1,?)`, item.ID, op.OperationID); err != nil {
				return err
			}
			previous := op.OperationID
			// Keep the canonical source-only payload: existing lifecycle guards
			// match it after operation.instance_id switches to the replacement.
			body := fmt.Sprintf(`{"instance_id":%d}`, item.SourceID)
			op.ID = 0
			op.OperationID = previous + "_retry"
			op.RequestPayload = body
			op.Status = "batch_pending"
			op.AttemptCount = 0
			op.StartedAt = nil
			op.FinishedAt = nil
			op.LeaseOwner = nil
			op.LeaseExpiresAt = nil
			op.ErrorCode = nil
			op.ErrorMessage = nil
			op.InstanceID = &item.SourceID
			op.CreatedAt = time.Now().UTC()
			op.UpdatedAt = op.CreatedAt
			op.AvailableAt = op.CreatedAt
			// Existing unique (user,type,key) must not alias the first attempt.
			op.IdempotencyKeyHash = fmtLifecycleRetryHash(op.OperationID)
			op.RequestHash = fmtLifecycleRetryHash(body)
			if _, err := tx.Collection("northbound_operations").Insert(&op); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(ctx, `INSERT INTO instance_lifecycle_batch_attempts (item_id,attempt,operation_id) VALUES (?,2,?)`, item.ID, op.OperationID); err != nil {
				return err
			}
			if err := tx.Collection("instance_lifecycle_batch_items").Find(db.Cond{"id": item.ID}).Update(map[string]any{"operation_id": op.OperationID, "state": "pending"}); err != nil {
				return err
			}
		}
		if !retried {
			continue
		}
		if err := tx.Collection("instance_lifecycle_batches").Find(db.Cond{"batch_id": b.BatchID}).Update(map[string]any{"retry_round": 1, "updated_at": time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}
