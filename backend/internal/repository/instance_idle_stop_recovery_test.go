package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
)

type recoveredStopCompleter interface {
	CompleteStopFromRecovery(context.Context, models.InstanceIdleStopOperation) (bool, error)
}

func TestIdleStopConsumesReleasedRecoveryMySQL(t *testing.T) {
	for _, kind := range []string{"idle", "manual"} {
		for _, state := range []string{"stopped", "deleting"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				repo := idleRuntimeRepository(t)
				store := NewInstanceAutoStopRepository(repo.sess)
				complete, ok := interface{}(store).(recoveredStopCompleter)
				if !ok {
					t.Fatal("receipt-aware stop completion missing")
				}
				exec := func(q string, args ...interface{}) {
					t.Helper()
					if _, err := repo.sess.SQL().Exec(q, args...); err != nil {
						t.Fatal(err)
					}
				}
				exec("CREATE TABLE runtime_management_operations(id VARCHAR(64) PRIMARY KEY,kind VARCHAR(24),status VARCHAR(24),request_json LONGTEXT,items_json LONGTEXT)")
				exec("CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,generation INT)")
				reason := "idle_timeout"
				if kind == "manual" {
					reason = "manual"
				}
				op := models.InstanceIdleStopOperation{ID: "stop", Kind: kind, InstanceID: 7, UserID: 3, Generation: 2, RuntimePodID: 5, GatewayID: "gw-7-2", PodUID: "pod-old", Endpoint: "https://10.0.0.1:19090", WorkspacePath: "/workspaces/openclaw/user-3/instance-7", StorageIdentity: "native-storage", Phase: "error"}
				exec("UPDATE instances SET status=?,runtime_generation=2 WHERE id=7", state)
				initialReason := reason
				if kind == "idle" {
					initialReason = ""
				}
				exec("UPDATE instance_model_activity SET generation=2,stop_operation_id='stop',stop_phase='error',stopped_reason=?,blocker='native stop failed' WHERE instance_id=7", initialReason)
				exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,'error',?,NOW(),NOW())", op.ID, op.InstanceID, op.UserID, op.Generation, op.RuntimePodID, op.GatewayID, op.PodUID, op.Endpoint, op.WorkspacePath, op.StorageIdentity, op.Kind)
				scope := runtimeeviction.Scope{Version: 1, OperationID: "recovery", Namespace: "runtime", PodName: "old", PodUID: op.PodUID, NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", RuntimeType: "openclaw", GatewayID: op.GatewayID, WorkspaceRoot: "/workspaces", InstanceID: 7, UserID: 3, Generation: 2}
				receipt := &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC()}
				item := map[string]interface{}{"instance_id": 7, "source_pod_id": 5, "source_generation": 2, "source_gateway_id": op.GatewayID, "source_pod_uid": op.PodUID, "recovery": map[string]interface{}{"phase": "released", "result_state": state, "scope": scope, "receipt": receipt, "source_user_id": 3, "source_endpoint": op.Endpoint, "source_pod": map[string]interface{}{"metadata": map[string]interface{}{"uid": op.PodUID, "name": "old", "namespace": "runtime", "annotations": map[string]string{"clawmanager.io/fault-eviction-storage": strings.Repeat("b", 64)}}, "spec": map[string]interface{}{"nodeName": "node", "containers": []interface{}{map[string]interface{}{"name": "slot-0", "volumeMounts": []interface{}{map[string]interface{}{"name": "retained-workspace", "mountPath": "/workspaces"}}}}, "volumes": []interface{}{map[string]interface{}{"name": "retained-workspace", "persistentVolumeClaim": map[string]interface{}{"claimName": "workspace"}}}}}, "source_binding": models.InstanceRuntimeBinding{ID: 1, InstanceID: 7, RuntimePodID: 5, RuntimeType: "openclaw", GatewayID: op.GatewayID, GatewayPort: 20000, WorkspacePath: op.WorkspacePath, Generation: 2}}}
				raw, _ := json.Marshal([]interface{}{item})
				exec("INSERT INTO runtime_management_operations VALUES('recovery','eviction','completed','{\"mode\":\"recovery\"}',?)", string(raw))
				for _, change := range []struct{ apply, restore string }{
					{"UPDATE instance_model_activity SET stop_operation_id='newer' WHERE instance_id=7", "UPDATE instance_model_activity SET stop_operation_id='stop' WHERE instance_id=7"},
					{"UPDATE instances SET runtime_generation=3 WHERE id=7", "UPDATE instances SET runtime_generation=2 WHERE id=7"},
					{"UPDATE instance_idle_stop_operations SET pod_uid='other' WHERE id='stop'", "UPDATE instance_idle_stop_operations SET pod_uid='pod-old' WHERE id='stop'"},
					{"INSERT INTO instance_runtime_bindings VALUES(7,3)", "DELETE FROM instance_runtime_bindings"},
					{"UPDATE instances SET user_id=4 WHERE id=7", "UPDATE instances SET user_id=3 WHERE id=7"},
				} {
					exec(change.apply)
					done, err := complete.CompleteStopFromRecovery(t.Context(), op)
					if err == nil && done {
						t.Fatalf("changed source completed: %s", change.apply)
					}
					exec(change.restore)
				}
				journal := item["recovery"].(map[string]interface{})
				for _, invalid := range []string{"receipt", "scope", "generation", "authority"} {
					badReceipt := *receipt
					badScope := scope
					switch invalid {
					case "receipt":
						badReceipt.ScopeDigest = strings.Repeat("0", 64)
						journal["receipt"] = &badReceipt
					case "scope":
						badScope.Namespace = ""
						journal["scope"] = badScope
					case "generation":
						badScope.Generation = 3
						journal["scope"] = badScope
					case "authority":
						journal["receipt"] = nil
					}
					changed, _ := json.Marshal([]interface{}{item})
					exec("UPDATE runtime_management_operations SET items_json=?", string(changed))
					if done, _ := complete.CompleteStopFromRecovery(t.Context(), op); done {
						t.Fatalf("invalid %s authority accepted", invalid)
					}
					journal["receipt"] = receipt
					journal["scope"] = scope
				}
				exec("UPDATE runtime_management_operations SET items_json='broken'")
				if done, err := complete.CompleteStopFromRecovery(t.Context(), op); err == nil || done {
					t.Fatal("malformed journal accepted")
				}
				exec("UPDATE runtime_management_operations SET items_json=?", string(raw))
				done, err := complete.CompleteStopFromRecovery(t.Context(), op)
				if err != nil || !done {
					t.Fatalf("completed=%v err=%v", done, err)
				}
				var status, stopID, phase, retainedReason, stopKind string
				var generation int
				row, _ := repo.sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=7")
				if err = row.Scan(&status, &generation); err != nil {
					t.Fatal(err)
				}
				row, _ = repo.sess.SQL().QueryRow("SELECT stop_operation_id,stop_phase,stopped_reason FROM instance_model_activity WHERE instance_id=7")
				if err = row.Scan(&stopID, &phase, &retainedReason); err != nil {
					t.Fatal(err)
				}
				row, _ = repo.sess.SQL().QueryRow("SELECT stop_kind,phase FROM instance_idle_stop_operations WHERE id='stop'")
				var operationPhase string
				if err = row.Scan(&stopKind, &operationPhase); err != nil {
					t.Fatal(err)
				}
				if status != state || generation != 2 || stopID != "" || phase != "completed" || retainedReason != reason || stopKind != kind || operationPhase != "completed" {
					t.Fatalf("unexpected continuation %s/%d activity=%s/%s/%s stop=%s/%s", status, generation, stopID, phase, retainedReason, stopKind, operationPhase)
				}
			})
		}
	}
}
