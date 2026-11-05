package services

import (
	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNFSBrowserUsesInstanceCredentialsAndNoIDMappedMount(t *testing.T) {
	instance := &models.Instance{ID: 42, UserID: 7}
	deployment := desiredBrowserWorkerDeployment("browser", "lab", browserWorkerLabels(instance), "openclaw/user-7/instance-42/home/.openclaw/browser-worker", "workspace", 1, browserWorkerDefaults(42))
	if err := configureNFSBrowserWorker(deployment, instance); err != nil {
		t.Fatal(err)
	}
	pod := deployment.Spec.Template.Spec
	if pod.HostUsers != nil && !*pod.HostUsers {
		t.Fatal("NFS idmapped mount")
	}
	prep := pod.InitContainers[0]
	if prep.SecurityContext.RunAsUser == nil || *prep.SecurityContext.RunAsUser != 200042 || *prep.SecurityContext.RunAsGroup != 200042 {
		t.Fatal("profile preparation must use stable instance identity")
	}
	if strings.Contains(strings.Join(prep.Args, " "), "chown") || strings.Contains(strings.Join(prep.Args, " "), "chmod") {
		t.Fatal("existing data ownership must be preserved")
	}
	if prep.VolumeMounts[0].SubPath != "browsers/user-7/instance-42/profile" {
		t.Fatal("preparer can see unrelated instances")
	}
	if pod.Containers[0].VolumeMounts[0].SubPath != "browsers/user-7/instance-42/profile" || pod.Containers[0].VolumeMounts[0].MountPath != "/config/chromium" {
		t.Fatal("browser profile must avoid traversal through private native home")
	}
	for name, value := range map[string]string{"USER_ID": "200042", "GROUP_ID": "200042", "TAKE_CONFIG_OWNERSHIP": "0"} {
		found := false
		for _, env := range pod.Containers[0].Env {
			if env.Name == name && env.Value == value {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
	for _, c := range append([]corev1.Container{}, pod.InitContainers...) {
		for _, cap := range c.SecurityContext.Capabilities.Add {
			if cap == "SYS_ADMIN" || cap == "NET_ADMIN" {
				t.Fatal("outer admin capability")
			}
		}
	}
}

func TestNFSBrowserConfigWriteIsFencedAndUsesInstanceIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("trusted credential helper requires root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-nfs-browser-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 7, 42)
	directory := filepath.Join(workspace, "home", ".openclaw")
	if err = os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	os.Chown(directory, 200042, 200042)
	p := filepath.Join(directory, "openclaw.json")
	os.WriteFile(p, []byte(`{"browser":{"enabled":false}}`), 0600)
	os.Chown(p, 200042, 200042)
	instance := &models.Instance{ID: 42, UserID: 7, RuntimeGeneration: 3, RuntimeSecurityProfile: RuntimeSecurityProfile, WorkspacePath: &workspace}
	writes := &workspaceWriteFake{}
	worker := &BrowserWorkerService{client: &cmk8s.Client{WorkspaceRoot: base, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	ConfigureBrowserWorkspaceNFSIO(worker, "nfs-rootless-v1", base, writes, nfsTeamGuard{})
	if err = worker.patchOpenClawConfig(instance, true, ""); err != nil {
		t.Fatal(err)
	}
	if writes.begins != 1 || writes.completes != 1 || writes.active {
		t.Fatalf("browser config bypassed lifetime fence: %+v", writes)
	}
	doc, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(doc), protectedBrowserURL) {
		t.Fatalf("config not patched: %s %v", doc, err)
	}
	worker.nfsWorkspace.mutationGuard = nil
	if err = worker.patchOpenClawConfig(instance, false, ""); err == nil {
		t.Fatal("unfenced browser config write permitted")
	}
}

func TestNFSBrowserCreatingDefersBeforeProtectedGeneration(t *testing.T) {
	kube := fake.NewSimpleClientset()
	repo := &memoryBrowserWorkerRepo{config: browserWorkerDefaults(12)}
	repo.config.Enabled = true
	worker := &BrowserWorkerService{client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}, repo: repo}
	instance := &models.Instance{ID: 12, UserID: 3, Status: "creating"}
	if err := worker.Reconcile(context.Background(), instance, repo.config); err != nil {
		t.Fatal(err)
	}
	if len(kube.Actions()) != 0 {
		t.Fatal("unprotected creating instance admitted Kubernetes browser resources")
	}
	instance.Status = "running"
	if err := worker.Reconcile(context.Background(), instance, repo.config); err == nil {
		t.Fatal("running unprotected NFS browser accepted")
	}
}
