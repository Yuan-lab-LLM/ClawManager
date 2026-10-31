package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
	"time"
)

func TestOwnerTeamReadinessRequiresCurrentGatewayAndPod(t *testing.T) {
	for _, name := range []string{"ready", "unassigned", "creating", "old-generation", "old-pod", "old-agent", "stale-heartbeat", "gateway-stopped", "gateway-other-instance", "pro-ready", "pro-unready"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			report, live := ordinaryPlacementFixture()
			labels := map[string]string{"app": "pool", "instance-id": "901"}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: live.PodName, Namespace: live.Namespace, UID: types.UID(live.PodUID), Labels: labels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: live.ImageRef}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: *live.PodIP, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "tenant", Labels: map[string]string{"clawmanager.io/runtime-type": "openclaw"}}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pool"}}}}
			client := &k8s.Client{Namespace: "tenant", Clientset: fake.NewSimpleClientset(dep, pod)}
			bindings := newFakeRuntimeBindingRepo()
			bindings.bindings[901] = &models.InstanceRuntimeBinding{InstanceID: 901, RuntimePodID: report.ID, RuntimeType: "openclaw", GatewayID: "gw-901-1", GatewayPort: 20000, Generation: 1, State: "running"}
			agent := &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-901-1": {GatewayID: "gw-901-1", InstanceID: 901, Generation: 1, State: "running"}}}
			i := &models.Instance{ID: 901, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", RuntimeGeneration: 1, Status: "running"}
			switch name {
			case "unassigned":
				delete(bindings.bindings, 901)
			case "creating":
				i.Status = "creating"
			case "old-generation":
				i.RuntimeGeneration = 2
			case "old-pod":
				report.PodUID = ptrString("deleted")
			case "old-agent":
				report.PodUID = nil
			case "stale-heartbeat":
				old := time.Now().Add(-time.Hour)
				report.LastSeenAt = &old
			case "gateway-stopped":
				agent.states["gw-901-1"].State = "stopped"
			case "gateway-other-instance":
				agent.states["gw-901-1"].InstanceID = 902
			case "pro-ready", "pro-unready":
				i.InstanceMode = "pro"
				i.RuntimeType = "desktop"
				pod.Namespace = client.GetNamespace(1)
				if name == "pro-unready" {
					pod.Status.Conditions[0].Status = corev1.ConditionFalse
				}
				client.Clientset = fake.NewSimpleClientset(pod)
			}
			s := &instanceService{bindingRepo: bindings, runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{report.ID: &report}}, agentClient: agent, podService: k8s.NewPodServiceWithClient(client)}
			ready, err := s.OwnerTeamRuntimeReady(ctx, i)
			want := name == "ready" || name == "old-agent" || name == "pro-ready"
			if err != nil || ready != want {
				t.Fatalf("ready=%t want=%t err=%v", ready, want, err)
			}
		})
	}
}
