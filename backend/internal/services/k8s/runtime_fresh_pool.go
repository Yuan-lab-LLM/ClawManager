package k8s

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const RuntimeFreshPoolPurposePrefix = "fresh-instance-"
const freshPoolSourceUID = "clawmanager.io/fresh-source-uid"
const freshPoolActivationUID = "clawmanager.io/fresh-activation-uid"
const freshPoolActivationImage = "clawmanager.io/fresh-activation-image"
const freshPoolActivationGeneration = "clawmanager.io/fresh-activation-generation"

type FreshRuntimePoolScope struct {
	Source           RuntimePodScaleTarget
	SourceGeneration int64
	Name, Image      string
	InstanceID       int
}

type FreshRuntimePoolDeploymentService interface {
	EnsureFreshRuntimePool(context.Context, FreshRuntimePoolScope) (*appsv1.Deployment, error)
	ActivateFreshRuntimePool(context.Context, FreshRuntimePoolScope, string) error
}

func (s *runtimeDeploymentService) freshRuntimeTemplates(ctx context.Context, scope FreshRuntimePoolScope) (*appsv1.Deployment, error) {
	source, err := s.client.AppsV1().Deployments(scope.Source.Namespace).Get(ctx, scope.Source.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if source.UID == "" || string(source.UID) != scope.Source.DeploymentUID || scope.SourceGeneration < 1 || source.Generation != scope.SourceGeneration || source.DeletionTimestamp != nil || source.Labels["clawmanager.io/runtime-type"] != scope.Source.RuntimeType || scope.InstanceID < 1 || scope.Name == "" || scope.Name == source.Name || source.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
		return nil, fmt.Errorf("fresh Runtime source identity or protocol changed")
	}
	coordinator := runtimeContainerImage(source.Spec.Template.Spec.Containers)
	if runtimeImageDigest(scope.Image) == "" || runtimeImageDigest(coordinator) == "" || strings.SplitN(scope.Image, "@", 2)[0] != strings.SplitN(coordinator, "@", 2)[0] {
		return nil, fmt.Errorf("fresh Runtime image requires the approved source repository and an immutable digest")
	}
	purpose := RuntimeFreshPoolPurposePrefix + strconv.Itoa(scope.InstanceID)
	target, err := buildUpgradeRuntimePool(source, source.Namespace, source.Name, scope.Name, scope.Image, purpose, nil)
	if err != nil {
		return nil, err
	}
	one := int32(1)
	target.Spec.Replicas = &one
	target.Annotations = map[string]string{freshPoolSourceUID: scope.Source.DeploymentUID}
	for _, labels := range []map[string]string{target.Labels, target.Spec.Template.Labels, target.Spec.Selector.MatchLabels} {
		delete(labels, runtimeUpgradeIDLabel)
		delete(labels, runtimePoolRoleLabel)
		delete(labels, runtimeSourceLabel)
		labels[upgradeLabPurposeLabel] = purpose
	}
	for i := range target.Spec.Template.Spec.TopologySpreadConstraints {
		target.Spec.Template.Spec.TopologySpreadConstraints[i].LabelSelector = target.Spec.Selector.DeepCopy()
	}
	for i := range target.Spec.Template.Spec.Containers {
		removeEnvVar(&target.Spec.Template.Spec.Containers[i], "CLAWMANAGER_RUNTIME_UPGRADE_ID")
	}
	return target, nil
}

func freshRuntimeMatches(current, desired *appsv1.Deployment) bool {
	return current.UID != "" && current.DeletionTimestamp == nil && current.Annotations[freshPoolSourceUID] == desired.Annotations[freshPoolSourceUID] && current.Labels[upgradeLabPurposeLabel] == desired.Labels[upgradeLabPurposeLabel] && current.Labels["clawmanager.io/runtime-type"] == desired.Labels["clawmanager.io/runtime-type"] && apiequality.Semantic.DeepEqual(current.Spec, desired.Spec)
}

func (s *runtimeDeploymentService) EnsureFreshRuntimePool(ctx context.Context, scope FreshRuntimePoolScope) (*appsv1.Deployment, error) {
	desired, err := s.freshRuntimeTemplates(ctx, scope)
	if err != nil {
		return nil, err
	}
	deployments := s.client.AppsV1().Deployments(scope.Source.Namespace)
	current, err := deployments.Get(ctx, scope.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return deployments.Create(ctx, desired, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	if !freshRuntimeMatches(current, desired) {
		return nil, fmt.Errorf("fresh Runtime controller has a different scope or template")
	}
	return current, nil
}

func (s *runtimeDeploymentService) ActivateFreshRuntimePool(ctx context.Context, scope FreshRuntimePoolScope, uid string) error {
	desired, err := s.freshRuntimeTemplates(ctx, scope)
	if err != nil {
		return err
	}
	deployments := s.client.AppsV1().Deployments(scope.Source.Namespace)
	current, err := deployments.Get(ctx, scope.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !freshRuntimeMatches(current, desired) || string(current.UID) != uid || current.Status.ReadyReplicas != 1 || current.Status.ObservedGeneration < current.Generation {
		return fmt.Errorf("fresh Runtime controller changed or is not ready")
	}
	if freshRuntimeActivated(current) && current.Annotations[freshPoolActivationImage] == scope.Image {
		return nil
	}
	current.Annotations[freshPoolActivationUID] = uid
	current.Annotations[freshPoolActivationImage] = scope.Image
	current.Annotations[freshPoolActivationGeneration] = strconv.FormatInt(current.Generation+1, 10)
	current.Labels[runtimeSchedulingLabel] = "true"
	_, err = deployments.Update(ctx, current, metav1.UpdateOptions{})
	return err
}

// FreshRuntimePreviewImage joins the running Pod's native identity with the
// separately admitted immutable image, within the one approved member's scope.
func FreshRuntimePreviewImage(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, instanceID int, runtimeType string) (string, error) {
	purpose := pod.Labels[upgradeLabPurposeLabel]
	if !strings.HasPrefix(purpose, RuntimeFreshPoolPurposePrefix) {
		return "", nil
	}
	if instanceID < 1 || purpose != RuntimeFreshPoolPurposePrefix+strconv.Itoa(instanceID) {
		return "", fmt.Errorf("fresh Runtime is outside the authorized instance scope")
	}
	s := &runtimeDeploymentService{client: client}
	owner, err := s.ResolveRuntimePodController(ctx, pod.Namespace, pod.Name, string(pod.UID))
	if err != nil || owner.RuntimeType != runtimeType {
		return "", fmt.Errorf("fresh Runtime controller identity is unavailable")
	}
	d, err := client.AppsV1().Deployments(pod.Namespace).Get(ctx, owner.DeploymentName, metav1.GetOptions{})
	if err != nil || string(d.UID) != owner.DeploymentUID || !freshRuntimeActivated(d) || d.Labels[upgradeLabPurposeLabel] != purpose || d.Spec.Template.Labels[upgradeLabPurposeLabel] != purpose {
		return "", fmt.Errorf("fresh Runtime has no matching active approval")
	}
	return runtimeContainerImage(d.Spec.Template.Spec.Containers), nil
}

func freshRuntimeActivated(d *appsv1.Deployment) bool {
	if d.UID == "" || d.DeletionTimestamp != nil || d.Annotations[freshPoolSourceUID] == "" || d.Annotations[freshPoolActivationUID] != string(d.UID) || d.Labels[runtimeSchedulingLabel] != "true" {
		return false
	}
	image := runtimeContainerImage(d.Spec.Template.Spec.Containers)
	if runtimeImageDigest(image) == "" || len(d.Spec.Template.Spec.Containers) < 2 {
		return false
	}
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Image != image {
			return false
		}
	}
	if approved := d.Annotations[freshPoolActivationImage]; approved != "" {
		return approved == image && d.Annotations[freshPoolActivationGeneration] == strconv.FormatInt(d.Generation, 10)
	}
	// Initial deployments made before the digest annotation used generation 1
	// for prepare and generation 2 for UID-only activation. A later spec change
	// cannot inherit that legacy approval.
	return d.Generation == 2
}
