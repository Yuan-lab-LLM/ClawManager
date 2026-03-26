package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretService handles Kubernetes Secret reads.
type SecretService struct {
	client *Client
}

// NewSecretService creates a new secret service.
func NewSecretService() *SecretService {
	return &SecretService{
		client: globalClient,
	}
}

// GetSecretValue returns a decoded string value from a Kubernetes Secret.
func (s *SecretService) GetSecretValue(ctx context.Context, namespace, name, key string) (string, error) {
	if s.client == nil || s.client.Clientset == nil {
		return "", fmt.Errorf("k8s client not initialized")
	}

	secret, err := s.client.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret key %s not found in %s/%s", key, namespace, name)
	}

	return string(value), nil
}
