package systembackup

import (
	"clawreef/internal/systembackup/team"
	"reflect"
	"testing"
	"time"
)

func TestRuntimeRecoveryBudgetIsCapturedWithoutReplayingPlacement(t *testing.T) {
	policy, ok := Policy("instance_runtime_recovery")
	if !ok || policy.Category != "instances" || policy.BackupStrategy != "mysql_dump" || policy.RestoreStrategy != "normalize" {
		t.Fatal("durable retry budget is missing from encrypted backup")
	}
	tables, err := BuiltinSchema()
	if err != nil {
		t.Fatal(err)
	}
	var table SQLTable
	for _, candidate := range tables {
		if candidate.Name == "instance_runtime_recovery" {
			table = candidate
		}
	}
	if table.Name == "" || !reflect.DeepEqual(table.PrimaryKey, []string{"instance_id"}) || len(table.Columns) != 10 {
		t.Fatal("recovery table missing from static schema contract")
	}
	row := team.Row{"phase": team.Value("placing"), "instance_id": team.Value("7"), "attempts": team.Value("3"), "next_retry_at": team.Value("2026-11-06 13:00:00.000000"), "reason": team.Value("gateway_process_exited"), "healthy_since": team.Value("2026-11-06 12:59:00.000000"), "source_pod_id": team.Value("9"), "source_gateway_port": team.Value("20000"), "generation": team.Value("3"), "updated_at": team.Value("2026-11-06 12:00:00.000000")}
	change, err := NormalizeDataRow(table, row, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"instance_id", "attempts", "reason", "generation", "updated_at"} {
		if !reflect.DeepEqual(change.After[field], row[field]) {
			t.Fatalf("retry budget/history %s was cleared", field)
		}
	}
	for _, field := range []string{"next_retry_at", "healthy_since"} {
		if !change.After[field].IsNull {
			t.Fatalf("restored %s can replay automatic work", field)
		}
	}
	for _, field := range []string{"source_pod_id", "source_gateway_port"} {
		if string(change.After[field].Data) != "0" {
			t.Fatalf("restored %s references a foreign runtime", field)
		}
	}
	if string(change.After["phase"].Data) != "" {
		t.Fatal("restored stop/placement proof may replay across installations")
	}
	if !change.Quarantined || !reflect.DeepEqual(change.Before, row) {
		t.Fatal("normalization lost immutable original recovery history")
	}
}

func TestRuntimeFailoverIntentIsNotRunnableBackupData(t *testing.T) {
	policy, ok := Policy("runtime_failover_intents")
	if !ok || policy.BackupStrategy != "metadata_only" || policy.RestoreStrategy != "reference_only" {
		t.Fatalf("live source failover authority must be excluded: %+v", policy)
	}
}
