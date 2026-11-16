package services

import (
	"clawreef/internal/repository"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestDurableLiteRestartPreservesSourceAndRecoversWithoutReplay(t *testing.T) {
	s := idleLifecycleDB(t)
	ctx := t.Context()
	for _, q := range []string{
		`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES(1,7,'hermes','gateway','lite','running',2,'/workspaces/hermes/user-7/instance-1',UTC_TIMESTAMP())`,
		`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,'hermes','runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`,
		`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,'hermes','gw-1-2',20000,'/workspaces/hermes/user-7/instance-1','running',2)`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	agent := &manualStopFailureAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, store: store, fail: true, t: t}
	svc := &instanceService{instanceRepo: repository.NewInstanceRepository(s), bindingRepo: repository.NewInstanceRuntimeBindingRepository(s), runtimePodRepo: repository.NewRuntimePodRepository(s), agentClient: agent, autoStopRepo: store}
	configureLifecycleModels(t, svc)
	if err := svc.Restart(1); err == nil {
		t.Fatal("unconfirmed old source reported restarted")
	}
	i, err := svc.instanceRepo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if i.RuntimeGeneration != 2 {
		t.Fatal("source generation changed before confirmed stop")
	}
	if err = svc.instanceRepo.UpdateRuntimeState(ctx, 1, "running", 2, nil); err == nil {
		t.Fatal("old reporter revived durable stop")
	}
	pending, err := store.PendingStops(ctx)
	if err != nil || len(pending) != 1 || pending[0].Kind != "restart" {
		t.Fatalf("restart intent %+v %v", pending, err)
	}
	op := pending[0]
	agent.fail = false
	// A new controller instance resumes the same persisted source operation.
	controller := NewInstanceAutoStopService(store, svc, nil)
	controller.resume(ctx, op)
	i, err = svc.instanceRepo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if i.RuntimeGeneration != 3 || i.Status != "creating" {
		t.Fatalf("restart not accepted once: %+v", i)
	}
	b, _ := svc.bindingRepo.GetByInstanceID(ctx, 1)
	if b != nil {
		t.Fatal("old binding retained after confirmed stop")
	}
	controller.resume(ctx, op)
	i, _ = svc.instanceRepo.GetByID(1)
	if i.RuntimeGeneration != 3 {
		t.Fatal("late stop snapshot replayed Start")
	}
	// Simulate loss of the final progress write after Start was committed.
	if _, err = s.SQL().Exec(`UPDATE instance_idle_stop_operations SET phase='restart_ready' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.resumeRestartFollowup(ctx, 1); err != nil {
		t.Fatal(err)
	}
	i, _ = svc.instanceRepo.GetByID(1)
	if i.RuntimeGeneration != 3 {
		t.Fatal("lost response replayed Start")
	}
	// Explicit Stop cancels an unstarted follow-up, including stale snapshots.
	if err = svc.stopV2InstanceForKind(ctx, i, "restart"); err != nil {
		t.Fatal(err)
	}
	follow, err := store.RestartFollowup(ctx, 1)
	if err != nil || follow == nil {
		t.Fatalf("missing follow-up %v", err)
	}
	if err = svc.Stop(1); err != nil {
		t.Fatal(err)
	}
	controller.resume(ctx, *follow)
	i, _ = svc.instanceRepo.GetByID(1)
	if i.RuntimeGeneration != 3 || i.Status != "stopped" {
		t.Fatal("cancelled restart restarted instance")
	}
}

func TestManualLiteStopPersistsActualStorageBeforeNativeTeardown(t *testing.T) {
	s := idleLifecycleDB(t)
	ctx := t.Context()
	for _, q := range []string{
		`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES(1,7,'hermes','gateway','lite','running',2,'/workspaces/hermes/user-7/instance-1',UTC_TIMESTAMP())`,
		`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,'hermes','runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`,
		`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,'hermes','gw-1-2',20000,'/workspaces/hermes/user-7/instance-1','running',2)`,
	} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: types.UID("source-uid")}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "runtime", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}, {Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime", UID: types.UID("claim-uid")}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "retained-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	kube := fake.NewSimpleClientset(pod, claim)
	legacy := &manualStopFailureAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, store: store, fail: true, t: t}
	client := &slotRuntimeAgentClient{kube: kube, legacy: legacy, protectedInstance: func(context.Context, int) (bool, error) { return false, nil }}
	svc := &instanceService{instanceRepo: repository.NewInstanceRepository(s), bindingRepo: repository.NewInstanceRuntimeBindingRepository(s), runtimePodRepo: repository.NewRuntimePodRepository(s), agentClient: client, autoStopRepo: store}
	i, err := svc.instanceRepo.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.stopV2InstanceForKind(ctx, i, "manual"); err == nil {
		t.Fatal("uncertain native teardown accepted")
	}
	ops, err := store.PendingStops(ctx)
	if err != nil || len(ops) != 1 {
		t.Fatalf("intent missing: %v %v", ops, err)
	}
	want := runtimeStorageIdentityForTest(1, "pvc:runtime/data:claim-uid:retained-pv", "/workspaces", "", false, false)
	if ops[0].StorageIdentity != want {
		t.Fatalf("source storage lost before teardown: %q want %q", ops[0].StorageIdentity, want)
	}
}
