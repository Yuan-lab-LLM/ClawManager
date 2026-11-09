package models

import (
	"net/url"
	"strings"
)

const (
	ReasoningControlNone             = ""
	ReasoningControlDeepSeekThinking = "deepseek-thinking"
	ReasoningControlThinkingType     = "thinking-type"
	ReasoningControlEnableThinking   = "enable-thinking"
	ReasoningControlChatTemplate     = "chat-template-kwargs"
)

// Explicit modes are administrator assertions about the endpoint's wire format,
// not inferred model capabilities. Empty retains legacy automatic behavior.
func ValidReasoningControlMode(mode string) bool {
	switch mode {
	case "", "none", ReasoningControlDeepSeekThinking, ReasoningControlThinkingType,
		ReasoningControlEnableThinking, ReasoningControlChatTemplate:
		return true
	}
	return false
}

func EffectiveLLMReasoningControl(model *LLMModel) string {
	if model == nil {
		return ReasoningControlNone
	}
	protocol := ResolveLLMProtocolTypeOrDefault(model.ProviderType, model.ProtocolType)
	if protocol != ProtocolTypeOpenAI && protocol != ProtocolTypeOpenAICompatible {
		return ReasoningControlNone
	}
	mode := strings.TrimSpace(model.ReasoningControlMode)
	if mode == "none" || !ValidReasoningControlMode(mode) {
		return ReasoningControlNone
	}
	if mode != "" {
		return mode
	}
	return ResolveLLMReasoningControl(model.ProviderType, model.ProtocolType, model.BaseURL, model.ProviderModelName)
}

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
	if !LLMChatAvailable(model) {
		model.ReasoningControl = ReasoningControlNone
		model.SupportsReasoning = false
		return
	}
	model.ReasoningControl = EffectiveLLMReasoningControl(model)
	model.SupportsReasoning = model.ReasoningControl != ReasoningControlNone
}
