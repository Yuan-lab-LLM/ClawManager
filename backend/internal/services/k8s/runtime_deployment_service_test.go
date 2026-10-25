package k8s

import (
	"context"
	"fmt"
	"strings"
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
	requireEnv(t, container, "CLAWMANAGER_DEPLOYMENT_NAME", "runtime-hermes")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_IMAGE_REF", "registry/hermes:v2")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", "")
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

func TestRuntimeDeploymentReportsImmutableImageDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	image := "registry/openclaw@sha256:" + digest
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "runtime-openclaw", Namespace: "runtime-system", RuntimeType: "openclaw", Image: image, Replicas: 1})
	requireEnv(t, deployment.Spec.Template.Spec.Containers[0], "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", "sha256:"+digest)
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

	if err := service.RolloutImage(context.Background(), "runtime-system", "runtime-hermes", "registry/hermes:v2", "", 1, 2); err != nil {
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

func TestRuntimeDeploymentServiceCreatesIsolatedUpgradePool(t *testing.T) {
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "registry/openclaw:old", Replicas: 3, WorkspacePVCClaimName: "workspaces"})
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	targetImage := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	if err := service.EnsureUpgradePool(context.Background(), "runtime-system", "openclaw-runtime", "openclaw-runtime-u81", targetImage, "81"); err != nil {
		t.Fatal(err)
	}
	target, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "openclaw-runtime-u81", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if target.Spec.Replicas == nil || *target.Spec.Replicas != 3 {
		t.Fatalf("target replicas = %v, want 3", target.Spec.Replicas)
	}
	if target.Spec.Selector.MatchLabels["app"] != "openclaw-runtime-u81" || target.Labels["clawmanager.io/source-deployment"] != "openclaw-runtime" {
		t.Fatalf("target labels/selectors are not isolated: labels=%v selector=%v", target.Labels, target.Spec.Selector.MatchLabels)
	}
	if target.Labels[runtimeSchedulingLabel] != "false" || target.Spec.Template.Labels[runtimeSchedulingLabel] != "false" {
		t.Fatalf("upgrade target must start unschedulable: labels=%v template=%v", target.Labels, target.Spec.Template.Labels)
	}
	container := target.Spec.Template.Spec.Containers[0]
	if container.Image != targetImage {
		t.Fatalf("target runtime image = %s", container.Image)
	}
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", "81")
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", "openclaw-runtime-u81")
	requireEnv(t, container, "CLAWMANAGER_DEPLOYMENT_NAME", "openclaw-runtime-u81")
	unchanged, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "openclaw-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Spec.Template.Spec.Containers[0].Image != "registry/openclaw:old" {
		t.Fatalf("source deployment image was mutated: %s", unchanged.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestRuntimeDeploymentServiceActivatesAndSafelyDeletesUpgradePool(t *testing.T) {
	const namespace = "runtime-system"
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: namespace, RuntimeType: "openclaw", Image: "registry/openclaw:old", Replicas: 1, WorkspacePVCClaimName: "workspaces"})
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	targetImage := "registry/openclaw@sha256:" + strings.Repeat("c", 64)
	if err := service.EnsureUpgradePool(context.Background(), namespace, source.Name, "openclaw-runtime-u9", targetImage, "9"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetUpgradePoolActive(context.Background(), namespace, source.Name, "openclaw-runtime-u9", "9", true); err != nil {
		t.Fatal(err)
	}
	activeSource, _ := client.AppsV1().Deployments(namespace).Get(context.Background(), source.Name, metav1.GetOptions{})
	activeTarget, _ := client.AppsV1().Deployments(namespace).Get(context.Background(), "openclaw-runtime-u9", metav1.GetOptions{})
	if activeSource.Labels[runtimeSchedulingLabel] != "false" || activeTarget.Labels[runtimeSchedulingLabel] != "true" {
		t.Fatalf("activation labels source=%v target=%v", activeSource.Labels, activeTarget.Labels)
	}
	if err := service.SetUpgradePoolActive(context.Background(), namespace, source.Name, "openclaw-runtime-u9", "9", false); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteUpgradePool(context.Background(), namespace, source.Name, "openclaw-runtime-u9", "9"); err == nil {
		t.Fatal("expected deletion to fail before target is scaled to zero")
	}
	if err := service.Scale(context.Background(), namespace, "openclaw-runtime-u9", 0); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteUpgradePool(context.Background(), namespace, source.Name, "openclaw-runtime-u9", "wrong"); err == nil {
		t.Fatal("expected ownership mismatch to fail closed")
	}
	if err := service.DeleteUpgradePool(context.Background(), namespace, source.Name, "openclaw-runtime-u9", "9"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AppsV1().Deployments(namespace).Get(context.Background(), "openclaw-runtime-u9", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("upgrade target still exists: %v", err)
	}
}

func TestRuntimeDeploymentServiceScopedCommitKeepsSourceScheduling(t *testing.T) {
	const ns = "runtime-system"
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: ns, RuntimeType: "openclaw", Image: "old", Replicas: 1, WorkspacePVCClaimName: "workspaces"})
	source.Labels[runtimeSchedulingLabel] = "true"
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	if err := service.EnsureUpgradePool(context.Background(), ns, source.Name, "openclaw-runtime-u99", "registry/core@sha256:"+strings.Repeat("c", 64), "99"); err != nil {
		t.Fatal(err)
	}
	preserver := service.(interface {
		SetUpgradePoolActivePreservingSource(context.Context, string, string, string, string) error
	})
	if err := preserver.SetUpgradePoolActivePreservingSource(context.Background(), ns, source.Name, "openclaw-runtime-u99", "99"); err != nil {
		t.Fatal(err)
	}
	old, _ := client.AppsV1().Deployments(ns).Get(context.Background(), source.Name, metav1.GetOptions{})
	next, _ := client.AppsV1().Deployments(ns).Get(context.Background(), "openclaw-runtime-u99", metav1.GetOptions{})
	if old.Labels[runtimeSchedulingLabel] != "true" || next.Labels[runtimeSchedulingLabel] != "true" || *old.Spec.Replicas != 1 {
		t.Fatal("scoped commit disabled the excluded instances' source")
	}
}

func TestRuntimeDeploymentServiceUpgradeLabOwnershipIsIsolatedAndDeleteIsFailClosed(t *testing.T) {
	const namespace = "runtime-system"
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: namespace, RuntimeType: "openclaw", Image: "registry/openclaw:serving", Replicas: 2, WorkspacePVCClaimName: "workspaces"})
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	lab, ok := service.(RuntimeUpgradeLabDeploymentService)
	if !ok {
		t.Fatal("runtime deployment service does not expose upgrade lab operations")
	}
	labImage := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	if err := lab.EnsureUpgradeLabPool(context.Background(), namespace, source.Name, "openclaw-upgrade-lab-r7-source", labImage, "lab-r7", 1); err != nil {
		t.Fatal(err)
	}
	created, err := client.AppsV1().Deployments(namespace).Get(context.Background(), "openclaw-upgrade-lab-r7-source", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Labels[upgradeLabPurposeLabel] != upgradeLabPurposeValue || created.Labels[upgradeLabRunLabel] != "lab-r7" {
		t.Fatalf("lab ownership labels = %#v", created.Labels)
	}
	if created.Spec.Replicas == nil || *created.Spec.Replicas != 1 || created.Spec.Selector.MatchLabels["app"] != created.Name {
		t.Fatalf("lab pool is not isolated: replicas=%v selector=%#v", created.Spec.Replicas, created.Spec.Selector)
	}
	if got := created.Spec.Template.Spec.Containers[0].Image; got != labImage {
		t.Fatalf("lab image = %q", got)
	}
	serving, err := client.AppsV1().Deployments(namespace).Get(context.Background(), source.Name, metav1.GetOptions{})
	if err != nil || serving.Spec.Template.Spec.Containers[0].Image != "registry/openclaw:serving" {
		t.Fatalf("serving deployment was changed: deployment=%#v err=%v", serving, err)
	}
	if err := lab.DeleteUpgradeLabPool(context.Background(), namespace, source.Name, "lab-r7"); err == nil {
		t.Fatal("cleanup accepted a serving deployment")
	}
	if _, err := client.AppsV1().Deployments(namespace).Get(context.Background(), source.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("serving deployment was removed: %v", err)
	}
	if err := lab.DeleteUpgradeLabPool(context.Background(), namespace, created.Name, "wrong-run"); err == nil {
		t.Fatal("cleanup accepted the wrong lab owner")
	}
	if err := lab.DeleteUpgradeLabPool(context.Background(), namespace, created.Name, "lab-r7"); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeDeploymentServiceCreatesFullStandbySurgeForOpenClawUpgrade(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name: "openclaw-runtime", Namespace: "runtime-system", RuntimeType: "openclaw",
		Image: "registry/openclaw:old", Replicas: 4, WorkspaceNFSServer: "nfs.local", WorkspaceNFSPath: "/exports",
	})
	client := fake.NewSimpleClientset(deployment)
	service := NewRuntimeDeploymentService(client)
	if err := service.RolloutImage(context.Background(), "runtime-system", "openclaw-runtime", "registry/openclaw@sha256:"+strings.Repeat("a", 64), "81", 3, 1); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "openclaw-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	container := updated.Spec.Template.Spec.Containers[0]
	requireEnv(t, container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", "81")
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Path != "/readyz" || container.ReadinessProbe.HTTPGet.Port.StrVal != "agent" {
		t.Fatalf("standby readiness probe = %#v", container.ReadinessProbe)
	}
	if got := updated.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue(); got != 0 {
		t.Fatalf("maxUnavailable = %d, want 0", got)
	}
	if got := updated.Spec.Strategy.RollingUpdate.MaxSurge.IntValue(); got != 4 {
		t.Fatalf("maxSurge = %d, want full replica count 4", got)
	}
	if err := service.RolloutImage(context.Background(), "runtime-system", "openclaw-runtime", "registry/openclaw:old", "", 0, 1); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := client.AppsV1().Deployments("runtime-system").Get(context.Background(), "openclaw-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	container = rolledBack.Spec.Template.Spec.Containers[0]
	if container.ReadinessProbe != nil {
		t.Fatalf("upgrade readiness probe survived rollback: %#v", container.ReadinessProbe)
	}
	for _, env := range container.Env {
		if env.Name == "CLAWMANAGER_RUNTIME_UPGRADE_ID" {
			t.Fatal("upgrade id survived rollback")
		}
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

	if err := service.RolloutImage(context.Background(), "runtime-system", "runtime-hermes", "registry/hermes:v2", "", 1, 1); err != nil {
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
			ContainerStatuses: []corev1.ContainerStatus{{Name: "runtime", ImageID: "docker-pullable://registry/openclaw-lite@sha256:" + strings.Repeat("a", 64)}},
		},
	}
	evicted := pod.DeepCopy()
	evicted.Name = "openclaw-runtime-old-evicted"
	evicted.Status.Phase = corev1.PodFailed
	evicted.Status.Reason = "Evicted"
	client := fake.NewSimpleClientset(deployment, pod, evicted)
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
	if got.ImageDigest != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("image digest = %q", got.ImageDigest)
	}
	if got.State != "ready" {
		t.Fatalf("state = %q, want ready", got.State)
	}
	if got.PodIP == nil || *got.PodIP != podIP || got.NodeName == nil || *got.NodeName != nodeName {
		t.Fatalf("pod network fields = ip:%v node:%v, want %s/%s", got.PodIP, got.NodeName, podIP, nodeName)
	}
}

func TestRuntimeDeploymentServiceListPodsSkipsRetainedZeroReplicaPool(t *testing.T) {
	active := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "registry/openclaw:v1", Replicas: 1})
	retained := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime-u45", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "registry/openclaw:v2", Replicas: 0})
	retained.Labels[runtimePoolRoleLabel] = "upgrade-target"
	retained.Labels[runtimeUpgradeIDLabel] = "45"
	retained.Labels[runtimeSourceLabel] = "openclaw-runtime"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "openclaw-runtime-a", Namespace: "runtime-system", Labels: map[string]string{"app": "openclaw-runtime"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: "registry/openclaw:v1"}}}}
	client := fake.NewSimpleClientset(active, retained, pod)
	podLists := 0
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		podLists++
		return false, nil, nil
	})
	service := NewRuntimeDeploymentService(client)

	if _, err := service.ListPods(context.Background(), "runtime-system", "openclaw"); err != nil {
		t.Fatalf("ListPods returned error: %v", err)
	}
	if podLists != 1 {
		t.Fatalf("pod list calls = %d, want only the active pool", podLists)
	}
}

func TestRuntimeDeploymentServiceListDeploymentPodsIsScoped(t *testing.T) {
	active := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime-u57", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "registry/openclaw:v1", Replicas: 1})
	unrelated := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "openclaw-runtime-u45", Namespace: "runtime-system", RuntimeType: "openclaw", Image: "registry/openclaw:v2", Replicas: 1})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "openclaw-runtime-u57-a", Namespace: "runtime-system", Labels: map[string]string{"app": "openclaw-runtime-u57", "clawmanager.io/runtime-type": "openclaw"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: "registry/openclaw:v1"}}}}
	client := fake.NewSimpleClientset(active, unrelated, pod)
	podLists := 0
	client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		podLists++
		selector := action.(k8stesting.ListAction).GetListRestrictions().Labels.String()
		if strings.Contains(selector, "openclaw-runtime-u45") {
			return true, nil, fmt.Errorf("unrelated pool must not be queried")
		}
		return false, nil, nil
	})
	service := NewRuntimeDeploymentService(client)

	pods, err := service.ListDeploymentPods(context.Background(), []RuntimeDeploymentRef{{Namespace: "runtime-system", Name: "openclaw-runtime-u57"}})
	if err != nil {
		t.Fatalf("ListDeploymentPods returned error: %v", err)
	}
	if podLists != 1 || len(pods) != 1 || pods[0].DeploymentName != "openclaw-runtime-u57" {
		t.Fatalf("scoped inventory calls=%d pods=%#v", podLists, pods)
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
