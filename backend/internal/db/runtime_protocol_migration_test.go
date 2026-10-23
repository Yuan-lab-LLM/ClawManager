package db

import (
	"strings"
	"testing"
)

func TestRuntimeProtocolMigrationDoesNotEnrollOrReplaceExistingInstances(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/064_add_runtime_protocol.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 1 || !strings.HasPrefix(statements[0], "CREATE TABLE IF NOT EXISTS gateway_runtime_protocols") {
		t.Fatalf("migration must only create an empty capability table: %v", statements)
	}
	for _, want := range []string{"instance_id INT NOT NULL PRIMARY KEY", "generation", "protocol", "capabilities_json", "REFERENCES instances(id) ON DELETE CASCADE"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
