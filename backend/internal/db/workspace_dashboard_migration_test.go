package db

import (
	"strings"
	"testing"
)

func TestWorkspaceObservationMigrationEmbedded(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/110_add_workspace_dashboard_observations.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CREATE TABLE IF NOT EXISTS workspace_dashboard_observations", "PRIMARY KEY (user_id,bucket_start)", "observed_at", "snapshot_json", "idx_workspace_observation_retention"} {
		if !strings.Contains(string(raw), want) {
			t.Fatal("missing", want)
		}
	}
	if len(splitSQLStatements(string(raw))) != 1 {
		t.Fatal("unexpected statements")
	}
}
