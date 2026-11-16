package db

import (
	"strings"
	"testing"
)

func TestWorkspaceUsageMigrationEmbedded(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/111_add_workspace_usage_observation.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"workspace_usage_observed_at DATETIME(6) NULL", "workspace_usage_epoch BIGINT NOT NULL DEFAULT 0", "workspace_usage_blocked BOOLEAN NOT NULL DEFAULT FALSE"} {
		if !strings.Contains(string(raw), column) {
			t.Fatal("missing", column)
		}
	}
}
