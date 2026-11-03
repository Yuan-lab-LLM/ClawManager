package db

import (
	"strings"
	"testing"
)

func TestRuntimeUpgradeMigrationOnlyCreatesEmptyFenceTable(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/066_add_runtime_upgrade_fence.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS gateway_runtime_upgrades") {
		t.Fatal(statements)
	}
	for _, required := range []string{"operation_id CHAR(32)", "generation INT", "owner_user_id", "ON DELETE RESTRICT"} {
		if !strings.Contains(statements[0], required) {
			t.Fatal("missing", required)
		}
	}
}

func TestRuntimeUpgradeStopMigrationOnlyCreatesEmptyTable(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/067_add_runtime_upgrade_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS gateway_runtime_upgrade_stops") {
		t.Fatal(statements)
	}
	for _, required := range []string{"operation_id CHAR(32)", "agent_epoch CHAR(32)", "pod_uid", "confirmed_at DATETIME(6) NULL", "REFERENCES gateway_runtime_upgrades(instance_id)"} {
		if !strings.Contains(statements[0], required) {
			t.Fatal("missing", required)
		}
	}
}

func TestWorkspaceWriteMigrationOnlyCreatesEmptyTable(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/068_add_workspace_write_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS instance_workspace_write_intents") {
		t.Fatal(statements)
	}
	for _, required := range []string{"operation_id CHAR(32)", "generation INT", "owner_user_id", "idx_workspace_write_instance", "ON DELETE CASCADE"} {
		if !strings.Contains(statements[0], required) {
			t.Fatal("missing", required)
		}
	}
}
