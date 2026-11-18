package systembackup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func realKubernetesCaptureSources(t *testing.T) CaptureSources {
	t.Helper()
	path := os.Getenv("TEST_SYSTEM_BACKUP_KUBE_AUTH_FILE")
	if path == "" {
		t.Skip("requires disposable Kubernetes fixture and GET-only service accounts")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("fixture credential unavailable")
	}
	defer clear(raw)
	var c struct {
		Namespace     string                `json:"namespace"`
		NamespaceUID  string                `json:"namespace_uid"`
		CA            string                `json:"ca"`
		ResourceToken string                `json:"resource_token"`
		SecretToken   string                `json:"secret_token"`
		Deployments   []KubernetesSelection `json:"deployments"`
		Secrets       []KubernetesSelection `json:"secrets"`
	}
	if e = json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &c); e != nil {
		t.Fatal("fixture credential invalid")
	}
	client := func(token string) dynamic.Interface {
		d, e := dynamic.NewForConfig(&rest.Config{Host: fixtureKubernetesServer(), BearerToken: token, Timeout: 10 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: []byte(c.CA), ServerName: "10.130.14.23"}})
		if e != nil {
			t.Fatal("fixture client invalid")
		}
		return d
	}
	return CaptureSources{Namespace: c.Namespace, NamespaceUID: c.NamespaceUID, DeploymentClient: client(c.ResourceToken), SecretClient: client(c.SecretToken), Deployments: c.Deployments, Secrets: c.Secrets}
}
func TestRealKubernetesCaptureAccountsAreReadOnlyAndSeparated(t *testing.T) {
	s := realKubernetesCaptureSources(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	budget := ObjectBudget{8 << 20, 100}
	for _, part := range []string{"deployment_resources", "secret_bundle"} {
		client, selection := s.DeploymentClient, s.Deployments
		if part == "secret_bundle" {
			client, selection = s.SecretClient, s.Secrets
		}
		var archive bytes.Buffer
		if e := CaptureKubernetesResources(ctx, client, &archive, s.Namespace, s.NamespaceUID, part, selection, budget); e != nil {
			t.Fatal(part, e)
		}
		if e := ReadKubernetesHistory(ctx, bytes.NewReader(archive.Bytes()), part, budget, nil); e != nil {
			t.Fatal(e)
		}
	}
	secrets := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	if _, e := s.DeploymentClient.Resource(secrets).Namespace(s.Namespace).Get(ctx, "backup-fixture-auth", metav1.GetOptions{}); !apierrors.IsForbidden(e) {
		t.Fatal("ordinary capture can read Secret or failed for another reason")
	}
	if _, e := s.SecretClient.Resource(pods).Namespace(s.Namespace).Get(ctx, "backup-mysql", metav1.GetOptions{}); !apierrors.IsForbidden(e) {
		t.Fatal("secret capture can read ordinary source resources")
	}
	if _, e := s.DeploymentClient.Resource(pods).Namespace(s.Namespace).Patch(ctx, "backup-mysql", types.MergePatchType, []byte(`{"metadata":{"labels":{"backup-permission-probe":"true"}}}`), metav1.PatchOptions{DryRun: []string{metav1.DryRunAll}}); !apierrors.IsForbidden(e) {
		t.Fatal("capture account can mutate resources")
	}
	if _, e := s.DeploymentClient.Resource(pods).Namespace("default").Get(ctx, "backup-mysql", metav1.GetOptions{}); !apierrors.IsForbidden(e) {
		t.Fatal("capture account crossed namespace")
	}
}
