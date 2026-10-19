package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

const managedAutoModelRef = "auto/auto"

// ErrModelConfigurationRequired is a configuration failure, not a pool or
// capacity failure. Keep the message for older API consumers; callers should
// classify it using errors.Is rather than inspecting wrapped text.
var ErrModelConfigurationRequired = &runtimeFailure{code: "MODEL_CONFIGURATION_REQUIRED", cause: errors.New("no active models are configured")}

var openCodeBuiltInProviders = []string{
	"openai",
	"anthropic",
	"google",
	"amazon-bedrock",
	"azure",
	"groq",
	"mistral",
	"deepseek",
}

type gatewayModelInjection struct {
	defaultModel            string
	codingAgentDefaultModel string
	modelsJSON              string
	providerModelsJSON      string
	reasoningJSON           string
	reasoningControlJSON    string
}

type providerModelRef struct {
	providerID  string
	modelID     string
	qualifiedID string
}

type openCodeGatewayConfig struct {
	Schema            string                            `json:"$schema"`
	Model             string                            `json:"model"`
	Providers         map[string]openCodeProviderConfig `json:"provider"`
	EnabledProviders  []string                          `json:"enabled_providers"`
	DisabledProviders []string                          `json:"disabled_providers"`
}

type openCodeProviderConfig struct {
	NPM     string                         `json:"npm"`
	Name    string                         `json:"name"`
	Options openCodeProviderOptions        `json:"options"`
	Models  map[string]openCodeModelConfig `json:"models"`
}

type openCodeProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
}

type openCodeModelConfig struct {
	Name string `json:"name"`
}

func (s *instanceService) resolveGatewayModelInjection() (*gatewayModelInjection, error) {
	items, err := s.listGatewayModels()
	if err != nil {
		return nil, err
	}

	modelIDs := []string{"auto"}
	providerModelRefs := []string{managedAutoModelRef}
	reasoning := map[string]bool{"auto": false}
	reasoningControl := map[string]string{"auto": models.ReasoningControlNone}
	seen := map[string]struct{}{"auto": {}}

	for _, item := range items {
		displayName := firstNonEmptyCatalogValue(item.DisplayName, item.ProviderModelName)
		if displayName == "" {
			continue
		}
		normalizedName := strings.ToLower(displayName)
		if _, exists := seen[normalizedName]; exists {
			continue
		}
		seen[normalizedName] = struct{}{}

		providerName := firstNonEmptyCatalogValue(item.CatalogProviderName, modelCatalogProviderName(item))
		qualifiedName := providerName + "/" + displayName
		modelIDs = append(modelIDs, displayName)
		providerModelRefs = append(providerModelRefs, qualifiedName)

		models.PopulateLLMReasoningCapability(&item)
		reasoningEnabled := item.SupportsReasoning && item.ReasoningEnabled
		reasoning[displayName] = reasoningEnabled
		reasoning[qualifiedName] = reasoningEnabled
		reasoningControl[displayName] = item.ReasoningControl
		reasoningControl[qualifiedName] = item.ReasoningControl
	}

	modelsJSON, err := marshalGatewayModelSetting("model list", modelIDs)
	if err != nil {
		return nil, err
	}
	providerModelsJSON, err := marshalGatewayModelSetting("provider model list", providerModelRefs)
	if err != nil {
		return nil, err
	}
	reasoningJSON, err := marshalGatewayModelSetting("model reasoning settings", reasoning)
	if err != nil {
		return nil, err
	}
	reasoningControlJSON, err := marshalGatewayModelSetting("model reasoning controls", reasoningControl)
	if err != nil {
		return nil, err
	}
	codingAgentDefaultModel := ""
	if len(modelIDs) > 1 {
		codingAgentDefaultModel = modelIDs[1]
	}

	return &gatewayModelInjection{
		defaultModel:            "auto",
		codingAgentDefaultModel: codingAgentDefaultModel,
		modelsJSON:              modelsJSON,
		providerModelsJSON:      providerModelsJSON,
		reasoningJSON:           reasoningJSON,
		reasoningControlJSON:    reasoningControlJSON,
	}, nil
}

func (s *instanceService) listGatewayModels() ([]models.LLMModel, error) {
	if s == nil || s.llmModelRepo == nil {
		return nil, fmt.Errorf("llm model repository not configured")
	}

	var (
		items []models.LLMModel
		err   error
	)
	if s.expandedModelCatalog != nil {
		items, err = s.expandedModelCatalog.ListExpandedActiveModels()
	} else {
		items, err = s.llmModelRepo.ListActive()
		if err == nil {
			for index := range items {
				if strings.TrimSpace(items[index].CatalogProviderName) == "" {
					items[index].CatalogProviderName = modelCatalogProviderName(items[index])
				}
			}
			items = models.ExpandLLMModelCatalog(items)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	if len(items) == 0 {
		return nil, ErrModelConfigurationRequired
	}
	return items, nil
}

func marshalGatewayModelSetting(name string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("failed to encode gateway %s: %w", name, err)
	}
	return string(raw), nil
}

func firstNonEmptyCatalogValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// buildOpenCodeGatewayConfig translates provider-qualified model references
// into OpenCode custom providers backed by the managed ClawManager gateway.
func buildOpenCodeGatewayConfig(providerModelsJSON string) (string, error) {
	refs, err := parseProviderModelRefs(providerModelsJSON)
	if err != nil {
		return "", err
	}

	providerOrder := make([]string, 0)
	providerModels := make(map[string]map[string]openCodeModelConfig)
	for _, ref := range refs {
		modelsForProvider, exists := providerModels[ref.providerID]
		if !exists {
			modelsForProvider = make(map[string]openCodeModelConfig)
			providerModels[ref.providerID] = modelsForProvider
			providerOrder = append(providerOrder, ref.providerID)
		}
		modelsForProvider[ref.modelID] = openCodeModelConfig{Name: ref.modelID}
	}

	providers := make(map[string]openCodeProviderConfig, len(providerOrder))
	for _, providerID := range providerOrder {
		providers[providerID] = openCodeProviderConfig{
			NPM:  "@ai-sdk/openai-compatible",
			Name: providerID,
			Options: openCodeProviderOptions{
				BaseURL: "{env:CLAWMANAGER_LLM_BASE_URL}",
				APIKey:  "{env:CLAWMANAGER_LLM_API_KEY}",
			},
			Models: providerModels[providerID],
		}
	}

	config := openCodeGatewayConfig{
		Schema:            "https://opencode.ai/config.json",
		Model:             refs[0].qualifiedID,
		Providers:         providers,
		EnabledProviders:  providerOrder,
		DisabledProviders: disabledOpenCodeProviders(providerOrder),
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to encode OpenCode gateway config: %w", err)
	}
	return string(raw), nil
}

func parseProviderModelRefs(raw string) ([]providerModelRef, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("invalid gateway model catalogue: %w", err)
	}

	refs := make([]providerModelRef, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		providerID, modelID, qualified := strings.Cut(value, "/")
		providerID = strings.TrimSpace(providerID)
		modelID = strings.TrimSpace(modelID)
		if !qualified || providerID == "" || modelID == "" {
			providerID = "auto"
			modelID = value
		}
		qualifiedID := providerID + "/" + modelID
		if _, exists := seen[qualifiedID]; exists {
			continue
		}
		seen[qualifiedID] = struct{}{}
		refs = append(refs, providerModelRef{
			providerID:  providerID,
			modelID:     modelID,
			qualifiedID: qualifiedID,
		})
	}
	if len(refs) == 0 {
		refs = append(refs, providerModelRef{
			providerID:  "auto",
			modelID:     "auto",
			qualifiedID: managedAutoModelRef,
		})
	}
	return refs, nil
}

func disabledOpenCodeProviders(enabledProviders []string) []string {
	enabled := make(map[string]struct{}, len(enabledProviders))
	for _, providerID := range enabledProviders {
		enabled[providerID] = struct{}{}
	}
	disabled := make([]string, 0, len(openCodeBuiltInProviders))
	for _, providerID := range openCodeBuiltInProviders {
		if _, configured := enabled[providerID]; !configured {
			disabled = append(disabled, providerID)
		}
	}
	return disabled
}
