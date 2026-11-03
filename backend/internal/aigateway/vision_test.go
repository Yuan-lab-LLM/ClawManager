package aigateway

import (
	"bytes"
	"clawreef/internal/models"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
)

func visionTestPNG(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}
func visionTestBlock(ref string) map[string]interface{} {
	return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": ref}}
}

func TestVisionDetectionIsStructural(t *testing.T) {
	for _, content := range []interface{}{"explain image_url and input_image", []interface{}{map[string]interface{}{"type": "text", "text": "image_base64"}}} {
		if requestRequiresVision(ChatCompletionRequest{Messages: []ChatMessage{{Content: content}}}) {
			t.Fatal("quoted text classified as an image")
		}
	}
	for _, typ := range []string{"image", "image_url", "input_image"} {
		content := []interface{}{map[string]interface{}{"type": "tool_result", "content": []interface{}{map[string]interface{}{"type": typ}}}}
		if !requestRequiresVision(ChatCompletionRequest{Messages: []ChatMessage{{Content: content}}}) {
			t.Fatal("missed nested image", typ)
		}
	}
}

func TestVisionNormalizationPreservesRawProviderExtensions(t *testing.T) {
	raw := []byte(`{"model":"vision","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},{"role":"assistant","content":null,"reasoning_content":"provider state","tool_calls":[{"id":"call_1","type":"function","function":{"name":"inspect","arguments":"{}"}}]}],"provider_option":true}`)
	var req ChatCompletionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	req.RawBody = raw
	if err := NormalizeVisionRequest(&req); err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(req.RawBody, &got); err != nil {
		t.Fatal(err)
	}
	assistant := got["messages"].([]interface{})[1].(map[string]interface{})
	if assistant["reasoning_content"] != "provider state" || got["provider_option"] != true || len(assistant["tool_calls"].([]interface{})) != 1 {
		t.Fatal("provider/tool continuation state lost")
	}
}

func TestVisionNormalizesAndPreservesAnthropicImages(t *testing.T) {
	data := visionTestPNG(t)
	content := []interface{}{map[string]interface{}{"type": "text", "text": "before"}, map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": data}}, map[string]interface{}{"type": "text", "text": "after"}}
	for _, role := range []string{"user", "tool"} {
		req := ChatCompletionRequest{Messages: []ChatMessage{{Role: role, Content: content, ToolCallID: "call_1"}}}
		req.RawBody, _ = json.Marshal(req)
		if err := NormalizeVisionRequest(&req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(req.RawBody), "image_url") {
			t.Fatal("raw proxy body not synchronized")
		}
		var blocks []anthropicContentBlock
		if role == "tool" {
			blocks = convertToolMessageToAnthropicBlocks(req.Messages[0])[0].Content.([]anthropicContentBlock)
		} else {
			blocks = convertUserMessageToAnthropicBlocks(req.Messages[0])
		}
		if len(blocks) != 3 || blocks[0].Text != "before" || blocks[1].Type != "image" || blocks[2].Text != "after" {
			t.Fatalf("image/order lost: %+v", blocks)
		}
		if blocks[1].Source.(map[string]interface{})["data"] != data {
			t.Fatal("image bytes changed")
		}
		if strings.Contains(flattenMessages(req.Messages), data) || strings.Contains(rawOrJSONRequestPayload(req), data) {
			t.Fatal("image data leaked into text/logs")
		}
	}
}

func TestVisionInvalidInputFailsClosed(t *testing.T) {
	for _, ref := range []string{"", "file:///etc/passwd", "http://example.com/a", "https://127.0.0.1/a", "https://169.254.169.254/a", "https://10.1.1.1/a", "https://u:p@example.com/a", "data:image/png;base64,eA==", "data:image/svg+xml;base64,AAAA"} {
		if _, err := NormalizeVisionContent(visionTestBlock(ref)); !errors.Is(err, models.ErrInvalidImageInput) {
			t.Fatalf("accepted invalid image %q: %v", ref, err)
		}
	}
	for _, block := range []map[string]interface{}{{"type": "input_image", "file_id": "file-secret"}, {"type": "image", "source": map[string]interface{}{"type": "file", "file_id": "file-secret"}}} {
		if _, err := NormalizeVisionContent(block); err == nil {
			t.Fatal("provider file reference must not be dropped or forwarded")
		}
	}
	items := make([]interface{}, maxVisionImages+1)
	for i := range items {
		items[i] = visionTestBlock("https://example.com/a.png")
	}
	if _, err := NormalizeVisionContent(items); err == nil {
		t.Fatal("image count not bounded")
	}
}

func TestVisionAccountingDoesNotTokenizeImageBytes(t *testing.T) {
	var estimates []int
	for _, data := range []string{"small", strings.Repeat("A", 100000)} {
		raw, _ := json.Marshal(map[string]interface{}{"messages": []interface{}{map[string]interface{}{"role": "user", "content": []interface{}{visionTestBlock("data:image/png;base64," + data)}}}})
		estimates = append(estimates, visionAccountingTokens(raw))
		if strings.Contains(visionSafePayload(raw), "base64") {
			t.Fatal("base64 leaked into audit")
		}
	}
	if estimates[0] != estimates[1] || estimates[0] < visionImageTokenReservation {
		t.Fatalf("incorrect image reservation %v", estimates)
	}
}

func TestVisionRejectsNestedWrappersAndInvalidDetail(t *testing.T) {
	nested := map[string]interface{}{"type": "unexpected", "content": []interface{}{visionTestBlock("https://example.com/a.png")}}
	req := ChatCompletionRequest{Messages: []ChatMessage{{Role: "user", Content: []interface{}{nested}}}}
	if err := NormalizeVisionRequest(&req); !errors.Is(err, models.ErrInvalidImageInput) {
		t.Fatalf("accepted nested wrapper: %v", err)
	}
	if blocks, handled := visionAnthropicBlocks(nested); !handled || len(blocks) != 0 {
		t.Fatal("internal wrapper conversion should fail closed")
	}
	for _, block := range []map[string]interface{}{
		{"type": "input_image", "image_url": "https://example.com/a.png", "detail": 12},
		{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/a.png", "detail": true}},
	} {
		if _, err := NormalizeVisionContent(block); !errors.Is(err, models.ErrInvalidImageInput) {
			t.Fatal("invalid detail silently discarded")
		}
	}
}

func TestVisionTransportPreservesProtocolSemantics(t *testing.T) {
	for _, tc := range []struct {
		role, protocol, detail string
		want                   bool
	}{
		{"user", models.ProtocolTypeOpenAI, "high", true},
		{"user", models.ProtocolTypeAnthropic, "auto", true},
		{"user", models.ProtocolTypeAnthropic, "high", false},
		{"tool", models.ProtocolTypeAnthropic, "", true},
		{"tool", models.ProtocolTypeOpenAI, "", false},
		{"system", models.ProtocolTypeAnthropic, "", false},
	} {
		block := visionTestBlock("https://example.com/a.png")
		block["image_url"].(map[string]interface{})["detail"] = tc.detail
		req := ChatCompletionRequest{Messages: []ChatMessage{{Role: tc.role, Content: []interface{}{block}}}}
		if got := visionTransportSupported(tc.protocol, req); got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func TestVisionOverrideAndUnknownRouting(t *testing.T) {
	req := ChatCompletionRequest{Messages: []ChatMessage{{Content: []interface{}{visionTestBlock("https://example.com/a.png")}}}}
	for _, supported := range []bool{false, true} {
		for _, source := range []string{"", "unknown", "provider_metadata"} {
			m := &models.LLMModel{IsActive: true, SupportsVision: !supported, CapabilitySource: source, VisionSupportOverride: &supported}
			if containsRoutingReason(routingRejectionReasons(m, req, routingDecision{}), "vision_not_supported") == supported {
				t.Fatal("manual override ignored")
			}
		}
	}
	if models.LLMVisionSupported(&models.LLMModel{}) {
		t.Fatal("unknown model asserted vision support")
	}
	if models.LLMVisionSupported(&models.LLMModel{CapabilitySource: "provider_metadata"}) {
		t.Fatal("unrelated metadata asserted vision")
	}
}

func TestRuntimeCatalogVisionDoesNotLeakHiddenCapability(t *testing.T) {
	items := routingModels()
	yes := true
	items[0].VisionSupportOverride = &yes
	items[0].VisibilityScope = "selected_users"
	items[0].AllowedUserIDs = []int{1}
	s := routingGateway(items, &stubAuditEventService{})
	for _, user := range []int{1, 2} {
		catalog, err := s.RuntimeCatalog(user, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range catalog {
			if m.ID == "auto" && m.Vision != (user == 1) {
				t.Fatalf("catalog leaked or lost vision for user %d: %+v", user, m)
			}
		}
	}
}

func TestExplicitVisionOptOutCannotEscapeViaFailover(t *testing.T) {
	items := routingModels()
	yes, no := true, false
	items[0].VisionSupportOverride = &no
	items[1].VisionSupportOverride = &yes
	items[1].ModelGroup = items[0].ModelGroup
	s := routingGateway(items, &stubAuditEventService{})
	req := ChatCompletionRequest{Model: items[0].DisplayName, Messages: []ChatMessage{{Role: "user", Content: []interface{}{visionTestBlock("https://example.com/a.png")}}}}
	if _, err := s.prepareChatRequest(t.Context(), 1, req); !errors.Is(err, models.ErrVisionNotSupported) {
		t.Fatalf("disabled explicit route escaped: %v", err)
	}
	req.Model = items[1].DisplayName
	prepared, err := s.prepareChatRequest(t.Context(), 1, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range prepared.deploymentCandidates {
		if !models.LLMVisionSupported(m) {
			t.Fatal("text-only fallback admitted")
		}
	}
}
