package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The caller must supply the snapshot from a COMPLETED automatic scale-in
// journal. Generic scale-out deliberately cannot reactivate zero-replica pools.
const runtimeIdleSleepAnnotation = "clawmanager.io/idle-sleep-operation"
const runtimeIdleWakeAnnotation = "clawmanager.io/idle-wake-operation"

var ErrRuntimeIdlePoolAlreadyActive = errors.New("Runtime pool already has live capacity")

type RuntimeIdlePoolDeploymentService interface {
	IdleRuntimePoolOperations(context.Context, string, string) ([]string, error)
	WakeIdleRuntimePool(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) error
}

// Resolve a bounded set of journal primary keys from current controllers;
// cold starts must not scan and deserialize the full scheduling audit history.
func (s *runtimeDeploymentService) IdleRuntimePoolOperations(ctx context.Context, namespace, runtimeType string) ([]string, error) {
	list, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/runtime-type=" + runtimeType})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, d := range list.Items {
		for _, key := range []string{runtimeIdleSleepAnnotation, runtimeIdleWakeAnnotation} {
			if id := d.Annotations[key]; id != "" {
				seen[id] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *runtimeDeploymentService) idleRuntimeStorageIdentity(ctx context.Context, namespace string, spec corev1.PodSpec) (string, error) {
	for _, v := range spec.Volumes {
		if v.Name != runtimeWorkspaceVolume || v.PersistentVolumeClaim == nil {
			continue
		}
		claim, err := s.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, v.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		identity := runtimeWorkspaceStorageIdentity(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace}, Spec: spec}, claim)
		if identity == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound {
			return "", fmt.Errorf("bound original workspace PVC required")
		}
		return identity, nil
	}
	return "", fmt.Errorf("persistent Runtime workspace volume required")
}

func (s *runtimeDeploymentService) WakeIdleRuntimePool(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot) error {
	if expected == nil || expected.Option.DeploymentUID == "" || expected.WorkspaceStorageIdentity == "" || target.Namespace != expected.Option.Namespace || target.DeploymentName != expected.Option.DeploymentName || target.RuntimeType != expected.Option.RuntimeType {
		return fmt.Errorf("complete automatic sleep snapshot required")
	}
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Check the ordinary role without treating zero replicas as a disabled role.
	active := d.DeepCopy()
	one := int32(1)
	active.Spec.Replicas = &one
	role := d.Labels[runtimePoolRoleLabel]
	if string(d.UID) != expected.Option.DeploymentUID || d.ResourceVersion == "" || d.Spec.Replicas == nil || d.Spec.Paused || !ordinaryRuntimeDeployment(*active) || (role != "" && role != "upgrade-target") || d.Labels[RuntimeScaleInOperationLabel] != "" || d.Labels["clawmanager.io/runtime-type"] != target.RuntimeType || !reflect.DeepEqual(d.Spec.Template, expected.Template) {
		return fmt.Errorf("sleeping Runtime pool identity, serving role or template changed")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != expected.WorkspaceStorageIdentity {
		return fmt.Errorf("sleeping Runtime workspace PVC identity changed")
	}
	if *d.Spec.Replicas > 0 {
		pods, err := s.runtimeScaleInPods(ctx, d)
		if err != nil {
			return err
		}
		for _, p := range pods {
			if p.DeletionTimestamp == nil && runtimeScaleInPodReady(p) {
				return ErrRuntimeIdlePoolAlreadyActive
			}
		}
		return nil // Lost response or another Start is waiting for native capacity.
	}
	if expected.IdleSleepOperation == "" || d.Annotations[runtimeIdleSleepAnnotation] != expected.IdleSleepOperation || d.Generation != expected.DeploymentGeneration+1 {
		return fmt.Errorf("current zero replicas do not belong to this automatic sleep operation")
	}
	pods, err := s.runtimeScaleInPods(ctx, d)
	if err != nil {
		return err
	}
	if len(pods) != 0 {
		return fmt.Errorf("sleeping Runtime pool still has terminating or unexpected Pods")
	}
	annotations := d.Annotations
	delete(annotations, runtimeIdleSleepAnnotation)
	annotations[runtimeIdleWakeAnnotation] = expected.IdleSleepOperation
	patch, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": string(d.UID)}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "replace", "path": "/spec/replicas", "value": one}, {"op": "add", "path": "/metadata/annotations", "value": annotations}})
	_, err = s.client.AppsV1().Deployments(target.Namespace).Patch(ctx, target.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
