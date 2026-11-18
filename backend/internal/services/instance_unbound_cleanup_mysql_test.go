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
	exec("UPDATE runtime_pods SET agent_endpoint='http://old:19091' WHERE id=5")
	for _, phase := range []string{"source_stopped", "placing"} {
		generation := 1
		if phase == "placing" {
			generation = 2
		}
		exec("UPDATE instance_runtime_recovery SET phase=?,generation=? WHERE instance_id=101", phase, generation)
		if err = service.cleanupV2GatewayBinding(ctx, instance); err != nil {
			t.Fatalf("%s recovery ignored retained source stop proof: %v", phase, err)
		}
	}
	// A failed stop can recover health without advancing generation at all.
	exec("UPDATE instance_runtime_recovery SET phase='',healthy_since=NOW(),generation=1 WHERE instance_id=101")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err != nil {
		t.Fatalf("same-generation healthy source lost its exact stop proof: %v", err)
	}
	// Healthy G2 proves that G2 executed. G1's same-Pod receipt cannot excuse
	// missing G2 credentials/publication: G2 still needs independent stop proof.
	exec("UPDATE instance_runtime_recovery SET phase='',healthy_since=NOW(),generation=2 WHERE instance_id=101")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("healthy current generation without retained stop proof accepted")
	}
	// The real replacement can be on another Pod. Preserve its immutable Secret
	// audit after the current ConfigMap is gone, as on deployed installations.
	replacement := assignment
	replacement.Generation, replacement.GatewayID = 2, "gw-101-2"
	replacement.PodUID, replacement.Endpoint = "replacement", "http://replacement:19091"
	if err = assignments.EnsureDesired(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	secret, err := kube.CoreV1().Secrets("runtime").Get(ctx, "lite-instance-101-g2", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := json.Marshal(k8sservice.RuntimeStoppedSourceAudit{InstanceID: 101, UserID: 1, Generation: 2, RuntimeType: "openclaw", PodUID: replacement.PodUID, Endpoint: replacement.Endpoint, GatewayID: replacement.GatewayID, WorkspacePath: workspace, Stopped: true})
	if err != nil {
		t.Fatal(err)
	}
	secret.Annotations = map[string]string{k8sservice.RuntimeStoppedSourceAnnotationPrefix + "2": string(proof)}
	if _, err = kube.CoreV1().Secrets("runtime").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = kube.CoreV1().ConfigMaps("runtime").Delete(ctx, "lite-instance-101", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.cleanupV2GatewayBinding(ctx, instance); err != nil {
		t.Fatalf("healthy replacement lost its retired source proof: %v", err)
	}
	exec("UPDATE runtime_pods SET pod_uid='replacement',agent_endpoint='http://replacement:19091' WHERE id=5")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err != nil {
		t.Fatalf("same-generation G2 source lost its exact stop proof: %v", err)
	}
	exec("UPDATE runtime_pods SET pod_uid='old',agent_endpoint='http://old:19091' WHERE id=5")
	if err = kube.CoreV1().Secrets("runtime").Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("lost current generation evidence accepted via predecessor receipt")
	}
	if _, err = kube.CoreV1().Secrets("runtime").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE instance_runtime_recovery SET healthy_since=NULL WHERE instance_id=101")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("unresolved recovery without healthy replacement accepted")
	}
	exec("UPDATE instance_runtime_recovery SET phase='placing' WHERE instance_id=101")
	exec("UPDATE runtime_pods SET pod_uid='replacement-uid' WHERE id=5")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("replacement Pod identity vouched for recovery source")
	}
	exec("UPDATE runtime_pods SET pod_uid='old' WHERE id=5")
	exec("UPDATE instance_runtime_recovery SET source_pod_id=999 WHERE instance_id=101")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("missing recovery source Pod accepted")
	}
	exec("UPDATE instance_runtime_recovery SET source_pod_id=5,generation=3 WHERE instance_id=101")
	if err = service.cleanupV2GatewayBinding(ctx, instance); err == nil {
		t.Fatal("a different generation's proof vouched for recovery source")
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
