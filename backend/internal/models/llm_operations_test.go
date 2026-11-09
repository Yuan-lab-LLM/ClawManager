package models

import "testing"

func TestModelOperationCapabilitiesAreIndependent(t *testing.T) {
	yes := true
	for _, kind := range []string{"", "chat", "image", "other", "future-kind"} {
		m := &LLMModel{ModelKind: kind, ProviderType: ProviderTypeLocal, VisionSupportOverride: &yes, ImageGenerationEnabled: true}
		chat := kind == "" || kind == ModelKindChat
		if LLMChatAvailable(m) != chat || LLMVisionInputAvailable(m) != chat {
			t.Fatalf("kind %q incorrectly exposes chat/vision", kind)
		}
		if LLMNativeImagesAvailable(m) != (kind == ModelKindImage) {
			t.Fatalf("image kind %q", kind)
		}
		m.ProtocolType = ProtocolTypeAnthropic
		if LLMNativeImagesAvailable(m) {
			t.Fatal("Anthropic exposed native images")
		}
	}
	if LLMChatAvailable(nil) || LLMNativeImagesAvailable(nil) {
		t.Fatal("nil capabilities")
	}
	items := []LLMModel{{ModelKind: "image"}, {DisplayName: "legacy"}, {ModelKind: "chat"}}
	chat := ChatModels(items)
	if len(chat) != 2 || chat[0].DisplayName != "legacy" || items[0].ModelKind != "image" {
		t.Fatal("filter changed original slice", items, chat)
	}
}
