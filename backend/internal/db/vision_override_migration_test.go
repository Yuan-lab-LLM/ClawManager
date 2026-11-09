package db

import (
	"strings"
	"testing"
)

func TestVisionOverrideMigrationIsAdditive(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/069_add_llm_model_vision_override.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "vision_support_override BOOLEAN NULL DEFAULT NULL") || len(splitSQLStatements(string(raw))) != 4 {
		t.Fatal("expected nullable idempotent override migration")
	}
	if strings.Contains(strings.ToUpper(string(raw)), "UPDATE LLM_MODELS") {
		t.Fatal("must not rewrite existing capability choices")
	}
}
