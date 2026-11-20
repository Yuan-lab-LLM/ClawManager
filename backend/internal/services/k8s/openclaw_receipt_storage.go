package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	openClawReceiptBindingsEnv   = "CLAWMANAGER_OPENCLAW_RECEIPT_PVC_BINDINGS"
	openClawReceiptAnnotation    = "clawmanager.io/task-receipt-pvc"
	openClawReceiptUIDAnnotation = "clawmanager.io/task-receipt-pvc-uid"
	openClawReceiptVolume        = "task-receipts"
	openClawReceiptRoot          = "/var/lib/clawmanager-task-receipts"
	openClawReceiptRootEnv       = "CLAWMANAGER_TASK_RECEIPT_ROOT"
)

type openClawReceiptBinding struct {
	InstanceID int    `json:"instance_id"`
	UserID     int    `json:"user_id"`
	ClaimName  string `json:"claim_name"`
}

// This administrator-only allowlist is intentionally not an instance option.
// A missing entry leaves a legacy instance untouched; it does not provision PVCs.
func selectOpenClawReceiptBinding(raw string, config PodConfig) (*openClawReceiptBinding, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 65536 {
		return nil, fmt.Errorf("receipt PVC bindings exceed 64 KiB")
	}
	var bindings []openClawReceiptBinding
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bindings); err != nil {
		return nil, fmt.Errorf("invalid receipt PVC bindings JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing receipt PVC bindings data")
	}
	instances := map[int]bool{}
	claims := map[string]bool{}
	var selected *openClawReceiptBinding
	for _, binding := range bindings {
		if binding.InstanceID <= 0 || binding.UserID <= 0 || len(validation.IsDNS1123Subdomain(binding.ClaimName)) != 0 || binding.ClaimName == "" {
			return nil, fmt.Errorf("receipt PVC binding requires positive instance/user IDs and a valid claim name")
		}
		claimKey := strconv.Itoa(binding.UserID) + "/" + binding.ClaimName
		if instances[binding.InstanceID] || claims[claimKey] {
			return nil, fmt.Errorf("duplicate instance or shared receipt PVC binding")
		}
		instances[binding.InstanceID], claims[claimKey] = true, true
		if binding.InstanceID == config.InstanceID {
			if binding.UserID != config.UserID {
				return nil, fmt.Errorf("receipt PVC owner changed; explicit storage migration is required")
			}
			copy := binding
			selected = &copy
		}
	}
	return selected, nil
}

func receiptPathsOverlap(a, b string) bool {
	a, b = path.Clean(a), path.Clean(b)
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/") || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/")
}

// Attach only pre-provisioned, explicitly owned, single-Pod filesystem claims.
// Filesystem ownership and artifact acceptance remain runtime checks.
func (s *InstanceDeploymentService) prepareOpenClawReceiptStorage(ctx context.Context, config PodConfig, desired *appsv1.Deployment) error {
	if !strings.EqualFold(strings.TrimSpace(config.Type), "openclaw") {
		return nil
	}
	binding, err := selectOpenClawReceiptBinding(os.Getenv(openClawReceiptBindingsEnv), config)
	if err != nil {
		return err
	}
	if binding == nil {
		if hasRuntimeReceiptStorage(desired) {
			return fmt.Errorf("receipt storage requires its explicit administrator binding")
		}
		return nil
	}
	if runtimeType := strings.ToLower(strings.TrimSpace(config.RuntimeType)); runtimeType != "" && runtimeType != "desktop" {
		return fmt.Errorf("receipt PVC bindings support only OpenClaw Pro desktop instances")
	}
	if desired.Spec.Replicas == nil || *desired.Spec.Replicas < 0 || *desired.Spec.Replicas > 1 {
		return fmt.Errorf("receipt storage requires zero or one replica")
	}
	pod := &desired.Spec.Template.Spec
	index := -1
	for i, container := range pod.Containers {
		if container.Name == "desktop" {
			if index != -1 {
				return fmt.Errorf("duplicate desktop container")
			}
			index = i
		}
	}
	if index == -1 {
		return fmt.Errorf("receipt storage requires a desktop container")
	}
	if pod.SecurityContext != nil && pod.SecurityContext.FSGroup != nil {
		return fmt.Errorf("receipt storage does not support pod-wide fsGroup ownership changes")
	}
	for _, volume := range pod.Volumes {
		if volume.Name == openClawReceiptVolume || volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == binding.ClaimName {
			return fmt.Errorf("receipt PVC must not reuse another workload volume")
		}
	}
	for i, container := range append(append([]corev1.Container{}, pod.Containers...), pod.InitContainers...) {
		for _, env := range container.Env {
			if i != index && (env.Name == openClawReceiptRootEnv || env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR") && (env.Value != "" || env.ValueFrom != nil) {
				return fmt.Errorf("receipt storage environment must only be set on the desktop container")
			}
		}
		for _, mount := range container.VolumeMounts {
			if receiptPathsOverlap(mount.MountPath, openClawReceiptRoot) {
				return fmt.Errorf("receipt directory overlaps an existing mount")
			}
		}
	}
	claim, err := s.inspectOpenClawReceiptClaim(ctx, config, desired.Namespace, binding)
	if err != nil {
		return err
	}
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: openClawReceiptVolume, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: binding.ClaimName}}})
	container := &pod.Containers[index]
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: openClawReceiptVolume, MountPath: openClawReceiptRoot})
	env := make([]corev1.EnvVar, 0, len(container.Env)+3)
	for _, entry := range container.Env {
		if entry.Name != openClawReceiptRootEnv && entry.Name != "CLAWMANAGER_TASK_RECEIPT_DIR" && entry.Name != "INSTANCE_ID" && entry.Name != "USER_ID" {
			env = append(env, entry)
		}
	}
	container.Env = append(env,
		corev1.EnvVar{Name: openClawReceiptRootEnv, Value: openClawReceiptRoot},
		corev1.EnvVar{Name: "INSTANCE_ID", Value: strconv.Itoa(config.InstanceID)},
		corev1.EnvVar{Name: "USER_ID", Value: strconv.Itoa(config.UserID)},
	)
	if desired.Spec.Template.Annotations == nil {
		desired.Spec.Template.Annotations = map[string]string{}
	}
	desired.Spec.Template.Annotations[openClawReceiptAnnotation] = binding.ClaimName
	desired.Spec.Template.Annotations[openClawReceiptUIDAnnotation] = string(claim.UID)
	desired.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	return nil
}

func (s *InstanceDeploymentService) inspectOpenClawReceiptClaim(ctx context.Context, config PodConfig, namespace string, binding *openClawReceiptBinding) (*corev1.PersistentVolumeClaim, error) {
	claim, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, binding.ClaimName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("cannot inspect dedicated receipt PVC: %w", err)
	}
	if claim.DeletionTimestamp != nil || claim.Status.Phase == corev1.ClaimLost || claim.UID == "" {
		return nil, fmt.Errorf("receipt PVC is terminating, lost or has no resource identity")
	}
	if claim.Spec.VolumeMode != nil && *claim.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
		return nil, fmt.Errorf("receipt PVC must use filesystem volume mode")
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOncePod {
		return nil, fmt.Errorf("receipt PVC requires ReadWriteOncePod access mode")
	}
	if claim.Labels["clawmanager.io/storage-purpose"] != "task-receipts" || claim.Labels["instance-id"] != strconv.Itoa(config.InstanceID) || claim.Labels["user-id"] != strconv.Itoa(config.UserID) {
		return nil, fmt.Errorf("receipt PVC ownership labels do not match this instance")
	}
	return claim, nil
}

// Scaling a stopped instance back up does not pass through EnsureDeployment.
// Revalidate storage before allowing that path to resume a potential writer.
func (s *InstanceDeploymentService) validateOpenClawReceiptStart(ctx context.Context, current *appsv1.Deployment, userID, instanceID int) error {
	config := PodConfig{InstanceID: instanceID, UserID: userID}
	binding, err := selectOpenClawReceiptBinding(os.Getenv(openClawReceiptBindingsEnv), config)
	if err != nil {
		return err
	}
	if binding == nil || current.Spec.Template.Annotations[openClawReceiptAnnotation] != binding.ClaimName || current.Spec.Template.Annotations[openClawReceiptUIDAnnotation] == "" || current.Spec.Template.Labels["instance-id"] != strconv.Itoa(instanceID) || current.Spec.Template.Labels["user-id"] != strconv.Itoa(userID) {
		return fmt.Errorf("receipt storage start requires its original administrator binding and owner")
	}
	if err := validateOpenClawReceiptLayout(current); err != nil {
		return err
	}
	claim, err := s.inspectOpenClawReceiptClaim(ctx, config, current.Namespace, binding)
	if err != nil {
		return err
	}
	if string(claim.UID) != current.Spec.Template.Annotations[openClawReceiptUIDAnnotation] {
		return fmt.Errorf("receipt PVC was replaced; explicit reconciliation is required before start")
	}
	return nil
}

// Removing an allowlist entry must not silently detach outstanding receipts.
func validateOpenClawReceiptTransition(current, desired *appsv1.Deployment) error {
	if !hasRuntimeReceiptStorage(current) {
		return nil
	}
	old := current.Spec.Template.Annotations[openClawReceiptAnnotation]
	if old == "" || current.Spec.Template.Annotations[openClawReceiptUIDAnnotation] == "" {
		return fmt.Errorf("existing receipt storage has no complete binding; explicit reconciliation is required")
	}
	if old != desired.Spec.Template.Annotations[openClawReceiptAnnotation] || current.Spec.Template.Labels["user-id"] != desired.Spec.Template.Labels["user-id"] || current.Spec.Template.Annotations[openClawReceiptUIDAnnotation] != desired.Spec.Template.Annotations[openClawReceiptUIDAnnotation] {
		return fmt.Errorf("receipt storage binding cannot be removed or changed without explicit migration")
	}
	// An annotation alone is not evidence that the same files will be mounted.
	// Reject altered paths/subpaths and secondary writers instead of silently
	// replacing them with the generated template and losing pending receipts.
	for _, deployment := range []*appsv1.Deployment{current, desired} {
		if err := validateOpenClawReceiptLayout(deployment); err != nil {
			return err
		}
	}
	return nil
}

func validateOpenClawReceiptLayout(deployment *appsv1.Deployment) error {
	claim := deployment.Spec.Template.Annotations[openClawReceiptAnnotation]
	pod := deployment.Spec.Template.Spec
	if pod.SecurityContext != nil && pod.SecurityContext.FSGroup != nil {
		return fmt.Errorf("existing receipt storage has pod-wide fsGroup; explicit reconciliation is required")
	}
	volumes, mounts, roots := 0, 0, 0
	for _, volume := range pod.Volumes {
		if volume.Name == openClawReceiptVolume {
			volumes++
			if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != claim || volume.PersistentVolumeClaim.ReadOnly {
				return fmt.Errorf("existing receipt volume requires explicit reconciliation")
			}
		} else if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == claim {
			return fmt.Errorf("receipt PVC is also used by another volume")
		}
	}
	for i, container := range append(append([]corev1.Container{}, pod.Containers...), pod.InitContainers...) {
		primary := i < len(pod.Containers) && container.Name == "desktop"
		identityCounts := map[string]int{}
		for _, mount := range container.VolumeMounts {
			if mount.Name == openClawReceiptVolume {
				mounts++
				if !primary || mount.MountPath != openClawReceiptRoot || mount.SubPath != "" || mount.SubPathExpr != "" || mount.ReadOnly || mount.MountPropagation != nil {
					return fmt.Errorf("existing receipt mount requires explicit reconciliation")
				}
			} else if receiptPathsOverlap(mount.MountPath, openClawReceiptRoot) {
				return fmt.Errorf("existing receipt directory overlaps another mount")
			}
		}
		for _, env := range container.Env {
			if primary && (env.Name == "INSTANCE_ID" || env.Name == "USER_ID") {
				identityCounts[env.Name]++
				label := "instance-id"
				if env.Name == "USER_ID" {
					label = "user-id"
				}
				if env.Value == "" || env.Value != deployment.Spec.Template.Labels[label] || env.ValueFrom != nil {
					return fmt.Errorf("existing receipt directory identity requires explicit reconciliation")
				}
			}
			if env.Name == openClawReceiptRootEnv {
				roots++
				if !primary || env.Value != openClawReceiptRoot || env.ValueFrom != nil {
					return fmt.Errorf("existing receipt root requires explicit reconciliation")
				}
			}
			if env.Name == "CLAWMANAGER_TASK_RECEIPT_DIR" && (env.Value != "" || env.ValueFrom != nil) {
				return fmt.Errorf("existing direct receipt directory requires explicit reconciliation")
			}
		}
		if primary && (identityCounts["INSTANCE_ID"] != 1 || identityCounts["USER_ID"] != 1) {
			return fmt.Errorf("missing or duplicate receipt directory identity")
		}
	}
	if volumes != 1 || mounts != 1 || roots != 1 {
		return fmt.Errorf("incomplete or duplicate receipt storage requires explicit reconciliation")
	}
	return nil
}
