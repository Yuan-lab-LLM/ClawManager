package services

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"clawreef/internal/models"
)

var protectedManagedRuntimeEnvKeys = map[string]struct{}{
	"CLAWMANAGER_LLM_BASE_URL":   {},
	"CLAWMANAGER_LLM_API_KEY":    {},
	"CLAWMANAGER_LLM_MODEL":      {},
	"CLAWMANAGER_LLM_PROVIDER":   {},
	"CLAWMANAGER_INSTANCE_TOKEN": {},
	"OPENAI_BASE_URL":            {},
	"OPENAI_API_BASE":            {},
	"OPENAI_API_KEY":             {},
	"OPENAI_MODEL":               {},
}

func isLLMGovernanceStrictEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMANAGER_LLM_GOVERNANCE_STRICT"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func isInstanceNetworkLockEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMANAGER_INSTANCE_NETWORK_LOCK"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isProtectedManagedRuntimeEnvKey(key string) bool {
	_, ok := protectedManagedRuntimeEnvKeys[strings.ToUpper(strings.TrimSpace(key))]
	return ok
}

func validateManagedRuntimeEnvironmentOverrides(instanceType string, overrides map[string]string) error {
	if !supportsManagedRuntimeIntegration(instanceType) || !isLLMGovernanceStrictEnabled() {
		return nil
	}
	for key := range overrides {
		if isProtectedManagedRuntimeEnvKey(key) {
			return fmt.Errorf("environment override %s is managed by the platform", strings.ToUpper(strings.TrimSpace(key)))
		}
	}
	return nil
}

func applyProtectedManagedRuntimeEnv(target, protected map[string]string) map[string]string {
	if len(protected) == 0 {
		return target
	}
	if target == nil {
		target = map[string]string{}
	}
	for key, value := range protected {
		target[key] = value
	}
	return target
}

const (
	defaultInstanceSHMSizeGB = 1
	maxInstanceSHMSizeGB     = 8
)

// popSHMSizeGB removes SHM_SIZE_GB from extraEnv and returns /dev/shm size in GiB.
// Desktop runtime defaults scale with instance memory. SHM_SIZE_GB=0 disables
// the custom emptyDir /dev/shm mount.
// Values above maxInstanceSHMSizeGB are clamped to protect node memory.
func popSHMSizeGB(extraEnv map[string]string, runtimeType string, memoryGB int) int {
	shmSizeGB := defaultSHMSizeGB(runtimeType, memoryGB)
	if shmVal, ok := extraEnv["SHM_SIZE_GB"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(shmVal)); err == nil {
			if parsed == 0 {
				shmSizeGB = 0
			} else if parsed > 0 {
				shmSizeGB = parsed
				if shmSizeGB > maxInstanceSHMSizeGB {
					shmSizeGB = maxInstanceSHMSizeGB
				}
			}
		}
		delete(extraEnv, "SHM_SIZE_GB")
	}
	return shmSizeGB
}

func defaultSHMSizeGB(runtimeType string, memoryGB int) int {
	if normalizeInstanceRuntimeType(runtimeType) != "desktop" {
		return defaultInstanceSHMSizeGB
	}
	switch {
	case memoryGB >= 12:
		return 4
	case memoryGB >= 8:
		return 2
	default:
		return defaultInstanceSHMSizeGB
	}
}

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
	overrides = ensureDesktopStreamProfileEnv(overrides, instance.RuntimeType)

	resolved := mergeEnvMaps(runtimeEnv, mergeEnvMaps(gatewayEnv, agentEnv))
	resolved = withInstanceProxyEnv(instance.Type, instance.ID, resolved)
	resolved["CLAWMANAGER_RUNTIME_TYPE"] = normalizeInstanceRuntimeType(instance.RuntimeType)
	if normalizeInstanceRuntimeType(instance.RuntimeType) == "shell" {
		resolved["CLAWMANAGER_DESKTOP_ENABLED"] = "false"
		delete(resolved, "SUBFOLDER")
	}
	resolved = mergeEnvMaps(resolved, overrides)
	if supportsManagedRuntimeIntegration(instance.Type) && isLLMGovernanceStrictEnabled() {
		resolved = applyProtectedManagedRuntimeEnv(resolved, mergeEnvMaps(gatewayEnv, agentEnv))
	}

	return resolved, nil
}

func buildInstanceGatewayEnv(instance *models.Instance, gatewayEnv map[string]string) (map[string]string, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance is required")
	}

	overrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil {
		return nil, err
	}

	resolved := mergeEnvMaps(gatewayEnv, nil)
	resolved = withInstanceProxyEnv(instance.Type, instance.ID, resolved)
	resolved["CLAWMANAGER_RUNTIME_TYPE"] = normalizeInstanceRuntimeType(instance.RuntimeType)
	resolved = mergeEnvMaps(resolved, overrides)
	if supportsManagedRuntimeIntegration(instance.Type) && isLLMGovernanceStrictEnabled() {
		resolved = applyProtectedManagedRuntimeEnv(resolved, gatewayEnv)
	}

	return resolved, nil
}
