package db

import (
	"strings"
	"testing"
)

func TestMigration059KeepsReasoningAdapterOptIn(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/059_add_llm_reasoning_control_mode.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"information_schema.COLUMNS", "COLUMN_NAME = 'reasoning_control_mode'", "VARCHAR(40) NOT NULL DEFAULT ''''"} {
		if !strings.Contains(string(raw), part) {
			t.Fatalf("missing %s", part)
		}
	}
	if len(splitSQLStatements(string(raw))) != 4 {
		t.Fatal("expected idempotent four-statement migration")
	}
}
