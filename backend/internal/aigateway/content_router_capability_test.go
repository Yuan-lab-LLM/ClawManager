package aigateway

import (
	"testing"

	"clawreef/internal/models"
)

func TestRoutingRejectsKnownIncapableToolModel(t *testing.T) {
	req := ChatCompletionRequest{Tools: []byte(`[{"type":"function"}`)}
	d := routingDecision{MinimumTier: 1}
	model := &models.LLMModel{IsActive: true, ModelGroup: "general", QualityTier: 2, CapabilitySource: "provider_metadata"}
	reasons := routingRejectionReasons(model, req, d)
	if !containsRoutingReason(reasons, "tools_not_supported") {
		t.Fatalf("expected tools capability rejection, got %v", reasons)
	}
}

func TestRoutingDetectsImageContent(t *testing.T) {
	req := ChatCompletionRequest{Messages: []ChatMessage{{Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,x"}}}}}}
	if !requestRequiresVision(req) {
		t.Fatal("image content should require vision capability")
	}
}

func containsRoutingReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func TestRoutingToolOverride(t *testing.T) {
	for _, source := range []string{"", "unknown", "default_rule", "name_rule", "provider_metadata"} {
		for _, discovered := range []bool{false, true} {
			for _, manual := range []bool{false, true} {
				model := &models.LLMModel{IsActive: true, QualityTier: 2, CapabilitySource: source, SupportsTools: discovered, ToolsSupportOverride: &manual}
				req := ChatCompletionRequest{Tools: []byte(`[{"type":"function","function":{"name":"read_file"}}]`)}
				reasons := routingRejectionReasons(model, req, routingDecision{})
				if containsRoutingReason(reasons, "tools_not_supported") == manual {
					t.Fatalf("manual=%v must override discovered=%v source=%s: %v", manual, discovered, source, reasons)
				}
			}
		}
	}
}

func TestRoutingUnknownToolsDoNotImplyUnsupported(t *testing.T) {
	for _, source := range []string{"", "unknown", "default_rule", "name_rule"} {
		model := &models.LLMModel{IsActive: true, CapabilitySource: source}
		reasons := routingRejectionReasons(model, ChatCompletionRequest{Tools: []byte(`[{"type":"function"}]`)}, routingDecision{})
		if containsRoutingReason(reasons, "tools_not_supported") {
			t.Fatalf("unknown source %q incorrectly rejected: %v", source, reasons)
		}
	}
}

func TestRoutingToolOptOutStillAllowsRequestsWithoutTools(t *testing.T) {
	no := false
	model := &models.LLMModel{IsActive: true, ToolsSupportOverride: &no}
	for _, raw := range []string{"", "[]", "null", " [] "} {
		reasons := routingRejectionReasons(model, ChatCompletionRequest{Tools: []byte(raw)}, routingDecision{})
		if containsRoutingReason(reasons, "tools_not_supported") {
			t.Fatalf("empty tools %q rejected: %v", raw, reasons)
		}
	}
}

func TestRoutingToolOverrideDoesNotBypassOtherCapabilitiesOrSecurity(t *testing.T) {
	yes := true
	model := &models.LLMModel{IsActive: true, ToolsSupportOverride: &yes, CapabilitySource: "provider_metadata"}
	req := ChatCompletionRequest{Tools: []byte(`[{"type":"function"}]`), Messages: []ChatMessage{{Content: []any{map[string]any{"type": "image_url"}}}}}
	reasons := routingRejectionReasons(model, req, routingDecision{SecureRequired: true})
	if containsRoutingReason(reasons, "tools_not_supported") || !containsRoutingReason(reasons, "secure_model_required") || !containsRoutingReason(reasons, "vision_not_supported") {
		t.Fatalf("tool override changed unrelated gates: %v", reasons)
	}
}
