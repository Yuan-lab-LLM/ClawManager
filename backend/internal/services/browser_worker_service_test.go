package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	originalSetFileOwnership := setFileOwnership
	setFileOwnership = func(string, int, int) error { return nil }
	t.Cleanup(func() { setFileOwnership = originalSetFileOwnership })

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
	service := &BrowserWorkerService{client: &cmk8s.Client{Namespace: "clawmanager"}}
	instance := &models.Instance{ID: 42, UserID: 7, WorkspacePath: &workspace}
	if err := service.patchOpenClawConfig(instance, true, strings.Repeat("a", 64)); err != nil {
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
	if err := service.patchOpenClawConfig(instance, false, ""); err != nil {
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

type memoryBrowserWorkerRepo struct{ config *models.InstanceBrowserWorker }

func (r *memoryBrowserWorkerRepo) Get(int) (*models.InstanceBrowserWorker, error) {
	if r.config == nil {
		return nil, nil
	}
	copy := *r.config
	return &copy, nil
}
func (r *memoryBrowserWorkerRepo) Upsert(config *models.InstanceBrowserWorker) error {
	copy := *config
	if r.config != nil {
		copy.Generation = r.config.Generation + 1
	}
	r.config = &copy
	return nil
}
func (r *memoryBrowserWorkerRepo) ListDesired(int) ([]models.InstanceBrowserWorker, error) {
	if r.config == nil {
		return nil, nil
	}
	return []models.InstanceBrowserWorker{*r.config}, nil
}
func (r *memoryBrowserWorkerRepo) UpdateObserved(_ int, generation int, status string, lastError *string) error {
	if r.config.Generation == generation {
		r.config.ObservedGeneration = generation
		r.config.Status = status
		r.config.LastError = lastError
	}
	return nil
}
func (r *memoryBrowserWorkerRepo) UpdateStatus(_ int, generation int, status string, lastError *string) error {
	if r.config.Generation == generation {
		r.config.Status = status
		r.config.LastError = lastError
	}
	return nil
}

func browserWorkerTestService(t *testing.T) (*BrowserWorkerService, *models.Instance, *memoryBrowserWorkerRepo) {
	t.Helper()
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_IMAGE", "example.invalid/browser:test")
	t.Setenv("CLAWMANAGER_CDP_PROXY_IMAGE", "example.invalid/relay:test")
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "")
	t.Setenv("POD_NAMESPACE", "")
	t.Setenv("RUNTIME_NAMESPACE", "")
	original := setFileOwnership
	setFileOwnership = func(string, int, int) error { return nil }
	t.Cleanup(func() { setFileOwnership = original })
	workspace := t.TempDir()
	instance := &models.Instance{ID: 42, UserID: 7, Type: RuntimeTypeOpenClaw, InstanceMode: InstanceModeLite, Status: "running", WorkspacePath: &workspace}
	instances := newV2LifecycleInstanceRepo()
	instances.byID[instance.ID] = instance
	repo := &memoryBrowserWorkerRepo{}
	service := NewBrowserWorkerService(repo, instances, &cmk8s.Client{Clientset: fake.NewSimpleClientset(), Namespace: "custom", WorkspacePVCClaimName: "workspaces"})
	return service, instance, repo
}

func writeBrowserWorkerTestConfig(t *testing.T, instance *models.Instance) string {
	t.Helper()
	path := filepath.Join(*instance.WorkspacePath, "home", ".openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"channels":{"preserve":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBrowserWorkerMissingConfigRetriesWithoutAcknowledgingGeneration(t *testing.T) {
	s, instance, repo := browserWorkerTestService(t)
	config, err := s.Update(context.Background(), instance, BrowserWorkerUpdate{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if config.Status != models.BrowserWorkerStatusError || config.ObservedGeneration != 0 {
		t.Fatalf("failed config was acknowledged: %#v", config)
	}
	path := writeBrowserWorkerTestConfig(t, instance)
	s.ReconcileAll(context.Background())
	if repo.config.ObservedGeneration != repo.config.Generation || repo.config.LastError != nil {
		t.Fatalf("retry did not converge: %#v", repo.config)
	}
	raw, _ := os.ReadFile(path)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	profile := root["browser"].(map[string]any)["profiles"].(map[string]any)["interactive"].(map[string]any)
	cdpURL, err := url.Parse(profile["cdpUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.AuthToken(context.Background(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := cdpURL.User.Password()
	if cdpURL.Host != "clawbrowser-42.custom-system.svc.cluster.local:9222" || cdpURL.User.Username() != "openclaw" || password != token {
		t.Fatal("incorrect authenticated CDP endpoint")
	}
	if _, ok := root["channels"]; !ok {
		t.Fatal("unrelated config was removed")
	}
}

func TestBrowserWorkerCreatingInstanceWaitsForRuntime(t *testing.T) {
	s, instance, repo := browserWorkerTestService(t)
	instance.Status = "creating"
	config, err := s.Update(context.Background(), instance, BrowserWorkerUpdate{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if config.Status != models.BrowserWorkerStatusProvisioning || config.ObservedGeneration != 0 {
		t.Fatalf("creation should remain pending: %#v", config)
	}
	instance.Status = "running"
	writeBrowserWorkerTestConfig(t, instance)
	s.ReconcileAll(context.Background())
	if repo.config.ObservedGeneration != 1 {
		t.Fatal("running instance did not apply config")
	}
}

func TestBrowserWorkerCredentialsArePersistentAndInstanceScoped(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	a, err := s.ensureAuthSecret(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.ensureAuthSecret(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	other := *instance
	other.ID++
	c, err := s.ensureAuthSecret(context.Background(), &other)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || a != b || a == c {
		t.Fatal("credentials must be strong, stable and unique per instance")
	}
}

func TestBrowserWorkerUsesAuthenticatedRelayAndNetworkPolicy(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	config := browserWorkerDefaults(instance.ID)
	config.Enabled = true
	if err := s.ensureResources(context.Background(), instance, config); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ns := s.Namespace()
	deployment, err := s.client.Clientset.AppsV1().Deployments(ns).Get(ctx, s.name(instance.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	browser, relay := deployment.Spec.Template.Spec.Containers[0], deployment.Spec.Template.Spec.Containers[1]
	for _, name := range []string{"WEB_LOCALHOST_ONLY", "VNC_LOCALHOST_ONLY"} {
		found := false
		for _, env := range browser.Env {
			if env.Name == name && env.Value == "1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not loopback-only", name)
		}
	}
	if relay.Ports[1].Name != "web" || relay.Ports[1].ContainerPort != 5801 {
		t.Fatal("GUI must terminate on authenticated relay, not raw Chromium")
	}
	found := false
	for _, env := range relay.Env {
		if env.Name == "BROWSER_WORKER_AUTH_TOKEN" && env.Value == "" && env.ValueFrom.SecretKeyRef.Name == s.name(instance.ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("relay credential is not a Secret reference")
	}
	policy, err := s.client.Clientset.NetworkingV1().NetworkPolicies(ns).Get(ctx, s.name(instance.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Spec.Ingress) != 2 || policy.Spec.Ingress[0].Ports[0].Port.IntVal != 5801 || policy.Spec.Ingress[1].Ports[0].Port.IntVal != 9222 {
		t.Fatal("policy must allow only authenticated GUI and CDP ports")
	}
	if deployment.Spec.Template.Spec.AutomountServiceAccountToken == nil || *deployment.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("browser worker must not mount Kubernetes API credentials")
	}
	if policy.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "custom-system" {
		t.Fatal("backend source namespace not restricted")
	}
}

func TestBrowserWorkerUnifiedDeleteCleansResourcesAndRetriesFailure(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	writeBrowserWorkerTestConfig(t, instance)
	if _, err := s.Update(context.Background(), instance, BrowserWorkerUpdate{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	instance.RuntimeType = RuntimeBackendGateway
	instances := s.instances.(*v2LifecycleInstanceRepo)
	lifecycle := &instanceService{instanceRepo: instances, resourceCleanup: func(context.Context, int) error { return errors.New("temporary component cleanup failure") }}
	if err := lifecycle.Delete(instance.ID); err == nil {
		t.Fatal("cleanup failure must keep instance retryable")
	}
	if instances.byID[instance.ID] == nil || instances.byID[instance.ID].Status != "deleting" {
		t.Fatal("failed cleanup lost deletion intent")
	}
	// The reconciler must never recreate components for a deleting instance.
	s.ReconcileAll(context.Background())
	lifecycle.resourceCleanup = s.DeleteForInstance
	if err := lifecycle.Delete(instance.ID); err != nil {
		t.Fatal(err)
	}
	if instances.byID[instance.ID] != nil {
		t.Fatal("instance record was not deleted")
	}
	ctx := context.Background()
	ns := s.Namespace()
	name := s.name(instance.ID)
	if _, err := s.client.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("deployment leaked")
	}
	if _, err := s.client.Clientset.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("service leaked")
	}
	if _, err := s.client.Clientset.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("secret leaked")
	}
	if _, err := s.client.Clientset.NetworkingV1().NetworkPolicies(ns).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("policy leaked")
	}
}

func TestBrowserWorkerOrphanCleanupAndDeletingInstance(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	if _, err := s.ensureAuthSecret(context.Background(), instance); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureResources(context.Background(), instance, browserWorkerDefaults(instance.ID)); err != nil {
		t.Fatal(err)
	}
	delete(s.instances.(*v2LifecycleInstanceRepo).byID, instance.ID)
	if err := s.cleanupOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.Clientset.AppsV1().Deployments(s.Namespace()).Get(context.Background(), s.name(instance.ID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("orphan was not reclaimed")
	}
}

func TestBrowserWorkerRequiresExplicitImagesAndValidCreateOptions(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	if err := s.ValidateCreate(instance, BrowserWorkerUpdate{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_IMAGE", "")
	if err := s.ValidateCreate(instance, BrowserWorkerUpdate{Enabled: true}); err == nil {
		t.Fatal("unpublished default must not be used")
	}
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_IMAGE", "example.invalid/browser:test")
	for _, update := range []BrowserWorkerUpdate{{DisplayWidth: 100}, {DisplayHeight: 2000}, {ResourceProfile: "invalid"}} {
		if err := s.ValidateCreate(instance, update); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
}

func TestBrowserWorkerCreateValidationUsesLifecycleModePrecedence(t *testing.T) {
	s, _, _ := browserWorkerTestService(t)
	for _, request := range []CreateInstanceRequest{
		{Type: RuntimeTypeOpenClaw, Mode: InstanceModePro, InstanceMode: InstanceModeLite},
		{Type: RuntimeTypeOpenClaw, RuntimeType: RuntimeBackendDesktop},
	} {
		if err := s.ValidateCreateRequest(request, BrowserWorkerUpdate{Enabled: true}); err == nil {
			t.Fatal("Pro request passed Lite component validation")
		}
	}
}

func TestBrowserWorkerCleanupWithoutFeatureDoesNotRequireKubernetes(t *testing.T) {
	s := NewBrowserWorkerService(&memoryBrowserWorkerRepo{}, nil, nil)
	if err := s.DeleteForInstance(context.Background(), 42); err != nil {
		t.Fatal("disabled component blocked normal instance deletion")
	}
	client := fake.NewSimpleClientset()
	s.client = &cmk8s.Client{Clientset: client, Namespace: "custom"}
	if err := s.DeleteForInstance(context.Background(), 42); err != nil || len(client.Actions()) != 0 {
		t.Fatal("unconfigured component introduced a Kubernetes API dependency")
	}
}

func TestBrowserWorkerStaleReconciliationCannotResurrectDeletedInstance(t *testing.T) {
	s, instance, repo := browserWorkerTestService(t)
	writeBrowserWorkerTestConfig(t, instance)
	config, err := s.Update(context.Background(), instance, BrowserWorkerUpdate{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	stale := *instance
	instance.Status = "deleting"
	if err := s.Reconcile(context.Background(), &stale, config); err != nil {
		t.Fatal(err)
	}
	delete(s.instances.(*v2LifecycleInstanceRepo).byID, instance.ID)
	if err := s.Reconcile(context.Background(), &stale, repo.config); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.Clientset.AppsV1().Deployments(s.Namespace()).Get(context.Background(), s.name(instance.ID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("stale controller recreated a deleted browser")
	}
}

func TestBrowserWorkerOrphanCleanupWorksWithoutSecret(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	if err := s.ensureResources(context.Background(), instance, browserWorkerDefaults(instance.ID)); err != nil {
		t.Fatal(err)
	}
	delete(s.instances.(*v2LifecycleInstanceRepo).byID, instance.ID)
	if err := s.cleanupOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.Clientset.CoreV1().Services(s.Namespace()).Get(context.Background(), s.name(instance.ID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("secret-less orphan service leaked")
	}
}

func TestBrowserWorkerCustomWorkerNamespacePolicyUsesActualSourceNamespaces(t *testing.T) {
	s, instance, _ := browserWorkerTestService(t)
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "browsers")
	t.Setenv("POD_NAMESPACE", "control")
	t.Setenv("RUNTIME_NAMESPACE", "runtimes")
	if err := s.ensureResources(context.Background(), instance, browserWorkerDefaults(instance.ID)); err != nil {
		t.Fatal(err)
	}
	policy, err := s.client.Clientset.NetworkingV1().NetworkPolicies("browsers").Get(context.Background(), s.name(instance.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for index, expected := range []string{"control", "runtimes"} {
		peer := policy.Spec.Ingress[index].From[0]
		if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != expected || peer.PodSelector == nil {
			t.Fatal("cross-namespace rule is missing namespace AND pod selector")
		}
	}
}

func TestBrowserWorkerPatchedCredentialsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions are verified by Linux CI")
	}
	s, instance, _ := browserWorkerTestService(t)
	path := writeBrowserWorkerTestConfig(t, instance)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.patchOpenClawConfig(instance, true, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("CDP credentials are readable by other instance UIDs")
	}
}
