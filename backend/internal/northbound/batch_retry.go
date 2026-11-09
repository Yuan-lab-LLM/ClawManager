package northbound

import (
	"clawreef/internal/models"
	"context"
	"time"
)

// Returns true when reconciliation consumed the attempt. No untrusted request
// may opt into retry behaviour: both operation IDs must be in the attempt ledger.
func (w *OperationWorker) reconcileBatchRetry(ctx context.Context, item *models.NorthboundOperation, request InstanceLifecycleRequest) bool {
	valid, err := w.service.repo.ValidLifecycleRetry(item.OperationID, request.PreviousOperationID)
	fail := func(message string) bool {
		_ = w.service.repo.MarkOperationFailed(ctx, item.OperationID, "BATCH_RETRY_UNSAFE", message, time.Now().UTC())
		return true
	}
	if err != nil || !valid {
		return fail("Unable to verify retry provenance")
	}
	previous, err := w.service.repo.GetOperationByID(request.PreviousOperationID)
	if err != nil || previous == nil {
		return fail("Previous attempt is unavailable")
	}
	if isResetOperation(item.OperationType) {
		reconciler, ok := w.service.instances.(interface {
			ReconcileResetRetry(int, string) (*models.Instance, error)
		})
		if !ok {
			return fail("Safe replacement reconciliation is unavailable")
		}
		var replacement *models.Instance
		err = w.runLifecycleActionWithLease(ctx, item, func() error {
			var e error
			replacement, e = reconciler.ReconcileResetRetry(request.InstanceID, previous.OperationID)
			return e
		})
		if err != nil {
			return fail("Replacement state could not be safely reconciled; no new reset was started")
		}
		if replacement != nil {
			now := time.Now().UTC()
			if err := w.service.repo.RequeueReplacementOperation(ctx, item.OperationID, replacement.ID, lifecycleReplacementWaiting, "Resuming existing replacement; never resetting the new instance", now, now); err != nil {
				return true
			}
			return true
		}
		return false
	}
	current, err := w.service.instances.GetByID(request.InstanceID)
	if err != nil || current == nil {
		return fail("Unable to reconcile restart target")
	}
	if current.Status == "running" {
		_ = w.service.repo.MarkOperationSucceeded(ctx, item.OperationID, current.ID, time.Now().UTC())
		return true
	}
	if current.Status != "error" && current.Status != "stopped" {
		return fail("Restart is still in progress; no duplicate restart was started")
	}
	return false
}
