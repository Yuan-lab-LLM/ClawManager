package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"time"
)

const coldSourceUIDAnnotation = "clawmanager.io/cold-source-uid"
const coldSourceHashAnnotation = "clawmanager.io/cold-source-template"

// This is an internal controller boundary, never a caller-supplied Pod template.
type RuntimeColdSuccessorService interface {
	ColdRuntimeStandbyReady(context.Context, RuntimeRolloutTarget, string) (bool, error)
	InspectColdRuntimeSuccessor(context.Context, RuntimeRolloutTarget, RuntimeRolloutTarget, string, string) error
	RetireColdRuntimeSuccessor(context.Context, RuntimeRolloutTarget, string, string, string, string) (bool, error)
	InspectRetiredRuntimePool(context.Context, RuntimeRolloutTarget, string) (string, error)
	CheckColdRuntimeExternalWriters(context.Context, *RuntimeOrphanSource) error
	EnsureColdRuntimeSuccessor(context.Context, RuntimeRolloutTarget, string, string, string, string) (*RuntimeRolloutTarget, error)
}

func (s *runtimeDeploymentService) CheckColdRuntimeExternalWriters(ctx context.Context, source *RuntimeOrphanSource) error {
	if source == nil || source.SourcePod == nil || source.ReplicaSetUID == "" {
		return fmt.Errorf("original retired controller capsule required")
	}
	d, err := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, source.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	rs, err := s.client.AppsV1().ReplicaSets(source.Namespace).Get(ctx, source.ReplicaSetName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	owner := metav1.GetControllerOf(rs)
	if string(d.UID) != source.DeploymentUID || string(rs.UID) != source.ReplicaSetUID || owner == nil || string(owner.UID) != source.DeploymentUID || owner.Kind != "Deployment" {
		return fmt.Errorf("original retired ReplicaSet ownership changed")
	}
	original, current := rs.Spec.Template.DeepCopy(), d.Spec.Template.DeepCopy()
	delete(original.Labels, "pod-template-hash")
	delete(current.Labels, "pod-template-hash")
	if !reflect.DeepEqual(original, current) {
		return fmt.Errorf("retired default template differs from its original ReplicaSet")
	}
	storage, err := RuntimeFaultStorageIdentity(ctx, s.client, source.SourcePod, source.WorkspaceRoot, time.Time{})
	if err != nil {
		return err
	}
	if storage != source.StorageDigest {
		return fmt.Errorf("retired default storage differs from its physical retirement proof")
	}
	return CheckRuntimeOrphanExternalWriters(ctx, s.client, source)
}

func (s *runtimeDeploymentService) InspectRetiredRuntimePool(ctx context.Context, source RuntimeRolloutTarget, operation string) (string, error) {
	d, err := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, source.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return s.inspectRetiredRuntimePoolSnapshot(ctx, source, operation, d)
}
func (s *runtimeDeploymentService) inspectRetiredRuntimePoolSnapshot(ctx context.Context, source RuntimeRolloutTarget, operation string, d *appsv1.Deployment) (string, error) {
	var err error
	if source.UID == "" || operation == "" || string(d.UID) != source.UID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.Labels[runtimeSchedulingLabel] != "false" || d.Annotations[orphanOperation] != operation || d.Labels["clawmanager.io/runtime-type"] != "openclaw" || runtimeContainerImage(d.Spec.Template.Spec.Containers) != source.Image || runtimeImageDigest(source.Image) == "" {
		return "", fmt.Errorf("approved cold source retirement identity changed")
	}
	hpas, err := s.client.AutoscalingV2().HorizontalPodAutoscalers(source.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, h := range hpas.Items {
		if h.Spec.ScaleTargetRef.Kind == "Deployment" && h.Spec.ScaleTargetRef.Name == source.Name {
			return "", fmt.Errorf("retired default has an active autoscaler")
		}
	}
	if err = s.orphanControllerConverged(ctx, d); err != nil {
		return "", err
	}
	pods, err := s.orphanOwnedPods(ctx, d)
	if err != nil {
		return "", err
	}
	if len(pods) != 0 {
		return "", fmt.Errorf("retired default still owns Pods")
	}
	spec := d.Spec.Template.Spec
	if len(spec.Containers) != 9 || d.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || d.Spec.Template.Labels["clawmanager.io/runtime-security"] != RuntimeNetworkSecurityProfile || spec.HostUsers == nil || *spec.HostUsers {
		return "", fmt.Errorf("cold source requires the approved eight isolated native slots")
	}
	for _, c := range spec.Containers {
		if c.Image != source.Image {
			return "", fmt.Errorf("cold source slot image differs from approved image")
		}
	}
	if err = validateRuntimeSlotTLSDeployment(d); err != nil {
		return "", err
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, spec)
	if err != nil {
		return "", err
	}
	root := ""
	for _, c := range spec.Containers {
		if c.Name == "runtime" {
			for _, e := range c.Env {
				if e.Name == "RUNTIME_WORKSPACE_ROOT" && e.ValueFrom == nil {
					root = e.Value
				}
			}
		}
	}
	physical, err := RuntimeFaultStorageIdentity(ctx, s.client, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: d.Namespace}, Spec: *spec.DeepCopy()}, root, time.Time{})
	if err != nil {
		return "", err
	}
	storage += "\n" + physical

	raw, err := json.Marshal(struct {
		Template corev1.PodTemplateSpec
		Storage  string
	}{d.Spec.Template, storage})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *runtimeDeploymentService) EnsureColdRuntimeSuccessor(ctx context.Context, source RuntimeRolloutTarget, hash, operation, name, id string) (*RuntimeRolloutTarget, error) {
	actual, err := s.InspectRetiredRuntimePool(ctx, source, operation)
	if err != nil {
		return nil, err
	}
	if hash == "" || actual != hash {
		return nil, fmt.Errorf("cold source template or storage changed")
	}
	if err = s.ensureUpgradePool(ctx, source.Namespace, source.Name, name, source.Image, id, &runtimeColdClone{Source: source, Hash: hash, Operation: operation}); err != nil {
		return nil, err
	}
	d, err := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	target, err := runtimeRolloutTarget(*d)
	return &target, err
}

type runtimeColdClone struct {
	Source    RuntimeRolloutTarget
	Hash      string
	Operation string
}

// Cleanup is scoped to the successor's immutable plan even if the source was
// changed after admission. It never calls source restoration or scheduling.
func (s *runtimeDeploymentService) RetireColdRuntimeSuccessor(ctx context.Context, source RuntimeRolloutTarget, hash, name, id, uid string) (bool, error) {
	if name == "" || name == source.Name || hash == "" || id == "" {
		return false, fmt.Errorf("complete distinct cold successor cleanup identity required")
	}
	d, err := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if (uid != "" && string(d.UID) != uid) || d.Labels[runtimeSourceLabel] != source.Name || d.Labels[runtimeUpgradeIDLabel] != id || d.Labels[runtimePoolRoleLabel] != "upgrade-target" || d.Annotations[coldSourceUIDAnnotation] != source.UID || d.Annotations[coldSourceHashAnnotation] != hash {
		return false, fmt.Errorf("cold cleanup target ownership changed")
	}
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
		patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"resourceVersion": d.ResourceVersion, "uid": d.UID}, "spec": map[string]any{"replicas": 0}})
		_, err = s.client.AppsV1().Deployments(source.Namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		return false, err
	}
	pods, err := s.orphanOwnedPods(ctx, d)
	if err != nil {
		return false, err
	}
	if len(pods) > 0 {
		return false, nil
	}
	policy := metav1.DeletePropagationBackground
	err = s.client.AppsV1().Deployments(source.Namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &d.UID, ResourceVersion: &d.ResourceVersion}})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	return true, nil
}
func (s *runtimeDeploymentService) InspectColdRuntimeSuccessor(ctx context.Context, target, source RuntimeRolloutTarget, hash, id string) error {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if target.Name == source.Name || target.UID == "" || string(d.UID) != target.UID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas != 1 || d.Labels[runtimeSourceLabel] != source.Name || d.Labels[runtimeUpgradeIDLabel] != id || d.Labels[runtimePoolRoleLabel] != "upgrade-target" || d.Annotations[coldSourceUIDAnnotation] != source.UID || d.Annotations[coldSourceHashAnnotation] != hash || runtimeContainerImage(d.Spec.Template.Spec.Containers) != source.Image {
		return fmt.Errorf("cold successor immutable controller proof changed")
	}
	return s.verifyColdSuccessorTemplate(ctx, d, source, hash, "", id)
}

func (s *runtimeDeploymentService) verifyColdSuccessorTemplate(ctx context.Context, target *appsv1.Deployment, source RuntimeRolloutTarget, hash, operation, id string) error {
	d, err := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, source.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if operation == "" {
		operation = d.Annotations[orphanOperation]
	}
	actual, err := s.inspectRetiredRuntimePoolSnapshot(ctx, source, operation, d)
	if err != nil {
		return err
	}
	if actual != hash {
		return fmt.Errorf("cold source template or storage changed")
	}
	expected, err := buildUpgradeRuntimePool(d, source.Namespace, source.Name, target.Name, source.Image, id, &runtimeColdClone{Source: source, Hash: hash, Operation: operation})
	if err != nil {
		return err
	}
	if target.Name == source.Name || target.DeletionTimestamp != nil || target.Spec.Paused || target.Spec.Replicas == nil || *target.Spec.Replicas != 1 || target.Labels[runtimePoolRoleLabel] != "upgrade-target" || target.Labels[runtimeUpgradeIDLabel] != id || target.Labels[runtimeSourceLabel] != source.Name || target.Annotations[coldSourceUIDAnnotation] != source.UID || target.Annotations[coldSourceHashAnnotation] != hash || !reflect.DeepEqual(target.Spec.Template, expected.Spec.Template) || !reflect.DeepEqual(target.Spec.Selector, expected.Spec.Selector) {
		return fmt.Errorf("cold successor native template differs from approved source transformation")
	}
	return nil
}

// Standby deliberately fails public /readyz until activation. Prove every
// native worker is running independently of that coordinator readiness gate.
func (s *runtimeDeploymentService) ColdRuntimeStandbyReady(ctx context.Context, target RuntimeRolloutTarget, podUID string) (bool, error) {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if target.UID == "" || string(d.UID) != target.UID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas != 1 || d.Labels[runtimePoolRoleLabel] != "upgrade-target" || d.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
		return false, fmt.Errorf("cold standby controller identity changed")
	}
	pods, err := s.orphanOwnedPods(ctx, d)
	if err != nil {
		return false, err
	}
	if len(pods) != 1 {
		return false, nil
	}
	p := pods[0]
	if podUID == "" || string(p.UID) != podUID || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || len(p.Spec.Containers) != 9 || len(p.Status.ContainerStatuses) != 9 {
		return false, nil
	}
	statuses := map[string]corev1.ContainerStatus{}
	for _, st := range p.Status.ContainerStatuses {
		if _, exists := statuses[st.Name]; exists {
			return false, fmt.Errorf("cold standby native worker identity is ambiguous")
		}
		statuses[st.Name] = st
	}
	for i, c := range p.Spec.Containers {
		name := "runtime"
		if i > 0 {
			name = fmt.Sprintf("slot-%d", i-1)
		}
		st, ok := statuses[name]
		if c.Name != name || c.Image != target.Image || !ok || st.State.Running == nil || (i > 0 && !st.Ready) {
			return false, nil
		}
	}
	return true, nil
}
