package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestAutoStopResumesFromReleasedFaultRecoveryMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(sess)
	service := &InstanceAutoStopService{store: store, lifecycle: &instanceService{}}
	for n, kind := range []string{"idle", "manual"} {
		id := 101 + n
		state := "stopped"
		reason := "idle_timeout"
		if kind == "manual" {
			state = "deleting"
			reason = "manual"
		}
		scope := runtimeeviction.Scope{Version: 1, OperationID: kind, Namespace: "runtime", PodName: "old", PodUID: "old-uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", RuntimeType: "openclaw", GatewayID: "gw-101-2", WorkspaceRoot: "/workspaces", InstanceID: id, UserID: 1, Generation: 2}
		if id == 102 {
			scope.GatewayID = "gw-102-2"
		}
		workspace := RuntimeWorkspacePathWithRoot("/workspaces", "openclaw", 1, id)
		op := models.InstanceIdleStopOperation{ID: kind, Kind: kind, InstanceID: id, UserID: 1, Generation: 2, RuntimePodID: 5, GatewayID: scope.GatewayID, PodUID: scope.PodUID, Endpoint: "https://10.0.0.1:19090", WorkspacePath: workspace, StorageIdentity: "native-encoding", Phase: "error"}
		exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(?,1,'openclaw','gateway','lite',?,2)", id, state)
		initialReason := reason
		if kind == "idle" {
			initialReason = ""
		}
		exec("INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stop_operation_id,stop_phase,stopped_reason,blocker) VALUES(?,2,NOW(),?,'error',?,'old native failure')", id, kind, initialReason)
		exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES(?,?,1,2,5,?,?,?,?,?,'error',?,NOW(),NOW())", kind, id, op.GatewayID, op.PodUID, op.Endpoint, workspace, op.StorageIdentity, kind)
		receipt := &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC()}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID(scope.PodUID), Namespace: scope.Namespace, Name: scope.PodName, Annotations: map[string]string{"clawmanager.io/fault-eviction-storage": strings.Repeat("b", 64)}}, Spec: corev1.PodSpec{NodeName: scope.NodeName, Containers: []corev1.Container{{Name: scope.ContainerName, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: scope.WorkspaceRoot}}}}, Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "workspace"}}}}}}
		item := RuntimeSchedulingItem{InstanceID: id, SourceGeneration: 2, SourcePodID: 5, SourcePodUID: scope.PodUID, SourceGatewayID: scope.GatewayID, Recovery: &RuntimeFaultEvictionJournal{Phase: "released", ResultState: state, Scope: scope, Receipt: receipt, SourceUserID: 1, SourceEndpoint: op.Endpoint, SourcePod: pod, SourceBinding: models.InstanceRuntimeBinding{ID: 1, InstanceID: id, RuntimePodID: 5, RuntimeType: "openclaw", GatewayID: scope.GatewayID, GatewayPort: 20000, WorkspacePath: workspace, Generation: 2}}}
		raw, _ := json.Marshal([]RuntimeSchedulingItem{item})
		exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES(?,'eviction','completed','{\"mode\":\"recovery\"}',?,NOW(),NOW())", kind, string(raw))
		service.resumeGuarded(t.Context(), op)
		activity, err := store.GetActivity(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if activity.StopOperationID != "" || activity.StopPhase != "completed" || activity.StoppedReason != reason {
			t.Fatalf("native resume left fence: %+v", activity)
		}
		var actualState string
		row, _ := sess.SQL().QueryRow("SELECT status FROM instances WHERE id=?", id)
		if err = row.Scan(&actualState); err != nil {
			t.Fatal(err)
		}
		if actualState != state {
			t.Fatalf("intent changed %s -> %s", state, actualState)
		}
	}
}

func TestAutoStopNativeConfirmedProofIgnoresMalformedRecoveryMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(sess)
	agent := &idleFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, store: store}
	lifecycle := &instanceService{autoStopRepo: store, bindingRepo: repository.NewInstanceRuntimeBindingRepository(sess), instanceRepo: repository.NewInstanceRepository(sess), agentClient: agent}
	service := &InstanceAutoStopService{store: store, lifecycle: lifecycle}
	exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('malformed','eviction','completed','{\"mode\":\"recovery\"}','broken',NOW(),NOW())")
	for n, kind := range []string{"idle", "manual"} {
		id := 201 + n
		exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(?,1,'openclaw','gateway','lite','stopped',2)", id)
		exec("INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stop_operation_id,stop_phase) VALUES(?,2,NOW(),?,'teardown_confirmed')", id, kind)
		exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES(?,?,1,2,5,'gateway','old','endpoint','/workspace','storage','teardown_confirmed',?,NOW(),NOW())", kind, id, kind)
		service.resumeGuarded(t.Context(), models.InstanceIdleStopOperation{ID: kind, Kind: kind, InstanceID: id, UserID: 1, Generation: 2, RuntimePodID: 5, Phase: "error"})
		activity, err := store.GetActivity(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if activity.StopOperationID != "" || activity.StopPhase != "completed" {
			t.Fatalf("native %s completion blocked: %+v", kind, activity)
		}
	}
	if agent.stops != 0 || agent.confirms != 0 {
		t.Fatal("confirmed native stop retried runtime side effects")
	}
}
