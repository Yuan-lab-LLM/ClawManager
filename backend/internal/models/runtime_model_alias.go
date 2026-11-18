package models

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// RuntimeModelAliasPrefix is a versioned wire contract with AgentsRuntime.
// Keep human-facing display names out of OpenClaw's policy-ref grammar.
const RuntimeModelAliasPrefix = "cm-model-v1-"

func RuntimeModelAlias(name string) string {
	return fmt.Sprintf("%s%x", RuntimeModelAliasPrefix, sha256.Sum256([]byte(strings.TrimSpace(name))))
}

// RuntimeCatalogProviderName mirrors the legacy injected provider namespace.
func RuntimeCatalogProviderName(m LLMModel) string {
	for _, candidate := range []string{m.CatalogProviderName, m.DisplayName, m.ProviderType, "provider"} {
		if name := strings.TrimSpace(candidate); name != "" {
			return strings.NewReplacer("/", "-", `\`, "-").Replace(name)
		}
	}
	return "provider"
}

type RuntimeProviderRef struct {
	ProviderID string `db:"provider_id" json:"provider_id"`
	ModelID    string `db:"model_ref" json:"model_id"`
}

// RuntimeOpenClawProviderRef mirrors AgentsRuntime managedmodelrefs.Normalize.
// Hash the original case-sensitive spelling BEFORE normalizing lookup keys.
func RuntimeOpenClawProviderRef(m LLMModel) RuntimeProviderRef {
	provider := RuntimeCatalogProviderName(m)
	name := strings.TrimSpace(m.DisplayName)
	if !runtimePolicyToken(provider, false) {
		provider = fmt.Sprintf("cm-provider-v1-%x", sha256.Sum256([]byte(provider)))
	}
	if !runtimePolicyToken(name, true) {
		name = RuntimeModelAlias(name)
	}
	return RuntimeProviderRef{ProviderID: strings.ToLower(provider), ModelID: strings.ToLower(name)}
}

func runtimePolicyToken(value string, slash bool) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' || slash && c == '/' {
			continue
		}
		return false
	}
	return true
}
