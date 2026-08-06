package models

import "testing"

func TestResolveLLMReasoningControl(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		providerType string
		protocolType string
		baseURL      string
		model        string
		want         string
	}{
		{"official DeepSeek flash", ProviderTypeOpenAICompatible, ProtocolTypeOpenAICompatible, "https://api.deepseek.com", "deepseek-v4-flash", ReasoningControlDeepSeekThinking},
		{"official DeepSeek versioned endpoint", ProviderTypeOpenAICompatible, ProtocolTypeOpenAICompatible, "https://api.deepseek.com/v1", "deepseek-reasoner", ReasoningControlDeepSeekThinking},
		{"custom endpoint cannot be assumed", ProviderTypeOpenAICompatible, ProtocolTypeOpenAICompatible, "https://gateway.example.com/v1", "deepseek-v4-flash", ReasoningControlNone},
		{"unrelated model", ProviderTypeOpenAICompatible, ProtocolTypeOpenAICompatible, "https://api.deepseek.com", "other-model", ReasoningControlNone},
		{"anthropic protocol does not use DeepSeek extension", ProviderTypeLocal, ProtocolTypeAnthropic, "https://api.deepseek.com", "deepseek-v4-flash", ReasoningControlNone},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveLLMReasoningControl(tc.providerType, tc.protocolType, tc.baseURL, tc.model); got != tc.want {
				t.Fatalf("ResolveLLMReasoningControl() = %q, want %q", got, tc.want)
			}
		})
	}
}
