package k8s

import (
	"bytes"
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRuntimeCapabilityKeyIsDedicatedAndPersistent(t *testing.T) {
	kube := fake.NewSimpleClientset()
	ctx := context.Background()
	a, err := RuntimeCapabilitySigningKey(ctx, kube, "test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RuntimeCapabilitySigningKey(ctx, kube, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || !bytes.Equal(a, b) {
		t.Fatal("key missing or changed across restart")
	}
	secret, err := kube.CoreV1().Secrets("test").Get(ctx, "clawmanager-runtime-capabilities", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || len(secret.Data) != 1 {
		t.Fatal("unexpected signing secret")
	}
	// Mutation of returned bytes must not alter the stored signing key.
	a[0] ^= 255
	c, err := RuntimeCapabilitySigningKey(ctx, kube, "test")
	if err != nil || !bytes.Equal(b, c) {
		t.Fatal("key alias exposed")
	}
}

func TestRuntimeCapabilityKeyRejectsCorruptSecret(t *testing.T) {
	kube := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-runtime-capabilities", Namespace: "test"}, Data: map[string][]byte{"signing-key": []byte("weak")}})
	if _, err := RuntimeCapabilitySigningKey(context.Background(), kube, "test"); err == nil {
		t.Fatal("silently regenerated invalid key")
	}
	if _, err := RuntimeCapabilitySigningKey(context.Background(), kube, "../bad"); err == nil {
		t.Fatal("accepted invalid namespace")
	}
}
