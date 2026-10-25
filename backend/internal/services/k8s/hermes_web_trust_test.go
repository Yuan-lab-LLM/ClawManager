package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHermesWebRolloutCompletesTrustAtomicallyAndRefreshesEndpoints(t *testing.T) {
	ctx := context.Background()
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "hermes-runtime", Namespace: "tenant", RuntimeType: "hermes", Image: "old", BackendURL: "http://manager.tenant.svc.cluster.local:9001"})
	e := &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "tenant"}, Subsets: []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.1.2.3", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "tenant", Name: "app"}}}}}}
	client := fake.NewSimpleClientset(d, e)
	s := &runtimeDeploymentService{client: client}
	if err := s.RolloutHermesWebImage(ctx, "tenant", "hermes-runtime", "image@sha256:abc", 1, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := client.AppsV1().Deployments("tenant").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	requireEnv(t, got.Spec.Template.Spec.Containers[0], "CLAWMANAGER_CONTROL_UI_ORIGIN", "http://manager.tenant.svc.cluster.local:9001")
	requireEnv(t, got.Spec.Template.Spec.Containers[0], "CLAWMANAGER_TRUSTED_PROXY_CIDRS", "10.1.2.3")
	e.Subsets[0].Addresses[0].IP = "10.1.2.4"
	if _, err := client.CoreV1().Endpoints("tenant").Update(ctx, e, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	env, err := s.HermesGatewayEnvironment(ctx, "tenant", "hermes-runtime")
	if err != nil || env["CLAWMANAGER_TRUSTED_PROXY_CIDRS"] != "10.1.2.4" {
		t.Fatalf("stale trust: %v %v", env, err)
	}
}

func TestHermesWebMissingTrustDoesNotChangeImage(t *testing.T) {
	ctx := context.Background()
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "hermes-runtime", Namespace: "tenant", RuntimeType: "hermes", Image: "old", BackendURL: "http://manager.tenant.svc.cluster.local:9001"})
	client := fake.NewSimpleClientset(d)
	s := &runtimeDeploymentService{client: client}
	if err := s.RolloutHermesWebImage(ctx, "tenant", "hermes-runtime", "new", 1, 1); err == nil {
		t.Fatal("missing endpoints accepted")
	}
	got, _ := client.AppsV1().Deployments("tenant").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if got.Spec.Template.Spec.Containers[0].Image != "old" {
		t.Fatal("changed image before trust validation")
	}
	// Old Hermes still follows the unchanged rollout path.
	if err := s.RolloutImage(ctx, "tenant", "hermes-runtime", "legacy", "", 1, 1); err != nil {
		t.Fatal(err)
	}
}

func TestHermesWebTrustRejectsCrossTenantAndWildcard(t *testing.T) {
	s := &runtimeDeploymentService{client: fake.NewSimpleClientset()}
	for _, values := range [][2]string{{"http://manager.other.svc.cluster.local:9001", "10.1.2.3"}, {"http://manager.tenant.svc.cluster.local:9001", "0.0.0.0/0"}} {
		_, err := s.hermesWebEnvironment(context.Background(), "tenant", corev1.Container{Env: []corev1.EnvVar{{Name: "CLAWMANAGER_CONTROL_UI_ORIGIN", Value: values[0]}, {Name: "CLAWMANAGER_TRUSTED_PROXY_CIDRS", Value: values[1]}}})
		if err == nil {
			t.Fatalf("unsafe config accepted: %v", values)
		}
	}
}
