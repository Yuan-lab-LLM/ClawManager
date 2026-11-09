package k8s

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestSlotPoolRolloutKeepsTLSReadinessAfterActivation(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "opencode", Image: "image-old", SlotCount: 2, AgentTokenSecretName: "tokens"})
	client := fake.NewSimpleClientset(deployment)
	service := NewRuntimeDeploymentService(client)
	for _, upgradeID := range []string{"81", ""} {
		if err := service.RolloutImage(context.Background(), "runtime", "pool", "image-new", upgradeID, 0, 1); err != nil {
			t.Fatal(err)
		}
		updated, err := client.AppsV1().Deployments("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		probe := updated.Spec.Template.Spec.Containers[0].ReadinessProbe
		if probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Scheme != corev1.URISchemeHTTPS || probe.HTTPGet.Path != "/readyz" {
			t.Fatalf("upgrade %q lost pool HTTPS readiness: %#v", upgradeID, probe)
		}
		if err := validateRuntimeSlotTLSDeployment(updated); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTLSConfiguredPoolWithoutLegacyReadinessProbeCanBeAdopted(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "opencode", Image: "image", SlotCount: 1, AgentTokenSecretName: "tokens"})
	deployment.Spec.Template.Spec.Containers[0].ReadinessProbe = nil
	if err := validateRuntimeSlotTLSDeployment(deployment); err != nil {
		t.Fatalf("valid TLS listener rejected because an older rollout removed its readiness probe: %v", err)
	}
	removeEnvVar(&deployment.Spec.Template.Spec.Containers[0], "RUNTIME_AGENT_TLS_CERT_FILE")
	if err := validateRuntimeSlotTLSDeployment(deployment); err == nil {
		t.Fatal("missing TLS certificate configuration was adopted")
	}
}

func TestUpgradePoolCloneSynchronizesAllSlotsAndTLS(t *testing.T) {
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "openclaw", Image: "old", SlotCount: 2, AgentTokenSecretName: "tokens", NetworkIsolation: true})
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	target := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	if err := service.EnsureUpgradePool(context.Background(), "runtime", "pool", "pool-u9", target, "9"); err != nil {
		t.Fatal(err)
	}
	pool, err := client.AppsV1().Deployments("runtime").Get(context.Background(), "pool-u9", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"clawmanager.io/runtime-protocol", "clawmanager.io/runtime-security"} {
		if pool.Spec.Template.Labels[key] != source.Spec.Template.Labels[key] {
			t.Fatalf("upgrade clone lost %s", key)
		}
	}
	for _, c := range pool.Spec.Template.Spec.Containers {
		if c.Image != target {
			t.Fatalf("%s retained source image %s", c.Name, c.Image)
		}
		requireEnv(t, c, "CLAWMANAGER_RUNTIME_UPGRADE_ID", "9")
		requireEnv(t, c, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", "pool-u9")
	}
	if err := validateRuntimeSlotTLSDeployment(pool); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeLabClonePreservesSlotProtocolAndTLS(t *testing.T) {
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "openclaw", Image: "old", SlotCount: 2, AgentTokenSecretName: "tokens", NetworkIsolation: true})
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	target := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	if err := service.(*runtimeDeploymentService).EnsureUpgradeLabPool(context.Background(), "runtime", "pool", "lab", target, "lab-9", 1); err != nil {
		t.Fatal(err)
	}
	pool, err := client.AppsV1().Deployments("runtime").Get(context.Background(), "lab", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pool.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
		t.Fatal("lab lost slot protocol")
	}
	for _, c := range pool.Spec.Template.Spec.Containers {
		if c.Image != target {
			t.Fatalf("%s image not synchronized", c.Name)
		}
		requireEnv(t, c, "CLAWMANAGER_RUNTIME_UPGRADE_ID", "lab-9")
	}
	if err := validateRuntimeSlotTLSDeployment(pool); err != nil {
		t.Fatal(err)
	}
}
