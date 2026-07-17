package services

import (
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestValidateManagedRuntimeEnvironmentOverridesRejectsProtectedKeys(t *testing.T) {
	err := validateManagedRuntimeEnvironmentOverrides("openclaw", map[string]string{
		"OPENAI_BASE_URL": "https://api.openai.com/v1",
	})
	if err == nil {
		t.Fatal("expected protected env override to be rejected")
	}
}

func TestApplyProtectedManagedRuntimeEnvRestoresGatewayValues(t *testing.T) {
	target := map[string]string{
		"OPENAI_BASE_URL": "https://api.openai.com/v1",
		"CUSTOM":          "value",
	}
	protected := map[string]string{
		"OPENAI_BASE_URL": "http://gateway.example/api/v1/gateway/llm",
	}
	result := applyProtectedManagedRuntimeEnv(target, protected)
	if result["OPENAI_BASE_URL"] != protected["OPENAI_BASE_URL"] {
		t.Fatalf("expected protected gateway url, got %q", result["OPENAI_BASE_URL"])
	}
	if result["CUSTOM"] != "value" {
		t.Fatalf("expected custom override to remain")
	}
}

func TestValidateManagedRuntimeEnvironmentOverridesAllowsCustomKeys(t *testing.T) {
	err := validateManagedRuntimeEnvironmentOverrides("openclaw", map[string]string{
		"CUSTOM_FLAG": "1",
	})
	if err != nil {
		t.Fatalf("expected custom override to be allowed, got %v", err)
	}
}

func TestNormalizeEnvironmentOverridesRejectsInvalidNames(t *testing.T) {
	if _, err := normalizeEnvironmentOverrides(map[string]string{
		"1INVALID": "value",
	}); err == nil {
		t.Fatalf("expected invalid environment variable name to fail validation")
	}
}

func TestBuildInstancePodEnvAppliesOverridesAfterDefaults(t *testing.T) {
	t.Setenv("CLAWMANAGER_EGRESS_PROXY_URL", "")
	t.Setenv("CLAWMANAGER_SYSTEM_NAMESPACE", "")
	t.Setenv("K8S_NAMESPACE", "")

	raw, err := marshalEnvironmentOverrides(map[string]string{
		"SUBFOLDER":                     "/custom-proxy",
		"KASM_SVC_ACCEPT_CUT_TEXT":      "-AcceptCutText 1",
		"SELKIES_CLIPBOARD_ENABLED":     "true|locked",
		"SELKIES_CLIPBOARD_IN_ENABLED":  "true|locked",
		"SELKIES_CLIPBOARD_OUT_ENABLED": "true|locked",
		"CUSTOM":                        "enabled",
	})
	if err != nil {
		t.Fatalf("marshalEnvironmentOverrides returned error: %v", err)
	}

	instance := &models.Instance{
		ID:                       42,
		Type:                     "webtop",
		EnvironmentOverridesJSON: raw,
	}

	env, err := buildInstancePodEnv(instance, defaultWebtopDesktopEnv("ClawManager Webtop"), nil, nil)
	if err != nil {
		t.Fatalf("buildInstancePodEnv returned error: %v", err)
	}

	if env["SUBFOLDER"] != "/custom-proxy" {
		t.Fatalf("expected SUBFOLDER override to win, got %q", env["SUBFOLDER"])
	}
	if env["KASM_SVC_ACCEPT_CUT_TEXT"] != "-AcceptCutText 1" {
		t.Fatalf("expected clipboard policy override to win, got %q", env["KASM_SVC_ACCEPT_CUT_TEXT"])
	}
	for _, key := range []string{
		"SELKIES_CLIPBOARD_ENABLED",
		"SELKIES_CLIPBOARD_IN_ENABLED",
		"SELKIES_CLIPBOARD_OUT_ENABLED",
	} {
		if got := env[key]; got != "true|locked" {
			t.Fatalf("expected %s override to win, got %q", key, got)
		}
	}
	if env["CUSTOM"] != "enabled" {
		t.Fatalf("expected custom environment variable to be merged")
	}
	if env["TITLE"] != "ClawManager Webtop" {
		t.Fatalf("expected default environment variable to remain available")
	}
}

func TestValidateManagedRuntimeEnvironmentOverridesSkipsNonManagedTypes(t *testing.T) {
	err := validateManagedRuntimeEnvironmentOverrides("ubuntu", map[string]string{
		"OPENAI_BASE_URL": "https://api.openai.com/v1",
	})
	if err != nil {
		t.Fatalf("expected non-managed type to skip validation, got %v", err)
	}
}

func TestValidateManagedRuntimeEnvironmentOverridesErrorMessage(t *testing.T) {
	err := validateManagedRuntimeEnvironmentOverrides("openclaw", map[string]string{
		"openai_api_key": "sk-test",
	})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected normalized key in error, got %v", err)
	}
}
