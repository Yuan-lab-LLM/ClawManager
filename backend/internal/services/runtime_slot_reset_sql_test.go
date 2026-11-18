package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/upper/db/v4"
	"os"
	"strings"
	"testing"
	"time"
)

type resetCommitFaultSession struct {
	db.Session
	committed bool
}

func (s resetCommitFaultSession) TxContext(ctx context.Context, fn func(db.Session) error, opts *sql.TxOptions) error {
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		if err := fn(tx); err != nil {
			return err
		}
		if !s.committed {
			return errors.New("injected commit rollback")
		}
		return nil
	}, opts)
	if err == nil {
		return errors.New("injected lost commit response")
	}
	return err
}

func TestSlotResetAtomicReleaseAndRecoveryMySQL(t *testing.T) {
	for _, scenario := range []string{"restore", "migrated", "same-generation-other-slot", "starting-successor", "user-stopped", "lost-receipt", "replay", "rollback", "commit-reply-lost", "auto-resume", "advanced-unbound", "native-owner-without-sql"} {
		t.Run(scenario, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			s := runtimeManagementMySQLService(sess)
			ctx := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			migration, err := os.ReadFile("../db/migrations/125_add_runtime_slot_reset_requests.sql")
			if err != nil {
				t.Fatal(err)
			}
			exec(string(migration))
			exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path) VALUES(101,1,'opencode','gateway','lite','running',4,'/workspaces/opencode/user-1/instance-101')")
			exec("INSERT INTO instance_desired_state(instance_id,desired_power_state) VALUES(101,'running')")
			exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots) VALUES(1,'opencode','test','pod','uid','test','image','error',8,1),(2,'opencode','test','new','newuid','test','image','ready',8,0)")
			exec("INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(71,101,1,'opencode','gw-101-4',20000,'/workspaces/opencode/user-1/instance-101','running',4)")
			old := models.InstanceRuntimeBinding{ID: 71, InstanceID: 101, RuntimePodID: 1, RuntimeType: "opencode", GatewayID: "gw-101-4", GatewayPort: 20000, WorkspacePath: "/workspaces/opencode/user-1/instance-101", Generation: 4}
			scope := runtimeeviction.SlotScope{Version: 1, OperationID: "reset-op", Namespace: "test", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
			receipt := &runtimeeviction.SlotReceipt{ScopeDigest: runtimeeviction.SlotScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, ContainerID: scope.ContainerID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, FinishedAt: time.Now().UTC(), Confirmed: true}
			writer := runtimeeviction.Scope{Version: 1, OperationID: scope.OperationID, Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, ContainerName: scope.ContainerName, ContainerID: scope.ContainerID, RuntimeType: "opencode", GatewayID: old.GatewayID, WorkspaceRoot: "/workspaces", InstanceID: 101, UserID: 1, Generation: 4}
			j := &RuntimeSlotResetJournal{Phase: "source_stopped", Scope: scope, Receipt: receipt, Slot: scope.ContainerName, PodUID: scope.PodUID, ContainerID: scope.ContainerID, InstanceID: 101, SourceBinding: &old, Source: models.RuntimePod{ID: 1, RuntimeType: "opencode"}, Owner: &runtimeSlotAuditOwner{InstanceID: 101, UserID: 1, Generation: 4, GatewayID: old.GatewayID, PodUID: "uid"}, WriterScope: &writer, WriterReceipt: &runtimeeviction.Receipt{ScopeDigest: runtimeeviction.ScopeDigest(writer), OperationID: writer.OperationID, PodUID: writer.PodUID, ContainerName: writer.ContainerName, NodeUID: writer.NodeUID, NodeBootID: writer.NodeBootID, StoppedContainerIDs: []string{writer.ContainerID}, LedgerDigest: strings.Repeat("b", 64), FinishedAt: time.Now().UTC(), Confirmed: true}}
			j.GatewayPort = old.GatewayPort
			if scenario == "migrated" {
				exec("UPDATE instance_runtime_bindings SET runtime_pod_id=2,gateway_port=20003 WHERE id=71")
			}
			if scenario == "same-generation-other-slot" {
				exec("UPDATE instance_runtime_bindings SET gateway_port=20003 WHERE id=71")
			}
			if scenario == "starting-successor" {
				exec("UPDATE instance_runtime_bindings SET gateway_port=20003,state='starting' WHERE id=71")
				exec("UPDATE instances SET status='creating' WHERE id=101")
			}
			if scenario == "user-stopped" {
				exec("UPDATE instance_desired_state SET desired_power_state='stopped' WHERE instance_id=101")
			}
			if scenario == "lost-receipt" {
				j.Receipt = nil
			}
			if scenario == "advanced-unbound" {
				exec("DELETE FROM instance_runtime_bindings WHERE instance_id=101")
				exec("UPDATE instances SET runtime_generation=6,status='error' WHERE id=101")
			}
			if scenario == "native-owner-without-sql" {
				exec("DELETE FROM instance_runtime_bindings WHERE instance_id=101")
				j.SourceBinding = nil
			}
			op := &RuntimeManagementOperation{ID: scope.OperationID, Kind: "slot_reset", Status: "processing", Items: []RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 1, SlotReset: j}}}
			raw, _ := json.Marshal(op.Items)
			exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,owner_token,created_at,updated_at) VALUES(?,'slot_reset','processing','{}',?,?,NOW(),NOW())", op.ID, string(raw), s.token)
			exec("INSERT INTO runtime_slot_reset_requests(request_id,operation_id,pod_uid,slot,source_pod_id,gateway_port,instance_id,source_generation,source_gateway_id,active,created_at) VALUES('request-key',?,'uid','slot-0',1,20000,101,4,'gw-101-4',1,NOW())", op.ID)
			if scenario == "rollback" || scenario == "commit-reply-lost" {
				s.sess = resetCommitFaultSession{Session: sess, committed: scenario == "commit-reply-lost"}
			}
			err = s.commitSlotResetRecovery(ctx, op)
			if scenario == "rollback" || scenario == "commit-reply-lost" {
				if !errors.Is(err, ErrRuntimeSlotResetCommitUncertain) {
					t.Fatalf("lost commit wasn't held: %v", err)
				}
				if op.Items[0].SlotReset.CleanupCommitted {
					t.Fatal("speculative memory checkpoint escaped uncertain commit")
				}
				var count int
				row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=101")
				if e := row.Scan(&count); e != nil {
					t.Fatal(e)
				}
				if (scenario == "rollback" && count != 1) || (scenario == "commit-reply-lost" && count != 0) {
					t.Fatalf("wrong commit fixture: %d", count)
				}
				s.sess = sess
				err = s.commitSlotResetRecovery(ctx, op)
			}
			if scenario == "lost-receipt" {
				if err == nil {
					t.Fatal("released without physical exit proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "replay" {
				if err = s.commitSlotResetRecovery(ctx, op); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "auto-resume" || scenario == "advanced-unbound" || scenario == "native-owner-without-sql" {
				i, e := s.scheduler.instanceRepo.GetByID(101)
				if e != nil {
					t.Fatal(e)
				}
				handled, _ := s.scheduler.resumeSlotResetRecoveryLocked(ctx, *i)
				if !handled {
					t.Fatal("committed reset recovery was not resumed automatically")
				}
				var generation int
				row, _ := sess.SQL().QueryRow("SELECT runtime_generation FROM instances WHERE id=101")
				if e = row.Scan(&generation); e != nil {
					t.Fatal(e)
				}
				want := 5
				if scenario == "advanced-unbound" {
					want = 7
				}
				if generation != want {
					t.Fatalf("reset remained in old unschedulable generation: %d", generation)
				}
				i, e = s.scheduler.instanceRepo.GetByID(101)
				if e != nil {
					t.Fatal(e)
				}
				s.scheduler.resumeSlotResetRecoveryLocked(ctx, *i)
				row, _ = sess.SQL().QueryRow("SELECT runtime_generation FROM instances WHERE id=101")
				if e = row.Scan(&generation); e != nil {
					t.Fatal(e)
				}
				if generation != want {
					t.Fatalf("replay advanced generation twice: %d", generation)
				}
				return
			}
			var bindings, recoveries int
			row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=101")
			row.Scan(&bindings)
			row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_recovery WHERE instance_id=101 AND phase='source_stopped'")
			row.Scan(&recoveries)
			var status string
			var generation int
			row, _ = sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=101")
			if err = row.Scan(&status, &generation); err != nil {
				t.Fatal(err)
			}
			successor := scenario == "migrated" || scenario == "same-generation-other-slot" || scenario == "starting-successor"
			if successor {
				if bindings != 1 || recoveries != 0 || generation != 4 {
					t.Fatalf("successor modified: bindings=%d recovery=%d generation=%d", bindings, recoveries, generation)
				}
			} else if scenario == "user-stopped" {
				if bindings != 0 || recoveries != 0 {
					t.Fatal("explicit stop was scheduled")
				}
			} else if bindings != 0 || recoveries != 1 || status != "creating" || generation != 4 {
				t.Fatalf("recovery not atomic: bindings=%d recovery=%d status=%s generation=%d", bindings, recoveries, status, generation)
			}
		})
	}
}
