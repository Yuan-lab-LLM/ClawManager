package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type runtimeRecoverySourceKey struct{}
type runtimeRecoveryPlacementSource struct {
	podID       int64
	gatewayPort int
}

// runtimeRecoverySource lets ordinary placement prefer a different slot after
// a confirmed source stop. It is only a placement hint, never stop authority.
func runtimeRecoverySource(ctx context.Context) (int64, int) {
	source, _ := ctx.Value(runtimeRecoverySourceKey{}).(runtimeRecoveryPlacementSource)
	return source.podID, source.gatewayPort
}

// reconcileInstanceRecoveryLocked is called with the elected scheduler's
// distributed lifecycle lock held. handled prevents legacy scheduling retries
// from bypassing persisted backoff or a permanent configuration failure.
func (s *RuntimeScheduler) reconcileInstanceRecoveryLocked(ctx context.Context, instance models.Instance, now time.Time) (bool, []error) {
	store, ok := s.instanceRepo.(repository.InstanceRuntimeRecoveryRepository)
	if !ok || !isSchedulerManagedV2Instance(instance) {
		return false, nil
	}
	if instance.Status != "creating" && instance.Status != "running" && instance.Status != "error" {
		return true, nil
	}
	state, err := store.GetRuntimeRecovery(ctx, instance.ID)
	if err != nil {
		return true, []error{err}
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return true, []error{err}
	}
	if binding != nil && (binding.Generation != instance.RuntimeGeneration || binding.InstanceID != instance.ID) {
		return false, nil
	}
	if binding != nil && binding.State == RuntimeGatewayBindingRunning {
		if state == nil {
			return false, nil
		}
		window := s.heartbeatTimeout
		if window <= 0 {
			window = 90 * time.Second
		}
		fresh := binding.LastHealthAt != nil && !binding.LastHealthAt.After(now) && now.Sub(*binding.LastHealthAt) <= window
		// A manager outage or an unreadable worker breaks the continuity proof.
		if now.Sub(state.UpdatedAt) > window {
			state.HealthySince = nil
		}
		next := runtimeRecoveryHealthy(*state, now, fresh)
		next.UpdatedAt = now
		_, err = store.SaveRuntimeRecovery(ctx, instance, next)
		if err != nil {
			return true, []error{err}
		}
		return false, nil
	}
	if runtimeRecoveryPermanentFailure(instance, binding) {
		if state != nil {
			state.NextRetryAt = nil
			state.HealthySince = nil
			state.UpdatedAt = now
			if instance.RuntimeErrorMessage != nil {
				state.Reason = *instance.RuntimeErrorMessage
			}
			_, err = store.SaveRuntimeRecovery(ctx, instance, *state)
			if err != nil {
				return true, []error{err}
			}
		}
		return true, nil
	}
	reason, failed := runtimeRecoveryFailureReason(instance, binding)
	if !failed {
		// Generic startup failure can conceal credentials or configuration.
		// Require an actionable reported crash before entering automatic retry.
		if state == nil && instance.RuntimeErrorCode != nil && *instance.RuntimeErrorCode == "GATEWAY_START_FAILED" {
			return true, nil
		}
		// Resume an interrupted recovery or a failed replacement only when a prior
		// attempt is durable. An absent/stale heartbeat alone never creates intent.
		if binding != nil || state == nil || state.Attempts == 0 || (instance.Status != "creating" && instance.Status != "error") {
			return false, nil
		}
		reason = state.Reason
	}
	if state == nil {
		state = &models.InstanceRuntimeRecovery{InstanceID: instance.ID}
	}
	if binding != nil {
		state.SourcePodID = binding.RuntimePodID
		state.SourceGatewayPort = binding.GatewayPort
	}
	next, due := runtimeRecoveryFailure(*state, now, reason)
	next.Generation = instance.RuntimeGeneration
	next.UpdatedAt = now
	if due {
		next.Attempts++
		next.NextRetryAt = nil
		if next.Attempts < len(runtimeRecoveryBackoff) {
			deadline := now.Add(runtimeRecoveryBackoff[next.Attempts])
			next.NextRetryAt = &deadline
		}
	}
	saved, err := store.SaveRuntimeRecovery(ctx, instance, next)
	if err != nil {
		return true, []error{err}
	}
	if !saved || !due {
		return true, nil
	}
	if binding != nil {
		if err = s.confirmRecoverySourceStopped(ctx, instance, binding); err != nil {
			return true, []error{err}
		}
	}
	// Recheck the exact status and generation after stop. No workspace content is
	// touched; a committed manual stop/sleep/delete cannot become creating here.
	advanced, err := store.AdvanceRuntimeRecovery(ctx, instance)
	if err != nil {
		return true, []error{err}
	}
	if !advanced {
		return true, nil
	}
	current, err := s.instanceRepo.GetByID(instance.ID)
	if err != nil {
		return true, []error{err}
	}
	if current == nil || current.Status != "creating" || current.RuntimeGeneration != instance.RuntimeGeneration+1 {
		return true, nil
	}
	placement := context.WithValue(ctx, runtimeRecoverySourceKey{}, runtimeRecoveryPlacementSource{next.SourcePodID, next.SourceGatewayPort})
	if err = s.assignInstance(placement, *current); err != nil && !isRuntimeAssignmentPending(err) {
		errs := []error{fmt.Errorf("recover instance %d attempt %d: %w", instance.ID, next.Attempts, err)}
		s.markInstanceError(ctx, *current, err, &errs)
		return true, errs
	}
	return true, nil
}

func (s *RuntimeScheduler) confirmRecoverySourceStopped(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding) error {
	if s.podRepo == nil || s.agentClient == nil || strings.TrimSpace(binding.GatewayID) == "" {
		return fmt.Errorf("recovery requires the exact source identity")
	}
	releaser, ok := s.bindingRepo.(repository.FailedGatewayBindingReleaser)
	if !ok {
		return fmt.Errorf("recovery requires conditional failed binding release")
	}
	pod, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return err
	}
	if pod == nil || strings.TrimSpace(stringValue(pod.PodUID)) == "" || strings.TrimSpace(stringValue(pod.AgentEndpoint)) == "" {
		return fmt.Errorf("recovery source Pod identity is unavailable")
	}
	if reader, ok := s.agentClient.(interface {
		GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
	}); ok {
		fresh, readErr := reader.GatewayState(ctx, strings.TrimSpace(*pod.AgentEndpoint), binding.GatewayID)
		if readErr != nil && !errors.Is(readErr, ErrRuntimeAgentNotFound) && !errors.Is(readErr, ErrRuntimeAgentUnsupported) {
			return fmt.Errorf("recovery source state is unreadable: %w", readErr)
		}
		if readErr == nil {
			if fresh == nil || fresh.InstanceID != instance.ID || fresh.Generation != binding.Generation || fresh.GatewayID != binding.GatewayID {
				return fmt.Errorf("recovery source state does not match the exact owner/generation")
			}
			switch strings.ToLower(strings.TrimSpace(fresh.State)) {
			case "error", "failed", "unhealthy", "stopped", "deleted":
			default:
				return fmt.Errorf("recovery source no longer reports a failed gateway")
			}
		}
	}
	// DeleteGateway is the existing assignment cancellation/reset protocol. A
	// 404, missing process, unreachable Pod or stale heartbeat is never a receipt.
	if err = s.agentClient.DeleteGateway(ctx, strings.TrimSpace(*pod.AgentEndpoint), binding.GatewayID); err != nil {
		return fmt.Errorf("recovery source stop is unconfirmed: %w", err)
	}
	var deleted bool
	if exact, ok := s.bindingRepo.(repository.ConfirmedRecoveryBindingReleaser); ok {
		deleted, err = exact.DeleteConfirmedRecoveryBinding(ctx, *binding)
	} else {
		deleted, err = releaser.DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx, instance.ID, binding.RuntimePodID, binding.Generation)
	}
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("recovery source binding changed after confirmed stop")
	}
	return nil
}
