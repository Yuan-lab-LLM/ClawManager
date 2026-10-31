package db

import (
	"strings"
	"testing"
)

func TestMigration054AddsQualityTierIdempotently(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/054_add_llm_model_quality_tier.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"information_schema.COLUMNS", "COLUMN_NAME = 'quality_tier'", "quality_tier INT NOT NULL DEFAULT 0", "PREPARE stmt FROM @stmt", "EXECUTE stmt", "DEALLOCATE PREPARE stmt"} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("missing %s", required)
		}
	}
	if len(splitSQLStatements(string(raw))) != 4 {
		t.Fatal("migration should contain four executable statements")
	}
}
