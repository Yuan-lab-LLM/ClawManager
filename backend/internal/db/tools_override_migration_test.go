package db

import (
	"strings"
	"testing"
)

func TestMigration058PreservesUnknownTools(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/058_add_llm_model_tools_override.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"information_schema.COLUMNS", "COLUMN_NAME = 'tools_support_override'", "tools_support_override BOOLEAN NULL DEFAULT NULL"} {
		if !strings.Contains(string(raw), part) {
			t.Fatalf("missing %s", part)
		}
	}
	if len(splitSQLStatements(string(raw))) != 4 {
		t.Fatal("expected idempotent four-statement migration")
	}
}
