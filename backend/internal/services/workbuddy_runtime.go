package services

import (
	"os"
	"strings"

	"clawreef/internal/models"
)

const (
	WorkbuddyRuntimeLinux   = "linux"
	WorkbuddyRuntimeWindows = "windows"

	defaultLinuxWorkbuddyImage = "ghcr.io/yuan-lab-llm/agentsruntime/workbuddy-linux:latest"
	defaultLinuxCodexImage     = "ghcr.io/yuan-lab-llm/agentsruntime/codex:latest"
)

// LinuxWorkbuddyImage returns the managed Linux WorkBuddy image. Northbound
// provisioning passes it explicitly so a Windows system-image default cannot
// override the Linux-only API contract.
func LinuxWorkbuddyImage() string {
	if image := strings.TrimSpace(os.Getenv("CLAWMANAGER_WORKBUDDY_LINUX_IMAGE")); image != "" {
		return image
	}
	return defaultLinuxWorkbuddyImage
}

func normalizeWorkbuddyRuntimeVariant(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WorkbuddyRuntimeLinux:
		return WorkbuddyRuntimeLinux
	case WorkbuddyRuntimeWindows:
		return WorkbuddyRuntimeWindows
	default:
		return ""
	}
}

func inferWorkbuddyVariantFromImage(image string) string {
	normalized := strings.ToLower(strings.TrimSpace(image))
	switch {
	case strings.Contains(normalized, "workbuddy-linux"):
		return WorkbuddyRuntimeLinux
	case strings.Contains(normalized, "windows-vm-workbuddy"), strings.Contains(normalized, "dockur/windows"):
		return WorkbuddyRuntimeWindows
	default:
		return ""
	}
}

func inferCodexVariantFromImage(image string) string {
	normalized := strings.ToLower(strings.TrimSpace(image))
	switch {
	case strings.Contains(normalized, "windows-vm-codex"), strings.Contains(normalized, "dockur/windows"):
		return WorkbuddyRuntimeWindows
	case strings.Contains(normalized, "agentsruntime/codex"), strings.Contains(normalized, "/codex:"):
		return WorkbuddyRuntimeLinux
	default:
		return ""
	}
}

func resolveWorkbuddyRuntimeVariantForRequest(req CreateInstanceRequest) string {
	if !strings.EqualFold(strings.TrimSpace(req.Type), "workbuddy") {
		return ""
	}
	if variant := normalizeWorkbuddyRuntimeVariant(req.RuntimeVariant); variant != "" {
		return variant
	}
	if req.ImageRegistry != nil {
		if selection, ok := runtimeImageOverrideForImage(req.Type, *req.ImageRegistry); ok {
			if variant := normalizeWorkbuddyRuntimeVariant(selection.RuntimeVariant); variant != "" {
				return variant
			}
		}
		if variant := inferWorkbuddyVariantFromImage(*req.ImageRegistry); variant != "" {
			return variant
		}
	}
	if selection, ok := runtimeImageOverride(req.Type); ok {
		if variant := normalizeWorkbuddyRuntimeVariant(selection.RuntimeVariant); variant != "" {
			return variant
		}
		if variant := inferWorkbuddyVariantFromImage(selection.Image); variant != "" {
			return variant
		}
	}
	return WorkbuddyRuntimeWindows
}

func resolveCodexRuntimeVariantForRequest(req CreateInstanceRequest) string {
	if !strings.EqualFold(strings.TrimSpace(req.Type), RuntimeTypeCodex) {
		return ""
	}
	if variant := normalizeWorkbuddyRuntimeVariant(req.RuntimeVariant); variant != "" {
		return variant
	}
	if req.ImageRegistry != nil {
		if selection, ok := runtimeImageOverrideForImage(req.Type, *req.ImageRegistry); ok {
			if variant := normalizeWorkbuddyRuntimeVariant(selection.RuntimeVariant); variant != "" {
				return variant
			}
		}
		if variant := inferCodexVariantFromImage(*req.ImageRegistry); variant != "" {
			return variant
		}
	}
	if selection, ok := runtimeImageOverride(req.Type); ok {
		if variant := normalizeWorkbuddyRuntimeVariant(selection.RuntimeVariant); variant != "" {
			return variant
		}
		if variant := inferCodexVariantFromImage(selection.Image); variant != "" {
			return variant
		}
	}
	return WorkbuddyRuntimeWindows
}

func resolveManagedRuntimeVariantForRequest(req CreateInstanceRequest) string {
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "workbuddy":
		return resolveWorkbuddyRuntimeVariantForRequest(req)
	case RuntimeTypeCodex:
		return resolveCodexRuntimeVariantForRequest(req)
	default:
		return ""
	}
}

func workbuddyRuntimeVariantForInstance(instance *models.Instance) string {
	if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Type), "workbuddy") {
		return ""
	}
	if variant := normalizeWorkbuddyRuntimeVariant(instance.RuntimeVariant); variant != "" {
		return variant
	}
	if strings.EqualFold(strings.TrimSpace(instance.MountPath), "/config") {
		return WorkbuddyRuntimeLinux
	}
	if strings.EqualFold(strings.TrimSpace(instance.MountPath), "/storage") {
		return WorkbuddyRuntimeWindows
	}
	if instance.ImageRegistry != nil {
		if variant := inferWorkbuddyVariantFromImage(*instance.ImageRegistry); variant != "" {
			return variant
		}
	}
	if instance.PVCName != nil && strings.TrimSpace(*instance.PVCName) != "" {
		return WorkbuddyRuntimeWindows
	}
	return WorkbuddyRuntimeLinux
}

func isWindowsWorkbuddyInstance(instance *models.Instance) bool {
	return workbuddyRuntimeVariantForInstance(instance) == WorkbuddyRuntimeWindows
}

func isWindowsCodexInstance(instance *models.Instance) bool {
	return codexRuntimeVariantForInstance(instance) == WorkbuddyRuntimeWindows
}

func isWindowsVMInstance(instance *models.Instance) bool {
	return isWindowsWorkbuddyInstance(instance) || isWindowsCodexInstance(instance)
}

func isLinuxWorkbuddyInstance(instance *models.Instance) bool {
	return workbuddyRuntimeVariantForInstance(instance) == WorkbuddyRuntimeLinux
}

func codexRuntimeVariantForInstance(instance *models.Instance) string {
	if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeCodex) {
		return ""
	}
	if variant := normalizeWorkbuddyRuntimeVariant(instance.RuntimeVariant); variant != "" {
		return variant
	}
	if strings.EqualFold(strings.TrimSpace(instance.MountPath), "/config") {
		return WorkbuddyRuntimeLinux
	}
	if strings.EqualFold(strings.TrimSpace(instance.MountPath), "/storage") {
		return WorkbuddyRuntimeWindows
	}
	if instance.ImageRegistry != nil {
		if variant := inferCodexVariantFromImage(*instance.ImageRegistry); variant != "" {
			return variant
		}
	}
	return WorkbuddyRuntimeWindows
}

func isLinuxCodexInstance(instance *models.Instance) bool {
	return codexRuntimeVariantForInstance(instance) == WorkbuddyRuntimeLinux
}

func buildRuntimeConfigForInstance(instance *models.Instance) InstanceRuntimeConfig {
	if instance == nil {
		return InstanceRuntimeConfig{Port: 3001, MountPath: "/config", Env: map[string]string{}}
	}
	config := buildRuntimeConfig(instance.Type, instance.OSType, instance.OSVersion, instance.ImageRegistry, instance.ImageTag)
	return applyManagedRuntimeVariant(config, instance.Type, managedRuntimeVariantForInstance(instance), instance.ImageRegistry == nil)
}

func managedRuntimeVariantForInstance(instance *models.Instance) string {
	if instance == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(instance.Type)) {
	case "workbuddy":
		return workbuddyRuntimeVariantForInstance(instance)
	case RuntimeTypeCodex:
		return codexRuntimeVariantForInstance(instance)
	default:
		return ""
	}
}

func applyManagedRuntimeVariant(config InstanceRuntimeConfig, instanceType, variant string, useVariantDefaultImage bool) InstanceRuntimeConfig {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "workbuddy":
		return applyWorkbuddyRuntimeVariant(config, variant, useVariantDefaultImage)
	case RuntimeTypeCodex:
		return applyCodexRuntimeVariant(config, variant, useVariantDefaultImage)
	default:
		return config
	}
}

func applyWorkbuddyRuntimeVariant(config InstanceRuntimeConfig, variant string, useVariantDefaultImage bool) InstanceRuntimeConfig {
	switch normalizeWorkbuddyRuntimeVariant(variant) {
	case WorkbuddyRuntimeLinux:
		config.Port = 3001
		config.MountPath = "/config"
		config.Env = defaultWebtopDesktopEnv("Workbuddy")
		if useVariantDefaultImage {
			config.Image = strings.TrimSpace(os.Getenv("CLAWMANAGER_WORKBUDDY_LINUX_IMAGE"))
			if config.Image == "" {
				config.Image = defaultLinuxWorkbuddyImage
			}
		}
	case WorkbuddyRuntimeWindows:
		config.Port = 8006
		config.MountPath = "/storage"
		config.Env = defaultWindowsWorkbuddyEnv()
	}
	return config
}

func applyCodexRuntimeVariant(config InstanceRuntimeConfig, variant string, useVariantDefaultImage bool) InstanceRuntimeConfig {
	switch normalizeWorkbuddyRuntimeVariant(variant) {
	case WorkbuddyRuntimeLinux:
		config.Port = 3001
		config.MountPath = "/config"
		config.Env = defaultWebtopDesktopEnv("Codex")
		config.Env["CODEX_HOME"] = "/config/.codex"
		config.Env["CLAWMANAGER_PROJECT_PATH"] = "/config/workspace"
		if useVariantDefaultImage {
			config.Image = strings.TrimSpace(os.Getenv("CLAWMANAGER_CODEX_LINUX_IMAGE"))
			if config.Image == "" {
				config.Image = defaultLinuxCodexImage
			}
		}
	case WorkbuddyRuntimeWindows:
		config.Port = 8006
		config.MountPath = "/storage"
		config.Env = defaultWindowsCodexEnv()
	}
	return config
}

func isWebtopRuntimeInstance(instance *models.Instance) bool {
	return instance != nil && !isWindowsVMInstance(instance) && (usesWebtopImage(instance.Type) || isLinuxWorkbuddyInstance(instance) || isLinuxCodexInstance(instance))
}

func supportsManagedRuntimeIntegrationForInstance(instance *models.Instance) bool {
	if isWindowsVMInstance(instance) {
		return false
	}
	return instance != nil && (supportsManagedRuntimeIntegration(instance.Type) || isLinuxWorkbuddyInstance(instance) || isLinuxCodexInstance(instance))
}

func supportsRuntimeConfigInjectionForInstance(instance *models.Instance) bool {
	if isWindowsVMInstance(instance) {
		return false
	}
	return instance != nil && (supportsRuntimeConfigInjection(instance.Type) || isLinuxWorkbuddyInstance(instance) || isLinuxCodexInstance(instance))
}

// IsWindowsVMRuntimeInstance reports whether the instance uses a Windows VM disk instead of a Linux /config workspace.
func IsWindowsVMRuntimeInstance(instance *models.Instance) bool {
	return isWindowsVMInstance(instance)
}
