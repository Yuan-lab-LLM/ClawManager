package k8s

import (
	"context"
	"crypto/rand"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

// RuntimeCapabilitySigningKey persists a dedicated backend-only signing key.
// No runtime Pod template may mount or project this Secret. An invalid existing
// key fails startup rather than rotating credentials unexpectedly.
func RuntimeCapabilitySigningKey(ctx context.Context, kube kubernetes.Interface, namespace string) ([]byte, error) {
	fail := errors.New("runtime capability signing secret unavailable")
	if kube == nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return nil, fail
	}
	const name = "clawmanager-runtime-capabilities"
	client := kube.CoreV1().Secrets(namespace)
	secret, err := client.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, fail
		}
		immutable := true
		secret, err = client.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{"signing-key": key}}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = client.Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil || secret == nil || secret.DeletionTimestamp != nil || secret.Type != corev1.SecretTypeOpaque || len(secret.Data["signing-key"]) < 32 || ctx.Err() != nil {
		return nil, fail
	}
	return append([]byte(nil), secret.Data["signing-key"]...), nil
}
