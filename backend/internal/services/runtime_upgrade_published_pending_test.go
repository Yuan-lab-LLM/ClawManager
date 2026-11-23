package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type publishedRecoveryAgent struct {
	RuntimeAgentClient
	deleteErr   error
	deletes     int
	keepRunning bool
	afterDelete func()
}

func (a *publishedRecoveryAgent) RequiresConfirmedGatewayStop() bool { return true }
func (a *publishedRecoveryAgent) verifyPublishedUpgradeReservation(_ context.Context, c runtimeUpgradeCandidate, b models.InstanceRuntimeBinding) (string, error) {
	if c.InstanceID != 481 || c.UserID != 1 || c.Generation != b.Generation || c.GatewayID != b.GatewayID || b.State != "creating" || b.GatewayPID != nil {
		return "", ErrUpgradeSourceState
	}
	if a.deletes > 0 && a.deleteErr == nil && !a.keepRunning {
		return "stopped", nil
	}
	return "running", nil
}
func (a *publishedRecoveryAgent) DeleteGateway(_ context.Context, endpoint, id string) error {
	a.deletes++
	if endpoint != "http://10.1.2.2:19090" || id != "pending-481-8" {
		return errors.New("wrong native stop identity")
	}
	if a.afterDelete != nil {
		a.afterDelete()
	}
	return a.deleteErr
}

func TestPublishedUpgradeReservationPinsOriginalLaunchWithoutMutation(t *testing.T) {
	for _, name := range []string{"valid", "owner", "generation", "workspace", "port", "pod", "request"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			kube := fake.NewSimpleClientset(slotTestPod())
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })}).(*slotRuntimeAgentClient)
			workspace := "/workspaces/openclaw/user-2/instance-7"
			_, _ = client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 2, AgentType: "openclaw", GatewayPort: 20000, WorkspacePath: workspace})
			podID := int64(9)
			b := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: podID, Generation: 2, GatewayID: "pending-7-2", GatewayPort: 20000, State: "creating", WorkspacePath: workspace}
			c := runtimeUpgradeCandidate{InstanceID: 7, UserID: 2, Generation: 2, RuntimePodID: &podID, GatewayID: b.GatewayID, WorkspacePath: workspace, SourcePodUID: "pod-uid", AgentEndpoint: "https://10.0.0.2:19090"}
			switch name {
			case "owner":
				c.UserID++
			case "generation":
				c.Generation++
			case "workspace":
				b.WorkspacePath += "-other"
			case "port":
				b.GatewayPort++
			case "pod":
				c.SourcePodUID = "replacement"
			case "request":
				cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
				cm.Data["request"] = strings.Replace(cm.Data["request"], `"user_id":2`, `"user_id":3`, 1)
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			}
			if _, err := client.verifyPublishedUpgradeReservation(ctx, c, b); (err == nil) != (name == "valid") {
				t.Fatalf("verification %v", err)
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
			if cm.Data["desired_state"] != "running" {
				t.Fatal("read-only verification changed lifecycle")
			}
		})
	}
}
