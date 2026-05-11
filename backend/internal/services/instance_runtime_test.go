package services

import "testing"

func TestDefaultImagePullPolicy_Default(t *testing.T) {
	t.Setenv("IMAGE_PULL_POLICY", "")
	got := defaultImagePullPolicy()
	if got != "IfNotPresent" {
		t.Fatalf("expected IfNotPresent, got %q", got)
	}
}

func TestDefaultImagePullPolicy_IgnoresEnvOverride(t *testing.T) {
	for _, envValue := range []string{"Always", "Never", "IfNotPresent", "   "} {
		t.Run(envValue, func(t *testing.T) {
			t.Setenv("IMAGE_PULL_POLICY", envValue)
			got := defaultImagePullPolicy()
			if got != "IfNotPresent" {
				t.Fatalf("expected IfNotPresent, got %q", got)
			}
		})
	}
}

func TestBuildRuntimeConfig_HermesUsesWebtopDefaults(t *testing.T) {
	config := buildRuntimeConfig("hermes", "hermes", "latest", nil, nil)

	if config.Port != 3001 {
		t.Fatalf("expected Hermes port 3001, got %d", config.Port)
	}
	if config.MountPath != "/config/.hermes" {
		t.Fatalf("expected Hermes mount path /config/.hermes, got %q", config.MountPath)
	}
	if config.Env["SUBFOLDER"] != "/" {
		t.Fatalf("expected Hermes default SUBFOLDER /, got %q", config.Env["SUBFOLDER"])
	}
	if !usesWebtopImage("hermes") {
		t.Fatalf("expected Hermes to use webtop proxy behavior")
	}
}
