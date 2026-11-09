package models

import "testing"

func TestEffectiveReasoningControlModes(t *testing.T) {
	for _, mode := range []string{ReasoningControlDeepSeekThinking, ReasoningControlThinkingType, ReasoningControlEnableThinking, ReasoningControlChatTemplate} {
		model := &LLMModel{ProviderType: ProviderTypeLocal, ProtocolType: ProtocolTypeOpenAICompatible, BaseURL: "https://proxy.example/v1", ProviderModelName: "alias", ReasoningControlMode: mode}
		PopulateLLMReasoningCapability(model)
		if !model.SupportsReasoning || model.ReasoningControl != mode {
			t.Fatalf("explicit adapter not used: %+v", model)
		}
		model.ProtocolType = ProtocolTypeAnthropic
		PopulateLLMReasoningCapability(model)
		if model.SupportsReasoning {
			t.Fatal("wrong upstream protocol accepted")
		}
	}
	model := &LLMModel{ProviderType: ProviderTypeLocal, BaseURL: "https://api.deepseek.com", ProviderModelName: "deepseek-v4-pro", ReasoningControlMode: "none"}
	if EffectiveLLMReasoningControl(model) != "" {
		t.Fatal("explicit passthrough must override auto detection")
	}
	if EffectiveLLMReasoningControl(nil) != "" {
		t.Fatal("nil model must be safe")
	}
}
