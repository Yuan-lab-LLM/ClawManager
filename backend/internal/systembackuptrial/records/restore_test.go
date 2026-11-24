package records

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func trialFixture() Snapshot {
	rows := map[string]Row{
		"users":                        {"id": Text("1"), "username": Text("operator"), "password_hash": Text("test"), "role": Text("admin"), "auth_provider": Text("local"), "is_active": Text("1")},
		"instances":                    {"id": Text("7"), "user_id": Text("1"), "type": Text("hermes"), "mount_path": Text("/config"), "openclaw_config_snapshot_id": Text("2"), "environment_overrides_json": Text(`{"STATIC":"value"}`)},
		"openclaw_injection_snapshots": {"id": Text("2"), "user_id": Text("1"), "instance_id": Text("7"), "rendered_env_json": Text(`{"FIXTURE":"test"}`), "status": Text("active")},
		"instance_config_revisions":    {"id": Text("3"), "instance_id": Text("7"), "published_by": Text("1"), "content_json": Text(`{"fixture":true}`), "status": Text("published")},
		"instance_desired_state":       {"id": Text("4"), "instance_id": Text("7"), "updated_by": Text("1"), "desired_config_revision_id": Text("3")},
	}
	s := Snapshot{Version: Version}
	for name, projection := range trialColumns {
		table := Table{Name: name, Engine: "InnoDB", PrimaryKey: []string{"id"}}
		row := make(Row)
		for _, name := range strings.Fields(projection) {
			table.Columns = append(table.Columns, Column{Name: name, Type: "varchar(255)", Nullable: true})
			row[name] = Cell{Null: true}
			if value, ok := rows[table.Name][name]; ok {
				row[name] = value
			}
		}
		if rows[name] != nil {
			table.Rows = []Row{row}
		}
		s.Tables = append(s.Tables, table)
	}
	return s
}

func TestTrialRetainsOnlyReferencedStartupData(t *testing.T) {
	s := trialFixture()
	if err := ValidateTrial(s); err != nil {
		t.Fatal(err)
	}
	for _, table := range s.Tables {
		if table.Name == "openclaw_injection_snapshots" {
			table.Rows[0]["user_id"] = Text("9")
		}
	}
	if err := ValidateTrial(s); !errors.Is(err, ErrData) {
		t.Fatalf("wrong-owner snapshot accepted: %v", err)
	}
}

func TestTrialNormalizePreservesConfigAndResetsBindings(t *testing.T) {
	s := trialFixture()
	var target []Table
	for _, table := range s.Tables {
		cols := append([]Column(nil), table.Columns...)
		for name := range trialReset[table.Name] {
			cols = append(cols, Column{Name: name, Type: "varchar(255)", Nullable: true})
		}
		if table.Name == "instances" {
			cols = append(cols, Column{Name: "owner_normalized", Extra: "STORED GENERATED"})
		}
		if table.Name == "users" {
			cols = append(cols, Column{Name: "local_username_key", Extra: "STORED GENERATED"})
		}
		table.Columns, table.Rows = cols, nil
		target = append(target, table)
	}
	plan, err := normalizeTrial(context.Background(), s, target, Placement{Namespace: "cm-drill-test", StorageClass: "isolated"})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range plan.Data.Tables {
		row := table.Rows[0]
		switch table.Name {
		case "instances":
			if row["user_id"].String() != "1" || row["status"].String() != "stopped" ||
				!row["workspace_path"].Null || !row["pod_name"].Null || row["mount_path"].String() != "/config" ||
				row["environment_overrides_json"].String() != `{"STATIC":"value"}` {
				t.Fatal("incorrect restored instance identity, placement or static config")
			}
		case "instance_desired_state":
			if row["desired_config_revision_id"].String() != "3" || row["desired_power_state"].String() != "stopped" {
				t.Fatal("incorrect revision binding or desired power state")
			}
		case "openclaw_injection_snapshots":
			if row["rendered_env_json"].String() != `{"FIXTURE":"test"}` {
				t.Fatal("incorrect rendered startup config")
			}
		}
	}
}
