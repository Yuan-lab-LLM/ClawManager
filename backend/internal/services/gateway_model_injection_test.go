package services

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestBuildOpenCodeGatewayConfigGroupsQualifiedModels(t *testing.T) {
	raw, err := buildOpenCodeGatewayConfig(`[
		"auto/auto",
		"deepseek/deepseek",
		"deepseek/deepseek-v4-pro",
		"modellist/org/model",
		"deepseek/deepseek-v4-pro"
	]`)
	if err != nil {
		t.Fatalf("buildOpenCodeGatewayConfig() error = %v", err)
	}

	var config openCodeGatewayConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("OpenCode config is not valid JSON: %v", err)
	}
	if config.Model != managedAutoModelRef {
		t.Fatalf("model = %q, want %q", config.Model, managedAutoModelRef)
	}
	if !slices.Equal(config.EnabledProviders, []string{"auto", "deepseek", "modellist"}) {
		t.Fatalf("enabled providers = %#v", config.EnabledProviders)
	}
	if slices.Contains(config.DisabledProviders, "deepseek") {
		t.Fatalf("configured provider deepseek remained disabled: %#v", config.DisabledProviders)
	}
	if got := config.Providers["deepseek"].Models; len(got) != 2 || got["deepseek-v4-pro"].Name != "deepseek-v4-pro" {
		t.Fatalf("deepseek models = %#v", got)
	}
	if got := config.Providers["modellist"].Models; got["org/model"].Name != "org/model" {
		t.Fatalf("model id containing slash was not preserved: %#v", got)
	}
	for providerID, provider := range config.Providers {
		if provider.Options.BaseURL != "{env:CLAWMANAGER_LLM_BASE_URL}" || provider.Options.APIKey != "{env:CLAWMANAGER_LLM_API_KEY}" {
			t.Fatalf("provider %q bypasses managed gateway env: %#v", providerID, provider.Options)
		}
	}
}

func TestBuildOpenCodeGatewayConfigDefaultsEmptyCatalogToAuto(t *testing.T) {
	raw, err := buildOpenCodeGatewayConfig(`[]`)
	if err != nil {
		t.Fatalf("buildOpenCodeGatewayConfig() error = %v", err)
	}
	var config openCodeGatewayConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	if config.Model != managedAutoModelRef || len(config.Providers) != 1 || config.Providers["auto"].Models["auto"].Name != "auto" {
		t.Fatalf("empty catalog fallback = %#v", config)
	}
}

func TestBuildOpenCodeGatewayConfigRejectsInvalidCatalog(t *testing.T) {
	if _, err := buildOpenCodeGatewayConfig(`not-json`); err == nil {
		t.Fatal("buildOpenCodeGatewayConfig() error = nil, want invalid catalogue error")
	}
}
