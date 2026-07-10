package services

import (
	"testing"

	"clawreef/internal/models"
)

func TestNormalizeEnvironmentOverrides(t *testing.T) {
	overrides, err := normalizeEnvironmentOverrides(map[string]string{
		" FOO ": "bar",
		"BAR_2": "",
	})
	if err != nil {
		t.Fatalf("normalizeEnvironmentOverrides returned error: %v", err)
	}

	if overrides["FOO"] != "bar" {
		t.Fatalf("expected trimmed key FOO to be preserved")
	}
	if value, ok := overrides["BAR_2"]; !ok || value != "" {
		t.Fatalf("expected empty override value to be preserved")
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

func TestBuildInstancePodEnvNormalizesDesktopStreamProfile(t *testing.T) {
	raw, err := marshalEnvironmentOverrides(map[string]string{
		"CLAWMANAGER_DESKTOP_STREAM_PROFILE": "standard",
		"SELKIES_ENCODER":                    "x264enc",
		"SELKIES_FRAMERATE":                  "35",
		"SELKIES_H264_CRF":                   "34",
	})
	if err != nil {
		t.Fatalf("marshalEnvironmentOverrides returned error: %v", err)
	}

	env, err := buildInstancePodEnv(&models.Instance{
		ID:                       42,
		Type:                     "openclaw",
		RuntimeType:              RuntimeBackendDesktop,
		EnvironmentOverridesJSON: raw,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildInstancePodEnv returned error: %v", err)
	}

	if got := env["SELKIES_ENCODER"]; got != "x264enc,jpeg" {
		t.Fatalf("SELKIES_ENCODER = %q, want x264enc,jpeg", got)
	}
	if got := env["SELKIES_USE_CSS_SCALING"]; got != "true" {
		t.Fatalf("SELKIES_USE_CSS_SCALING = %q, want true", got)
	}
}

func TestPopSHMSizeGB(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		hasValue bool
		runtime  string
		memoryGB int
		want     int
	}{
		{name: "desktop minimum preset", runtime: "desktop", memoryGB: 4, want: defaultInstanceSHMSizeGB},
		{name: "desktop medium preset", runtime: "desktop", memoryGB: 8, want: 2},
		{name: "desktop large preset", runtime: "desktop", memoryGB: 12, want: 4},
		{name: "desktop small fallback", runtime: "desktop", memoryGB: 2, want: defaultInstanceSHMSizeGB},
		{name: "shell default unchanged", runtime: "shell", memoryGB: 8, want: defaultInstanceSHMSizeGB},
		{name: "disable", value: "0", hasValue: true, runtime: "desktop", memoryGB: 8, want: 0},
		{name: "custom", value: "4", hasValue: true, runtime: "desktop", memoryGB: 4, want: 4},
		{name: "clamp", value: "128", hasValue: true, runtime: "desktop", memoryGB: 8, want: maxInstanceSHMSizeGB},
		{name: "invalid keeps dynamic desktop default", value: "nope", hasValue: true, runtime: "desktop", memoryGB: 8, want: 2},
		{name: "negative keeps dynamic desktop default", value: "-1", hasValue: true, runtime: "desktop", memoryGB: 4, want: defaultInstanceSHMSizeGB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extraEnv := map[string]string{"KEEP": "value"}
			if tt.hasValue {
				extraEnv["SHM_SIZE_GB"] = tt.value
			}

			got := popSHMSizeGB(extraEnv, tt.runtime, tt.memoryGB)
			if got != tt.want {
				t.Fatalf("expected shm size %d, got %d", tt.want, got)
			}
			if _, ok := extraEnv["SHM_SIZE_GB"]; ok {
				t.Fatalf("expected SHM_SIZE_GB to be removed from extra env")
			}
			if extraEnv["KEEP"] != "value" {
				t.Fatalf("expected unrelated env to be preserved")
			}
		})
	}
}
