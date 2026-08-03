package config

import (
	"testing"
	"time"
)

func TestLoadRuntimeDefaults(t *testing.T) {
	for _, key := range []string{
		"HERMES_RUNTIME_IMAGE",
		"OPENCLAW_RUNTIME_IMAGE",
		"RUNTIME_NAMESPACE",
		"K8S_NAMESPACE",
		"HOSTNAME",
		"PLATFORM_REDIS_URL",
		"TEAM_REDIS_URL",
		"RUNTIME_WORKSPACE_NFS_SERVER",
		"RUNTIME_GATEWAY_START_IN_FLIGHT_LIMIT",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Runtime.HermesImage, "ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest"; got != want {
		t.Fatalf("expected Hermes default image %q, got %q", want, got)
	}
	if got, want := cfg.Runtime.OpenClawImage, "ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest"; got != want {
		t.Fatalf("expected OpenClaw default image %q, got %q", want, got)
	}
	if got, want := cfg.Runtime.GatewayStartInFlightLimit, 32; got != want {
		t.Fatalf("gateway start in-flight limit = %d, want %d", got, want)
	}
}

func TestLoadRuntimeGatewayStartInFlightLimitOverride(t *testing.T) {
	t.Setenv("RUNTIME_GATEWAY_START_IN_FLIGHT_LIMIT", "100")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Runtime.GatewayStartInFlightLimit, 100; got != want {
		t.Fatalf("gateway start in-flight limit = %d, want %d", got, want)
	}
}

func TestLoadStorageProfileDefaultsDisableHostPathFallback(t *testing.T) {
	for _, key := range []string{
		"CLAWMANAGER_STORAGE_PROFILE",
		"K8S_HOSTPATH_FALLBACK_ENABLED",
		"K8S_PVC_BIND_TIMEOUT",
		"K8S_CONTROL_PLANE_STORAGE_CLASS",
		"K8S_INSTANCE_STORAGE_CLASS",
		"K8S_WORKSPACE_STORAGE_CLASS",
		"K8S_WORKSPACE_ACCESS_MODE",
		"K8S_STORAGE_CLASS",
		"RUNTIME_WORKSPACE_NFS_SERVER",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Storage.Profile, "cluster"; got != want {
		t.Fatalf("storage profile = %q, want %q", got, want)
	}
	if cfg.Storage.HostPathFallbackEnabled {
		t.Fatalf("hostPath fallback must default to disabled for cluster installs")
	}
	if got, want := cfg.GetPVCBindTimeout(), 2*time.Minute; got != want {
		t.Fatalf("PVC bind timeout = %s, want %s", got, want)
	}
	if got, want := cfg.GetControlPlaneStorageClass(), "standard"; got != want {
		t.Fatalf("control-plane storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetInstanceStorageClass(), "standard"; got != want {
		t.Fatalf("instance storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetWorkspaceStorageClass(), "standard"; got != want {
		t.Fatalf("workspace storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetWorkspaceAccessMode(), "ReadWriteMany"; got != want {
		t.Fatalf("workspace access mode = %q, want %q", got, want)
	}
	if got := cfg.Runtime.WorkspaceNFSServer; got != "" {
		t.Fatalf("workspace NFS server must not default to in-cluster service DNS, got %q", got)
	}
}

func TestLoadStorageProfileEnvOverrides(t *testing.T) {
	t.Setenv("CLAWMANAGER_STORAGE_PROFILE", "single-node")
	t.Setenv("K8S_HOSTPATH_FALLBACK_ENABLED", "true")
	t.Setenv("K8S_PVC_BIND_TIMEOUT", "45s")
	t.Setenv("K8S_CONTROL_PLANE_STORAGE_CLASS", "manual")
	t.Setenv("K8S_INSTANCE_STORAGE_CLASS", "manual")
	t.Setenv("K8S_WORKSPACE_STORAGE_CLASS", "manual-workspace")
	t.Setenv("K8S_WORKSPACE_ACCESS_MODE", "ReadWriteMany")
	t.Setenv("RUNTIME_WORKSPACE_PVC_CLAIM", "clawmanager-workspaces")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := cfg.Storage.Profile, "single-node"; got != want {
		t.Fatalf("storage profile = %q, want %q", got, want)
	}
	if !cfg.Storage.HostPathFallbackEnabled {
		t.Fatalf("hostPath fallback should be explicitly enabled")
	}
	if got, want := cfg.GetPVCBindTimeout(), 45*time.Second; got != want {
		t.Fatalf("PVC bind timeout = %s, want %s", got, want)
	}
	if got, want := cfg.GetControlPlaneStorageClass(), "manual"; got != want {
		t.Fatalf("control-plane storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetInstanceStorageClass(), "manual"; got != want {
		t.Fatalf("instance storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetWorkspaceStorageClass(), "manual-workspace"; got != want {
		t.Fatalf("workspace storage class = %q, want %q", got, want)
	}
	if got, want := cfg.GetWorkspaceAccessMode(), "ReadWriteMany"; got != want {
		t.Fatalf("workspace access mode = %q, want %q", got, want)
	}
	if got, want := cfg.Runtime.WorkspacePVCClaimName, "clawmanager-workspaces"; got != want {
		t.Fatalf("runtime workspace PVC claim = %q, want %q", got, want)
	}
}
