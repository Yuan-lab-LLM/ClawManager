package config

import (
	"testing"
	"time"
)

func TestLoadRuntimeDefaults(t *testing.T) {
	for _, key := range []string{
		"HERMES_RUNTIME_IMAGE",
		"OPENCLAW_RUNTIME_IMAGE",
		"OPENCODE_RUNTIME_IMAGE",
		"RUNTIME_NAMESPACE",
		"K8S_NAMESPACE",
		"HOSTNAME",
		"PLATFORM_REDIS_URL",
		"TEAM_REDIS_URL",
		"RUNTIME_WORKSPACE_NFS_SERVER",
		"RUNTIME_GATEWAY_START_IN_FLIGHT_LIMIT",
		"SKILL_REPORT_PERSISTENCE_ENABLED",
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
	if got, want := cfg.Runtime.OpenCodeImage, "ghcr.io/yuan-lab-llm/agentsruntime/opencode-lite:latest"; got != want {
		t.Fatalf("expected OpenCode default image %q, got %q", want, got)
	}
	if got, want := cfg.Runtime.GatewayStartInFlightLimit, 32; got != want {
		t.Fatalf("gateway start in-flight limit = %d, want %d", got, want)
	}
	if !cfg.Runtime.SkillReportPersistence {
		t.Fatal("skill report persistence must default to enabled")
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

func TestLoadDatabasePoolAndSkillReportOverrides(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "150")
	t.Setenv("DB_MAX_IDLE_CONNS", "50")
	t.Setenv("DB_CONN_MAX_LIFETIME", "30m")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "5m")
	t.Setenv("SKILL_REPORT_PERSISTENCE_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := cfg.Database.MaxOpenConns, 150; got != want {
		t.Fatalf("database max open connections = %d, want %d", got, want)
	}
	if got, want := cfg.Database.MaxIdleConns, 50; got != want {
		t.Fatalf("database max idle connections = %d, want %d", got, want)
	}
	if got, want := cfg.Database.ConnMaxLifetime, 30*time.Minute; got != want {
		t.Fatalf("database connection max lifetime = %s, want %s", got, want)
	}
	if got, want := cfg.Database.ConnMaxIdleTime, 5*time.Minute; got != want {
		t.Fatalf("database connection max idle time = %s, want %s", got, want)
	}
	if cfg.Runtime.SkillReportPersistence {
		t.Fatal("skill report persistence override was not applied")
	}
}

func TestLoadNorthboundDefaultsAndOverrides(t *testing.T) {
	t.Setenv("CLAWMANAGER_NORTHBOUND_ENABLED", "true")
	t.Setenv("NORTHBOUND_GATEWAY_ADDRESS", ":10443")
	t.Setenv("NORTHBOUND_TRUSTED_PROXIES", "10.0.0.0/8, 192.0.2.10")
	t.Setenv("NORTHBOUND_CHALLENGE_TTL", "45s")
	t.Setenv("NORTHBOUND_OPERATION_MAX_ATTEMPTS", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.Northbound.Enabled || cfg.Northbound.GatewayAddress != ":10443" {
		t.Fatalf("unexpected northbound endpoint config: %+v", cfg.Northbound)
	}
	if len(cfg.Northbound.GatewayTrustedProxies) != 2 || cfg.Northbound.GatewayTrustedProxies[1] != "192.0.2.10" {
		t.Fatalf("unexpected northbound trusted proxies: %v", cfg.Northbound.GatewayTrustedProxies)
	}
	if cfg.Northbound.ChallengeTTL != 45*time.Second || cfg.Northbound.OperationMaxAttempts != 7 {
		t.Fatalf("unexpected northbound lifecycle config: %+v", cfg.Northbound)
	}
	if cfg.Northbound.CoreInternalAddress != ":9002" || cfg.Northbound.CoreBaseURL == "" {
		t.Fatalf("northbound Core defaults are incomplete: %+v", cfg.Northbound)
	}
}

func TestLoadIEISystemSessionDefaultsToOneDay(t *testing.T) {
	t.Setenv("IEISYSTEM_SESSION_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := cfg.IEISystem.SessionTTL, 24*time.Hour; got != want {
		t.Fatalf("IEI system session TTL = %s, want %s", got, want)
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

func TestLoadEnterpriseLDAPDefaultsDisabled(t *testing.T) {
	for _, key := range []string{
		"AUTH_ENTERPRISE_ENABLED",
		"AUTH_ENTERPRISE_ALLOW_LOCAL_FALLBACK",
		"AUTH_ENTERPRISE_SYNC_ROLE",
		"LDAP_HOST",
		"LDAP_PORT",
		"LDAP_TLS_CA_FILE",
		"LDAP_TLS_SERVER_NAME",
		"LDAP_BASE_DN",
		"LDAP_ADMIN_GROUP_DNS",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Auth.Enterprise.Enabled {
		t.Fatalf("enterprise auth must default to disabled")
	}
	if !cfg.Auth.Enterprise.AllowLocalFallback {
		t.Fatalf("enterprise local fallback should default to enabled")
	}
	if cfg.Auth.Enterprise.SyncRole {
		t.Fatalf("enterprise role sync must default to disabled")
	}
	if got, want := cfg.Auth.Enterprise.LDAP.Port, 389; got != want {
		t.Fatalf("ldap port = %d, want %d", got, want)
	}
	if got, want := cfg.Auth.Enterprise.LDAP.UserFilter, "(&(objectClass=person)(uid=%s))"; got != want {
		t.Fatalf("ldap user filter = %q, want %q", got, want)
	}
	if got, want := cfg.Auth.Enterprise.LDAP.DefaultRole, "user"; got != want {
		t.Fatalf("ldap default role = %q, want %q", got, want)
	}
}

func TestLoadEnterpriseLDAPEnvOverrides(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "env-auth-config-key-32-byte-key!")
	t.Setenv("AUTH_ENTERPRISE_ENABLED", "true")
	t.Setenv("AUTH_ENTERPRISE_ALLOW_LOCAL_FALLBACK", "false")
	t.Setenv("AUTH_ENTERPRISE_SYNC_ROLE", "true")
	t.Setenv("LDAP_HOST", "ldap.example.com")
	t.Setenv("LDAP_PORT", "636")
	t.Setenv("LDAP_USE_TLS", "true")
	t.Setenv("LDAP_TLS_CA_FILE", "/etc/ssl/certs/company-ldap.pem")
	t.Setenv("LDAP_TLS_SERVER_NAME", "ldap.internal.example.com")
	t.Setenv("LDAP_BASE_DN", "dc=example,dc=com")
	t.Setenv("LDAP_GROUP_BASE_DN", "ou=Groups,dc=example,dc=com")
	t.Setenv("LDAP_ADMIN_GROUP_DNS", "cn=admins,ou=Groups,dc=example,dc=com; cn=ops,ou=Groups,dc=example,dc=com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	ldap := cfg.Auth.Enterprise.LDAP
	if !cfg.Auth.Enterprise.Enabled {
		t.Fatalf("enterprise auth should be enabled")
	}
	if cfg.Auth.Enterprise.AllowLocalFallback {
		t.Fatalf("enterprise local fallback should be disabled")
	}
	if !cfg.Auth.Enterprise.SyncRole {
		t.Fatalf("enterprise role sync should be enabled")
	}
	if got, want := ldap.Host, "ldap.example.com"; got != want {
		t.Fatalf("ldap host = %q, want %q", got, want)
	}
	if got, want := ldap.Port, 636; got != want {
		t.Fatalf("ldap port = %d, want %d", got, want)
	}
	if !ldap.UseTLS {
		t.Fatalf("ldap useTLS should be enabled")
	}
	if got, want := ldap.TLSCAFile, "/etc/ssl/certs/company-ldap.pem"; got != want {
		t.Fatalf("ldap TLS CA file = %q, want %q", got, want)
	}
	if got, want := ldap.TLSServerName, "ldap.internal.example.com"; got != want {
		t.Fatalf("ldap TLS server name = %q, want %q", got, want)
	}
	if got, want := cfg.Auth.ConfigEncryptionKey, "env-auth-config-key-32-byte-key!"; got != want {
		t.Fatalf("auth config encryption key = %q, want %q", got, want)
	}
	if got, want := len(ldap.AdminGroupDNs), 2; got != want {
		t.Fatalf("admin group count = %d, want %d", got, want)
	}
	if got, want := ldap.AdminGroupDNs[0], "cn=admins,ou=Groups,dc=example,dc=com"; got != want {
		t.Fatalf("first admin group DN = %q, want %q", got, want)
	}
	if got, want := ldap.AdminGroupDNs[1], "cn=ops,ou=Groups,dc=example,dc=com"; got != want {
		t.Fatalf("second admin group DN = %q, want %q", got, want)
	}
}
