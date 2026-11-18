package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
)

func TestIdleStopConsumesRetiredOrphanPodReceiptMySQL(t *testing.T) {
	for _, kind := range []string{"idle", "manual"} {
		for _, status := range []string{"stopped", "deleting"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				r := idleRuntimeRepository(t)
				store := NewInstanceAutoStopRepository(r.sess)
				exec := func(q string, args ...interface{}) {
					t.Helper()
					if _, err := r.sess.SQL().Exec(q, args...); err != nil {
						t.Fatal(err)
					}
				}
				exec("CREATE TABLE runtime_management_operations(id VARCHAR(64) PRIMARY KEY,kind VARCHAR(24),status VARCHAR(24),request_json LONGTEXT,items_json LONGTEXT)")
				exec("CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,generation INT)")
				op := models.InstanceIdleStopOperation{ID: "native", Kind: kind, InstanceID: 7, UserID: 3, Generation: 2, RuntimePodID: 5, GatewayID: "gw-7-2", PodUID: "old", Endpoint: "http://old:19091", WorkspacePath: "/workspaces/openclaw/user-3/instance-7", Phase: "error"}
				if kind == "manual" && status == "deleting" {
					op.GatewayID = "pending-7-2"
				}
				exec("UPDATE instances SET status=?,runtime_generation=2 WHERE id=7", status)
				exec("UPDATE instance_model_activity SET generation=2,stop_operation_id='native',stop_phase='error',stopped_reason='' WHERE instance_id=7")
				exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('native',7,3,2,5,'gw-7-2','old','http://old:19091',?,'','error',?,NOW(),NOW())", op.WorkspacePath, kind)
				exec("UPDATE instance_idle_stop_operations SET gateway_id=? WHERE id='native'", op.GatewayID)
				scope := runtimeeviction.PodScope{Version: 1, OperationID: "orphan", Namespace: "runtime", PodName: "old-pod", PodUID: "old", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", RuntimeType: "openclaw", WorkspaceRoot: "/workspaces", ControllerUID: "deployment-uid", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "99"}
				receipt := &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, InventoryDigest: strings.Repeat("b", 64), LedgerDigest: strings.Repeat("c", 64), FinishedAt: time.Now().UTC(), Confirmed: true}
				audit := map[string]interface{}{"instance_id": 7, "user_id": 3, "generation": 2, "runtime_type": "openclaw", "pod_uid": "old", "endpoint": op.Endpoint, "gateway_id": "gw-7-2", "workspace_path": op.WorkspacePath, "stopped": true}
				source := map[string]interface{}{"namespace": scope.Namespace, "pod_name": scope.PodName, "pod_uid": scope.PodUID, "node_name": scope.NodeName, "node_uid": scope.NodeUID, "node_boot_id": scope.NodeBootID, "runtime_type": scope.RuntimeType, "workspace_root": scope.WorkspaceRoot, "deployment_uid": scope.ControllerUID, "storage_digest": scope.StorageDigest, "terminal_resource_version": scope.TerminalResourceVersion}
				journal := map[string]interface{}{"phase": "sources_retired", "scope": scope, "receipt": receipt, "source": source, "publications": []interface{}{map[string]interface{}{"audit": audit, "secret_uid": "immutable-secret", "secret_version": "1"}}}
				save := func() {
					raw, _ := json.Marshal([]interface{}{map[string]interface{}{"orphan_recovery": journal}})
					exec("UPDATE runtime_management_operations SET items_json=?", string(raw))
				}
				exec("INSERT INTO runtime_management_operations VALUES('orphan','orphan_recovery','processing','{}','[]')")
				save()
				for _, invalid := range []string{"unfinished", "receipt", "scope", "generation", "uid", "endpoint", "owner", "binding", "new-generation", "new-activity"} {
					badReceipt := *receipt
					switch invalid {
					case "unfinished":
						journal["phase"] = "helper_done"
					case "receipt":
						badReceipt.ScopeDigest = strings.Repeat("0", 64)
						journal["receipt"] = &badReceipt
					case "scope":
						source["storage_digest"] = strings.Repeat("d", 64)
					case "generation":
						audit["generation"] = 3
					case "uid":
						audit["pod_uid"] = "other"
					case "endpoint":
						audit["endpoint"] = "http://other:19091"
					case "owner":
						audit["user_id"] = 4
					case "binding":
						exec("INSERT INTO instance_runtime_bindings VALUES(7,2)")
					case "new-generation":
						exec("UPDATE instances SET runtime_generation=3 WHERE id=7")
					case "new-activity":
						exec("UPDATE instance_model_activity SET stop_operation_id='new' WHERE instance_id=7")
					}
					save()
					if done, _ := store.CompleteStopFromRecovery(t.Context(), op); done {
						t.Fatalf("invalid %s receipt completed native stop", invalid)
					}
					journal["phase"] = "sources_retired"
					journal["receipt"] = receipt
					source["storage_digest"] = scope.StorageDigest
					audit["generation"] = 2
					audit["pod_uid"] = "old"
					audit["endpoint"] = op.Endpoint
					audit["user_id"] = 3
					exec("DELETE FROM instance_runtime_bindings")
					exec("UPDATE instances SET runtime_generation=2 WHERE id=7")
					exec("UPDATE instance_model_activity SET stop_operation_id='native' WHERE instance_id=7")
				}
				save()
				done, err := store.CompleteStopFromRecovery(t.Context(), op)
				if err != nil || !done {
					t.Fatalf("Pod receipt did not complete stop: %v/%v", done, err)
				}
				activity, err := store.GetActivity(t.Context(), 7)
				if err != nil {
					t.Fatal(err)
				}
				reason := "idle_timeout"
				if kind == "manual" {
					reason = "manual"
				}
				if activity.StopOperationID != "" || activity.StopPhase != "completed" || activity.StoppedReason != reason {
					t.Fatalf("activity lost intent: %+v", activity)
				}
				var latest string
				row, _ := r.sess.SQL().QueryRow("SELECT status FROM instances WHERE id=7")
				if err = row.Scan(&latest); err != nil {
					t.Fatal(err)
				}
				if latest != status {
					t.Fatalf("status %s changed to %s", status, latest)
				}
			})
		}
	}
}
