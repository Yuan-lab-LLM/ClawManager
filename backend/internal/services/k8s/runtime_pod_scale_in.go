package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const RuntimeScaleInProtectedLabel = "clawmanager.io/scale-in-protected"
const RuntimeScaleInOperationLabel = "clawmanager.io/scale-in-operation"
const RuntimeScaleInPolicyDenial = "CM_SCALE_IN_PROTECTED"
const runtimeScaleInCleanupBarrierAnnotation = "clawmanager.io/scale-in-cleanup-barrier"
const runtimeScaleInResizeOperationAnnotation = "clawmanager.io/scale-in-resize-operation"
const runtimeDeletionCostAnnotation = "controller.kubernetes.io/pod-deletion-cost"

type RuntimePodScaleInIdentity struct {
	PodName      string  `json:"pod_name"`
	PodUID       string  `json:"pod_uid"`
	DeletionCost *string `json:"deletion_cost,omitempty"`
}
type RuntimePodScaleInProtection struct {
	// Attached by the worker from its immutable journal; never duplicate it in JSON.
	ExpectedSnapshot *RuntimePodScaleSnapshot    `json:"-"`
	AutomaticIdle    bool                        `json:"automatic_idle,omitempty"`
	OperationID      string                      `json:"operation_id"`
	Namespace        string                      `json:"namespace"`
	DeploymentName   string                      `json:"deployment_name"`
	DeploymentUID    string                      `json:"deployment_uid"`
	PreviousReplicas int32                       `json:"previous_replicas"`
	Replicas         int32                       `json:"replicas"`
	SourcePods       []RuntimePodScaleInIdentity `json:"source_pods"`
	RetainedPods     []RuntimePodScaleInIdentity `json:"retained_pods"`
}
type RuntimePodScaleInDeploymentService interface {
	RuntimePodScaleDeploymentService
	InspectRuntimeScaleInPods(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) ([]RuntimePodScaleInIdentity, error)
	ProtectRuntimeScaleIn(context.Context, RuntimePodScaleInProtection) error
	ResizeRuntimeScaleIn(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot, RuntimePodScaleInProtection) error
	ObserveRuntimeScaleIn(context.Context, RuntimePodScaleInProtection) (bool, error)
	ReleaseRuntimeScaleIn(context.Context, RuntimePodScaleInProtection, bool) error
	ConfirmRuntimeScaleInAssignmentsEmpty(context.Context, string, []RuntimePodScaleInIdentity) error
}

// Discover all owned Pods, including old ReplicaSets. A surge, terminating Pod,
// replacement, or unknown sibling makes the reviewed removal set unusable.
func (s *runtimeDeploymentService) runtimeScaleInPods(ctx context.Context, d *appsv1.Deployment) ([]corev1.Pod, error) {
	list, err := s.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var result []corev1.Pod
	for _, p := range list.Items {
		ref := metav1.GetControllerOf(&p)
		if ref == nil || ref.Kind != "ReplicaSet" {
			continue
		}
		rs, err := s.client.AppsV1().ReplicaSets(d.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if rs.UID != ref.UID {
			return nil, fmt.Errorf("Runtime ReplicaSet identity changed")
		}
		owner := metav1.GetControllerOf(rs)
		if owner == nil || owner.Kind != "Deployment" || owner.Name != d.Name {
			continue
		}
		if owner.UID != d.UID {
			return nil, fmt.Errorf("Runtime Deployment owner UID changed")
		}
		result = append(result, p)
	}
	return result, nil
}
func (s *runtimeDeploymentService) InspectRuntimeScaleInPods(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot) ([]RuntimePodScaleInIdentity, error) {
	current, err := s.InspectRuntimeScale(ctx, target)
	if err != nil {
		return nil, err
	}
	if expected == nil || current.Option.DeploymentUID != expected.Option.DeploymentUID || current.Option.Replicas != expected.Option.Replicas || !reflect.DeepEqual(current.Template, expected.Template) {
		return nil, fmt.Errorf("default Runtime Deployment changed")
	}
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if d.Labels[RuntimeScaleInOperationLabel] != "" {
		return nil, fmt.Errorf("default Runtime scale-in already active")
	}
	pods, err := s.runtimeScaleInPods(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(pods) != int(current.Option.Replicas) || d.Status.ObservedGeneration < d.Generation || d.Status.UpdatedReplicas != current.Option.Replicas || d.Status.ReadyReplicas != current.Option.Replicas || d.Status.Replicas != current.Option.Replicas {
		return nil, fmt.Errorf("default Runtime Deployment must be fully converged before scale-in")
	}
	result := make([]RuntimePodScaleInIdentity, 0, len(pods))
	for _, p := range pods {
		if p.UID == "" || p.DeletionTimestamp != nil || !runtimeScaleInPodReady(p) || p.Labels[RuntimeScaleInProtectedLabel] != "" {
			return nil, fmt.Errorf("all Runtime siblings must be ready, stable and unprotected")
		}
		result = append(result, RuntimePodScaleInIdentity{PodName: p.Name, PodUID: string(p.UID), DeletionCost: runtimeScaleInDeletionCost(p)})
	}
	return result, nil
}
func runtimeScaleInPodReady(p corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
func validateRuntimeScaleInProof(p RuntimePodScaleInProtection) error {
	if p.OperationID == "" || p.DeploymentUID == "" || p.Replicas < 0 || p.PreviousReplicas <= p.Replicas || len(p.SourcePods) != int(p.PreviousReplicas-p.Replicas) || len(p.RetainedPods) != int(p.Replicas) {
		return fmt.Errorf("complete immutable scale-in proof required")
	}
	names, uids := map[string]bool{}, map[string]bool{}
	for _, id := range append(append([]RuntimePodScaleInIdentity{}, p.SourcePods...), p.RetainedPods...) {
		if id.PodName == "" || id.PodUID == "" || names[id.PodName] || uids[id.PodUID] {
			return fmt.Errorf("invalid Runtime Pod identities")
		}
		names[id.PodName], uids[id.PodUID] = true, true
	}
	return nil
}

func (s *runtimeDeploymentService) validateRuntimeScaleInSet(ctx context.Context, p RuntimePodScaleInProtection, readiness ...bool) (*appsv1.Deployment, []corev1.Pod, error) {
	if err := validateRuntimeScaleInProof(p); err != nil {
		return nil, nil, err
	}
	d, err := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	if string(d.UID) != p.DeploymentUID || d.Spec.Replicas == nil || *d.Spec.Replicas != p.PreviousReplicas || d.Spec.Paused || (d.Labels[RuntimeScaleInOperationLabel] != "" && d.Labels[RuntimeScaleInOperationLabel] != p.OperationID) {
		return nil, nil, fmt.Errorf("scale-in Deployment identity, replicas or execution owner changed")
	}
	pods, err := s.runtimeScaleInPods(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	if len(pods) != int(p.PreviousReplicas) {
		return nil, nil, fmt.Errorf("Runtime sibling set changed")
	}
	expected := map[string]string{}
	for _, id := range append(append([]RuntimePodScaleInIdentity{}, p.SourcePods...), p.RetainedPods...) {
		expected[id.PodName] = id.PodUID
	}
	for _, pod := range pods {
		if expected[pod.Name] != string(pod.UID) || pod.DeletionTimestamp != nil || ((len(readiness) == 0 || readiness[0]) && !runtimeScaleInPodReady(pod)) {
			return nil, nil, fmt.Errorf("Runtime Pod identity or readiness changed")
		}
	}
	return d, pods, nil
}
func (s *runtimeDeploymentService) scaleInPatchPod(ctx context.Context, pod corev1.Pod, opID string, retain bool) error {
	if prior := pod.Labels[RuntimeScaleInProtectedLabel]; prior != "" && prior != opID {
		return fmt.Errorf("Runtime Pod already protected by another operation")
	}
	labels := pod.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := pod.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	if retain {
		labels[RuntimeScaleInProtectedLabel] = opID
		annotations[runtimeDeletionCostAnnotation] = "2147483647"
	} else {
		annotations[runtimeDeletionCostAnnotation] = "-2147483648"
	}
	patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": string(pod.UID)}, {"op": "test", "path": "/metadata/resourceVersion", "value": pod.ResourceVersion}, {"op": "add", "path": "/metadata/labels", "value": labels}, {"op": "add", "path": "/metadata/annotations", "value": annotations}})
	_, err := s.client.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
func (s *runtimeDeploymentService) proveRuntimeScaleInPolicy(ctx context.Context, p RuntimePodScaleInProtection) error {
	for _, id := range p.RetainedPods {
		pod, err := s.client.CoreV1().Pods(p.Namespace).Get(ctx, id.PodName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if string(pod.UID) != id.PodUID || pod.Labels[RuntimeScaleInProtectedLabel] != p.OperationID || pod.DeletionTimestamp != nil {
			return fmt.Errorf("retained Runtime Pod protection changed")
		}
		uid := pod.UID
		err = s.client.CoreV1().Pods(p.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}, Preconditions: &metav1.Preconditions{UID: &uid}})
		if !apierrors.IsForbidden(err) || !strings.Contains(err.Error(), RuntimeScaleInPolicyDenial) {
			return fmt.Errorf("scale-in requires proven Pod deletion admission policy (%s): %v", RuntimeScaleInPolicyDenial, err)
		}
	}
	return nil
}
func (s *runtimeDeploymentService) ProtectRuntimeScaleIn(ctx context.Context, p RuntimePodScaleInProtection) error {
	d, pods, err := s.validateRuntimeScaleInSet(ctx, p)
	if err != nil {
		return err
	}
	// Acquire the controller guard before labeling Pods. CAS excludes another
	// replica writer; failure leaves durable operation proof for safe cleanup.
	labels := d.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	labels[RuntimeScaleInOperationLabel] = p.OperationID
	patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": p.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "add", "path": "/metadata/labels", "value": labels}})
	if _, err = s.client.AppsV1().Deployments(p.Namespace).Patch(ctx, p.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return err
	}
	retained := map[string]bool{}
	for _, id := range p.RetainedPods {
		retained[id.PodUID] = true
	}
	for _, pod := range pods {
		if err = s.scaleInPatchPod(ctx, pod, p.OperationID, retained[string(pod.UID)]); err != nil {
			return err
		}
	}
	return s.proveRuntimeScaleInPolicy(ctx, p)
}
func (s *runtimeDeploymentService) ResizeRuntimeScaleIn(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot, p RuntimePodScaleInProtection) error {
	d, _, err := s.validateRuntimeScaleInSet(ctx, p)
	if err != nil {
		return err
	}
	if d.Labels[RuntimeScaleInOperationLabel] != p.OperationID || expected == nil || p.DeploymentUID != expected.Option.DeploymentUID || !reflect.DeepEqual(d.Spec.Template, expected.Template) {
		return fmt.Errorf("scale-in controller owner or template changed")
	}
	if err = s.proveRuntimeScaleInPolicy(ctx, p); err != nil {
		return err
	}
	if err = s.ConfirmRuntimeScaleInAssignmentsEmpty(ctx, p.Namespace, p.SourcePods); err != nil {
		return err
	}
	annotations := d.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[runtimeScaleInResizeOperationAnnotation] = p.OperationID
	patches := []map[string]any{{"op": "test", "path": "/metadata/uid", "value": p.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "replace", "path": "/spec/replicas", "value": p.Replicas}}
	if p.AutomaticIdle && p.Replicas == 0 {
		annotations[runtimeIdleSleepAnnotation] = p.OperationID
		delete(annotations, runtimeIdleWakeAnnotation)
	}
	patches = append(patches, map[string]any{"op": "add", "path": "/metadata/annotations", "value": annotations})
	patch, _ := json.Marshal(patches)
	_, err = s.client.AppsV1().Deployments(p.Namespace).Patch(ctx, p.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func runtimeScaleInResizeOwned(d *appsv1.Deployment, p RuntimePodScaleInProtection) bool {
	marker := d.Annotations[runtimeScaleInResizeOperationAnnotation]
	return marker == p.OperationID || (marker == "" && p.AutomaticIdle && p.Replicas == 0 && d.Annotations[runtimeIdleSleepAnnotation] == p.OperationID)
}

// ObserveInterruptedRuntimeScaleIn admits only a replica write committed by this
// exact operation. A protection label alone also exists before resize, so it
// cannot distinguish an external/manual scale from our accepted CAS.
func (s *runtimeDeploymentService) ObserveInterruptedRuntimeScaleIn(ctx context.Context, p RuntimePodScaleInProtection, expected *RuntimePodScaleSnapshot) (bool, error) {
	if expected == nil || expected.WorkspaceStorageIdentity == "" || expected.Option.DeploymentUID != p.DeploymentUID || expected.Option.Namespace != p.Namespace || expected.Option.DeploymentName != p.DeploymentName || expected.Option.Replicas != p.PreviousReplicas {
		return false, fmt.Errorf("complete original scale-in snapshot required for interrupted recovery")
	}
	d, err := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if !runtimeScaleInResizeOwned(d, p) || d.Generation != expected.DeploymentGeneration+1 {
		return false, fmt.Errorf("interrupted scale-in has no matching operation resize proof")
	}
	p.ExpectedSnapshot = expected
	return s.ObserveRuntimeScaleIn(ctx, p)
}

func (s *runtimeDeploymentService) validateRuntimeScaleInSnapshot(ctx context.Context, d *appsv1.Deployment, expected *RuntimePodScaleSnapshot) error {
	if expected == nil {
		return nil
	} // Existing native callers retain their contract.
	if expected.Option.DeploymentUID != string(d.UID) || expected.Option.Namespace != d.Namespace || expected.Option.DeploymentName != d.Name || expected.Option.RuntimeType != d.Labels["clawmanager.io/runtime-type"] || expected.WorkspaceStorageIdentity == "" || !reflect.DeepEqual(d.Spec.Template, expected.Template) {
		return fmt.Errorf("scale-in original template or storage proof changed")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != expected.WorkspaceStorageIdentity {
		return fmt.Errorf("scale-in original workspace PVC identity changed")
	}
	return nil
}

func (s *runtimeDeploymentService) ObserveRuntimeScaleIn(ctx context.Context, p RuntimePodScaleInProtection) (bool, error) {
	return s.observeRuntimeScaleIn(ctx, p, true)
}
func (s *runtimeDeploymentService) observeRuntimeScaleIn(ctx context.Context, p RuntimePodScaleInProtection, requireProtection bool) (bool, error) {
	if err := validateRuntimeScaleInProof(p); err != nil {
		return false, err
	}
	d, err := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if string(d.UID) != p.DeploymentUID || (d.Labels[RuntimeScaleInOperationLabel] != p.OperationID && (requireProtection || d.Labels[RuntimeScaleInOperationLabel] != "")) || d.Spec.Replicas == nil || *d.Spec.Replicas != p.Replicas || d.Spec.Paused {
		return false, fmt.Errorf("scale-in desired Deployment identity or replica count changed")
	}
	if requireProtection && p.ExpectedSnapshot != nil && !runtimeScaleInResizeOwned(d, p) {
		return false, fmt.Errorf("scale-in resize operation proof changed")
	}
	if err = s.validateRuntimeScaleInSnapshot(ctx, d, p.ExpectedSnapshot); err != nil {
		return false, err
	}
	if err = s.ConfirmRuntimeScaleInAssignmentsEmpty(ctx, p.Namespace, p.SourcePods); err != nil {
		return false, err
	}
	pods, err := s.runtimeScaleInPods(ctx, d)
	if err != nil {
		return false, err
	}
	// Selected identities must really be absent, rather than still existing
	// under an unexpected owner or as a replacement hidden from the native set.
	owned := map[string]bool{}
	for _, pod := range pods {
		owned[string(pod.UID)] = true
	}
	for _, id := range p.SourcePods {
		pod, e := s.client.CoreV1().Pods(p.Namespace).Get(ctx, id.PodName, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			continue
		}
		if e != nil {
			return false, e
		}
		if string(pod.UID) != id.PodUID || !owned[id.PodUID] {
			return false, fmt.Errorf("source Runtime Pod identity or owner changed")
		}
	}
	sources := map[string]string{}
	for _, id := range p.SourcePods {
		sources[id.PodUID] = id.PodName
	}
	retained := map[string]string{}
	for _, id := range p.RetainedPods {
		retained[id.PodUID] = id.PodName
	}
	waiting := false
	for _, pod := range pods {
		if sources[string(pod.UID)] == pod.Name {
			waiting = true
			continue
		}
		if retained[string(pod.UID)] != pod.Name || (requireProtection && pod.Labels[RuntimeScaleInProtectedLabel] != p.OperationID) || pod.DeletionTimestamp != nil {
			return false, fmt.Errorf("retained Runtime set changed during scale-in")
		}
	}
	return !waiting && len(pods) == int(p.Replicas) && d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == p.Replicas && d.Status.UpdatedReplicas == p.Replicas && d.Status.ReadyReplicas == p.Replicas, nil
}

// beforeResize permits rollback only while replicas and the complete sibling
// set still match. After a replica write, protection lasts until observation.
func (s *runtimeDeploymentService) ReleaseRuntimeScaleIn(ctx context.Context, p RuntimePodScaleInProtection, beforeResize bool) error {
	d, err := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if marker := d.Annotations[runtimeScaleInResizeOperationAnnotation]; marker != "" && marker != p.OperationID {
		return fmt.Errorf("scale-in resize operation owner changed during cleanup")
	}
	if string(d.UID) != p.DeploymentUID || d.Spec.Replicas == nil {
		return fmt.Errorf("scale-in controller identity changed")
	}
	if err = s.validateRuntimeScaleInSnapshot(ctx, d, p.ExpectedSnapshot); err != nil {
		return err
	}
	if beforeResize {
		if _, _, err := s.validateRuntimeScaleInSet(ctx, p, false); err != nil {
			return err
		}
		// Fence a replica PATCH whose HTTP response was lost while the API server
		// may still be handling it. This metadata CAS advances resourceVersion:
		// either resize already won (old replicas test fails), or its old RV can
		// never commit after this barrier. A successful GET alone is insufficient.
		annotations := d.Annotations
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[runtimeScaleInCleanupBarrierAnnotation] = p.OperationID + "-" + uuid.NewString()
		barrier, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": p.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "test", "path": "/spec/replicas", "value": p.PreviousReplicas}, {"op": "add", "path": "/metadata/annotations", "value": annotations}})
		if _, err = s.client.AppsV1().Deployments(p.Namespace).Patch(ctx, p.DeploymentName, types.JSONPatchType, barrier, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("unconfirmed replica write cleanup barrier: %w", err)
		}

	} else {
		ready, e := s.observeRuntimeScaleIn(ctx, p, false)
		if e != nil {
			return e
		}
		if !ready {
			return fmt.Errorf("scale-in removal is not yet observed")
		}
	}
	if owner := d.Labels[RuntimeScaleInOperationLabel]; owner != "" && owner != p.OperationID {
		return fmt.Errorf("scale-in owner changed")
	}
	retained := map[string]bool{}
	for _, id := range p.RetainedPods {
		retained[id.PodUID] = true
	}
	for _, id := range append(append([]RuntimePodScaleInIdentity{}, p.SourcePods...), p.RetainedPods...) {
		pod, e := s.client.CoreV1().Pods(p.Namespace).Get(ctx, id.PodName, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			if beforeResize || retained[id.PodUID] {
				return fmt.Errorf("Runtime Pod disappeared during scale-in cleanup")
			}
			continue
		}
		if e != nil {
			return e
		}
		if string(pod.UID) != id.PodUID {
			return fmt.Errorf("Runtime Pod identity changed during scale-in cleanup")
		}
		if owner := pod.Labels[RuntimeScaleInProtectedLabel]; owner != "" && owner != p.OperationID {
			return fmt.Errorf("Runtime Pod protection owner changed")
		}
		labels := pod.Labels
		delete(labels, RuntimeScaleInProtectedLabel)
		annotations := pod.Annotations
		if id.DeletionCost == nil {
			delete(annotations, runtimeDeletionCostAnnotation)
		} else {
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[runtimeDeletionCostAnnotation] = *id.DeletionCost
		}
		patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": id.PodUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": pod.ResourceVersion}, {"op": "add", "path": "/metadata/labels", "value": labels}, {"op": "add", "path": "/metadata/annotations", "value": annotations}})
		if _, e = s.client.CoreV1().Pods(p.Namespace).Patch(ctx, id.PodName, types.JSONPatchType, patch, metav1.PatchOptions{}); e != nil {
			return e
		}
	}
	// Read again because Pod cleanup can take time; never clear another owner's guard.
	d, err = s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if marker := d.Annotations[runtimeScaleInResizeOperationAnnotation]; marker != "" && marker != p.OperationID {
		return fmt.Errorf("scale-in resize operation owner changed during cleanup")
	}
	if string(d.UID) != p.DeploymentUID || (d.Labels[RuntimeScaleInOperationLabel] != "" && d.Labels[RuntimeScaleInOperationLabel] != p.OperationID) {
		return fmt.Errorf("scale-in controller owner changed")
	}
	if !beforeResize {
		if err = s.validateRuntimeScaleInSnapshot(ctx, d, p.ExpectedSnapshot); err != nil {
			return err
		}
		if d.Spec.Replicas == nil || *d.Spec.Replicas != p.Replicas || d.Spec.Paused {
			return fmt.Errorf("scale-in desired replicas changed during cleanup")
		}
	}
	labels := d.Labels
	delete(labels, RuntimeScaleInOperationLabel)
	annotations := d.Annotations
	delete(annotations, runtimeScaleInCleanupBarrierAnnotation)
	delete(annotations, runtimeScaleInResizeOperationAnnotation)
	patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": p.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "add", "path": "/metadata/labels", "value": labels}, {"op": "add", "path": "/metadata/annotations", "value": annotations}})
	_, err = s.client.AppsV1().Deployments(p.Namespace).Patch(ctx, p.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
func (s *runtimeDeploymentService) ConfirmRuntimeScaleInAssignmentsEmpty(ctx context.Context, namespace string, pods []RuntimePodScaleInIdentity) error {
	selected := map[string]bool{}
	for _, id := range pods {
		selected[id.PodUID] = true
	}
	list, err := s.client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if err != nil {
		return err
	}
	for _, cm := range list.Items {
		assignment, state, e := storedRuntimeAssignment(&cm)
		if e != nil {
			return fmt.Errorf("invalid native Runtime assignment: %w", e)
		}
		if selected[assignment.PodUID] && state != "stopped" {
			return fmt.Errorf("source Runtime Pod has a live or pending native assignment")
		}
	}
	return nil
}

func runtimeScaleInDeletionCost(p corev1.Pod) *string {
	if value, ok := p.Annotations[runtimeDeletionCostAnnotation]; ok {
		return &value
	}
	return nil
}
