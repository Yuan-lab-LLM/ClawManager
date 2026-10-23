package db

import (
	"strings"
	"testing"
)

func TestModelAccessMigrationDefaultsAndIndependentGrants(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/062_add_model_user_access.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, want := range []string{"information_schema.COLUMNS", "DEFAULT ''all''", "access_policy_version", "PRIMARY KEY (model_id, user_id)", "ON DELETE CASCADE", "idx_model_user_grants_user"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if len(splitSQLStatements(sql)) != 9 {
		t.Fatalf("unexpected statement parsing: %d", len(splitSQLStatements(sql)))
	}
}

func TestRuntimeTaskMigrationPreservesRevokedTasksAfterModelDeletion(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/063_add_gateway_runtime_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, want := range []string{"runtime_acknowledged", "revoked", "generation", "PRIMARY KEY (task_key, model_id)", "REFERENCES instances(id) ON DELETE CASCADE"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(sql, "REFERENCES llm_models") {
		t.Fatal("deleting a model must preserve its task cancellation evidence")
	}
	if len(splitSQLStatements(sql)) != 2 {
		t.Fatal("unexpected migration statement count")
	}
}
