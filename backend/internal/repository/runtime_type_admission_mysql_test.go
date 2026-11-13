package repository

import (
	"clawreef/internal/models"
	"context"
	"strings"
	"testing"
)

func TestRuntimeTypedRolloutAdmissionMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,deployment_name,image_ref,agent_endpoint,state,capacity) VALUES(11,'deepseek-harness','runtime','deepseek-harness','deepseek-harness','image','http://agent','ready',1)`)
	exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation) VALUES(51,'deepseek-harness','lite','running',1)`)
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_port,gateway_id,workspace_path) VALUES(51,11,'deepseek-harness',19000,'gateway','/workspace')`)
	cases := []struct{ name, kind, request, items string }{
		{"scale request", "scale_in", `{"runtime_type":"deepseek-harness"}`, `[]`},
		{"normalized request", "scale_in", `{"runtime_type":" DEEPSEEK-HARNESS "}`, `[]`},
		{"placement", "eviction", `{}`, `[{"source_placement":{"runtime_type":"deepseek-harness"}}]`},
		{"source pod", "migration", `{}`, `[{"source_pod_id":11}]`},
		{"target pod", "balance", `{}`, `[{"target_pod_id":11}]`},
		{"binding", "eviction", `{}`, `[{"instance_id":51}]`},
		{"empty history", "balance", `{"source_pod_id":11}`, `[]`},
		{"empty binding history", "balance", `{"instance_ids":[51]}`, `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec(`DELETE FROM runtime_rollouts`)
			exec(`DELETE FROM runtime_management_operations`)
			exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('held',?,'needs_attention',?,?,NOW(),NOW())`, tc.kind, tc.request, tc.items)
			repo := NewRuntimeRolloutRepository(sess)
			if err := repo.Create(ctx, &models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: "image", Status: "pending"}); err != nil {
				t.Fatalf("held DSH blocked OpenClaw: %v", err)
			}
			if err := repo.Create(ctx, &models.RuntimeRollout{RuntimeType: "deepseek-harness", TargetImageRef: "image", Status: "pending"}); err == nil {
				t.Fatal("same-type held operation admitted rollout")
			}
		})
	}
}

func TestRuntimePreparationUnknownScopeFailsClosedMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('unknown','rollout_wake','completed',JSON_OBJECT('phase','ready','ready_until_unix',UNIX_TIMESTAMP()+3600),'[]',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	if err := CheckRuntimePreparationReservationsForType(context.Background(), sess, "", "openclaw"); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("unknown preparation scope must fail closed: %v", err)
	}
}

func TestRuntimeUnsupportedScopeFailsClosedMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('unsupported','scale_in','needs_attention','{"runtime_type":"unsupported-native"}','[]',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	if err := CheckRuntimeManagementOperationsForType(context.Background(), sess, "openclaw", ""); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("unsupported historical type escaped isolation: %v", err)
	}
}

func TestRuntimeTypedRolloutAndRecoveryConflictsMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	ctx := context.Background()
	exec := func(query string) {
		t.Helper()
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase) VALUES(71,'deepseek-harness','image','running','preflight')`)
	check := func() {
		t.Helper()
		if err := CheckRuntimeControlAdmission(ctx, sess, "openclaw", ""); err != nil {
			t.Fatalf("other-type conflict: %v", err)
		}
		if err := CheckRuntimeControlAdmission(ctx, sess, "deepseek-harness", ""); err == nil {
			t.Fatal("same-type conflict admitted")
		}
	}
	check()
	exec(`UPDATE runtime_rollouts SET status='error' WHERE id=71`)
	exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation) VALUES(72,'deepseek-harness','lite','error',1)`)
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,source_generation,recovery_status,created_at,updated_at) VALUES(71,72,'failed',1,'needs_attention',NOW(),NOW())`)
	check()
	exec(`UPDATE runtime_rollouts SET runtime_type='unsupported-native' WHERE id=71`)
	if err := CheckRuntimeControlAdmission(ctx, sess, "openclaw", ""); err == nil {
		t.Fatal("unknown historical recovery scope admitted")
	}
}

func TestRuntimePartialHistoricalScopeFailsClosedMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,deployment_name,image_ref,agent_endpoint,state,capacity) VALUES(11,'deepseek-harness','runtime','dsh','dsh','image','http://agent','ready',1)`)
	exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation) VALUES(51,'deepseek-harness','lite','running',1),(52,'openclaw','lite','error',1)`)
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_port,gateway_id,workspace_path) VALUES(51,11,'deepseek-harness',19000,'gateway','/workspace')`)
	for _, tc := range []struct{ name, request, items string }{
		{"partial request items", `{"instance_ids":[51,52]}`, `[{"instance_id":51},{"instance_id":52}]`},
		{"partial request empty history", `{"instance_ids":[51,52]}`, `[]`},
		{"request-only missing item", `{"instance_ids":[51,52]}`, `[{"instance_id":51}]`},
		{"source with unknown request-only instance", `{"source_pod_id":11,"instance_ids":[52]}`, `[]`},
		{"known source unknown item", `{"source_pod_id":11}`, `[{"instance_id":52}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec(`DELETE FROM runtime_management_operations`)
			exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('partial-held','migration','needs_attention',?,?,NOW(),NOW())`, tc.request, tc.items)
			for _, kind := range []string{"openclaw", "deepseek-harness"} {
				if err := CheckRuntimeManagementOperationsForType(ctx, sess, kind, ""); err == nil || !strings.Contains(err.Error(), "partial-held") || !strings.Contains(err.Error(), "scope") {
					t.Fatalf("%s partial historical scope must fail closed with operation ID: %v", kind, err)
				}
			}
		})
	}
	exec(`DELETE FROM runtime_management_operations`)
	exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('explicit-held','migration','needs_attention','{"runtime_type":"deepseek-harness","instance_ids":[51,52]}','[{"instance_id":51},{"instance_id":52}]',NOW(),NOW())`)
	if err := CheckRuntimeManagementOperationsForType(ctx, sess, "openclaw", ""); err != nil {
		t.Fatalf("explicit whole-operation scope rejected: %v", err)
	}
	if err := CheckRuntimeManagementOperationsForType(ctx, sess, "deepseek-harness", ""); err == nil {
		t.Fatal("explicit same-type scope escaped")
	}
}

func TestRuntimeHistoricalRequestIDsCanUseMatchingFrozenItemEvidenceMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('mixed-held','migration','needs_attention','{"instance_ids":[51,52]}','[{"instance_id":51,"source_placement":{"runtime_type":"deepseek-harness"}},{"instance_id":52,"source_placement":{"runtime_type":"openclaw"}}]',NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"openclaw", "deepseek-harness"} {
		err := CheckRuntimeManagementOperationsForType(context.Background(), sess, kind, "")
		if err == nil || strings.Contains(err.Error(), "scope") || !strings.Contains(err.Error(), "mixed-held") {
			t.Fatalf("matching item evidence must retain the union of known scopes: %v", err)
		}
	}
}
