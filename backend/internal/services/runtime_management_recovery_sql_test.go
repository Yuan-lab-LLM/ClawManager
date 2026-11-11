package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRuntimeRecoveryConsumesProvenPreviousPlacementMySQL(t *testing.T) {
	for _, scenario := range []string{"retired placement", "same slot previous generation", "missing prior proof", "same slot missing prior proof", "current still running", "different prior UID", "unfinished prior stop", "newer checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			s, kube, _ := recoveryFlowFixture(t, "stopped")
			ctx := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := s.recoveryEvictionPreflight(ctx, RuntimeSchedulingRequest{SourcePodID: 1, Mode: "recovery", InstanceIDs: []int{101}})
			if err != nil || !plan.Allowed {
				t.Fatalf("preflight: %+v %v", plan, err)
			}
			item := plan.Items[0]
			item.Recovery.Phase, item.Recovery.SourceStopConfirmed = "source_stopped", true
			raw, _ := json.Marshal([]RuntimeSchedulingItem{item})
			exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,owner_token,created_at,updated_at) VALUES('op','eviction','processing','{}',?,?,NOW(),NOW())", string(raw), s.token)
			exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'op',NOW())")
			exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,agent_endpoint,deployment_name,image_ref,state,capacity) VALUES(9,'opencode','runtime','previous','previous','https://10.1.0.9:19090','pool','image','error',8)")
			priorPod, priorPort, priorUID, priorEndpoint := 9, 20021, "previous", "https://10.1.0.9:19090"
			if strings.HasPrefix(scenario, "same slot") {
				priorPod, priorPort, priorUID, priorEndpoint = 1, 20000, "old", item.Recovery.SourceEndpoint
			}
			exec("INSERT INTO instance_runtime_recovery(instance_id,attempts,reason,source_pod_id,source_gateway_port,generation,phase,updated_at) VALUES(101,1,'replaced',?,?,4,'placing',NOW())", priorPod, priorPort)
			assignments := k8sservice.NewRuntimeAssignmentService(kube, "runtime")
			if scenario != "current still running" {
				if err = assignments.MarkDraining(ctx, item.Recovery.SourceEndpoint, item.SourceGatewayID); err != nil {
					t.Fatal(err)
				}
				if err = assignments.ConfirmStopped(ctx, item.Recovery.SourceEndpoint, item.SourceGatewayID); err != nil {
					t.Fatal(err)
				}
			}
			secret, err := kube.CoreV1().Secrets("runtime").Get(ctx, "lite-instance-101-g4", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			secret.Name, secret.UID, secret.ResourceVersion = "lite-instance-101-g3", "previous-secret", ""
			secret.Labels["clawmanager.io/generation"] = "3"
			proof, _ := json.Marshal(k8sservice.RuntimeStoppedSourceAudit{InstanceID: 101, UserID: 1, Generation: 3, RuntimeType: "opencode", PodUID: priorUID, Endpoint: priorEndpoint, GatewayID: "gw-101-3", WorkspacePath: item.Recovery.SourceBinding.WorkspacePath, Stopped: true})
			secret.Annotations = map[string]string{k8sservice.RuntimeStoppedSourceAnnotationPrefix + "3": string(proof)}
			if strings.Contains(scenario, "missing prior proof") {
				secret.Annotations = nil
			}
			if _, err = kube.CoreV1().Secrets("runtime").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "different prior UID":
				exec("UPDATE runtime_pods SET pod_uid='reused' WHERE id=9")
			case "unfinished prior stop":
				exec("UPDATE instance_runtime_recovery SET phase='source_stopped' WHERE instance_id=101")
			case "newer checkpoint":
				exec("UPDATE instance_runtime_recovery SET generation=5 WHERE instance_id=101")
			}
			state, err := s.commitRecoveredEviction(ctx, item, "op")
			allowed := scenario == "retired placement" || scenario == "same slot previous generation"
			if allowed && (err != nil || state != "stopped") {
				t.Fatalf("proven prior placement blocked: %s %v", state, err)
			}
			if !allowed && err == nil {
				t.Fatal("unproven/conflicting prior writer consumed")
			}
			for _, table := range []string{"instance_runtime_recovery", "instance_runtime_bindings"} {
				var count int
				row, _ := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM " + table + " WHERE instance_id=101")
				if err = row.Scan(&count); err != nil {
					t.Fatal(err)
				}
				if (allowed && count != 0) || (!allowed && count != 1) {
					t.Fatalf("%s unexpected count %d", table, count)
				}
			}
		})
	}
}

func TestRuntimeRecoverySQLReleaseLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, status, desired, want string
		generation                  int
	}{
		{"running", "running", "running", "waiting_capacity", 4},
		{"pending receipt", "running", "running", "waiting_capacity", 4},
		{"already fenced", "error", "running", "waiting_capacity", 5},
		{"creating deadline", "creating", "running", "waiting_capacity", 4},
		{"explicit stop intent", "creating", "running", "stopped", 4},
		{"latest stopped", "stopped", "running", "stopped", 4},
		{"desired stop", "running", "stopped", "stopped", 4},
		{"deleting", "deleting", "running", "deleting", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			s := runtimeManagementMySQLService(sess)
			ctx := context.Background()
			exec := func(q string, args ...interface{}) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO instances(id,user_id,status,runtime_generation) VALUES(101,1,?,?)", tc.status, tc.generation)
			exec("INSERT INTO instance_desired_state(instance_id,desired_power_state) VALUES(101,?)", tc.desired)
			exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots) VALUES(1,'opencode','test','pod','uid','test','image','error',3,3)")
			exec("INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(71,101,1,'opencode','gw',20000,'/workspace','running',4)")
			exec("INSERT INTO instances(id,user_id,status,runtime_generation) VALUES(102,2,'running',4)")
			exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(102,1,'opencode','neighbor',20003,'/neighbor','running',4)")
			exec("INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,start_deadline_at,stop_operation_id) VALUES(101,4,NOW(),DATE_ADD(NOW(),INTERVAL 1 MINUTE),?)", map[bool]string{true: "stop-op", false: ""}[tc.name == "explicit stop intent"])
			binding := models.InstanceRuntimeBinding{ID: 71, InstanceID: 101, RuntimePodID: 1, RuntimeType: "opencode", GatewayID: "gw", GatewayPort: 20000, WorkspacePath: "/workspace", Generation: 4}
			item := RuntimeSchedulingItem{InstanceID: 101, SourcePodID: 1, SourcePodUID: "uid", SourceGatewayID: "gw", SourceGeneration: 4, Recovery: &RuntimeFaultEvictionJournal{Phase: "source_stopped", SourceStopConfirmed: true, SourceUserID: 1, SourceBinding: binding, DesiredStatus: "running"}}
			if tc.status == "creating" {
				item.Recovery.DesiredStatus = "creating"
			}
			if tc.name == "pending receipt" {
				exec("UPDATE instance_runtime_bindings SET gateway_id='pending-101-4' WHERE id=71")
				item.SourceGatewayID = "pending-101-4"
				item.Recovery.SourceBinding.GatewayID = "pending-101-4"
				scope := runtimeeviction.Scope{Version: 1, Namespace: "test", PodName: "pod", NodeName: "node", WorkspaceRoot: "/workspaces", OperationID: "op", InstanceID: 101, UserID: 1, Generation: 4, GatewayID: "gw-101-4", RuntimeType: "opencode", PodUID: "uid", ContainerName: "slot-0", NodeUID: "node", NodeBootID: "boot"}
				item.Recovery.Scope = scope
				item.Recovery.SourceStopConfirmed = false
				item.Recovery.Receipt = &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: "op", PodUID: "uid", ContainerName: "slot-0", NodeUID: "node", NodeBootID: "boot", LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC()}
			}
			other := RuntimeSchedulingItem{InstanceID: 999, State: "completed"}
			raw, _ := json.Marshal([]RuntimeSchedulingItem{item, other})
			exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,owner_token,created_at,updated_at) VALUES('op','eviction','processing','{}',?,?,NOW(),NOW())", string(raw), s.token)
			exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'op',NOW())")
			if tc.name == "running" {
				// Persisted phase alone must not manufacture teardown authority.
				item.Recovery.SourceStopConfirmed = false
				unconfirmed, _ := json.Marshal([]RuntimeSchedulingItem{item, other})
				exec("UPDATE runtime_management_operations SET items_json=? WHERE id='op'", string(unconfirmed))
				if _, err := s.commitRecoveredEviction(ctx, item, "op"); err == nil {
					t.Fatal("unconfirmed stop accepted")
				}
				item.Recovery.SourceStopConfirmed = true
				exec("UPDATE runtime_management_operations SET items_json=? WHERE id='op'", string(raw))
				// A recreated Pod or reused gateway port must roll back both
				// lifecycle and slot release, even with valid old stop proof.
				for _, mutation := range []struct{ change, restore string }{
					{"UPDATE runtime_pods SET pod_uid='replacement' WHERE id=1", "UPDATE runtime_pods SET pod_uid='uid' WHERE id=1"},
					{"UPDATE instance_runtime_bindings SET gateway_port=20006 WHERE id=71", "UPDATE instance_runtime_bindings SET gateway_port=20000 WHERE id=71"},
					{"UPDATE runtime_management_operations SET owner_token='other' WHERE id='op'", ""},
				} {
					exec(mutation.change)
					if _, err := s.commitRecoveredEviction(ctx, item, "op"); err == nil {
						t.Fatal("changed identity accepted")
					}
					var count int
					row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE id=71")
					if err := row.Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 1 {
						t.Fatal("failed transaction released source")
					}
					if mutation.restore != "" {
						exec(mutation.restore)
					} else {
						exec("UPDATE runtime_management_operations SET owner_token=? WHERE id='op'", s.token)
					}
				}
			}
			exec("INSERT INTO runtime_failover_intents(instance_id,user_id,generation,runtime_type,source_pod_id,source_pod_uid,source_endpoint,gateway_id,gateway_port,workspace_path,reason,updated_at) VALUES(101,1,4,'opencode',1,'uid','','gw',20000,'/workspace','test',NOW())")
			if tc.name == "pending receipt" {
				exec("UPDATE runtime_failover_intents SET gateway_id='pending-101-4' WHERE instance_id=101")
			}
			exec("INSERT INTO instance_runtime_recovery(instance_id,attempts,reason,source_pod_id,source_gateway_port,generation,updated_at) VALUES(101,1,'test',1,20000,4,NOW())")
			exec("UPDATE runtime_failover_intents SET generation=5 WHERE instance_id=101")
			if _, err := s.commitRecoveredEviction(ctx, item, "op"); err == nil {
				t.Fatal("newer failover intent consumed")
			}
			exec("UPDATE runtime_failover_intents SET generation=4 WHERE instance_id=101")
			for retry := 0; retry < 2; retry++ {
				got, err := s.commitRecoveredEviction(ctx, item, "op")
				if err != nil || got != tc.want {
					t.Fatalf("retry %d result=%q err=%v", retry, got, err)
				}
			}
			for _, table := range []string{"runtime_failover_intents", "instance_runtime_recovery"} {
				var count int
				row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM " + table + " WHERE instance_id=101")
				if err := row.Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("exact old intent retained in %s", table)
				}
			}
			var state string
			var generation, used int
			row, _ := sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=101")
			if err := row.Scan(&state, &generation); err != nil {
				t.Fatal(err)
			}
			expectedGeneration := tc.generation
			if tc.want == "waiting_capacity" {
				expectedGeneration = 5
			}
			if generation != expectedGeneration {
				t.Fatalf("generation=%d want %d", generation, expectedGeneration)
			}
			if tc.want == "waiting_capacity" {
				var activityGeneration int
				var deadline interface{}
				row, _ := sess.SQL().QueryRow("SELECT generation,start_deadline_at FROM instance_model_activity WHERE instance_id=101")
				if err := row.Scan(&activityGeneration, &deadline); err != nil {
					t.Fatal(err)
				}
				if activityGeneration != 5 || deadline != nil {
					t.Fatalf("activity not reset: gen=%d deadline=%v", activityGeneration, deadline)
				}
			}
			if tc.want == "waiting_capacity" && state != "creating" {
				t.Fatalf("status=%s", state)
			}
			row, _ = sess.SQL().QueryRow("SELECT used_slots FROM runtime_pods WHERE id=1")
			if err := row.Scan(&used); err != nil {
				t.Fatal(err)
			}
			if used != 1 {
				t.Fatalf("neighbor occupancy lost: %d", used)
			}
			row, _ = sess.SQL().QueryRow("SELECT items_json FROM runtime_management_operations WHERE id='op'")
			if err := row.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var items []RuntimeSchedulingItem
			if err := json.Unmarshal(raw, &items); err != nil {
				t.Fatal(err)
			}
			if items[0].Recovery.Phase != "released" || items[1].State != "completed" {
				t.Fatalf("checkpoint=%s", raw)
			}
			if tc.name == "running" {
				// Model losing the release reply and retrying with an old item
				// after a newer user stop. Replay reads only the durable result.
				exec("UPDATE instances SET status='stopped',runtime_generation=6 WHERE id=101")
				state, err := s.commitRecoveredEviction(ctx, item, "op")
				if err != nil || state != "waiting_capacity" {
					t.Fatalf("lost reply replay state=%q err=%v", state, err)
				}
				row, _ := sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=101")
				var latest string
				var gen int
				if err := row.Scan(&latest, &gen); err != nil {
					t.Fatal(err)
				}
				if latest != "stopped" || gen != 6 {
					t.Fatalf("replay changed newer intent: %s/%d", latest, gen)
				}
			}

		})
	}
}
