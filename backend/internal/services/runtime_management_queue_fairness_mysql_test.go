package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestRuntimeManagementMySQLPendingRecoveryDoesNotStarveOtherTypePreparation(t *testing.T) {
	for _, status := range []string{"pending", "processing"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			sess, maintenance, _ := runtimeScaleMySQLFixture(t, RuntimeTypeOpenClaw, "openclaw-runtime", "hermes-runtime")
			s := NewRuntimeManagementService(sess, maintenance.scheduler, repository.NewNorthboundRepository(sess))
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'hermes','gateway','lite','deleting',3,NOW(6))")
			exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(101,2,'hermes','gw-101-3',20000,'/workspaces/hermes/user-1/instance-101','error',3)")
			const recoveryID = "older-hermes-recovery"
			exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,?,NOW(6))", recoveryID)
			request, _ := json.Marshal(RuntimeSchedulingRequest{Mode: "recovery", SourcePodID: 2, InstanceIDs: []int{101}})
			items, _ := json.Marshal([]RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 2, State: "processing", SourceGeneration: 3, SourceGatewayID: "gw-101-3", SourcePodUID: "hermes-runtime-pod-uid", Recovery: &RuntimeFaultEvictionJournal{Phase: "stopping"}}})
			old := time.Now().UTC().Add(-time.Hour)
			exec("INSERT INTO runtime_management_operations(id,kind,status,owner_token,request_json,items_json,created_at,updated_at) VALUES(?,'eviction',?,'interrupted-owner',?,?,?,?)", recoveryID, status, string(request), string(items), old, old)
			// A real lifecycle owner prevents this operation from making progress.
			// Each attempted recovery must retain its checkpoint and yield the queue.
			release, err := acquireInstanceLifecycleOperation(ctx, s.scheduler.instanceRepo, 101)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			prepared, err := s.PrepareRollout(ctx, RuntimeTypeOpenClaw, nil)
			if err != nil {
				t.Fatal(err)
			}
			for tick := 0; tick < 2; tick++ {
				if err = s.Process(ctx); err != nil {
					t.Fatal(err)
				}
			}
			prepared, _, _, err = s.runtimeManagementOperation(ctx, prepared.ID)
			if err != nil || prepared.Status != "completed" {
				t.Fatalf("pending Hermes recovery starved another type's preparation: %+v %v", prepared, err)
			}
			held, _, _, err := s.runtimeManagementOperation(ctx, recoveryID)
			if err != nil || held.Status != "pending" || held.Items[0].Recovery.Phase != "stopping" {
				t.Fatalf("fair scheduling changed the recovery checkpoint: %+v %v", held, err)
			}
			runtimeManagementMySQLGuard(t, sess, 101, recoveryID)
			binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
			if err != nil || binding == nil || binding.Generation != 3 {
				t.Fatalf("unconfirmed recovery released its source: %+v %v", binding, err)
			}
			if err = s.assertOwner(ctx, recoveryID); err == nil {
				t.Fatal("pending operation retained execution authority")
			}
			instance, err := s.scheduler.instanceRepo.GetByID(101)
			if err != nil || instance.Status != "deleting" {
				t.Fatalf("fair scheduling changed the instance intent: %+v %v", instance, err)
			}
			if concurrentRelease, err := s.scheduler.instanceRepo.(instanceLifecycleLocker).AcquireLifecycleLock(ctx, 101); err == nil {
				concurrentRelease()
				t.Fatal("concurrent lifecycle owner was bypassed")
			} else if !errors.Is(err, models.ErrInstanceOperationInProgress) {
				t.Fatalf("unexpected concurrent lifecycle error: %v", err)
			}
		})
	}
}
