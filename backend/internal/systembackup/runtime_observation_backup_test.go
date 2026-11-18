package systembackup

import (
	"testing"
	"time"

	"clawreef/internal/systembackup/team"
)

func TestRuntimeObservationBackupSchemaAndRestoreInvalidation(t *testing.T) {
	tables, err := BuiltinSchema()
	if err != nil {
		t.Fatal(err)
	}
	var table SQLTable
	for _, candidate := range tables {
		if candidate.Name == "runtime_pods" {
			table = candidate
		}
	}
	fields := map[string]SQLColumn{}
	for _, field := range table.Columns {
		fields[field.Name] = field
	}
	for name, typ := range map[string]string{"observation_json": "json", "observation_at": "datetime(6)"} {
		if fields[name].Type != typ || fields[name].Nullable != "YES" {
			t.Fatalf("observation field absent from backup schema: %s", name)
		}
	}
	row := team.Row{"id": team.Value("9"), "state": team.Value("ready"), "draining": team.Value("0"), "pod_uid": team.Value("pod-uid"), "observation_json": team.Value(`{"pod_uid":"pod-uid","slots":[]}`), "observation_at": team.Value("2026-11-09 12:00:00.000000")}
	change, err := NormalizeDataRow(table, row, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"pod_uid", "observation_json", "observation_at"} {
		if !change.After[field].IsNull {
			t.Fatalf("restore retained old Runtime evidence: %s", field)
		}
	}
	if !change.Quarantined {
		t.Fatal("restore discarded original observation history")
	}
}
