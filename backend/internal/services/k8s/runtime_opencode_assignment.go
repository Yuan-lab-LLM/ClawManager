package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

// Read immutable original environment privately. Callers validate native path
// overrides and discard it; business credentials must never enter checkpoints.
func (s *RuntimeAssignmentService) OpenCodeUpgradeSource(ctx context.Context, id int) (RuntimeAssignment, string, error) {
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return RuntimeAssignment{}, "", err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return a, state, err
	}
	if a.RuntimeType != "opencode" || cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "lite-instance" {
		return a, state, fmt.Errorf("OpenCode source assignment ownership changed")
	}
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	if err != nil {
		return a, state, err
	}
	a.Environment = map[string]string{}
	for key, value := range secret.Data {
		a.Environment[key] = string(value)
	}
	if secret.Name != runtimeAssignmentSecretName(a.InstanceID, a.Generation) || secret.Namespace != s.namespace || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || len(secret.Data) == 0 {
		return a, state, fmt.Errorf("OpenCode original business Secret is not immutable and owned")
	}
	for key, want := range map[string]string{"clawmanager.io/instance-id": strconv.Itoa(a.InstanceID), "clawmanager.io/user-id": strconv.Itoa(a.UserID), "clawmanager.io/generation": strconv.Itoa(a.Generation), "clawmanager.io/runtime-type": a.RuntimeType} {
		if secret.Labels[key] != want {
			return a, state, fmt.Errorf("OpenCode original business Secret ownership differs")
		}
	}
	return a, state, nil
}

func (s *RuntimeAssignmentService) OpenCodeUpgradeStopReleased(ctx context.Context, id int, intent RuntimeContainerStopIntent) error {
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return err
	}
	if state != "stopped" || !containerIntentAssignment(intent, a) || cm.Data["container_stop_released"] != "true" || cm.Data["container_termination_proof"] == "" {
		return fmt.Errorf("OpenCode source C1 release is incomplete")
	}
	return nil
}
