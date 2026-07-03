package k8s

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestBuildRuntimeDeployment(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-openclaw",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw:latest",
		Replicas:           2,
		WorkspaceNFSServer: "10.0.0.9",
		WorkspaceNFSPath:   "/exports/workspaces",
		AgentControlToken:  "control-secret",
		AgentReportToken:   "report-secret",
	})

	if deployment.Name != "runtime-openclaw" || deployment.Namespace != "runtime-system" {
		t.Fatalf("unexpected deployment identity: %s/%s", deployment.Namespace, deployment.Name)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("expected replicas 2, got %#v", deployment.Spec.Replicas)
	}
	if got := deployment.Labels["clawmanager.io/runtime-type"]; got != "openclaw" {
		t.Fatalf("expected runtime-type label openclaw, got %q", got)
	}

	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected one container, got %d", len(deployment.Spec.Template.Spec.Containers))
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Name != "runtime" || container.Image != "registry/openclaw:latest" {
		t.Fatalf("unexpected runtime container: %#v", container)
	}
	requireContainerPort(t, container, "agent", 19090)
	requireVolumeMount(t, container, "workspaces", "/workspaces")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_TYPE", "openclaw")
	requireEnv(t, container, "CLAWMANAGER_AGENT_PORT", "19090")
	requireEnv(t, container, "CLAWMANAGER_GATEWAY_PORT_START", "20000")
	requireEnv(t, container, "CLAWMANAGER_GATEWAY_PORT_END", "20299")
	requireEnv(t, container, "CLAWMANAGER_AGENT_CONTROL_TOKEN", "control-secret")
	requireEnv(t, container, "CLAWMANAGER_AGENT_REPORT_TOKEN", "report-secret")

	if got := container.Resources.Requests[corev1.ResourceCPU]; got.Cmp(resource.MustParse("500m")) != 0 {
		t.Fatalf("expected CPU request 500m, got %s", got.String())
	}
	if got := container.Resources.Requests[corev1.ResourceMemory]; got.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatalf("expected memory request 1Gi, got %s", got.String())
	}

	if len(deployment.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("expected one volume, got %#v", deployment.Spec.Template.Spec.Volumes)
	}
	volume := deployment.Spec.Template.Spec.Volumes[0]
	if volume.Name != "workspaces" || volume.NFS == nil {
		t.Fatalf("expected workspaces NFS volume, got %#v", volume)
	}
	if volume.NFS.Server != "10.0.0.9" || volume.NFS.Path != "/exports/workspaces" {
		t.Fatalf("unexpected NFS source: %#v", volume.NFS)
	}
}

func TestBuildRuntimeDeploymentUsesWorkspacePVCWhenClaimConfigured(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:                  "runtime-openclaw",
		Namespace:             "runtime-system",
		RuntimeType:           "openclaw",
		Image:                 "registry/openclaw:latest",
		Replicas:              2,
		WorkspacePVCClaimName: "clawmanager-workspaces",
		WorkspaceNFSServer:    "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:      "/exports/workspaces",
	})

	if len(deployment.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("expected one volume, got %#v", deployment.Spec.Template.Spec.Volumes)
	}
	volume := deployment.Spec.Template.Spec.Volumes[0]
	if volume.Name != "workspaces" || volume.PersistentVolumeClaim == nil {
		t.Fatalf("expected workspaces PVC volume, got %#v", volume)
	}
	if got, want := volume.PersistentVolumeClaim.ClaimName, "clawmanager-workspaces"; got != want {
		t.Fatalf("PVC claim name = %q, want %q", got, want)
	}
	if volume.NFS != nil {
		t.Fatalf("PVC workspace volume must not also include NFS source: %#v", volume.NFS)
	}
}

func TestBuildRuntimeDeploymentKeepsCapacityLimitOutOfRuntimeEnv(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-openclaw",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw:latest",
		Replicas:           2,
		WorkspaceNFSServer: "10.0.0.9",
		WorkspaceNFSPath:   "/exports/workspaces",
		WorkspaceMountPath: "/runtime-workspaces",
		GatewayPortStart:   21000,
		GatewayPortEnd:     21049,
	})

	container := deployment.Spec.Template.Spec.Containers[0]
	requireVolumeMount(t, container, "workspaces", "/runtime-workspaces")
	requireEnv(t, container, "CLAWMANAGER_GATEWAY_PORT_START", "21000")
	requireEnv(t, container, "CLAWMANAGER_GATEWAY_PORT_END", "21049")
	requireEnvAbsent(t, container, "CLAWMANAGER_MAX_GATEWAYS_PER_POD")
	requireEnvAbsent(t, container, "RUNTIME_MAX_GATEWAYS_PER_POD")
}

func TestBuildRuntimeDeploymentInjectsAgentV2Environment(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-hermes",
		Namespace:          "runtime-system",
		RuntimeType:        "hermes",
		Image:              "registry/hermes:v2",
		Replicas:           1,
		WorkspaceNFSServer: "10.0.0.9",
		WorkspaceNFSPath:   "/exports/workspaces",
		WorkspaceMountPath: "/workspaces",
		GatewayPortStart:   21000,
		GatewayPortEnd:     21099,
		AgentControlToken:  "control-secret",
		AgentReportToken:   "report-secret",
		BackendURL:         "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001",
		TrustedProxyCIDRs:  "10.42.0.0/16,10.43.0.0/16",
	})

	container := deployment.Spec.Template.Spec.Containers[0]
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_TYPE", "hermes")
	requireEnv(t, container, "CLAWMANAGER_BACKEND_URL", "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", "runtime-hermes")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_IMAGE_REF", "registry/hermes:v2")
	requireEnv(t, container, "RUNTIME_WORKSPACE_ROOT", "/workspaces")
	requireEnv(t, container, "RUNTIME_AGENT_LISTEN_ADDR", "0.0.0.0:19090")
	requireEnv(t, container, "RUNTIME_AGENT_PUBLIC_PORT", "19090")
	requireEnv(t, container, "RUNTIME_AGENT_CONTROL_TOKEN", "control-secret")
	requireEnv(t, container, "RUNTIME_AGENT_REPORT_TOKEN", "report-secret")
	requireEnv(t, container, "RUNTIME_GATEWAY_PORT_START", "21000")
	requireEnv(t, container, "RUNTIME_GATEWAY_PORT_END", "21099")
	requireEnv(t, container, "HERMES_TUI_DIR", "/usr/local/lib/hermes-agent/ui-tui")
	requireEnv(t, container, "CLAWMANAGER_TRUSTED_PROXY_CIDRS", "10.42.0.0/16,10.43.0.0/16")
	requireEnvFieldRef(t, container, "POD_NAME", "metadata.name")
	requireEnvFieldRef(t, container, "POD_NAMESPACE", "metadata.namespace")
	requireEnvFieldRef(t, container, "POD_IP", "status.podIP")
	requireEnvFieldRef(t, container, "NODE_NAME", "spec.nodeName")
}

func TestRuntimeDeploymentServiceEnsureCreatesAndUpdates(t *testing.T) {
	client := fake.NewSimpleClientset()
	service := NewRuntimeDeploymentService(client)
	spec := RuntimeDeploymentSpec{
		Name:               "runtime-hermes",
		Namespace:          "runtime-system",
		RuntimeType:        "hermes",
		Image:              "registry/hermes:v1",
		Replicas:           1,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	}

	if err := service.Ensure(context.Background(), spec); err != nil {
		t.Fatalf("Ensure create returned error: %v", err)
	}
	created, err := client.AppsV1().Deployments(spec.Namespace).Get(context.Background(), spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created deployment: %v", err)
	}
	if created.Spec.Replicas == nil || *created.Spec.Replicas != 1 {
		t.Fatalf("expected created replicas 1, got %#v", created.Spec.Replicas)
	}

	spec.Image = "registry/hermes:v2"
	spec.Replicas = 3
	if err := service.Ensure(context.Background(), spec); err != nil {
		t.Fatalf("Ensure update returned error: %v", err)
	}
	updated, err := client.AppsV1().Deployments(spec.Namespace).Get(context.Background(), spec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if updated.Spec.Template.Spec.Containers[0].Image != "registry/hermes:v2" {
		t.Fatalf("expected updated image, got %q", updated.Spec.Template.Spec.Containers[0].Image)
	}
	if updated.Spec.Replicas == nil || *updated.Spec.Replicas != 3 {
		t.Fatalf("expected updated replicas 3, got %#v", updated.Spec.Replicas)
	}
}

func TestRuntimeDeploymentServiceEnsurePreservesUnownedMetadata(t *testing.T) {
	existing := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-openclaw",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw:v1",
		Replicas:           1,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	})
	existing.Labels["admission.example.com/injected"] = "keep"
	existing.Annotations = map[string]string{"admission.example.com/sidecar": "enabled"}
	existing.Finalizers = []string{"protect.example.com/runtime"}
	existing.Spec.Template.Labels["admission.example.com/template-label"] = "keep"
	existing.Spec.Template.Annotations = map[string]string{"admission.example.com/template-annotation": "enabled"}
	client := fake.NewSimpleClientset(existing)
	service := NewRuntimeDeploymentService(client)

	if err := service.Ensure(context.Background(), RuntimeDeploymentSpec{
		Name:               "runtime-openclaw",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw:v2",
		Replicas:           3,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	}); err != nil {
		t.Fatalf("Ensure update returned error: %v", err)
	}

	updated, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "runtime-openclaw", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if updated.Labels["admission.example.com/injected"] != "keep" {
		t.Fatalf("expected unrelated label to survive, got %#v", updated.Labels)
	}
	if updated.Annotations["admission.example.com/sidecar"] != "enabled" {
		t.Fatalf("expected annotation to survive, got %#v", updated.Annotations)
	}
	if len(updated.Finalizers) != 1 || updated.Finalizers[0] != "protect.example.com/runtime" {
		t.Fatalf("expected finalizer to survive, got %#v", updated.Finalizers)
	}
	if updated.Labels["clawmanager.io/runtime-type"] != "openclaw" {
		t.Fatalf("expected owned runtime label to be present, got %#v", updated.Labels)
	}
	if updated.Spec.Template.Labels["admission.example.com/template-label"] != "keep" {
		t.Fatalf("expected unrelated template label to survive, got %#v", updated.Spec.Template.Labels)
	}
	if updated.Spec.Template.Annotations["admission.example.com/template-annotation"] != "enabled" {
		t.Fatalf("expected template annotation to survive, got %#v", updated.Spec.Template.Annotations)
	}
	if updated.Spec.Template.Labels["clawmanager.io/runtime-type"] != "openclaw" {
		t.Fatalf("expected owned template runtime label to be present, got %#v", updated.Spec.Template.Labels)
	}
	if updated.Spec.Template.Spec.Containers[0].Image != "registry/openclaw:v2" {
		t.Fatalf("expected image update while preserving template metadata, got %q", updated.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestRuntimeDeploymentServiceEnsureRejectsSelectorChange(t *testing.T) {
	existing := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-openclaw", Namespace: "runtime-system"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "different"}},
		},
	}
	client := fake.NewSimpleClientset(existing)
	service := NewRuntimeDeploymentService(client)

	err := service.Ensure(context.Background(), RuntimeDeploymentSpec{
		Name:        "runtime-openclaw",
		Namespace:   "runtime-system",
		RuntimeType: "openclaw",
		Image:       "registry/openclaw:latest",
		Replicas:    1,
	})
	if err == nil {
		t.Fatalf("expected selector mismatch error")
	}
}

func TestRuntimeDeploymentServiceScale(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-openclaw",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw:latest",
		Replicas:           1,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	})
	deployment.Spec.Template.Annotations = map[string]string{"admission.example.com/template": "keep"}
	client := fake.NewSimpleClientset(deployment)
	service := NewRuntimeDeploymentService(client)

	if err := service.Scale(context.Background(), "runtime-system", "runtime-openclaw", 4); err != nil {
		t.Fatalf("Scale returned error: %v", err)
	}
	scaled, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "runtime-openclaw", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get scaled deployment: %v", err)
	}
	if scaled.Spec.Replicas == nil || *scaled.Spec.Replicas != 4 {
		t.Fatalf("expected replicas 4, got %#v", scaled.Spec.Replicas)
	}
	if scaled.Spec.Template.Spec.Containers[0].Image != "registry/openclaw:latest" {
		t.Fatalf("expected Scale to preserve image, got %q", scaled.Spec.Template.Spec.Containers[0].Image)
	}
	if scaled.Spec.Template.Annotations["admission.example.com/template"] != "keep" {
		t.Fatalf("expected Scale to preserve template metadata, got %#v", scaled.Spec.Template.Annotations)
	}
}

func TestRuntimeDeploymentServiceRolloutImage(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-hermes",
		Namespace:          "runtime-system",
		RuntimeType:        "hermes",
		Image:              "registry/hermes:v1",
		Replicas:           3,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	})
	client := fake.NewSimpleClientset(deployment)
	service := NewRuntimeDeploymentService(client)

	if err := service.RolloutImage(context.Background(), "runtime-system", "runtime-hermes", "registry/hermes:v2", 1, 2); err != nil {
		t.Fatalf("RolloutImage returned error: %v", err)
	}

	updated, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "runtime-hermes", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	container := updated.Spec.Template.Spec.Containers[0]
	if container.Image != "registry/hermes:v2" {
		t.Fatalf("expected updated image, got %q", container.Image)
	}
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_IMAGE_REF", "registry/hermes:v2")
	if updated.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Fatalf("expected RollingUpdate strategy, got %q", updated.Spec.Strategy.Type)
	}
	if updated.Spec.Strategy.RollingUpdate == nil {
		t.Fatal("expected rolling update strategy")
	}
	if got := updated.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue(); got != 1 {
		t.Fatalf("maxUnavailable = %d, want 1", got)
	}
	if got := updated.Spec.Strategy.RollingUpdate.MaxSurge.IntValue(); got != 2 {
		t.Fatalf("maxSurge = %d, want 2", got)
	}
}

func TestRuntimeDeploymentServiceRolloutImageRetriesConflict(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "runtime-hermes",
		Namespace:          "runtime-system",
		RuntimeType:        "hermes",
		Image:              "registry/hermes:v1",
		Replicas:           1,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	})
	client := fake.NewSimpleClientset(deployment)
	updateAttempts := 0
	client.Fake.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "runtime-hermes", fmt.Errorf("stale resource version"))
		}
		return false, nil, nil
	})
	service := NewRuntimeDeploymentService(client)

	if err := service.RolloutImage(context.Background(), "runtime-system", "runtime-hermes", "registry/hermes:v2", 1, 1); err != nil {
		t.Fatalf("RolloutImage returned error: %v", err)
	}
	if updateAttempts < 2 {
		t.Fatalf("update attempts = %d, want retry after conflict", updateAttempts)
	}

	updated, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "runtime-hermes", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated deployment: %v", err)
	}
	if got := updated.Spec.Template.Spec.Containers[0].Image; got != "registry/hermes:v2" {
		t.Fatalf("image = %q, want registry/hermes:v2", got)
	}
}

func TestRuntimeDeploymentServiceListPodsReturnsRuntimeDeploymentPods(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:               "openclaw-runtime",
		Namespace:          "runtime-system",
		RuntimeType:        "openclaw",
		Image:              "registry/openclaw-lite:final2",
		Replicas:           1,
		WorkspaceNFSServer: "nfs.local",
		WorkspaceNFSPath:   "/exports",
	})
	podIP := "10.42.0.12"
	nodeName := "node-a"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openclaw-runtime-abc",
			Namespace: "runtime-system",
			Labels: map[string]string{
				"app":                         "openclaw-runtime",
				"clawmanager.io/runtime-type": "openclaw",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name:  "runtime",
				Image: "registry/openclaw-lite:final2",
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: podIP,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	client := fake.NewSimpleClientset(deployment, pod)
	service := NewRuntimeDeploymentService(client)

	pods, err := service.ListPods(context.Background(), "runtime-system", "openclaw")
	if err != nil {
		t.Fatalf("ListPods returned error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("pods length = %d, want 1: %#v", len(pods), pods)
	}
	got := pods[0]
	if got.RuntimeType != "openclaw" || got.Namespace != "runtime-system" || got.DeploymentName != "openclaw-runtime" || got.PodName != "openclaw-runtime-abc" {
		t.Fatalf("runtime pod identity = %+v, want openclaw runtime pod", got)
	}
	if got.ImageRef != "registry/openclaw-lite:final2" {
		t.Fatalf("image = %q, want registry/openclaw-lite:final2", got.ImageRef)
	}
	if got.State != "ready" {
		t.Fatalf("state = %q, want ready", got.State)
	}
	if got.PodIP == nil || *got.PodIP != podIP || got.NodeName == nil || *got.NodeName != nodeName {
		t.Fatalf("pod network fields = ip:%v node:%v, want %s/%s", got.PodIP, got.NodeName, podIP, nodeName)
	}
}

func requireContainerPort(t *testing.T, container corev1.Container, name string, port int32) {
	t.Helper()
	for _, got := range container.Ports {
		if got.Name == name && got.ContainerPort == port {
			return
		}
	}
	t.Fatalf("expected container port %s=%d, got %#v", name, port, container.Ports)
}

func requireVolumeMount(t *testing.T, container corev1.Container, name, mountPath string) {
	t.Helper()
	for _, got := range container.VolumeMounts {
		if got.Name == name && got.MountPath == mountPath {
			return
		}
	}
	t.Fatalf("expected volume mount %s at %s, got %#v", name, mountPath, container.VolumeMounts)
}

func requireEnv(t *testing.T, container corev1.Container, name, value string) {
	t.Helper()
	for _, got := range container.Env {
		if got.Name == name && got.Value == value {
			return
		}
	}
	t.Fatalf("expected env %s=%q, got %#v", name, value, container.Env)
}

func requireEnvAbsent(t *testing.T, container corev1.Container, name string) {
	t.Helper()
	for _, got := range container.Env {
		if got.Name == name {
			t.Fatalf("expected env %s to be absent, got %#v", name, container.Env)
		}
	}
}

func requireEnvFieldRef(t *testing.T, container corev1.Container, name, fieldPath string) {
	t.Helper()
	for _, got := range container.Env {
		if got.Name != name || got.ValueFrom == nil || got.ValueFrom.FieldRef == nil {
			continue
		}
		if got.ValueFrom.FieldRef.FieldPath == fieldPath {
			return
		}
	}
	t.Fatalf("expected env %s from fieldRef %q, got %#v", name, fieldPath, container.Env)
}
