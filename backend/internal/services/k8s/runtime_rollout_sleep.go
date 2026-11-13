package k8s

import (
	"context"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sort"
)

// Separate upgrade inventory keeps historical zero-replica pools out of ordinary
// placement. Only an explicit automatic-sleep marker is a preparation candidate.
type RuntimeRolloutSleepService interface {
	SleepingRuntimePools(context.Context, string, string) ([]RuntimePodScaleTarget, error)
	ValidateRuntimeRolloutSleep(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) (bool, error)
	CaptureRuntimeRolloutSleep(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) (*RuntimePodScaleSnapshot, error)
}

func (s *runtimeDeploymentService) SleepingRuntimePools(ctx context.Context, namespace, runtimeType string) ([]RuntimePodScaleTarget, error) {
	if namespace == "" || runtimeType == "" {
		return nil, fmt.Errorf("Runtime namespace and type required")
	}
	items, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/runtime-type=" + runtimeType})
	if err != nil {
		return nil, err
	}
	result := []RuntimePodScaleTarget{}
	for _, d := range items.Items {
		if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
			continue
		}
		active := d.DeepCopy()
		one := int32(1)
		active.Spec.Replicas = &one
		if !ordinaryRuntimeDeployment(*active) {
			continue
		}
		result = append(result, RuntimePodScaleTarget{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: d.Name, DeploymentUID: string(d.UID), IdlePool: true})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeploymentName < result[j].DeploymentName })
	return result, nil
}

// Read-only proof used both before publication and before every wake retry.
// A resumed wake must carry the marker from the same replica CAS; an unrelated
// manual scale-up is never treated as the operation's successful result.
func (s *runtimeDeploymentService) ValidateRuntimeRolloutSleep(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot) (bool, error) {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return s.validateRuntimeRolloutSleep(ctx, target, expected, d)
}

// Capture records the current validated image/template for a new operation;
// the historical sleep journal remains unchanged.
func (s *runtimeDeploymentService) CaptureRuntimeRolloutSleep(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot) (*RuntimePodScaleSnapshot, error) {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	active, err := s.validateRuntimeRolloutSleep(ctx, target, expected, d)
	if err != nil {
		return nil, err
	}
	current := *expected
	current.Template = *d.Spec.Template.DeepCopy()
	current.Option.ImageRef = runtimeContainerImage(d.Spec.Template.Spec.Containers)
	current.Option.Replicas = *d.Spec.Replicas
	current.ResourceVersion = d.ResourceVersion
	current.DeploymentGeneration = d.Generation - 1
	if active {
		current.DeploymentGeneration--
	}
	current.WakeConfigurationFrozen = true
	return &current, nil
}

func (s *runtimeDeploymentService) validateRuntimeRolloutSleep(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot, d *appsv1.Deployment) (bool, error) {
	if expected == nil || expected.Option.DeploymentUID == "" || expected.WorkspaceStorageIdentity == "" || expected.IdleSleepOperation == "" || expected.DeploymentGeneration < 0 || target.Namespace != expected.Option.Namespace || target.DeploymentName != expected.Option.DeploymentName || target.RuntimeType != expected.Option.RuntimeType {
		return false, fmt.Errorf("complete automatic sleep proof required for upgrade preparation")
	}
	active := d.DeepCopy()
	one := int32(1)
	active.Spec.Replicas = &one
	if string(d.UID) != expected.Option.DeploymentUID || (target.DeploymentUID != "" && target.DeploymentUID != string(d.UID)) || d.Spec.Replicas == nil || d.Spec.Paused || !ordinaryRuntimeDeployment(*active) || d.Labels[RuntimeScaleInOperationLabel] != "" || d.Labels["clawmanager.io/runtime-type"] != target.RuntimeType || !runtimeSleepTemplateMatches(d.Spec.Template, expected) {
		return false, fmt.Errorf("sleeping Runtime identity, role or template changed; upgrade preparation blocked")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return false, err
	}
	if storage != expected.WorkspaceStorageIdentity {
		return false, fmt.Errorf("sleeping Runtime PVC identity changed")
	}
	if *d.Spec.Replicas > 0 {
		// Ordinary capacity can expand after its owned wake. A frozen preparation
		// still requires the exact one-replica result that it admitted.
		if expected.WakeConfigurationFrozen && *d.Spec.Replicas != 1 {
			return false, fmt.Errorf("frozen Runtime preparation replica count changed")
		}
		if d.Annotations[runtimeIdleWakeAnnotation] != expected.IdleSleepOperation || d.Annotations[runtimeIdleSleepAnnotation] != "" || !runtimeSleepGenerationMatches(d.Generation, expected, 2) {
			return false, fmt.Errorf("Runtime wake does not belong to this automatic sleep proof")
		}
		return true, nil
	}
	if *d.Spec.Replicas != 0 || d.Annotations[runtimeIdleSleepAnnotation] != expected.IdleSleepOperation || !runtimeSleepGenerationMatches(d.Generation, expected, 1) {
		return false, fmt.Errorf("zero replicas do not belong to the confirmed automatic sleep operation")
	}
	pods, err := s.runtimeScaleInPods(ctx, d)
	if err != nil {
		return false, err
	}
	if len(pods) != 0 {
		return false, fmt.Errorf("sleeping Runtime has remaining or unexpected Pods")
	}
	return false, nil
}
