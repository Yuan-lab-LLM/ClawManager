package services

import (
	"testing"

	"clawreef/internal/models"
)

type runtimeImageProviderStub struct {
	config RuntimeImageConfig
	ok     bool
}

func (s runtimeImageProviderStub) GetRuntimeImage(string) (RuntimeImageConfig, bool) {
	return s.config, s.ok
}

func (s runtimeImageProviderStub) GetRuntimeImageForRuntimeType(string, string) (RuntimeImageConfig, bool) {
	return s.config, s.ok
}

func (s runtimeImageProviderStub) GetRuntimeImageForImage(string, string) (RuntimeImageConfig, bool) {
	return s.config, s.ok
}

func TestBuildRuntimeConfigForInstancePreservesLinuxWorkbuddy(t *testing.T) {
	image := "10.130.14.23:5000/workbuddy-linux:2026.8.1"
	instance := &models.Instance{
		Type:           "workbuddy",
		RuntimeVariant: WorkbuddyRuntimeLinux,
		ImageRegistry:  &image,
		MountPath:      "/config",
	}

	config := buildRuntimeConfigForInstance(instance)
	if config.Image != image || config.Port != 3001 || config.MountPath != "/config" {
		t.Fatalf("unexpected Linux Workbuddy config: %#v", config)
	}
	if config.Env["TITLE"] != "Workbuddy" || config.Env["SUBFOLDER"] != "/" {
		t.Fatalf("expected Webtop environment, got %#v", config.Env)
	}
	if !isWebtopRuntimeInstance(instance) || isWindowsWorkbuddyInstance(instance) {
		t.Fatal("Linux Workbuddy runtime classification is incorrect")
	}
}

func TestBuildRuntimeConfigForInstanceKeepsWindowsWorkbuddy(t *testing.T) {
	instance := &models.Instance{Type: "workbuddy", RuntimeVariant: WorkbuddyRuntimeWindows}
	config := buildRuntimeConfigForInstance(instance)
	if config.Port != 8006 || config.MountPath != "/storage" {
		t.Fatalf("unexpected Windows Workbuddy config: %#v", config)
	}
	if !isWindowsWorkbuddyInstance(instance) || isWebtopRuntimeInstance(instance) {
		t.Fatal("Windows Workbuddy runtime classification is incorrect")
	}
}

func TestCodexIsClassifiedAsWindowsVM(t *testing.T) {
	instance := &models.Instance{Type: RuntimeTypeCodex}
	config := buildRuntimeConfigForInstance(instance)
	if config.Port != 8006 || config.MountPath != "/storage" {
		t.Fatalf("unexpected Windows Codex config: %#v", config)
	}
	if config.Env["VERSION"] != "11" || config.Env["LANGUAGE"] != "Chinese" || config.Env["REGION"] != "zh-CN" {
		t.Fatalf("Windows Codex did not receive the Windows 11 Chinese defaults: %#v", config.Env)
	}
	if !isWindowsCodexInstance(instance) || !isWindowsVMInstance(instance) || isWebtopRuntimeInstance(instance) {
		t.Fatal("Windows Codex runtime classification is incorrect")
	}
	if supportsManagedRuntimeIntegrationForInstance(instance) || supportsRuntimeConfigInjectionForInstance(instance) {
		t.Fatal("Windows Codex must not receive guest-only runtime integration")
	}
}

func TestBuildRuntimeConfigForLinuxCodex(t *testing.T) {
	image := "10.130.14.23:5000/codex:2026.8.1"
	instance := &models.Instance{
		Type:           RuntimeTypeCodex,
		RuntimeVariant: WorkbuddyRuntimeLinux,
		ImageRegistry:  &image,
		MountPath:      "/config",
	}

	config := buildRuntimeConfigForInstance(instance)
	if config.Image != image || config.Port != 3001 || config.MountPath != "/config" {
		t.Fatalf("unexpected Linux Codex config: %#v", config)
	}
	if config.Env["CODEX_HOME"] != "/config/.codex" || config.Env["CLAWMANAGER_PROJECT_PATH"] != "/config/workspace" {
		t.Fatalf("expected Linux Codex workspace environment, got %#v", config.Env)
	}
	if isWindowsCodexInstance(instance) || isWindowsVMInstance(instance) || !isWebtopRuntimeInstance(instance) {
		t.Fatal("Linux Codex runtime classification is incorrect")
	}
	if !supportsManagedRuntimeIntegrationForInstance(instance) || !supportsRuntimeConfigInjectionForInstance(instance) {
		t.Fatal("Linux Codex must receive managed runtime integration")
	}
}

func TestLinuxCodexDoesNotRequireWindowsResources(t *testing.T) {
	req := CreateInstanceRequest{
		Type:           RuntimeTypeCodex,
		RuntimeVariant: WorkbuddyRuntimeLinux,
		Mode:           InstanceModePro,
		CPUCores:       2,
		MemoryGB:       4,
		DiskGB:         50,
	}
	if err := validateWindowsWorkbuddyRequest(req); err != nil {
		t.Fatalf("Linux Codex request rejected by Windows constraints: %v", err)
	}
}

func TestCodexRequestUsesConfiguredRuntimeVariant(t *testing.T) {
	previous := runtimeImageSettingsProvider
	t.Cleanup(func() { runtimeImageSettingsProvider = previous })
	runtimeImageSettingsProvider = runtimeImageProviderStub{
		config: RuntimeImageConfig{Image: "registry/custom-codex:latest", RuntimeType: "desktop", RuntimeVariant: WorkbuddyRuntimeLinux},
		ok:     true,
	}

	got := resolveCodexRuntimeVariantForRequest(CreateInstanceRequest{Type: RuntimeTypeCodex})
	if got != WorkbuddyRuntimeLinux {
		t.Fatalf("expected configured Linux Codex variant, got %q", got)
	}
}

func TestLegacyWorkbuddyVariantInference(t *testing.T) {
	linuxImage := "registry.example/workbuddy-linux:old"
	linux := &models.Instance{Type: "workbuddy", MountPath: "/config", ImageRegistry: &linuxImage}
	if got := workbuddyRuntimeVariantForInstance(linux); got != WorkbuddyRuntimeLinux {
		t.Fatalf("expected legacy Linux inference, got %q", got)
	}

	windows := &models.Instance{Type: "workbuddy", MountPath: "/storage"}
	if got := workbuddyRuntimeVariantForInstance(windows); got != WorkbuddyRuntimeWindows {
		t.Fatalf("expected Windows inference, got %q", got)
	}
}

func TestLinuxWorkbuddyDoesNotRequireWindowsResources(t *testing.T) {
	req := CreateInstanceRequest{
		Type:           "workbuddy",
		RuntimeVariant: WorkbuddyRuntimeLinux,
		Mode:           InstanceModePro,
		CPUCores:       2,
		MemoryGB:       4,
		DiskGB:         50,
	}
	if err := validateWindowsWorkbuddyRequest(req); err != nil {
		t.Fatalf("Linux Workbuddy request rejected by Windows constraints: %v", err)
	}
}

func TestLinuxWorkbuddyProxyBehavior(t *testing.T) {
	if !usesWebtopRuntime("workbuddy", 3001) || !usesHTTPSUpstream("workbuddy", 3001) {
		t.Fatal("Linux Workbuddy must retain Webtop HTTPS proxy behavior")
	}
	if usesWebtopRuntime("workbuddy", 8006) || usesHTTPSUpstream("workbuddy", 8006) {
		t.Fatal("Windows Workbuddy must retain noVNC HTTP proxy behavior")
	}
}

func TestLinuxCodexProxyBehavior(t *testing.T) {
	if !usesWebtopRuntime(RuntimeTypeCodex, 3001) || !usesHTTPSUpstream(RuntimeTypeCodex, 3001) {
		t.Fatal("Linux Codex must use Webtop HTTPS proxy behavior")
	}
	if usesWebtopRuntime(RuntimeTypeCodex, 8006) || usesHTTPSUpstream(RuntimeTypeCodex, 8006) {
		t.Fatal("Windows Codex must retain noVNC HTTP proxy behavior")
	}
}

func TestClaudeCodeProxyBehavior(t *testing.T) {
	if !usesWebtopRuntime(RuntimeTypeClaudeCode, 3001) {
		t.Fatal("Claude Code must use Webtop proxy behavior")
	}
	if !usesHTTPSUpstream(RuntimeTypeClaudeCode, 3001) {
		t.Fatal("Claude Code must use HTTPS upstream on port 3001")
	}
}

func TestLinuxWorkbuddyRetainsManagedRuntimeIntegration(t *testing.T) {
	linux := &models.Instance{Type: "workbuddy", RuntimeVariant: WorkbuddyRuntimeLinux}
	windows := &models.Instance{Type: "workbuddy", RuntimeVariant: WorkbuddyRuntimeWindows}
	if !supportsManagedRuntimeIntegrationForInstance(linux) || !supportsRuntimeConfigInjectionForInstance(linux) {
		t.Fatal("Linux Workbuddy must retain managed runtime integration")
	}
	if supportsManagedRuntimeIntegrationForInstance(windows) || supportsRuntimeConfigInjectionForInstance(windows) {
		t.Fatal("Windows Workbuddy must not receive guest-only runtime integration")
	}
}
