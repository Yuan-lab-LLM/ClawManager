package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type cancellationStopAgent struct {
	*fakeRuntimeAgentClient
	entered chan struct{}
}

func (a *cancellationStopAgent) DeleteGateway(ctx context.Context, _, _ string) error {
	close(a.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestDeletingIntentCancelsInFlightStopAcrossReplica(t *testing.T) {
	session := idleLifecycleDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, q := range []string{
		`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(1,7,'openclaw','gateway','lite','running',2,UTC_TIMESTAMP())`,
		`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,'openclaw','runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`,
		`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,'openclaw','gw-1-2',20000,'/workspace','running',2)`,
	} {
		if _, err := session.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(session)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	op, err := store.BeginManualStop(ctx, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "source-uid", Endpoint: "https://10.0.0.2:19090", WorkspacePath: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	agent := &cancellationStopAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, entered: make(chan struct{})}
	service := &instanceService{instanceRepo: repository.NewInstanceRepository(session), bindingRepo: repository.NewInstanceRuntimeBindingRepository(session), runtimePodRepo: repository.NewRuntimePodRepository(session), agentClient: agent, autoStopRepo: store}
	release, err := service.acquireLifecycleOperation(1)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { defer release(); done <- service.resumeManualStop(ctx, *op) }()
	select {
	case <-agent.entered:
	case <-ctx.Done():
		t.Fatal("old stop did not contact runtime")
	}
	// A second API replica commits deletion through SQL, without the old owner's
	// process-local cancellation registry or lifecycle named lock.
	if _, err := session.SQL().Exec(`UPDATE instances SET status='deleting' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("old stop was not canceled: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("deleting intent did not cancel the old stop to release its lifecycle lock")
	}
	a, err := store.GetActivity(context.Background(), 1)
	if err != nil || a.StopOperationID != op.ID {
		t.Fatalf("cancel erased the stop journal/proof: %+v %v", a, err)
	}
	force := &forceDeletionAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, failure: errors.New("termination proof still pending")}
	service.agentClient = force
	instance, err := service.instanceRepo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.cleanupV2GatewayBinding(context.Background(), instance); err == nil || force.forced != 1 {
		t.Fatalf("old stop journal blocked explicit force cleanup: forced=%d err=%v", force.forced, err)
	}
	binding, err := service.bindingRepo.GetByInstanceID(context.Background(), 1)
	if err != nil || binding == nil {
		t.Fatalf("unconfirmed force erased binding: %+v %v", binding, err)
	}
	releaseNext, err := service.acquireLifecycleOperation(1)
	if err != nil {
		t.Fatalf("old stop retained lifecycle lock: %v", err)
	}
	releaseNext()
}
