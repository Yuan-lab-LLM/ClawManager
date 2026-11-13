package aigateway

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"clawreef/internal/models"
)

// This proxy's V4.1 route rejects medium and treats reasoning_content and
// reasoning as aliases of one field. Hermes sends both on local gateway routes.
// Keep this observed compatibility contract separate from reasoning capability
// detection: it must not enable thinking or change other deployments' dialects.
func applyIcompifyDeepSeekCompatibility(payload map[string]json.RawMessage, model *models.LLMModel) {
	endpoint, err := url.Parse(model.BaseURL)
	if err != nil || !strings.EqualFold(endpoint.Hostname(), "api.icompify.com") || model.ProviderModelName != "deepseek-v4.1-flash" {
		return
	}
	var effort string
	if json.Unmarshal(payload["reasoning_effort"], &effort) == nil && effort == "medium" {
		payload["reasoning_effort"] = json.RawMessage(`"high"`)
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil {
		return
	}
	changed := false
	for _, message := range messages {
		var role string
		var reasoning, content *string
		if json.Unmarshal(message["role"], &role) == nil && role == "assistant" &&
			json.Unmarshal(message["reasoning"], &reasoning) == nil &&
			json.Unmarshal(message["reasoning_content"], &content) == nil &&
			reasoning != nil && content != nil && *reasoning == *content {
			delete(message, "reasoning")
			changed = true
		}
	}
	if changed {
		payload["messages"], _ = json.Marshal(messages)
	}
}

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
