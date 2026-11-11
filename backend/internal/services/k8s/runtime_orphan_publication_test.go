package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
	"time"
)

func TestOrphanHistoricalProofRequiresOldUIDAttribution(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.UserID = 3
	a.PodUID = "new-pod"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-3/instance-7"}`)
	if e := s.EnsureDesired(ctx, a); e != nil {
		t.Fatal(e)
	}
	cm := assignmentCM(t, client)
	cm.UID = "latest-cm"
	cm.ResourceVersion = "1"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	source := &RuntimeOrphanSource{Namespace: "tenant", PodName: "old", PodUID: "old-pod", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", DeploymentUID: "controller", RuntimeType: a.RuntimeType, WorkspaceRoot: "/workspaces", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "terminal"}
	records, e := s.SnapshotRuntimeOrphanPublications(ctx, source, nil, "https://10.0.0.1:19090")
	if e != nil || len(records) != 0 {
		t.Fatalf("inferred old UID from later generation credential: %+v %v", records, e)
	}
	yes := true
	older := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: runtimeAssignmentSecretName(a.InstanceID, a.Generation-1), Namespace: "tenant", UID: "old-secret", ResourceVersion: "2", Labels: map[string]string{"clawmanager.io/instance-id": "7", "clawmanager.io/user-id": "3", "clawmanager.io/generation": "2", "clawmanager.io/runtime-type": a.RuntimeType}}, Type: corev1.SecretTypeOpaque, Immutable: &yes}
	if _, e = client.CoreV1().Secrets("tenant").Create(ctx, older, metav1.CreateOptions{}); e != nil {
		t.Fatal(e)
	}
	records, e = s.SnapshotRuntimeOrphanPublications(ctx, source, []runtimeeviction.PodLedgerOwner{{InstanceID: 7, UserID: 3, Generation: 2, RuntimeType: a.RuntimeType, PodUID: "old-pod", Slot: "slot-0", State: "running"}}, "https://10.0.0.1:19090")
	if e != nil || len(records) != 1 || records[0].CurrentSource != nil {
		t.Fatalf("bad attributed history %+v %v", records, e)
	}
	if e = s.FenceRuntimeOrphanPublications(ctx, records, "operation"); e != nil {
		t.Fatal(e)
	}
	scope := source.Scope("operation")
	receipt := &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, InventoryDigest: strings.Repeat("b", 64), LedgerDigest: strings.Repeat("c", 64), Confirmed: true, FinishedAt: time.Now().UTC()}
	if e = s.ConfirmRuntimeOrphanPublications(ctx, records, scope, receipt); e != nil {
		t.Fatal(e)
	}
	after := assignmentCM(t, client)
	if after.Data["desired_state"] != "running" || after.Data["pod_uid"] != "new-pod" || after.Annotations[RuntimeStoppedSourceAnnotationPrefix+"2"] == "" {
		t.Fatal("historical cleanup changed current publisher")
	}
}
