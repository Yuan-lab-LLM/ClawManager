package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"clawreef/internal/services/k8s"
)

const inClusterNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// SecretReference describes a parsed Kubernetes Secret reference.
type SecretReference struct {
	Namespace string
	Name      string
	Key       string
}

// SecretRefService resolves model secret references against Kubernetes Secrets.
type SecretRefService interface {
	ResolveString(ctx context.Context, value *string, secretRef *string) (*string, error)
}

type secretRefService struct {
	secretService *k8s.SecretService
}

// NewSecretRefService creates a new secret ref resolver.
func NewSecretRefService() SecretRefService {
	return &secretRefService{
		secretService: k8s.NewSecretService(),
	}
}

func (s *secretRefService) ResolveString(ctx context.Context, value *string, secretRef *string) (*string, error) {
	if value != nil {
		trimmed := strings.TrimSpace(*value)
		if trimmed != "" {
			return &trimmed, nil
		}
	}

	if secretRef == nil || strings.TrimSpace(*secretRef) == "" {
		return nil, nil
	}

	ref, err := ParseSecretReference(*secretRef)
	if err != nil {
		return nil, err
	}
	if ref.Namespace == "" {
		ref.Namespace = defaultSecretNamespace()
		if ref.Namespace == "" {
			return nil, errors.New("secret namespace is required in secret ref")
		}
	}

	secretValue, err := s.secretService.GetSecretValue(ctx, ref.Namespace, ref.Name, ref.Key)
	if err != nil {
		return nil, err
	}
	secretValue = strings.TrimSpace(secretValue)
	if secretValue == "" {
		return nil, fmt.Errorf("secret value is empty for %s/%s:%s", ref.Namespace, ref.Name, ref.Key)
	}

	return &secretValue, nil
}

// ParseSecretReference supports:
// - name:key
// - namespace/name:key
// - k8s-secret/name:key
// - k8s-secret/namespace/name:key
func ParseSecretReference(value string) (*SecretReference, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("secret ref is required")
	}

	trimmed = strings.TrimPrefix(trimmed, "k8s-secret/")
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("secret ref format is invalid")
	}

	pathPart := strings.TrimSpace(parts[0])
	keyPart := strings.TrimSpace(parts[1])
	if pathPart == "" || keyPart == "" {
		return nil, errors.New("secret ref format is invalid")
	}

	pathSegments := strings.Split(pathPart, "/")
	switch len(pathSegments) {
	case 1:
		return &SecretReference{
			Name: pathSegments[0],
			Key:  keyPart,
		}, nil
	case 2:
		return &SecretReference{
			Namespace: pathSegments[0],
			Name:      pathSegments[1],
			Key:       keyPart,
		}, nil
	default:
		return nil, errors.New("secret ref format is invalid")
	}
}

func defaultSecretNamespace() string {
	if value := strings.TrimSpace(os.Getenv("CLAWMANAGER_SECRET_NAMESPACE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("MODEL_SECRET_NAMESPACE")); value != "" {
		return value
	}

	if data, err := os.ReadFile(inClusterNamespacePath); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return value
		}
	}

	return ""
}
