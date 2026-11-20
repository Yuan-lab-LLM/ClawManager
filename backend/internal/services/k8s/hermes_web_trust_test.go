package k8s

import (
	"context"
	"errors"
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

func TestHermesWebTrustDiscoversControlPlaneDuringColdStart(t *testing.T) {
	endpoints := &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "tenant"}, Subsets: []corev1.EndpointSubset{{NotReadyAddresses: []corev1.EndpointAddress{
		{IP: "10.1.2.3", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "tenant", Name: "app"}},
		{IP: "10.1.2.4", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "other", Name: "app"}},
		{IP: "10.1.2.5"},
		{IP: "10.1.2.6", TargetRef: &corev1.ObjectReference{Kind: "Service", Namespace: "tenant", Name: "app"}},
		{IP: "invalid", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "tenant", Name: "app"}},
	}}}}
	s := &runtimeDeploymentService{client: fake.NewSimpleClientset(endpoints)}
	env, err := s.hermesWebEnvironment(context.Background(), "tenant", corev1.Container{Env: []corev1.EnvVar{{Name: "CLAWMANAGER_BACKEND_URL", Value: "http://manager.tenant.svc.cluster.local:9001"}}})
	if err != nil {
		t.Fatalf("cold-start Service Pod identity must not depend on HTTP readiness: %v", err)
	}
	if env["CLAWMANAGER_TRUSTED_PROXY_CIDRS"] != "10.1.2.3" {
		t.Fatalf("unexpected cold-start proxy identities: %v", env)
	}
}

func TestGuardedHermesWebRolloutChecksSameImageTemplateChange(t *testing.T) {
	const image = "runtime/hermes@sha256:abc"
	dep := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "hermes-runtime", Namespace: "tenant", RuntimeType: "hermes", Image: image, Replicas: 1, BackendURL: "http://manager.tenant.svc.cluster.local:9001", SlotCount: 8})
	dep.UID = "source-uid"
	endpoints := &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "tenant"}, Subsets: []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{
		{IP: "10.1.2.3", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "tenant", Name: "app"}},
	}}}}
	client := fake.NewSimpleClientset(dep, endpoints)
	service := &runtimeDeploymentService{client: client}
	source := RuntimeRolloutTarget{Namespace: dep.Namespace, Name: dep.Name, UID: string(dep.UID), Image: image}
	occupied := errors.New("occupied")
	check := func(context.Context) error { return occupied }
	if err := service.RolloutGuardedPoolImage(t.Context(), source, "hermes", image, true, check); !errors.Is(err, occupied) {
		t.Fatalf("same image bypassed an occupied pool: %v", err)
	}
	for _, action := range client.Actions() {
		if action.Matches("update", "deployments") {
			t.Fatal("changed the occupied Deployment template")
		}
	}
	if err := service.RolloutGuardedPoolImage(t.Context(), source, "hermes", image, true, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if err := service.RolloutGuardedPoolImage(t.Context(), source, "hermes", image, true, check); err != nil {
		t.Fatalf("identical template needs only verification: %v", err)
	}
	for _, action := range client.Actions() {
		if action.Matches("update", "deployments") {
			t.Fatal("wrote an identical template")
		}
	}
	source.UID = "recreated"
	if err := service.RolloutGuardedPoolImage(t.Context(), source, "hermes", image, true, check); err == nil {
		t.Fatal("recreated Deployment was accepted")
	}
}
