package k8s

import (
	"clawreef/internal/config"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func elasticRuntimeSpecs(t *testing.T) ([]RuntimeDeploymentSpec, error) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return RuntimeSlotDeploymentSpecs(cfg.Runtime, "pool-agent")
}

func elasticRuntimeEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"RUNTIME_CONTAINER_SLOTS": "8", "RUNTIME_POD_CPU_REQUEST": "4", "RUNTIME_POD_CPU_LIMIT": "4",
		"RUNTIME_POD_MEMORY_REQUEST": "16Gi", "RUNTIME_POD_MEMORY_LIMIT": "16Gi",
		"RUNTIME_SLOT_CPU_REQUEST": "100m", "RUNTIME_SLOT_CPU_LIMIT": "1",
		"RUNTIME_SLOT_MEMORY_REQUEST": "1Gi", "RUNTIME_SLOT_MEMORY_LIMIT": "4Gi",
		"RUNTIME_POD_USER_NAMESPACES": "true",
	} {
		t.Setenv(key, value)
	}
}

func TestElasticRuntimePodBudgetFromEnvironment(t *testing.T) {
	elasticRuntimeEnvironment(t)
	specs, err := elasticRuntimeSpecs(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("runtime pools=%d", len(specs))
	}
	for _, spec := range specs {
		pod := BuildRuntimeDeployment(spec).Spec.Template.Spec
		if pod.Resources == nil {
			t.Fatal("Pod must carry its own shared resource budget")
		}
		for _, resources := range []corev1.ResourceList{pod.Resources.Requests, pod.Resources.Limits} {
			if resources.Memory().Cmp(resource.MustParse("16Gi")) != 0 || resources.Cpu().Cmp(resource.MustParse("4")) != 0 {
				t.Fatal("Pod budget not propagated")
			}
		}
		if pod.HostUsers == nil || *pod.HostUsers {
			t.Fatal("modern sandbox needs an explicit Pod user namespace")
		}
		if len(pod.Containers) != 9 {
			t.Fatal("eight slots and coordinator required")
		}
		for _, slot := range pod.Containers[1:] {
			if slot.Resources.Requests.Memory().Cmp(resource.MustParse("1Gi")) != 0 || slot.Resources.Limits.Memory().Cmp(resource.MustParse("4Gi")) != 0 {
				t.Fatal("slot burst range not propagated")
			}
		}
	}
}

func TestElasticRuntimeRejectsImpossibleBudgets(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"aggregate memory reservation", "RUNTIME_POD_MEMORY_REQUEST", "8Gi"},
		{"aggregate CPU reservation", "RUNTIME_POD_CPU_REQUEST", "500m"},
		{"request above limit", "RUNTIME_POD_MEMORY_LIMIT", "8Gi"},
		{"slot above parent", "RUNTIME_SLOT_MEMORY_LIMIT", "17Gi"},
		{"invalid quantity", "RUNTIME_POD_MEMORY_LIMIT", "invalid"},
		{"incomplete Pod budget", "RUNTIME_POD_CPU_LIMIT", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			elasticRuntimeEnvironment(t)
			t.Setenv(tc.key, tc.value)
			if _, err := elasticRuntimeSpecs(t); err == nil {
				t.Fatal("invalid shared Pod budget accepted")
			}
		})
	}
}

func TestElasticRuntimeRefusesOldServerBeforeMutation(t *testing.T) {
	elasticRuntimeEnvironment(t)
	specs, err := elasticRuntimeSpecs(t)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset()
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.20.5"}
	err = NewRuntimeDeploymentService(client).Ensure(context.Background(), specs[0])
	if err == nil || !strings.Contains(err.Error(), "1.34") {
		t.Fatalf("want explicit PodLevelResources version rejection, got %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" || action.GetVerb() == "update" || action.GetVerb() == "patch" {
			t.Fatal("unsupported server was mutated")
		}
	}
}

func TestLegacySlotPodOmitsModernResourceFields(t *testing.T) {
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "legacy", RuntimeType: "opencode", Image: "test", SlotCount: 8})
	if d.Spec.Template.Spec.Resources != nil || d.Spec.Template.Spec.HostUsers != nil {
		t.Fatal("legacy deployment changed")
	}
}

func TestElasticBootstrapRefusesOldServerBeforeAnyWrite(t *testing.T) {
	elasticRuntimeEnvironment(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Runtime.AgentControlToken = "control"
	cfg.Runtime.AgentReportToken = "report"
	client := fake.NewSimpleClientset()
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.20.5"}
	err = EnsureRuntimeSlotPools(context.Background(), client, cfg.Runtime)
	if err == nil || !strings.Contains(err.Error(), "1.34") {
		t.Fatalf("missing early version rejection: %v", err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" || a.GetVerb() == "update" || a.GetVerb() == "patch" {
			t.Fatal("unsupported bootstrap mutated cluster")
		}
	}
}

func TestElasticBootstrapRejectsServingPoolResourceDrift(t *testing.T) {
	for _, mode := range []string{"enable", "budget", "userns", "slot"} {
		t.Run(mode, func(t *testing.T) {
			elasticRuntimeEnvironment(t)
			loaded, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			cfg := loaded.Runtime
			cfg.Namespace = "tenant"
			cfg.WorkspacePVCClaimName = "workspace"
			cfg.AgentControlToken = "control"
			cfg.AgentReportToken = "report"
			specs, err := RuntimeSlotDeploymentSpecs(cfg, "clawmanager-lite-agent")
			if err != nil {
				t.Fatal(err)
			}
			sc := "longhorn"
			objects := []runtime.Object{
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "one"}},
				&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: sc}, Provisioner: "driver.longhorn.io"},
				&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "tenant"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "volume", StorageClassName: &sc}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}},
				&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "volume"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: sc, ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspace"}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "driver.longhorn.io"}}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}},
			}
			for _, spec := range specs {
				if mode == "enable" {
					spec.PodResources = nil
					spec.PodUserNamespaces = false
				}
				objects = append(objects, BuildRuntimeDeployment(spec))
			}
			switch mode {
			case "budget":
				cfg.PodResources.MemoryRequest = "20Gi"
				cfg.PodResources.MemoryLimit = "20Gi"
			case "userns":
				cfg.PodUserNamespaces = false
			case "slot":
				cfg.SlotResources.MemoryLimit = "3Gi"
			}
			client := fake.NewSimpleClientset(objects...)
			client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.35.0"}
			err = EnsureRuntimeSlotPools(context.Background(), client, cfg)
			if err == nil || !strings.Contains(err.Error(), "coordinated rollout") {
				t.Fatalf("resource drift silently accepted: %v", err)
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" && a.GetResource().Resource == "deployments" {
					t.Fatal("serving pool changed during bootstrap")
				}
			}
		})
	}
}
