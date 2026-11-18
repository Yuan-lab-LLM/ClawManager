package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EnableRuntimePodScheduling restores the Deployment admission gate, leaving
// every Pod's independent SQL pause intact. It never changes the Pod template,
// image, replicas, native drain state, or a rollout's activation authority.
func (s *runtimeDeploymentService) EnableRuntimePodScheduling(ctx context.Context, target RuntimePodControllerIdentity) error {
	if target.Namespace == "" || target.PodName == "" || target.PodUID == "" || target.DeploymentName == "" || target.RuntimeType == "" {
		return fmt.Errorf("complete Runtime Pod identity is required to resume scheduling")
	}
	identity, err := s.ResolveRuntimePodController(ctx, target.Namespace, target.PodName, target.PodUID)
	if err != nil {
		return err
	}
	if identity.DeploymentName != target.DeploymentName || identity.RuntimeType != target.RuntimeType {
		return fmt.Errorf("Runtime Pod controller or runtime type changed")
	}
	pod, err := s.client.CoreV1().Pods(target.Namespace).Get(ctx, target.PodName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(pod.UID) != target.PodUID || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("current running Runtime Pod identity is required")
	}
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if !ready {
		return fmt.Errorf("Runtime Pod is not ready; scheduling cannot be resumed")
	}
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(d.UID) != identity.DeploymentUID || d.ResourceVersion == "" || d.DeletionTimestamp != nil || d.Spec.Replicas == nil || *d.Spec.Replicas < 1 || d.Spec.Paused || d.Labels["clawmanager.io/runtime-type"] != target.RuntimeType {
		return fmt.Errorf("Runtime Deployment identity or serving state changed")
	}
	for _, labels := range []map[string]string{d.Labels, d.Spec.Template.Labels, pod.Labels} {
		role := labels[runtimePoolRoleLabel]
		if strings.HasPrefix(d.Name, "openclaw-upgrade-lab-") || labels[upgradeLabPurposeLabel] == upgradeLabPurposeValue || labels["clawmanager.io/pool-purpose"] == upgradeLabPurposeValue || (role != "" && (role != "upgrade-target" || d.Labels[runtimePoolRoleLabel] != role || d.Labels[runtimeSchedulingLabel] != "true")) {
			return fmt.Errorf("upgrade or laboratory pool must be activated through its rollout before scheduling can resume")
		}
	}
	if d.Labels[RuntimeScaleInOperationLabel] != "" || d.Annotations[runtimeIdleSleepAnnotation] != "" || d.Annotations[runtimeScaleInCleanupBarrierAnnotation] != "" {
		return fmt.Errorf("Runtime Deployment maintenance is active; scheduling cannot be resumed")
	}
	// The wake annotation is a retained journal, not an in-progress operation.
	// Match inventory's boolean parsing so "False" and "0" cannot report a
	// successful resume while leaving the effective gate disabled.
	raw, exists := d.Labels[runtimeSchedulingLabel]
	if !exists {
		return nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("invalid Runtime Deployment scheduling label: %w", err)
	}
	if enabled {
		return nil
	}
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(d.UID)},
		{"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion},
		{"op": "replace", "path": "/metadata/labels/clawmanager.io~1scheduling-enabled", "value": "true"},
	})
	if err != nil {
		return err
	}
	_, err = s.client.AppsV1().Deployments(target.Namespace).Patch(ctx, d.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
