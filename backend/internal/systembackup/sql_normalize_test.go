package systembackup

import (
	"bytes"
	"clawreef/internal/systembackup/team"
	"context"
	"testing"
	"time"
)

func TestExpiredCredentialsReadBackWithDeclaredSQLPrecision(t *testing.T) {
	db := fixtureDatabase(t)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE northbound_auth_challenges(id BIGINT PRIMARY KEY,status VARCHAR(32),expires_at DATETIME(6)) ENGINE=InnoDB",
		"CREATE TABLE northbound_sessions(id BIGINT PRIMARY KEY,status VARCHAR(32),access_expires_at DATETIME(6),refresh_expires_at DATETIME(6)) ENGINE=InnoDB",
		"INSERT INTO northbound_auth_challenges VALUES(1,'pending','2026-10-24 12:34:56.123456')",
		"INSERT INTO northbound_sessions VALUES(1,'active','2026-10-24 12:34:56.123456','2026-10-25 12:34:56.654321')",
	} {
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	var original, normalized bytes.Buffer
	if _, e := ExportMySQL(ctx, db, &original, []string{"northbound_auth_challenges", "northbound_sessions"}); e != nil {
		t.Fatal(e)
	}
	if _, e := NormalizeSQLSnapshot(ctx, bytes.NewReader(original.Bytes()), &normalized, time.Now().UTC(), func(SQLTable, team.Change) error { return nil }); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"UPDATE northbound_auth_challenges SET status='used',expires_at='2000-01-01 00:00:00'", "UPDATE northbound_sessions SET status='revoked',access_expires_at='2000-01-01 00:00:00',refresh_expires_at='2000-01-01 00:00:00'"} {
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := VerifyMySQL(ctx, db, bytes.NewReader(normalized.Bytes()), 100); e != nil {
		t.Fatal("normalized dates failed exact database readback", e)
	}
}

func TestNormalizePassedPreflightRetainsOriginalWithoutResumingUpgrade(t *testing.T) {
	row := team.Row{"id": team.Value("7"), "status": team.Value("preflight_passed"), "phase": team.Value("preflight"), "preflight_json": team.Value(`{"saved":"original assessment"}`), "auto_rollback": team.Value("1")}
	table := SQLTable{Name: "runtime_rollouts", Columns: []SQLColumn{{Name: "status"}, {Name: "phase"}, {Name: "preflight_json"}, {Name: "auto_rollback"}}}
	change, e := NormalizeDataRow(table, row, time.Now().UTC())
	if e != nil {
		t.Fatal("valid preflight state rejected", e)
	}
	if string(change.Before["status"].Data) != "preflight_passed" || string(change.After["status"].Data) != "error" || string(change.After["auto_rollback"].Data) != "0" || !bytes.Equal(change.After["preflight_json"].Data, row["preflight_json"].Data) {
		t.Fatal("lost evidence or retained executable preflight")
	}
}

func TestNormalizeCurrentUpgradeIntermediateStates(t *testing.T) {
	for _, state := range []string{"prepared", "snapshotted", "restart_ready", "gateway_verified", "restored"} {
		row := team.Row{"id": team.Value("1"), "state": team.Value(state), "operation_token": team.Value("source-secret")}
		table := SQLTable{Name: "runtime_upgrade_items", Columns: []SQLColumn{{Name: "state"}, {Name: "operation_token", Nullable: "YES"}}}
		change, e := NormalizeDataRow(table, row, time.Now().UTC())
		if e != nil {
			t.Errorf("current state %s rejected: %v", state, e)
			continue
		}
		if string(change.Before["state"].Data) != state || string(change.After["state"].Data) != "failed" || !change.After["operation_token"].IsNull || !change.Quarantined {
			t.Errorf("unsafe intermediate upgrade restore: %+v", change)
		}
	}
}

func TestRestoredGatewayStateCannotAuthenticateOrReplay(t *testing.T) {
	cases := []struct {
		table string
		row   team.Row
		want  map[string]string
	}{
		{"gateway_keys", team.Row{"status": team.Value("active"), "token_hash": team.Value("unique-original-hash"), "expires_at": team.Null()}, map[string]string{"status": "revoked", "token_hash": "unique-original-hash", "expires_at": "2000-01-01 00:00:00"}},
		{"routing_classifier_settings", team.Row{"routing_mode": team.Value("laya"), "base_url": team.Value("https://private.invalid"), "encrypted_api_key": team.Value("ciphertext")}, map[string]string{"routing_mode": "rules", "base_url": "", "encrypted_api_key": ""}},
		{"gateway_image_artifacts", team.Row{"ready": team.Value("1"), "expires_at": team.Value("2027-01-01 00:00:00")}, map[string]string{"ready": "0", "expires_at": "2000-01-01 00:00:00"}},
		{"gateway_image_jobs", team.Row{"state": team.Value("running"), "settled": team.Value("0"), "analytics_recorded": team.Value("0"), "charge": team.Value("0.25")}, map[string]string{"state": "failed", "settled": "1", "analytics_recorded": "1", "charge": "0.25"}},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			table := SQLTable{Name: tc.table}
			for name := range tc.row {
				table.Columns = append(table.Columns, SQLColumn{Name: name})
			}
			change, err := NormalizeDataRow(table, tc.row, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tc.want {
				if string(change.After[name].Data) != want {
					t.Fatalf("%s=%q, want %q", name, change.After[name].Data, want)
				}
				if !bytes.Equal(change.Before[name].Data, tc.row[name].Data) {
					t.Fatal("original evidence changed")
				}
			}
		})
	}
	for _, name := range []string{"gateway_runtime_tasks", "gateway_runtime_task_models", "gateway_runtime_protocols", "gateway_runtime_maintenance", "gateway_runtime_upgrades", "gateway_runtime_upgrade_stops", "instance_workspace_write_intents"} {
		policy, ok := Policy(name)
		if !ok || policy.BackupStrategy != "metadata_only" || policy.RestoreStrategy != "reference_only" {
			t.Fatalf("live runtime coordination %s would be replayed", name)
		}
	}
}

func TestIdleStopRestoreDoesNotReplayLiveOperations(t *testing.T) {
	for _, name := range []string{"instance_model_activity_requests", "instance_idle_stop_operations"} {
		p, ok := Policy(name)
		if !ok || p.BackupStrategy != "metadata_only" || p.RestoreStrategy != "reference_only" {
			t.Fatalf("live idle state may replay after restore: %s %+v", name, p)
		}
	}
	now := time.Date(2026, 11, 2, 1, 2, 3, 0, time.UTC)
	table := SQLTable{Name: "instance_model_activity", Columns: []SQLColumn{{Name: "start_deadline_at", Nullable: "YES"}, {Name: "stop_operation_id", Nullable: "NO"}}}
	row := team.Row{"auto_stop_disabled": team.Value("1"), "monitoring_since": team.Value("2020-01-01 00:00:00"), "stop_operation_id": team.Value("old-live-op"), "stop_phase": team.Value("stopping"), "start_deadline_at": team.Value("2020-01-01 00:00:00")}
	change, err := NormalizeDataRow(table, row, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(change.After["auto_stop_disabled"].Data) != "1" || string(change.After["stop_operation_id"].Data) != "" || !change.After["start_deadline_at"].IsNull || string(change.After["monitoring_since"].Data) != "2026-11-02 01:02:03.000000" {
		t.Fatalf("unsafe restored idle metadata: %+v", change.After)
	}
}
