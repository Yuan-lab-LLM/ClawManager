package systembackup

import (
	"bytes"
	"context"
	"encoding/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"testing"
)

func TestKubernetesCaptureSeparatesSecretsAndNeverApplies(t *testing.T) {
	ctx := context.Background()
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "team-token", "namespace": "source", "uid": "secret-uid"}, "data": map[string]any{"token": "cHJpdmF0ZQ=="}}}
	deployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "team-runtime", "namespace": "source", "uid": "deployment-uid"}, "spec": map[string]any{"replicas": int64(1)}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), secret, deployment)
	for _, tc := range []struct {
		part      string
		selection KubernetesSelection
	}{{"secret_bundle", KubernetesSelection{"", "v1", "secrets", "team-token", "secret-uid"}}, {"deployment_resources", KubernetesSelection{"apps", "v1", "deployments", "team-runtime", "deployment-uid"}}} {
		var archive bytes.Buffer
		b := ObjectBudget{4 << 20, 10}
		if e := CaptureKubernetesResources(ctx, client, &archive, "source", "ns-uid", tc.part, []KubernetesSelection{tc.selection}, b); e != nil {
			t.Fatal(e)
		}
		count := 0
		if e := ReadKubernetesHistory(ctx, bytes.NewReader(archive.Bytes()), tc.part, b, func(s KubernetesSelection, raw json.RawMessage) error {
			count++
			if s != tc.selection || len(raw) == 0 {
				t.Fatal("resource data lost")
			}
			return nil
		}); e != nil || count != 1 {
			t.Fatal("resource history failed", e)
		}
		if e := ReadKubernetesHistory(ctx, bytes.NewReader(archive.Bytes()[:archive.Len()-15]), tc.part, b, nil); e == nil {
			t.Fatal("truncated resource accepted")
		}
		changed := tc.selection
		changed.UID = "replacement"
		if e := CaptureKubernetesResources(ctx, client, &bytes.Buffer{}, "source", "ns-uid", tc.part, []KubernetesSelection{changed}, b); e == nil {
			t.Fatal("UID drift accepted")
		}
	}
	if e := CaptureKubernetesResources(ctx, client, &bytes.Buffer{}, "source", "ns-uid", "deployment_resources", []KubernetesSelection{{"", "v1", "secrets", "team-token", "secret-uid"}}, ObjectBudget{4 << 20, 10}); e == nil {
		t.Fatal("secret leaked into ordinary capture")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("capture mutated Kubernetes", action.GetVerb())
		}
	}
}
