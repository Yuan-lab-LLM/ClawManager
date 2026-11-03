package services

import (
	"context"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

// validateInstanceStartConfiguration shares the actual configuration builders
// but operates on a copy. It must precede fencing, Stop, binding cleanup and
// persisting edited environment overrides. Placeholder credentials are never
// stored or sent to a runtime; actual token provisioning stays in Start.
func (s *instanceService) validateInstanceStartConfiguration(ctx context.Context, instance *models.Instance) error {
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := s.enforceInstanceModeLimits(ctx, modeForExistingInstance(instance), instance.CPUCores, instance.MemoryGB, instance.DiskGB, instance.GPUCount); err != nil {
		return err
	}
	if !supportsManagedRuntimeIntegrationForInstance(instance) {
		return nil
	}
	prepared := *instance
	placeholder := "preflight-only-not-a-credential"
	if prepared.AccessToken == nil || strings.TrimSpace(*prepared.AccessToken) == "" {
		prepared.AccessToken = &placeholder
	}
	env, err := s.buildGatewayEnvReadOnly(&prepared)
	if err != nil {
		return fmt.Errorf("validate instance gateway configuration: %w", err)
	}
	bootstrap, err := s.runtimeBootstrapEnv(&prepared)
	if err != nil {
		return fmt.Errorf("validate instance bootstrap configuration: %w", err)
	}
	if _, enabled := defaultAgentControlBaseURL(); enabled {
		if prepared.AgentBootstrapToken == nil || strings.TrimSpace(*prepared.AgentBootstrapToken) == "" {
			prepared.AgentBootstrapToken = &placeholder
		}
		agent, err := s.buildAgentEnv(&prepared)
		if err != nil {
			return err
		}
		env = mergeEnvMaps(env, agent)
	}
	_, err = buildInstanceGatewayEnv(&prepared, mergeEnvMaps(env, bootstrap))
	return err
}
