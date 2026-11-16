package k8s

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestFailedStartRetiresOnlyCurrentDeadlineNotHistoricalDeleteProtection(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	s := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "old"
	a.Request = json.RawMessage(`{"workspace_path":"/workspaces/openclaw/user-4/instance-7"}`)
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	a.Generation, a.GatewayID, a.PodUID = 4, "gw-7-4", "current"
	if err := s.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	check := func() error {
		return s.CheckFailedStartStopped(ctx, 7, 4, 4, "openclaw", "/workspaces/openclaw/user-4/instance-7")
	}
	if err := check(); err == nil {
		t.Fatal("running publication released Start")
	}
	if err := s.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("draining publication released Start")
	}
	if err := s.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	cm := assignmentCM(t, client)
	delete(cm.Annotations, RuntimeStoppedSourceAnnotationPrefix+"3")
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := check(); err != nil {
		t.Fatalf("current stopped Start blocked by unrelated old credential: %v", err)
	}
	if err := s.CheckUnboundInstanceStopped(ctx, 7, 4, 4, "openclaw", []models.RuntimeCleanupSource{}); err == nil {
		t.Fatal("workspace delete protection weakened")
	}
	for _, v := range []struct {
		gen       int
		workspace string
	}{{5, "/workspaces/openclaw/user-4/instance-7"}, {4, "/other"}} {
		if err := s.CheckFailedStartStopped(ctx, 7, 4, v.gen, "openclaw", v.workspace); err == nil {
			t.Fatal("changed Start identity accepted")
		}
	}
	cm = assignmentCM(t, client)
	cm.Data["container_stop_intent"] = "unconfirmed"
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("unconfirmed native exit released Start")
	} else if errors.Is(err, ErrCurrentStartPublicationUnavailable) {
		t.Fatal("unconfirmed teardown incorrectly allowed legacy fallback")
	}
	if err := client.CoreV1().ConfigMaps("tenant").Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := check(); !errors.Is(err, ErrCurrentStartPublicationUnavailable) {
		t.Fatalf("unpublished Start cannot use full historical verifier: %v", err)
	}
}
