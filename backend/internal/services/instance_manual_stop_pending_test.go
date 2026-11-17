package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"clawreef/internal/repository"
)

type unavailableManualStopAgent struct{ *fakeRuntimeAgentClient }

func (a *unavailableManualStopAgent) DeleteGateway(context.Context, string, string) error {
	return fmt.Errorf("%w: replacement slot is in restart backoff", ErrRuntimeAgentUnavailable)
}

func TestManualStopAcceptsDurableUnavailableSourceWithoutReleasingIt(t *testing.T) {
	sess := idleLifecycleDB(t)
	for _, query := range []string{
		`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES(1,7,'opencode','gateway','lite','running',2,'/workspaces/opencode/user-7/instance-1',UTC_TIMESTAMP())`,
		`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,'opencode','runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',8,1)`,
		`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,'opencode','gw-1-2',20000,'/workspaces/opencode/user-7/instance-1','running',2)`,
	} {
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(sess)
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := &instanceService{instanceRepo: repository.NewInstanceRepository(sess), bindingRepo: repository.NewInstanceRuntimeBindingRepository(sess), runtimePodRepo: repository.NewRuntimePodRepository(sess), agentClient: &unavailableManualStopAgent{&fakeRuntimeAgentClient{}}, autoStopRepo: store}
	i, err := svc.instanceRepo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	err = svc.stopV2InstanceForKind(t.Context(), i, "manual")
	if !errors.Is(err, ErrInstanceStopPending) {
		t.Fatalf("durable stop should be accepted pending confirmation, got %v", err)
	}
	ops, err := store.PendingStops(t.Context())
	if err != nil || len(ops) != 1 || ops[0].Kind != "manual" || ops[0].Generation != 2 || ops[0].PodUID != "source-uid" {
		t.Fatalf("source operation not preserved: %+v %v", ops, err)
	}
	b, _ := svc.bindingRepo.GetByInstanceID(t.Context(), 1)
	pod, _ := svc.runtimePodRepo.GetByID(t.Context(), 11)
	i, _ = svc.instanceRepo.GetByID(1)
	if b == nil || b.Generation != 2 || pod.UsedSlots != 1 || i.RuntimeGeneration != 2 || i.Status != "stopped" {
		t.Fatal("pending stop released or changed source authority before proof")
	}
	// Re-observation must not turn a real identity rejection into acceptance.
	svc.agentClient = &fakeRuntimeAgentClient{deleteErr: errors.New("source identity changed")}
	if err = svc.stopV2InstanceForKind(t.Context(), i, "manual"); err == nil || errors.Is(err, ErrInstanceStopPending) {
		t.Fatalf("real source rejection was hidden: %v", err)
	}
}
