package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBrowserWorkerSupportsOnlyOpenClawLite(t *testing.T) {
	if !browserWorkerSupportedInstance(&models.Instance{Type: RuntimeTypeOpenClaw, InstanceMode: InstanceModeLite}) {
		t.Fatal("OpenClaw Lite should support Browser Worker")
	}
	for _, instance := range []*models.Instance{
		nil,
		{Type: RuntimeTypeOpenClaw, InstanceMode: InstanceModePro},
		{Type: RuntimeTypeHermes, InstanceMode: InstanceModeLite},
	} {
		if browserWorkerSupportedInstance(instance) {
			t.Fatalf("unexpected supported instance: %#v", instance)
		}
	}
}

func TestBrowserWorkerCreatesIsolatedKubernetesResources(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	service := &BrowserWorkerService{client: &cmk8s.Client{
		Clientset: clientset, Namespace: "clawmanager",
		WorkspacePVCClaimName: "clawmanager-workspaces",
	}}
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running"}
	config := browserWorkerDefaults(instance.ID)
	config.Enabled = true

	if err := service.ensureResources(context.Background(), instance, config); err != nil {
		t.Fatalf("ensure resources: %v", err)
	}
	deployment, err := clientset.AppsV1().Deployments("clawmanager-system").Get(context.Background(), "clawbrowser-42", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("replicas = %v, want 1", deployment.Spec.Replicas)
	}
	if got := deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "clawmanager-workspaces" {
		t.Fatalf("workspace PVC = %q", got)
	}
	if got := deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].SubPath; got != "openclaw/user-7/instance-42/home/.openclaw/browser-worker" {
		t.Fatalf("profile subpath = %q", got)
	}
	if len(deployment.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("init containers = %d, want 1", len(deployment.Spec.Template.Spec.InitContainers))
	}
	prepare := deployment.Spec.Template.Spec.InitContainers[0]
	if prepare.Name != "prepare-profile" || len(prepare.VolumeMounts) != 1 || prepare.VolumeMounts[0].MountPath != "/workspace" {
		t.Fatalf("unexpected profile initializer: %#v", prepare)
	}
	if _, err := clientset.CoreV1().Services("clawmanager-system").Get(context.Background(), "clawbrowser-42", metav1.GetOptions{}); err != nil {
		t.Fatalf("get service: %v", err)
	}
}

func TestBrowserWorkerConfigPatchIsReversible(t *testing.T) {
	workspace := t.TempDir()
	configDir := filepath.Join(workspace, "home", ".openclaw")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "openclaw.json")
	original := []byte("{\n  \"browser\": {\"enabled\": false},\n  \"channels\": {\"keep\": true}\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &BrowserWorkerService{client: &cmk8s.Client{Namespace: "clawmanager-system"}}
	instance := &models.Instance{ID: 42, UserID: 7, WorkspacePath: &workspace}
	if err := service.patchOpenClawConfig(instance, true); err != nil {
		t.Fatalf("enable patch: %v", err)
	}
	var enabled map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &enabled); err != nil {
		t.Fatal(err)
	}
	if _, ok := enabled["channels"]; !ok {
		t.Fatal("unrelated config was removed")
	}
	if err := service.patchOpenClawConfig(instance, false); err != nil {
		t.Fatalf("disable patch: %v", err)
	}
	var restored map[string]any
	raw, _ = os.ReadFile(path)
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	browser := restored["browser"].(map[string]any)
	if browser["enabled"] != false {
		t.Fatalf("browser config was not restored: %#v", browser)
	}
}
