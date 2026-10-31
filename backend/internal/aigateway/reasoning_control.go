package aigateway

import (
	"encoding/json"
	"fmt"

	"clawreef/internal/models"
)

func clearAlternativeThinkingSwitches(payload map[string]json.RawMessage) error {
	delete(payload, "enable_thinking")
	if raw, exists := payload["chat_template_kwargs"]; exists {
		var options map[string]json.RawMessage
		if err := json.Unmarshal(raw, &options); err != nil {
			return fmt.Errorf("invalid chat_template_kwargs: %w", err)
		}
		delete(options, "enable_thinking")
		delete(payload, "chat_template_kwargs")
		if len(options) > 0 {
			payload["chat_template_kwargs"], _ = json.Marshal(options)
		}
	}
	return nil
}

// Mixed client dialects must not override an explicit opt-out. Keep effort
// separately so DeepSeek can normalize it even when thinking.type is present.
func combinedReasoningIntent(payload map[string]json.RawMessage) reasoningRequestIntent {
	result := reasoningRequestIntent{}
	for _, key := range []string{"thinking", "enable_thinking", "chat_template_kwargs", "reasoning_effort", "reasoning"} {
		if raw, ok := payload[key]; ok {
			intent := openAICompatibleReasoningIntent(map[string]json.RawMessage{key: raw})
			if !intent.set {
				continue
			}
			if !result.set {
				result.enabled = true
			}
			result.set = true
			result.enabled = result.enabled && intent.enabled
			if intent.effort != "" {
				result.effort = intent.effort
			}
		}
	}
	return result
}

// All gateway ingress protocols converge on this outbound contract after the
// actual deployment is selected. Only explicitly configured endpoints use it.
func applyConfiguredReasoningControl(payload map[string]json.RawMessage, model *models.LLMModel, agent *string, control string) error {
	intent := combinedReasoningIntent(payload)
	enabled := model.ReasoningEnabled
	if isManagedOpenClawRequest(agent) {
		enabled = enabled && intent.set && intent.enabled
	} else if intent.set {
		enabled = enabled && intent.enabled
	}
	// Preserve unrelated template options while removing competing switches.
	template := map[string]json.RawMessage{}
	if raw, ok := payload["chat_template_kwargs"]; ok {
		if err := json.Unmarshal(raw, &template); err != nil {
			return fmt.Errorf("invalid chat_template_kwargs: %w", err)
		}
	}
	if template == nil {
		template = map[string]json.RawMessage{}
	}
	delete(template, "enable_thinking")
	delete(payload, "chat_template_kwargs")
	delete(payload, "enable_thinking")
	delete(payload, "thinking")
	// Effort fields can contradict the chosen switch or have vendor-specific
	// values. These three adapters control on/off, not reasoning effort.
	delete(payload, "reasoning_effort")
	delete(payload, "reasoning")
	switch control {
	case models.ReasoningControlThinkingType:
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		payload["thinking"], _ = json.Marshal(map[string]string{"type": state})
	case models.ReasoningControlEnableThinking:
		payload["enable_thinking"], _ = json.Marshal(enabled)
	case models.ReasoningControlChatTemplate:
		template["enable_thinking"], _ = json.Marshal(enabled)
	}
	if len(template) > 0 {
		payload["chat_template_kwargs"], _ = json.Marshal(template)
	}
	return nil
}
