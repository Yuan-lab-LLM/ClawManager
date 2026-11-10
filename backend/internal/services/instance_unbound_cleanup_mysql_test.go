package services

import (
	"context"
	"encoding/json"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8sservice "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestUnboundCleanupRetainsSQLSourceHistoryMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	workspace := "/workspaces/openclaw/user-1/instance-101"
	exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path) VALUES(101,1,'openclaw','gateway','lite','stopped',2,?)", workspace)
	exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity) VALUES(5,'openclaw','runtime','old','old','pool','image','error',1)")
	repo := repository.NewInstanceRepository(sess)
	history := repo.(interface {
		RuntimeCleanupHistory(context.Context, *models.Instance) ([]models.RuntimeCleanupSource, error)
	})
	instance := &models.Instance{ID: 101, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: "stopped", RuntimeGeneration: 2, WorkspacePath: &workspace}
	sourceItem := RuntimeSchedulingItem{InstanceID: 101, SourceGeneration: 1, SourcePodID: 5, SourcePodUID: "old", SourceGatewayID: "gw-101-1", State: "failed"}
	raw, _ := json.Marshal([]RuntimeSchedulingItem{sourceItem})
	exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('old-migration','migration','failed','{}',?,NOW(),NOW())", string(raw))
	sources, err := history.RuntimeCleanupHistory(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].StoppedProof || sources[0].PodUID != "old" {
		t.Fatalf("old migration source lost: %+v", sources)
	}
	kube := fake.NewSimpleClientset()
	assignments := k8sservice.NewRuntimeAssignmentService(kube, "runtime")
	assignment := k8sservice.RuntimeAssignment{InstanceID: 101, UserID: 1, Generation: 1, RuntimeType: "openclaw", Endpoint: "http://old:19091", GatewayID: "gw-101-1", PodUID: "old", Request: json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-1/instance-101","gateway_port":20000}`)}
	if err := assignments.EnsureDesired(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-instance-101", metav1.DeleteOptions{})
	service := &instanceService{instanceRepo: repo, bindingRepo: repository.NewInstanceRuntimeBindingRepository(sess), agentClient: &slotRuntimeAgentClient{assignments: assignments}}
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("missing binding and deleted assignment erased uncertain migration source")
	}
	exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('native-stop',101,1,1,5,'gw-101-1','old','http://old:19091',?,'storage','completed','manual',NOW(),NOW())", workspace)
	if err = service.cleanupV2GatewayBinding(ctx, instance); err != nil {
		t.Fatalf("verified native historical stop blocked: %v", err)
	}
	exec("UPDATE instance_idle_stop_operations SET pod_uid='different-old-uid' WHERE id='native-stop'")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("different stopped Pod UID vouched for old migration writer")
	}
	exec("UPDATE instance_idle_stop_operations SET pod_uid='old' WHERE id='native-stop'")
	exec("INSERT INTO instance_runtime_recovery(instance_id,attempts,reason,source_pod_id,source_gateway_port,generation,updated_at) VALUES(101,1,'uncertain',5,20000,1,NOW())")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("uncertain recovery intent was ignored")
	}
	exec("DELETE FROM instance_runtime_recovery")
	exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(101,5,'openclaw','gw-101-1',20000,?,'running',1)", workspace)
	if _, err = history.RuntimeCleanupHistory(ctx, instance); err == nil {
		t.Fatal("new binding raced cleanup inventory")
	}
	exec("DELETE FROM instance_runtime_bindings")
	changed := *instance
	changed.UserID = 2
	if _, err = history.RuntimeCleanupHistory(ctx, &changed); err == nil {
		t.Fatal("changed owner accepted")
	}
}
