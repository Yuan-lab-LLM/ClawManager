package systembackup

import (
	"context"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestHistoryPlanPairsJournalWithFrozenImmutableKeys(t *testing.T) {
	yes := true
	namespace := "fixture"
	key := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-observability-key-v1", Namespace: namespace, UID: "key-uid"}, Immutable: &yes}
	restore := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-history-restore-v1", Namespace: namespace, UID: "restore-uid"}, Immutable: &yes}
	selection := []KubernetesSelection{{Version: "v1", Resource: "secrets", Name: key.Name, UID: string(key.UID)}, {Version: "v1", Resource: "secrets", Name: restore.Name, UID: string(restore.UID)}}
	config, _ := json.Marshal(map[string]any{"source": map[string]any{"namespace": namespace, "namespace_uid": "namespace-uid", "secrets": selection}})
	operation := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-source-config", Namespace: namespace, UID: "config-uid"}, Immutable: &yes, Data: map[string][]byte{"config.json": config}}
	client := fake.NewSimpleClientset(key, restore, operation)
	ordinary := CaptureJobRequest{VerifyJobRequest: VerifyJobRequest{Namespace: namespace, NamespaceUID: "namespace-uid"}, SourceVolumes: []CaptureSourceVolume{{PVCIdentity: PVCIdentity{Name: "journal", UID: "journal-uid"}, ID: "observability"}}}
	secret := CaptureJobRequest{VerifyJobRequest: ordinary.VerifyJobRequest, OperationSecret: operation.Name, OperationSecretUID: string(operation.UID)}
	if err := freezeHistorySecretPair(context.Background(), client, ordinary, &secret); err != nil {
		t.Fatal(err)
	}
	if len(secret.RequiredSecrets) != 2 {
		t.Fatal("paired key identities not frozen")
	}
	if err := validateRequiredCaptureSecrets(secret.RequiredSecrets, selection[:1]); err == nil {
		t.Fatal("collector omitted restore credential")
	}
	if err := validateRequiredCaptureSecrets(secret.RequiredSecrets, selection); err != nil {
		t.Fatal(err)
	}
	key.UID = types.UID("replaced")
	client.CoreV1().Secrets(namespace).Update(context.Background(), key, metav1.UpdateOptions{})
	if err := freezeHistorySecretPair(context.Background(), client, ordinary, &secret); err == nil {
		t.Fatal("changed encryption key identity accepted")
	}
}
