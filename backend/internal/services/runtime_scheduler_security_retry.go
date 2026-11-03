package services

import (
	"clawreef/internal/models"
	"context"
	"fmt"
)

// A protected slot rereads SQL before publishing credentials. Recoverable
// scheduling failures must first regain a durable creating intent. The CAS
// refuses concurrent stop/restart and any binding; pending reservations remain
// held until the existing authoritative report reconciliation releases them.
func (s *RuntimeScheduler) retryProtectedSchedulingError(ctx context.Context, instance models.Instance) (models.Instance, bool, error) {
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || client.security == nil || !isRuntimeErrorInstance(instance) {
		return instance, true, nil
	}
	if !isRecoverableRuntimeSchedulingError(instance) {
		return instance, false, nil
	}
	repo, ok := s.instanceRepo.(interface {
		RetryRuntimeSchedulingError(context.Context, models.Instance) (bool, error)
	})
	if !ok {
		return instance, false, fmt.Errorf("protected scheduling retry requires conditional persistence")
	}
	changed, err := repo.RetryRuntimeSchedulingError(ctx, instance)
	if err != nil || !changed {
		return instance, false, err
	}
	instance.Status = "creating"
	instance.RuntimeErrorCode = nil
	instance.RuntimeErrorMessage = nil
	return instance, true, nil
}
