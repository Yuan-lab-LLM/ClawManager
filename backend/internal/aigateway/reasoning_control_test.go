package aigateway

import (
	"encoding/json"
	"testing"

	"clawreef/internal/models"
)

func TestConfiguredReasoningWireFormats(t *testing.T) {
	for _, mode := range []string{models.ReasoningControlDeepSeekThinking, models.ReasoningControlThinkingType, models.ReasoningControlEnableThinking, models.ReasoningControlChatTemplate} {
		for _, enabled := range []bool{false, true} {
			model := &models.LLMModel{ProviderType: "local", ProtocolType: "openai-compatible", BaseURL: "https://proxy.example/v1", ProviderModelName: "custom-model", ReasoningControlMode: mode, ReasoningEnabled: enabled}
			body, err := buildProviderRequestBody(ChatCompletionRequest{RawBody: []byte(`{"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled"},"enable_thinking":true,"chat_template_kwargs":{"enable_thinking":true,"other":42},"tools":[{"type":"function","function":{"name":"lookup"}}]}`)}, model)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			var options map[string]json.RawMessage
			if err := json.Unmarshal(payload["chat_template_kwargs"], &options); err != nil {
				t.Fatal(err)
			}
			if string(options["other"]) != "42" || len(payload["tools"]) == 0 {
				t.Fatalf("lost unrelated fields: %s", body)
			}
			wantBool := "false"
			wantType := "disabled"
			if enabled {
				wantBool, wantType = "true", "enabled"
			}
			switch mode {
			case models.ReasoningControlEnableThinking:
				if string(payload["enable_thinking"]) != wantBool || payload["thinking"] != nil || options["enable_thinking"] != nil {
					t.Fatalf("wrong boolean wire: %s", body)
				}
			case models.ReasoningControlChatTemplate:
				if string(options["enable_thinking"]) != wantBool || payload["thinking"] != nil || payload["enable_thinking"] != nil {
					t.Fatalf("wrong template wire: %s", body)
				}
			default:
				var thinking struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(payload["thinking"], &thinking); err != nil || thinking.Type != wantType || payload["enable_thinking"] != nil || options["enable_thinking"] != nil {
					t.Fatalf("wrong thinking wire: %s", body)
				}
			}
		}
	}
}

func TestConfiguredReasoningClientOptOutAndAdminCeiling(t *testing.T) {
	for _, raw := range []string{`{"thinking":{"type":"disabled"}}`, `{"enable_thinking":false}`, `{"chat_template_kwargs":{"enable_thinking":false}}`, `{"reasoning_effort":"none"}`, `{"reasoning":{"effort":"none"}}`, `{"thinking":{"type":"disabled"},"enable_thinking":true}`, `{"thinking":{"type":"enabled"},"reasoning_effort":"none"}`} {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &payload)
		model := &models.LLMModel{ProviderType: "local", ReasoningControlMode: models.ReasoningControlEnableThinking, ReasoningEnabled: true}
		if err := applyReasoningControl(payload, model, nil); err != nil {
			t.Fatal(err)
		}
		if string(payload["enable_thinking"]) != "false" {
			t.Fatalf("opt-out ignored: %s => %s", raw, payload["enable_thinking"])
		}
	}
	for _, enabled := range []bool{false, true} {
		model := &models.LLMModel{ProviderType: "local", ReasoningControlMode: models.ReasoningControlEnableThinking, ReasoningEnabled: enabled}
		payload := map[string]json.RawMessage{}
		agent := "openclaw"
		if err := applyReasoningControl(payload, model, &agent); err != nil {
			t.Fatal(err)
		}
		if string(payload["enable_thinking"]) != "false" {
			t.Fatal("OpenClaw must opt in per request")
		}
	}
}

func TestConfiguredReasoningDefaultLeavesCustomUpstreamUntouched(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		payload := map[string]json.RawMessage{"enable_thinking": json.RawMessage(`true`), "reasoning_effort": json.RawMessage(`"high"`)}
		model := &models.LLMModel{ProviderType: "local", BaseURL: "https://proxy.example/v1", ProviderModelName: "qwen3", ReasoningControlMode: mode}
		if err := applyReasoningControl(payload, model, nil); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 2 || string(payload["enable_thinking"]) != "true" {
			t.Fatal("legacy passthrough changed")
		}
	}
}

func TestConfiguredReasoningRejectsMalformedTemplate(t *testing.T) {
	model := &models.LLMModel{ProviderType: "local", ReasoningControlMode: models.ReasoningControlChatTemplate}
	payload := map[string]json.RawMessage{"chat_template_kwargs": json.RawMessage(`[]`)}
	if err := applyReasoningControl(payload, model, nil); err == nil {
		t.Fatal("expected invalid template error")
	}
}
