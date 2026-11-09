package db

import (
	"strings"
	"testing"
)

func TestRuntimeMaintenanceMigrationLeavesOldInstancesUntouched(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/065_add_runtime_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS gateway_runtime_maintenance") {
		t.Fatal(statements)
	}
	for _, value := range []string{"operation_id CHAR(32)", "reported_at DATETIME(6) NULL", "queued_inputs", "active_jobs", "REFERENCES instances(id)"} {
		if !strings.Contains(string(raw), value) {
			t.Fatal("missing", value)
		}
	}
}
