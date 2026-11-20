package db

import (
	"strings"
	"testing"
)

func TestRuntimeShutdownAuditMigrationDiagnosticOnly(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/080_add_runtime_shutdown_audits.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS runtime_shutdown_audits") {
		t.Fatalf("unexpected migration %v", statements)
	}
	for _, want := range []string{"owner_user_id INT NOT NULL", "generation INT NOT NULL", "claim_id CHAR(64)", "UNIQUE KEY uq_runtime_shutdown_observation", "ix_runtime_shutdown_history", "REFERENCES instances(id) ON DELETE CASCADE"} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(want)
		}
	}
}
