package models

import (
	"net/url"
	"strings"
)

const (
	ReasoningControlNone             = ""
	ReasoningControlDeepSeekThinking = "deepseek-thinking"
)

// ResolveLLMReasoningControl returns only reasoning controls whose enable and
// disable semantics ClawManager can enforce on the provider wire protocol.
// Unknown OpenAI-compatible endpoints deliberately remain unsupported: a
// model name alone is not proof that a proxy implements the vendor extension.
func ResolveLLMReasoningControl(providerType, protocolType, baseURL, providerModelName string) string {
	protocol := ResolveLLMProtocolTypeOrDefault(providerType, protocolType)
	if protocol != ProtocolTypeOpenAI && protocol != ProtocolTypeOpenAICompatible {
		return ReasoningControlNone
	}

	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "api.deepseek.com") {
		return ReasoningControlNone
	}

	modelName := strings.ToLower(strings.TrimSpace(providerModelName))
	if strings.HasPrefix(modelName, "deepseek-") {
		return ReasoningControlDeepSeekThinking
	}
	return ReasoningControlNone
}

func PopulateLLMReasoningCapability(model *LLMModel) {
	if model == nil {
		return
	}
	model.ReasoningControl = ResolveLLMReasoningControl(
		model.ProviderType,
		model.ProtocolType,
		model.BaseURL,
		model.ProviderModelName,
	)
	model.SupportsReasoning = model.ReasoningControl != ReasoningControlNone
}
