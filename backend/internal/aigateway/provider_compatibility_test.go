package aigateway

import (
	"encoding/json"
	"testing"

	"clawreef/internal/models"
)

func TestBuildProviderRequestIcompifyDeepSeekEffort(t *testing.T) {
	for _, effort := range []string{"medium", "high", "xhigh", "max", "none"} {
		t.Run(effort, func(t *testing.T) {
			model := &models.LLMModel{ProviderType: "local", ProtocolType: "openai-compatible", BaseURL: "https://api.icompify.com/v1", ProviderModelName: "deepseek-v4.1-flash"}
			body, err := buildProviderRequestBody(ChatCompletionRequest{RawBody: []byte(`{"messages":[],"reasoning_effort":"` + effort + `","provider_option":true}`)}, model)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			want := effort
			if effort == "medium" {
				want = "high"
			}
			if string(payload["reasoning_effort"]) != `"`+want+`"` {
				t.Fatalf("reasoning_effort = %s, want %q", payload["reasoning_effort"], want)
			}
			if payload["thinking"] != nil || string(payload["provider_option"]) != "true" || model.ReasoningEnabled {
				t.Fatal("compatibility must preserve unrelated options and reasoning configuration")
			}
		})
	}
}

func TestBuildProviderRequestIcompifyDeepSeekReasoningAliases(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL, model string
		wantAlias            bool
	}{
		{"observed route", "https://api.icompify.com/v1", "deepseek-v4.1-flash", false},
		{"other endpoint", "https://proxy.example/v1", "deepseek-v4.1-flash", true},
		{"hostname suffix", "https://api.icompify.com.example/v1", "deepseek-v4.1-flash", true},
		{"other model", "https://api.icompify.com/v1", "glm-5.3-flash", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &models.LLMModel{ProviderType: "local", ProtocolType: "openai-compatible", BaseURL: tc.baseURL, ProviderModelName: tc.model}
			body, err := buildProviderRequestBody(ChatCompletionRequest{RawBody: []byte(`{"messages":[{"role":"assistant","content":null,"reasoning":"state","reasoning_content":"state","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"assistant","content":"answer","reasoning":"different","reasoning_content":"canonical"}],"reasoning_effort":"medium"}`)}, model)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Messages []map[string]json.RawMessage `json:"messages"`
				Effort   string                       `json:"reasoning_effort"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			_, alias := payload.Messages[0]["reasoning"]
			if alias != tc.wantAlias || string(payload.Messages[0]["reasoning_content"]) != `"state"` || len(payload.Messages[0]["tool_calls"]) == 0 {
				t.Fatal("equal reasoning aliases must collapse only on the observed route while retaining tool state")
			}
			if string(payload.Messages[1]["reasoning"]) != `"different"` || string(payload.Messages[1]["reasoning_content"]) != `"canonical"` {
				t.Fatal("distinct provider state must not be discarded")
			}
			if tc.wantAlias && payload.Effort != "medium" {
				t.Fatal("unrelated route effort changed")
			}
		})
	}
}

func TestIcompifyDeepSeekPreservesNonStringReasoningState(t *testing.T) {
	model := &models.LLMModel{BaseURL: "https://api.icompify.com/v1", ProviderModelName: "deepseek-v4.1-flash"}
	for _, state := range []string{`null`, `{"signature":"opaque"}`} {
		payload := map[string]json.RawMessage{"messages": json.RawMessage(`[{"role":"assistant","reasoning":` + state + `,"reasoning_content":""}]`)}
		applyIcompifyDeepSeekCompatibility(payload, model)
		var messages []map[string]json.RawMessage
		if err := json.Unmarshal(payload["messages"], &messages); err != nil {
			t.Fatal(err)
		}
		if string(messages[0]["reasoning"]) != state {
			t.Fatalf("non-string reasoning state changed: %s", messages[0]["reasoning"])
		}
	}
}
