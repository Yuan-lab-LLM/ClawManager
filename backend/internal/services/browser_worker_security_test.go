package services

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	cmk8s "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBrowserWorkerCustomArgsSurviveNativeShellEval(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("native Chromium image requires a POSIX shell")
	}
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "protected"}[protected], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			kube := fake.NewSimpleClientset()
			worker := &BrowserWorkerService{client: &cmk8s.Client{Clientset: kube, WorkspacePVCClaimName: "workspaces"}}
			instance := &models.Instance{ID: 42, UserID: 7, RuntimeGeneration: 2, Status: "running"}
			want := []string{"--remote-debugging-address=127.0.0.1"}
			if protected {
				instance.RuntimeSecurityProfile = RuntimeSecurityProfile
				if err := ConfigureBrowserRuntimeSecurity(worker, func(context.Context, int) (runtimeauth.Identity, error) {
					return runtimeauth.Identity{InstanceID: 42, UserID: 7, Generation: 2, PodUID: "runtime-pod"}, nil
				}); err != nil {
					t.Fatal(err)
				}
				want = append(want, "--proxy-server=http://127.0.0.1:31280", "--proxy-bypass-list=<-loopback>")
			}
			if err := worker.ensureResources(ctx, instance, browserWorkerDefaults(42)); err != nil {
				t.Fatal(err)
			}
			deployment, err := kube.AppsV1().Deployments(worker.namespace()).Get(ctx, worker.name(42), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			value, count := "", 0
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "CHROMIUM_CUSTOM_ARGS" {
					value, count = env.Value, count+1
				}
			}
			if count != 1 {
				t.Fatal("deployment must set exactly one Chromium custom-argument value")
			}
			// Exact custom-argument consumer from pinned jlesage app/params.
			// Each output line is one parsed word consumed by Chromium's launcher.
			command := exec.CommandContext(ctx, shell, "-c", `set -e
if [ -n "${CHROMIUM_CUSTOM_ARGS:-}" ]; then
    eval 'for word in '$CHROMIUM_CUSTOM_ARGS'; do echo "$word"; done'
fi`)
			command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "CHROMIUM_CUSTOM_ARGS=" + value}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("native custom-argument shell eval failed: %v: %s", err, output)
			}
			parts := bytes.Split(bytes.TrimSuffix(output, []byte{'\n'}), []byte{'\n'})
			got := make([]string, len(parts))
			for index, part := range parts {
				got[index] = string(part)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("native Chromium argv = %q; want %q", got, want)
			}
		})
	}
}

func TestProtectedBrowserConfigAndBackupsContainOnlyLocalEndpoint(t *testing.T) {
	original := setFileOwnership
	setFileOwnership = func(string, int, int) error { return nil }
	t.Cleanup(func() { setFileOwnership = original })
	workspace := t.TempDir()
	directory := filepath.Join(workspace, "home", ".openclaw")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "openclaw.json")
	old := `{"user_key":"preserve-third-party","browser":{"profiles":{"interactive":{"cdpUrl":"http://openclaw:old-worker-sentinel@clawbrowser-42.clawmanager-system.svc.cluster.local:9222"}}}}`
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".bak-browser-worker", ".bak", ".browser-worker-state"} {
		if err := os.WriteFile(path+suffix, []byte(old), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := &BrowserWorkerService{client: &cmk8s.Client{Namespace: "clawmanager"}}
	instance := &models.Instance{ID: 42, UserID: 7, WorkspacePath: &workspace, RuntimeSecurityProfile: RuntimeSecurityProfile}
	if err := service.patchOpenClawConfig(instance, true, strings.Repeat("new-worker-sentinel", 3)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ".bak-browser-worker", ".bak", ".browser-worker-state"} {
		raw, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "worker-sentinel") {
			t.Fatalf("managed browser credential remains in generated file: %s", suffix)
		}
	}
	raw, _ := os.ReadFile(path)
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("invalidnativeconfig")
	}
	if config["user_key"] != "preserve-third-party" {
		t.Fatal("unrelated user key removed")
	}
	if !strings.Contains(string(raw), "http://clawmanager-local-services.invalid:31282") {
		t.Fatal("nativebrowsernotusingfixedlocalentry")
	}
}

func TestProtectedBrowserNetworkBoundaryAndProfileMount(t *testing.T) {
	ctx := context.Background()
	kube := fake.NewSimpleClientset()
	worker := &BrowserWorkerService{client: &cmk8s.Client{Clientset: kube, WorkspacePVCClaimName: "workspaces"}}
	instance := &models.Instance{ID: 42, UserID: 7, RuntimeGeneration: 2, Status: "running", RuntimeSecurityProfile: RuntimeSecurityProfile}
	config := browserWorkerDefaults(42)
	if err := worker.ensureResources(ctx, instance, config); err == nil {
		t.Fatal("protected browser started without network authority")
	}
	if err := ConfigureBrowserRuntimeSecurity(worker, func(context.Context, int) (runtimeauth.Identity, error) {
		return runtimeauth.Identity{InstanceID: 42, UserID: 7, Generation: 2, PodUID: "runtime-pod"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.ensureResources(ctx, instance, config); err != nil {
		t.Fatal(err)
	}
	policy, err := kube.NetworkingV1().NetworkPolicies(worker.namespace()).Get(ctx, worker.name(42), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Spec.PolicyTypes) != 2 || len(policy.Spec.Ingress) != 1 || len(policy.Spec.Ingress[0].Ports) != 2 {
		t.Fatal("worker not restricted to management ingress")
	}
	if len(policy.Spec.Egress) != 2 || len(policy.Spec.Egress[0].Ports) != 1 || policy.Spec.Egress[0].Ports[0].Port.IntVal != 9004 || policy.Spec.Egress[0].To[0].PodSelector.MatchLabels["app"] != "clawmanager-app" || policy.Spec.Egress[0].To[0].NamespaceSelector == nil {
		t.Fatal("worker egress grants more than the authenticated backend proxy")
	}
	for _, rule := range policy.Spec.Egress {
		for _, peer := range rule.To {
			if peer.IPBlock != nil {
				t.Fatal("worker pages retain direct egress beyond management authorization")
			}
		}
	}
	for _, rule := range policy.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.PodSelector.MatchLabels["app"] != "clawmanager-app" {
				t.Fatal("runtime can reach worker directly")
			}
		}
	}
	for _, value := range []string{"10.244.1.2", "169.254.169.254", "10.130.81.54", "100.100.100.200", "8.8.8.8", "fd00::1", "::1"} {
		address := netip.MustParseAddr(value)
		for _, rule := range policy.Spec.Egress {
			for _, peer := range rule.To {
				if peer.IPBlock == nil {
					continue
				}
				prefix := netip.MustParsePrefix(peer.IPBlock.CIDR)
				if !prefix.Contains(address) {
					continue
				}
				denied := false
				for _, except := range peer.IPBlock.Except {
					if netip.MustParsePrefix(except).Contains(address) {
						denied = true
					}
				}
				if !denied {
					t.Fatalf("protected browser can reach denied address %s", value)
				}
			}
		}
	}
	deployment, err := kube.AppsV1().Deployments(worker.namespace()).Get(ctx, worker.name(42), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	prepare := deployment.Spec.Template.Spec.InitContainers[0]
	if prepare.VolumeMounts[0].SubPath != "openclaw/user-7/instance-42" || strings.Contains(prepare.Args[0], "/workspace/openclaw/user-") {
		t.Fatal("initializer sees other instances' workspace")
	}
}

func TestProtectedBrowserScrubsURLsInArrays(t *testing.T) {
	value := map[string]any{"history": []any{"http://openclaw:sentinel@worker:9222", "https://user:own-key@third-party"}}
	scrubProtectedBrowserURLs(value, "worker:9222")
	encoded, _ := json.Marshal(value)
	if strings.Contains(string(encoded), "sentinel") || !strings.Contains(string(encoded), "own-key") {
		t.Fatal("array credential boundary")
	}
}

func TestProtectedBrowserRejectsFileEscapes(t *testing.T) {
	for _, hardlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "symlink", true: "hardlink"}[hardlink], func(t *testing.T) {
			workspace := t.TempDir()
			directory := filepath.Join(workspace, "home", ".openclaw")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte(`{"outside":"unchanged"}`), 0600); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if hardlink {
				link = os.Link
			}
			if err := link(outside, filepath.Join(directory, "openclaw.json")); err != nil {
				t.Fatal(err)
			}
			if err := patchProtectedBrowserConfig(workspace, "worker:9222", true); err == nil {
				t.Fatal("escaped file accepted")
			}
			raw, _ := os.ReadFile(outside)
			if string(raw) != `{"outside":"unchanged"}` {
				t.Fatal("outside file modified")
			}
		})
	}
}
