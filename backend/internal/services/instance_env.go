package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"clawreef/internal/models"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalizeEnvironmentOverrides(overrides map[string]string) (map[string]string, error) {
	if len(overrides) == 0 {
		return nil, nil
	}

	normalized := make(map[string]string, len(overrides))
	for rawKey, value := range overrides {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, fmt.Errorf("environment variable name cannot be empty")
		}
		if !envNamePattern.MatchString(key) {
			return nil, fmt.Errorf("invalid environment variable name: %s", key)
		}
		if _, exists := normalized[key]; exists {
			return nil, fmt.Errorf("duplicate environment variable name: %s", key)
		}
		normalized[key] = value
	}

	return normalized, nil
}

func marshalEnvironmentOverrides(overrides map[string]string) (*string, error) {
	if len(overrides) == 0 {
		return nil, nil
	}

	raw, err := json.Marshal(overrides)
	if err != nil {
		return nil, fmt.Errorf("failed to encode environment overrides: %w", err)
	}

	encoded := string(raw)
	return &encoded, nil
}

func parseEnvironmentOverridesJSON(raw *string) (map[string]string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}

	var overrides map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(*raw)), &overrides); err != nil {
		return nil, fmt.Errorf("failed to decode environment overrides: %w", err)
	}

	normalized, err := normalizeEnvironmentOverrides(overrides)
	if err != nil {
		return nil, err
	}

	return normalized, nil
}

func buildInstancePodEnv(instance *models.Instance, runtimeEnv, gatewayEnv, agentEnv map[string]string) (map[string]string, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance is required")
	}

	overrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil {
		return nil, err
	}

	resolved := mergeEnvMaps(runtimeEnv, mergeEnvMaps(gatewayEnv, agentEnv))
	resolved = withInstanceProxyEnv(instance.Type, instance.ID, resolved)
	resolved = mergeEnvMaps(resolved, overrides)

	return resolved, nil
}
