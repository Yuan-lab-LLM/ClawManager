package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReservedAssignments retains ownership independently of a lost SQL binding.
// Only confirmed, finalized stops release a physical slot reservation.
func (s *RuntimeAssignmentService) ReservedAssignments(ctx context.Context, endpoint, podUID, runtimeType string) ([]RuntimeAssignment, error) {
	if err := s.validService(); err != nil {
		return nil, err
	}
	if endpoint == "" || podUID == "" || runtimeType == "" {
		return nil, fmt.Errorf("slot reservation target identity is required")
	}
	list, err := s.client.CoreV1().ConfigMaps(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if err != nil {
		return nil, err
	}
	var result []RuntimeAssignment
	for _, cm := range list.Items {
		if cm.Data["pod_uid"] != podUID && (cm.Data["pod_uid"] != "" || cm.Data["endpoint"] != endpoint) {
			continue
		}
		a, state, err := storedRuntimeAssignment(&cm)
		if err != nil || cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "lite-instance" || a.PodUID != podUID || a.Endpoint != endpoint || a.RuntimeType != runtimeType {
			return nil, fmt.Errorf("persisted slot reservation identity is unavailable: %s", cm.Name)
		}
		if state == "stopped" && (cm.Data["container_termination_proof"] == "" || cm.Data["container_stop_released"] == "true") {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}
