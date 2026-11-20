package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const runtimeReceiptBindingsEnv = "CLAWMANAGER_OPENCLAW_RUNTIME_RECEIPT_PVC_BINDINGS"

type runtimeReceiptBinding struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	ClaimName  string `json:"claim_name"`
}

func selectRuntimeReceiptBinding(raw string, deployment *appsv1.Deployment) (*runtimeReceiptBinding, error) {
	if strings.TrimSpace(raw) == "" || deployment.Labels["clawmanager.io/runtime-type"] != "openclaw" {
		return nil, nil
	}
	if len(raw) > 65536 {
		return nil, fmt.Errorf("runtime receipt bindings exceed 64 KiB")
	}
	var bindings []runtimeReceiptBinding
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bindings); err != nil {
		return nil, fmt.Errorf("invalid runtime receipt bindings JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing runtime receipt bindings data")
	}
	pools, claims := map[string]bool{}, map[string]bool{}
	var selected *runtimeReceiptBinding
	for _, binding := range bindings {
		if binding.Namespace == "" || binding.Deployment == "" || binding.ClaimName == "" || len(validation.IsDNS1123Label(binding.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(binding.Deployment)) != 0 || len(validation.IsDNS1123Subdomain(binding.ClaimName)) != 0 {
			return nil, fmt.Errorf("runtime receipt binding requires valid namespace, deployment and claim names")
		}
		poolKey, claimKey := binding.Namespace+"/"+binding.Deployment, binding.Namespace+"/"+binding.ClaimName
		if pools[poolKey] || claims[claimKey] {
			return nil, fmt.Errorf("duplicate runtime or shared receipt PVC binding")
		}
		pools[poolKey], claims[claimKey] = true, true
		if binding.Namespace == deployment.Namespace && binding.Deployment == deployment.Name {
			copy := binding
			selected = &copy
		}
	}
	return selected, nil
}

// Detect storage intent even if somebody removed the binding annotation.
// Secret-backed envFrom is intentionally not resolved: administrators must not
// inject receipt storage configuration through envFrom outside this contract.
func hasRuntimeReceiptStorage(deployment *appsv1.Deployment) bool {
	if deployment.Spec.Template.Annotations[openClawReceiptAnnotation] != "" || deployment.Spec.Template.Annotations[openClawReceiptUIDAnnotation] != "" {
		return true
	}
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == openClawReceiptVolume {
			return true
		}
	}
	for _, container := range append(append([]corev1.Container{}, deployment.Spec.Template.Spec.Containers...), deployment.Spec.Template.Spec.InitContainers...) {
		for _, mount := range container.VolumeMounts {
			if mount.Name == openClawReceiptVolume || mount.MountPath == openClawReceiptRoot || strings.HasPrefix(mount.MountPath, openClawReceiptRoot+"/") {
				return true
			}
		}
		for _, env := range container.Env {
			if (env.Name == openClawReceiptRootEnv || env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR") && (env.Value != "" || env.ValueFrom != nil) {
				return true
			}
		}
	}
	return false
}

func rejectRuntimeReceiptClone(deployment *appsv1.Deployment) error {
	binding, err := selectRuntimeReceiptBinding(os.Getenv(runtimeReceiptBindingsEnv), deployment)
	if err != nil {
		return err
	}
	if binding != nil || hasRuntimeReceiptStorage(deployment) {
		return fmt.Errorf("runtime %s/%s uses receipt storage; upgrade/lab pool cloning requires a storage migration protocol", deployment.Namespace, deployment.Name)
	}
	return nil
}

// Old completed upgrades may retain a target after deleting the source. Keep
// that idempotent legacy path, while still checking planned source bindings.
func (s *runtimeDeploymentService) checkRuntimeReceiptCloneSource(ctx context.Context, namespace, name, runtimeType string) error {
	source, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		source = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"clawmanager.io/runtime-type": runtimeType}}}
	} else if err != nil {
		return err
	}
	return rejectRuntimeReceiptClone(source)
}

// Admin-selected, single-Pod Lite pools only. This does not enable task protocol.
// It is called from both reconciliation and the actual image rollout path.
func (s *runtimeDeploymentService) prepareRuntimeReceiptStorage(ctx context.Context, desired *appsv1.Deployment) error {
	binding, err := selectRuntimeReceiptBinding(os.Getenv(runtimeReceiptBindingsEnv), desired)
	if err != nil {
		return err
	}
	if binding == nil {
		if hasRuntimeReceiptStorage(desired) {
			return fmt.Errorf("runtime receipt storage requires its explicit administrator binding")
		}
		return nil
	}
	if desired.Spec.Replicas == nil || *desired.Spec.Replicas < 0 || *desired.Spec.Replicas > 1 {
		return fmt.Errorf("runtime receipt storage currently supports zero or one Pod")
	}
	pod := &desired.Spec.Template.Spec
	if pod.SecurityContext != nil && pod.SecurityContext.FSGroup != nil {
		return fmt.Errorf("runtime receipt storage does not support pod-wide fsGroup")
	}
	index := -1
	for i, container := range pod.Containers {
		if container.Name == "runtime" {
			if index != -1 {
				return fmt.Errorf("duplicate runtime container")
			}
			index = i
		}
	}
	if index == -1 {
		return fmt.Errorf("receipt storage requires a runtime container")
	}
	oldClaim := desired.Spec.Template.Annotations[openClawReceiptAnnotation]
	oldUID := desired.Spec.Template.Annotations[openClawReceiptUIDAnnotation]
	if oldClaim != "" && oldClaim != binding.ClaimName {
		return fmt.Errorf("runtime receipt PVC cannot change without explicit migration")
	}
	volumeCount, mountCount, rootCount := 0, 0, 0
	for _, volume := range pod.Volumes {
		if volume.Name == openClawReceiptVolume {
			volumeCount++
			if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != binding.ClaimName || volume.PersistentVolumeClaim.ReadOnly {
				return fmt.Errorf("invalid runtime receipt volume")
			}
		} else if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == binding.ClaimName {
			return fmt.Errorf("runtime receipt PVC must not reuse a workspace or another volume")
		}
	}
	containers := append(append([]corev1.Container{}, pod.Containers...), pod.InitContainers...)
	for i, container := range containers {
		for _, mount := range container.VolumeMounts {
			if mount.Name == openClawReceiptVolume {
				mountCount++
				if i != index || mount.MountPath != openClawReceiptRoot || mount.SubPath != "" || mount.SubPathExpr != "" || mount.ReadOnly || mount.MountPropagation != nil {
					return fmt.Errorf("receipt volume must only be mounted at its dedicated runtime root")
				}
			} else if receiptPathsOverlap(mount.MountPath, openClawReceiptRoot) {
				return fmt.Errorf("runtime receipt directory overlaps an existing mount")
			}
		}
		for _, env := range container.Env {
			if env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR" && (env.Value != "" || env.ValueFrom != nil) {
				return fmt.Errorf("Lite receipt directories must be allocated by the runtime agent")
			}
			if env.Name == openClawReceiptRootEnv {
				rootCount++
				if i != index || env.Value != openClawReceiptRoot || env.ValueFrom != nil {
					return fmt.Errorf("invalid runtime receipt root environment")
				}
			}
		}
	}
	if volumeCount > 1 || mountCount > 1 || rootCount > 1 {
		return fmt.Errorf("duplicate runtime receipt configuration")
	}
	claim, err := s.client.CoreV1().PersistentVolumeClaims(desired.Namespace).Get(ctx, binding.ClaimName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot inspect runtime receipt PVC: %w", err)
	}
	if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase == corev1.ClaimLost || claim.Spec.VolumeMode != nil && *claim.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
		return fmt.Errorf("runtime receipt PVC must be an existing, non-terminating filesystem claim")
	}
	if oldUID != "" && oldUID != string(claim.UID) {
		return fmt.Errorf("runtime receipt PVC was replaced; explicit reconciliation is required")
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOncePod {
		return fmt.Errorf("runtime receipt PVC requires ReadWriteOncePod access mode")
	}
	if claim.Labels["clawmanager.io/storage-purpose"] != "task-receipts" || claim.Annotations["clawmanager.io/runtime-deployment"] != desired.Name {
		return fmt.Errorf("runtime receipt PVC does not belong to this pool")
	}
	if volumeCount == 0 {
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: openClawReceiptVolume, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: binding.ClaimName}}})
	}
	if mountCount == 0 {
		pod.Containers[index].VolumeMounts = append(pod.Containers[index].VolumeMounts, corev1.VolumeMount{Name: openClawReceiptVolume, MountPath: openClawReceiptRoot})
	}
	upsertEnvVar(&pod.Containers[index], openClawReceiptRootEnv, openClawReceiptRoot)
	if desired.Spec.Template.Annotations == nil {
		desired.Spec.Template.Annotations = map[string]string{}
	}
	desired.Spec.Template.Annotations[openClawReceiptAnnotation] = binding.ClaimName
	desired.Spec.Template.Annotations[openClawReceiptUIDAnnotation] = string(claim.UID)
	desired.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	return nil
}

func validateRuntimeReceiptTransition(current, desired *appsv1.Deployment) error {
	if !hasRuntimeReceiptStorage(current) {
		return nil
	}
	if !hasRuntimeReceiptStorage(desired) {
		return fmt.Errorf("runtime receipt storage cannot be removed without explicit migration")
	}
	for _, key := range []string{openClawReceiptAnnotation, openClawReceiptUIDAnnotation} {
		if current.Spec.Template.Annotations[key] != "" && current.Spec.Template.Annotations[key] != desired.Spec.Template.Annotations[key] {
			return fmt.Errorf("runtime receipt storage binding changed; explicit migration is required")
		}
	}
	for _, volume := range current.Spec.Template.Spec.Volumes {
		if volume.Name == openClawReceiptVolume && (volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != desired.Spec.Template.Annotations[openClawReceiptAnnotation]) {
			return fmt.Errorf("existing runtime receipt volume cannot be replaced during reconciliation")
		}
	}
	for _, container := range current.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == openClawReceiptRootEnv && (env.Value != openClawReceiptRoot || env.ValueFrom != nil) || env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR" && (env.Value != "" || env.ValueFrom != nil) {
				return fmt.Errorf("existing runtime receipt directory requires explicit migration")
			}
		}
	}
	return nil
}
