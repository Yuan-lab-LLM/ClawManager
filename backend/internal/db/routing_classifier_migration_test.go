package db

import (
	"strings"
	"testing"
)

func TestRoutingClassifierMigration(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/060_add_routing_classifier_settings.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.Contains(statements[0], "CREATE TABLE IF NOT EXISTS routing_classifier_settings") || !strings.Contains(statements[0], "encrypted_api_key") {
		t.Fatal(statements)
	}
	if strings.Contains(string(raw), "ALTER TABLE llm_models") || strings.Contains(string(raw), "enabled") {
		t.Fatal("migration must not change live routing")
	}
}

func TestRoutingClassifierModeMigration(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/061_add_routing_classifier_mode.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.Contains(statements[0], "ADD COLUMN routing_mode VARCHAR(16) NOT NULL DEFAULT ''") {
		t.Fatal(statements)
	}
	if strings.Contains(string(raw), "UPDATE ") || strings.Contains(string(raw), "DEFAULT 'hybrid'") {
		t.Fatal("migration must not enable external routing")
	}
}
