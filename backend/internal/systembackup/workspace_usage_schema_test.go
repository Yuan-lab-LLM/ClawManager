package systembackup

import "testing"

func TestWorkspaceUsageBackupSchema(t *testing.T) {
	tables, err := BuiltinSchema()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"workspace_usage_observed_at": "datetime(6)", "workspace_usage_epoch": "bigint", "workspace_usage_blocked": "tinyint(1)"}
	for _, table := range tables {
		if table.Name != "instances" {
			continue
		}
		for _, column := range table.Columns {
			if typ, ok := want[column.Name]; ok {
				if column.Type != typ {
					t.Fatal(column)
				}
				if column.Name == "workspace_usage_observed_at" && column.Nullable != "YES" {
					t.Fatal("legacy zero must remain unobserved")
				}
				delete(want, column.Name)
			}
		}
	}
	if len(want) != 0 {
		t.Fatal("missing observation schema metadata", want)
	}
	policy, ok := Policy("instances")
	if !ok || policy.BackupStrategy != "mysql_dump" || policy.RestoreStrategy != "normalize" {
		t.Fatal("instance observation lost backup policy")
	}
}
