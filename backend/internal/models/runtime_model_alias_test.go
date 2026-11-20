package models

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeModelAliasWireContract(t *testing.T) {
	if got := RuntimeModelAlias(" abc "); got != "cm-model-v1-ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal(got)
	}
}

func TestOpenClawProviderRefWireContract(t *testing.T) {
	for _, name := range []string{"Original Name", "glm-5.3-public (openai)", "中文 模型", "Vendor/Model (public)", "model\\name", "/leading", "trailing/", "double//slash"} {
		m := LLMModel{DisplayName: name}
		ref := RuntimeOpenClawProviderRef(m)
		provider := RuntimeCatalogProviderName(m)
		if strings.ContainsAny(provider, " ()中文") {
			want := fmt.Sprintf("cm-provider-v1-%x", sha256.Sum256([]byte(provider)))
			if ref.ProviderID != want {
				t.Fatalf("provider hash mismatch: %q -> %q", name, ref.ProviderID)
			}
		}
		if ref.ModelID != RuntimeModelAlias(name) {
			t.Fatalf("model alias mismatch: %q -> %q", name, ref.ModelID)
		}
	}
	if ref := RuntimeOpenClawProviderRef(LLMModel{DisplayName: "Org/Model:V1"}); ref.ProviderID != "org-model:v1" || ref.ModelID != "org/model:v1" {
		t.Fatal("valid namespace/model should be retained", ref)
	}
	if RuntimeOpenClawProviderRef(LLMModel{DisplayName: "Original Name"}) == RuntimeOpenClawProviderRef(LLMModel{DisplayName: "original name"}) {
		t.Fatal("case-sensitive native hash was lost")
	}
}
