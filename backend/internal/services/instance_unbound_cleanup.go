package services

import (
	"context"
	"fmt"

	"clawreef/internal/models"
)

func (c *slotRuntimeAgentClient) VerifyFailedStartCleanup(ctx context.Context, instance *models.Instance) error {
	if c.assignments == nil || instance == nil || instance.WorkspacePath == nil {
		return fmt.Errorf("failed Start identity is unavailable")
	}
	return c.assignments.CheckFailedStartStopped(ctx, instance.ID, instance.UserID, instance.RuntimeGeneration, instance.Type, *instance.WorkspacePath)
}

func (s *instanceService) verifyUnboundGatewayCleanup(ctx context.Context, instance *models.Instance) error {
	verifier, ok := s.agentClient.(interface {
		VerifyUnboundGatewayCleanup(context.Context, *models.Instance, []models.RuntimeCleanupSource) error
	})
	if !ok {
		return fmt.Errorf("unbound Runtime cleanup stop-proof capability is unavailable")
	}

	history, ok := s.instanceRepo.(interface {
		RuntimeCleanupHistory(context.Context, *models.Instance) ([]models.RuntimeCleanupSource, error)
	})
	if !ok {
		return fmt.Errorf("unbound Runtime cleanup publication history is unavailable")
	}
	sources, err := history.RuntimeCleanupHistory(ctx, instance)
	if err != nil {
		return err
	}
	return verifier.VerifyUnboundGatewayCleanup(ctx, instance, sources)
}

func (c *slotRuntimeAgentClient) VerifyUnboundGatewayCleanup(ctx context.Context, instance *models.Instance, sources []models.RuntimeCleanupSource) error {
	if c.assignments == nil || instance == nil {
		return fmt.Errorf("unbound Runtime assignment inventory is unavailable")
	}
	return c.assignments.CheckUnboundInstanceStopped(ctx, instance.ID, instance.UserID, instance.RuntimeGeneration, instance.Type, sources)
}

// Resource teardown must not erase publication evidence before the no-binding
// branch is verified. The common cleanup repeats the proof at the release point.
func (s *instanceService) preflightUnboundGatewayCleanup(ctx context.Context, instance *models.Instance) error {
	if _, ok := v2RuntimeTypeForInstance(instance); !ok {
		return nil
	}

	if err := s.requireGatewayCleanupProofCapabilities(); err != nil {
		return err
	}
	if s.bindingRepo == nil {
		return fmt.Errorf("Runtime binding inventory is unavailable for cleanup")
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return err
	}
	if binding == nil {
		return s.verifyUnboundGatewayCleanup(ctx, instance)
	}
	return nil
}

func (s *instanceService) requireGatewayCleanupProofCapabilities() error {
	if _, ok := s.agentClient.(interface {
		VerifyUnboundGatewayCleanup(context.Context, *models.Instance, []models.RuntimeCleanupSource) error
	}); !ok {
		return fmt.Errorf("Runtime cleanup stop-proof capability is unavailable")
	}
	if _, ok := s.instanceRepo.(interface {
		RuntimeCleanupHistory(context.Context, *models.Instance) ([]models.RuntimeCleanupSource, error)
	}); !ok {
		return fmt.Errorf("Runtime cleanup publication history is unavailable")
	}
	return nil
}
