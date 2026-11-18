package k8s

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretService handles Kubernetes Secret reads.
type SecretService struct {
	client           *Client
	namespaceService *NamespaceService
}

// NewSecretService creates a new secret service.
func NewSecretService() *SecretService {
	return &SecretService{
		client:           globalClient,
		namespaceService: NewNamespaceService(),
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

// CreateOwnerTeamSecret never rotates a token on replay. Only an exact,
// labelled Team secret may be reused; name equality alone is not ownership.
func (s *SecretService) CreateOwnerTeamSecret(ctx context.Context, userID, teamID int, name string, data map[string]string) (map[string]string, error) {
	if s.client == nil || s.client.Clientset == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	secret, err := s.client.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		values := map[string][]byte{}
		for k, v := range data {
			values[k] = []byte(v)
		}
		secret, err = s.client.Clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app": "clawreef", "managed-by": "clawreef", "team-id": strconv.Itoa(teamID)}}, Type: corev1.SecretTypeOpaque, Data: values}, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			secret, err = s.client.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	if secret.Labels["team-id"] != strconv.Itoa(teamID) || secret.Labels["managed-by"] != "clawreef" {
		return nil, fmt.Errorf("Owner Team secret ownership conflict")
	}
	result := map[string]string{}
	for k := range data {
		if len(secret.Data[k]) == 0 {
			return nil, fmt.Errorf("Owner Team secret is incomplete")
		}
		result[k] = string(secret.Data[k])
	}
	return result, nil
}

// UpsertSecret creates or updates a secret in the user's namespace.
func (s *SecretService) UpsertSecret(ctx context.Context, userID int, name string, data map[string]string, labels map[string]string) error {
	if s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	if _, err := s.namespaceService.EnsureNamespace(ctx, userID); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	namespace := s.client.GetNamespace(userID)
	secretData := map[string][]byte{}
	for key, value := range data {
		secretData[key] = []byte(value)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	existing, err := s.client.Clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil && existing != nil {
		existing.Data = secretData
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for key, value := range labels {
			existing.Labels[key] = value
		}
		if _, err := s.client.Clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update secret %s/%s: %w", namespace, name, err)
		}
		return nil
	}
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to inspect secret %s/%s: %w", namespace, name, err)
	}

	if _, err := s.client.Clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// DeleteSecret deletes a Secret from the user's namespace. Missing secrets are treated as already deleted.
func (s *SecretService) DeleteSecret(ctx context.Context, userID int, name string) error {
	if s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	if err := s.client.Clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete secret %s/%s: %w", namespace, name, err)
	}
	return nil
}
