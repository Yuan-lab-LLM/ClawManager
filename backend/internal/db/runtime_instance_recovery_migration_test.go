package db

import (
	"strings"
	"testing"
)

func TestRuntimeRecoveryMigrationOnlyAddsEmptyRecoveryJournal(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/103_add_instance_runtime_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS instance_runtime_recovery") {
		t.Fatalf("recovery migration changes existing lifecycle: %v", statements)
	}
	for _, required := range []string{"instance_id INT NOT NULL PRIMARY KEY", "attempts", "next_retry_at DATETIME(6)", "healthy_since DATETIME(6)", "source_pod_id", "source_gateway_port", "generation", "reason", "REFERENCES instances(id) ON DELETE CASCADE"} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("missing %s", required)
		}
	}
}

func TestRuntimeRecoveryPhaseMigrationPreservesExistingAttempts(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/106_add_runtime_recovery_phase.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.Contains(statements[0], "ADD COLUMN phase VARCHAR(32) NOT NULL DEFAULT ''") || strings.Contains(strings.ToUpper(string(raw)), "UPDATE ") {
		t.Fatal("phase migration must preserve existing lifecycle and retry budgets")
	}
}
