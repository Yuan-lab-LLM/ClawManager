package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"k8s.io/apimachinery/pkg/types"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type unboundCleanupVerifier interface {
	CheckUnboundInstanceStopped(context.Context, int, int, int, string, []models.RuntimeCleanupSource) error
}

func TestRetainedStoppedAssignmentRequiresFinalizedOwnerAndWorkspace(t *testing.T) {
	ctx := t.Context()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "old"
	workspace := "/workspaces/openclaw/user-4/instance-7"
	a.Request = json.RawMessage(`{"workspace_path":"` + workspace + `","gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	check := func(user int, path string) error {
		_, err := s.RetainedStoppedAssignment(ctx, 7, user, 4, "openclaw", path)
		return err
	}
	if check(4, workspace) == nil {
		t.Fatal("live assignment accepted as stopped")
	}
	if err := s.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if check(4, workspace) == nil {
		t.Fatal("container finalization was not required")
	}
	cm := assignmentCM(t, client)
	cm.Data["container_termination_proof"] = `{}`
	cm.Data["container_stop_released"] = "true"
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// This layer checks publication provenance; the caller independently decodes
	// and validates the opaque native termination proof before placement.
	if err := check(4, workspace); err != nil {
		t.Fatal(err)
	}
	old, err := client.CoreV1().Secrets("tenant").Get(ctx, "lite-instance-7-g3", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old.Name = "lite-instance-7-g1"
	old.ResourceVersion = ""
	old.Annotations = nil
	old.Labels["clawmanager.io/generation"] = "1"
	if _, err = client.CoreV1().Secrets("tenant").Create(ctx, old, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = check(4, workspace); err != nil {
		t.Fatalf("old credential blocked nondestructive resume: %v", err)
	}
	if err = s.CheckUnboundInstanceStopped(ctx, 7, 4, 4, "openclaw", nil); err == nil {
		t.Fatal("resume relaxed destructive history protection")
	}
	newer := old.DeepCopy()
	newer.Name = "lite-instance-7-g4"
	newer.Labels["clawmanager.io/generation"] = "4"
	if _, err = client.CoreV1().Secrets("tenant").Create(ctx, newer, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if check(4, workspace) == nil {
		t.Fatal("newer uncertain publication allowed resume")
	}
	if err = client.CoreV1().Secrets("tenant").Delete(ctx, newer.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if check(5, workspace) == nil || check(4, "/other") == nil {
		t.Fatal("owner/workspace change accepted")
	}
	cm.Data["container_stop_released"] = "false"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	if check(4, workspace) == nil {
		t.Fatal("unfinished release accepted")
	}
}

func TestUnboundCleanupRequiresPublicationStopProof(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	verifier, ok := interface{}(s).(unboundCleanupVerifier)
	if !ok {
		t.Fatal("unbound cleanup proof capability missing")
	}
	check := func() error { return verifier.CheckUnboundInstanceStopped(ctx, 7, 4, 3, "openclaw", nil) }
	if err := verifier.CheckUnboundInstanceStopped(ctx, 7, 4, 1, "openclaw", nil); err != nil {
		t.Fatalf("never published blocked: %v", err)
	}
	a := assignmentFixture()
	a.PodUID = "old"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7","gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("running unbound writer permitted cleanup")
	}
	if err := s.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("draining writer permitted cleanup")
	}
	if err := s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := check(); err != nil {
		t.Fatalf("verified stopped writer blocked: %v", err)
	}
	cm := assignmentCM(t, client)
	cm.Data["container_stop_intent"] = "pending"
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	if err := check(); err == nil {
		t.Fatal("unfinalized container release accepted")
	}
	cm.Data["container_stop_intent"] = ""
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	client.CoreV1().ConfigMaps("tenant").Delete(ctx, cm.Name, metav1.DeleteOptions{})
	if err := check(); err == nil {
		t.Fatal("missing CM with generation credentials accepted as unpublished")
	}
}

func TestUnboundCleanupRetainsHistoryAcrossGenerationAdvance(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	verifier, ok := interface{}(s).(unboundCleanupVerifier)
	if !ok {
		t.Fatal("unbound cleanup proof capability missing")
	}
	a := assignmentFixture()
	a.PodUID = "old"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7","gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.MarkDraining(ctx, a.Endpoint, a.GatewayID)
	s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID)
	a.Generation = 4
	a.GatewayID = "gw-7-4"
	a.PodUID = "new"
	a.Endpoint = "http://new:19091"
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.MarkDraining(ctx, a.Endpoint, a.GatewayID)
	s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID)
	source := models.RuntimeCleanupSource{Generation: 3, RuntimePodID: 5, PodUID: "old", GatewayID: "gw-7-3", WorkspacePath: "/workspaces/openclaw/user-4/instance-7"}
	if err := verifier.CheckUnboundInstanceStopped(ctx, 7, 4, 4, "openclaw", []models.RuntimeCleanupSource{source}); err != nil {
		t.Fatalf("stopped migration history blocked: %v", err)
	}
	source.PodUID = "unproven-migration-source"
	if err := verifier.CheckUnboundInstanceStopped(ctx, 7, 4, 4, "openclaw", []models.RuntimeCleanupSource{source}); err == nil {
		t.Fatal("different historical source accepted")
	}
	cm := assignmentCM(t, client)
	delete(cm.Annotations, "clawmanager.io/stopped-source-3")
	client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{})
	if err := verifier.CheckUnboundInstanceStopped(ctx, 7, 4, 4, "openclaw", nil); err == nil {
		t.Fatal("old generation credentials without stop proof accepted")
	}
}

func TestUnboundCleanupConsumesExactSecretOnlyStoppedAudit(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "retired-old"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7"}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	client.CoreV1().ConfigMaps("tenant").Delete(ctx, "lite-instance-7", metav1.DeleteOptions{})
	secret, err := client.CoreV1().Secrets("tenant").Get(ctx, "lite-instance-7-g3", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	source := models.RuntimeCleanupSource{Generation: 3, PodUID: a.PodUID, GatewayID: a.GatewayID, WorkspacePath: "/workspaces/openclaw/user-4/instance-7"}
	proof := RuntimeStoppedSourceAudit{InstanceID: 7, UserID: 4, Generation: 3, RuntimeType: "openclaw", PodUID: a.PodUID, Endpoint: a.Endpoint, GatewayID: a.GatewayID, WorkspacePath: source.WorkspacePath, Stopped: true}
	for _, wrong := range []bool{true, false} {
		value := proof
		if wrong {
			value.PodUID = "different-uid"
		}
		raw, _ := json.Marshal(value)
		secret.Annotations = map[string]string{RuntimeStoppedSourceAnnotationPrefix + "3": string(raw)}
		if _, err = client.CoreV1().Secrets("tenant").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		err = s.CheckUnboundInstanceStopped(ctx, 7, 4, 3, "openclaw", []models.RuntimeCleanupSource{source})
		if wrong && err == nil {
			t.Fatal("secret audit for different Pod UID accepted")
		}
		if !wrong && err != nil {
			t.Fatalf("verified old source Secret audit blocked: %v", err)
		}
	}
}

func TestGenerationAdvanceRetiresOnlyCompletedOrphanFence(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "old-pod"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7","gateway_port":20000}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	publication := func() RuntimeOrphanPublication {
		t.Helper()
		cm := assignmentCM(t, client)
		cm.UID = "same-cm"
		cm.ResourceVersion = "1"
		if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		secret, err := client.CoreV1().Secrets("tenant").Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		secret.UID = types.UID("secret-" + strconv.Itoa(a.Generation))
		secret.ResourceVersion = "1"
		if _, err = client.CoreV1().Secrets("tenant").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		source, err := s.FaultEvictionSource(ctx, a.Endpoint, a.GatewayID)
		if err != nil {
			t.Fatal(err)
		}
		return RuntimeOrphanPublication{Audit: RuntimeStoppedSourceAudit{InstanceID: a.InstanceID, UserID: a.UserID, Generation: a.Generation, RuntimeType: a.RuntimeType, PodUID: a.PodUID, Endpoint: a.Endpoint, GatewayID: a.GatewayID, WorkspacePath: "/workspaces/openclaw/user-4/instance-7", Stopped: true}, SecretUID: string(secret.UID), SecretVersion: secret.ResourceVersion, CMUID: string(cm.UID), CMGeneration: a.Generation, CurrentSource: source}
	}
	old := publication()
	scope := runtimeeviction.PodScope{Version: 1, OperationID: "old-operation", Namespace: "tenant", PodName: "old", PodUID: a.PodUID, NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", RuntimeType: a.RuntimeType, WorkspaceRoot: "/workspaces", ControllerUID: "controller", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "terminal"}
	receipt := &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, InventoryDigest: strings.Repeat("b", 64), LedgerDigest: strings.Repeat("c", 64), FinishedAt: time.Now().UTC(), Confirmed: true}
	if err := s.FenceRuntimeOrphanPublications(ctx, []RuntimeOrphanPublication{old}, scope.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmRuntimeOrphanPublications(ctx, []RuntimeOrphanPublication{old}, scope, receipt); err != nil {
		t.Fatal(err)
	}
	oldCM := assignmentCM(t, client)
	oldAudit := oldCM.Annotations[RuntimeStoppedSourceAnnotationPrefix+"3"]
	oldReceipt := oldCM.Annotations["clawmanager.io/orphan-stop-receipt-3"]
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if assignmentCM(t, client).Annotations[orphanOperation] != scope.OperationID {
		t.Fatal("same-generation retry discarded the active orphan fence")
	}
	a.Generation = 4
	a.GatewayID = "gw-7-4"
	a.PodUID = "new-pod"
	a.Endpoint = "http://new-pod:19091"
	blockedCM := assignmentCM(t, client)
	blockedCM.Data["container_termination_proof"] = "unfinalized-proof"
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, blockedCM, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDesired(ctx, a); err == nil {
		t.Fatal("generation advanced before old source release was finalized")
	}
	blockedCM = assignmentCM(t, client)
	if blockedCM.Annotations[orphanOperation] != scope.OperationID {
		t.Fatal("failed generation advance discarded the orphan fence")
	}
	delete(blockedCM.Data, "container_termination_proof")
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, blockedCM, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	next := publication()
	if err := s.FenceRuntimeOrphanPublications(ctx, []RuntimeOrphanPublication{next}, "new-operation"); err != nil {
		t.Fatalf("completed previous orphan operation blocked later generation recovery: %v", err)
	}
	nextCM := assignmentCM(t, client)
	if nextCM.Annotations[RuntimeStoppedSourceAnnotationPrefix+"3"] != oldAudit || nextCM.Annotations["clawmanager.io/orphan-stop-receipt-3"] != oldReceipt {
		t.Fatal("retiring active operation marker discarded historical source proof")
	}
	if nextCM.Annotations[orphanOperation] != "new-operation" {
		t.Fatal("next recovery failed to own current generation")
	}
}
