package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeManagementMySQLFailedStopRetrySurvivesManagerRestart(t *testing.T) {
	for _, state := range []string{"running", "stopped", "error"} {
		t.Run(state, func(t *testing.T) {
			sess, service, agent := runtimeManagementMySQLMoveFixture(t)
			ctx := context.Background()
			agent.deleteErr = context.DeadlineExceeded
			failed, err := service.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Process(ctx); err != nil {
				t.Fatal(err)
			}
			if len(agent.createRequests) != 0 {
				t.Fatal("unconfirmed source launched target")
			}
			// Heartbeats may arrive while waiting for kubelet's stop evidence.
			if err = service.scheduler.bindingRepo.UpdateState(ctx, 101, 1, state, nil); err != nil {
				t.Fatal(err)
			}
			if err = service.scheduler.podRepo.UpdateHeartbeat(ctx, 1, "ready", 0, 2, false, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			agent.deleteErr = nil
			recovered := NewRuntimeManagementService(sess, service.scheduler, service.guards)
			op, err := recovered.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2, RetryOperationID: failed.ID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = recovered.Process(ctx); err != nil {
				t.Fatal(err)
			}
			if err = recovered.Process(ctx); err != nil {
				t.Fatal(err)
			}
			history, err := recovered.History(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range history {
				if entry.ID == op.ID && entry.Status != "completed" {
					t.Fatalf("retry did not complete: %+v", entry)
				}
			}
			instance, err := recovered.scheduler.instanceRepo.GetByID(101)
			if err != nil || instance.Status != "running" || instance.RuntimeGeneration != 2 {
				t.Fatalf("retry changed restart generation: %+v %v", instance, err)
			}
			if len(agent.createRequests) != 1 || len(agent.deleteRequests) != 2 || agent.deleteRequests[1].gatewayID != "gw-101-1" {
				t.Fatal("retry duplicated target or changed source identity")
			}
			duplicate, err := recovered.Preflight(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 1, RetryOperationID: failed.ID})
			if err != nil || duplicate.Allowed {
				t.Fatalf("completed retry was replayed against a new generation: %+v %v", duplicate, err)
			}
			for id, want := range map[int64]int{1: 0, 2: 1} {
				p, err := recovered.scheduler.podRepo.GetByID(ctx, id)
				if err != nil || p.UsedSlots != want {
					t.Fatalf("occupancy drift: %+v %v", p, err)
				}
			}
		})
	}
}

func TestRuntimeManagementMySQLFailedStopRetryRejectsChangedIntent(t *testing.T) {
	for _, change := range []string{"missing-operation", "new-generation", "source-pod", "manual-stop", "other-instance"} {
		t.Run(change, func(t *testing.T) {
			sess, service, agent := runtimeManagementMySQLMoveFixture(t)
			ctx := context.Background()
			agent.deleteErr = errors.New("no proof")
			op, err := service.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Process(ctx); err != nil {
				t.Fatal(err)
			}
			request := RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, RetryOperationID: op.ID}
			query := ""
			switch change {
			case "missing-operation":
				request.RetryOperationID = "missing"
			case "new-generation":
				query = "UPDATE instances SET runtime_generation=3 WHERE id=101"
			case "source-pod":
				query = "UPDATE runtime_pods SET pod_uid='replaced' WHERE id=1"
			case "manual-stop":
				query = "INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stopped_reason) VALUES(101,2,NOW(),'manual')"
			case "other-instance":
				query = "UPDATE runtime_management_operations SET items_json='[]'"
			}
			if query != "" {
				if _, err = sess.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := service.Preflight(ctx, request)
			if err != nil || plan.Allowed {
				t.Fatalf("changed source retry admitted: %+v %v", plan, err)
			}
			if len(agent.createRequests) != 0 || len(agent.deleteRequests) != 1 {
				t.Fatal("rejected retry reached runtime")
			}
		})
	}
}

func TestRuntimeManagementMySQLMoveReconcilesStopHeartbeat(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	service.scheduler.agentClient = &runtimeManagementAfterStopAgent{strictStopRuntimeAgent: agent, afterStop: func() {
		if err := service.scheduler.bindingRepo.UpdateState(ctx, 101, 1, "stopped", nil); err != nil {
			t.Fatal(err)
		}
		if err := service.scheduler.podRepo.UpdateHeartbeat(ctx, 1, "ready", 0, 2, false, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := service.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	i, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || i.Status != "running" || len(agent.createRequests) != 1 {
		t.Fatalf("heartbeat prevented confirmed move: %+v %v", i, err)
	}
}
