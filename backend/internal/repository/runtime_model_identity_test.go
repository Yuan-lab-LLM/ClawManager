package repository

import (
	"clawreef/internal/models"
	"regexp"
	"testing"
)

func TestRuntimeModelIdentityUniqueAndIndependentOfName(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newRuntimeModelID()
		if err != nil || !regexp.MustCompile(`^cm-deploy-v1-[0-9a-f]{32}$`).MatchString(id) || seen[id] {
			t.Fatalf("invalid or reused ID: %q %v", id, err)
		}
		seen[id] = true
	}
	m := &models.LLMModel{DisplayName: " Old Model ", RuntimeModelID: "cm-deploy-v1-test"}
	before := runtimeModelAliases(m)
	m.DisplayName = "New Model"
	after := runtimeModelAliases(m)
	if before[0] != "old model" || before[1] != after[1] || before[2] == after[2] {
		t.Fatal("name and identity must be independent", before, after)
	}
}
