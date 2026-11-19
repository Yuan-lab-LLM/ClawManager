package k8s

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const openCodeForceRequestAnnotation = "clawmanager.io/opencode-force-request"

type OpenCodeForceTargetService interface {
	ObserveOpenCodeForceTargetZero(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string) (bool, error)
	ObserveOpenCodeForceTarget(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string, string) (bool, error)
	ScaleOpenCodeForceTargetZero(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string) error
	OpenCodeForceOldPodsGone(context.Context, *OpenCodeUpgradeSource, string) (bool, error)
	ReplaceOpenCodeForceTarget(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string, string) error
}

func normalizedOpenCodeForceAnnotations(values map[string]string, request string) map[string]string {
	out := copyStringMap(values)
	delete(out, "deployment.kubernetes.io/revision")
	if request != "" {
		out[openCodeForceRequestAnnotation] = request
	}
	return out
}
func (s *runtimeDeploymentService) validateOpenCodeForceTarget(ctx context.Context, d *appsv1.Deployment, old *OpenCodeUpgradeSource, spec appsv1.DeploymentSpec, request string) error {
	if old == nil || d == nil || old.Target.UID == "" || string(d.UID) != old.Target.UID || d.Name != old.Target.Name || d.Namespace != old.Target.Namespace || d.DeletionTimestamp != nil || len(d.OwnerReferences) != 0 || d.Spec.Paused || !apiequality.Semantic.DeepEqual(d.Spec, spec) || !apiequality.Semantic.DeepEqual(d.Labels, old.Labels) || d.Labels[runtimeSchedulingLabel] != "false" {
		return fmt.Errorf("OpenCode force target UID/template/standby identity changed")
	}
	want := normalizedOpenCodeForceAnnotations(old.Annotations, request)
	if !apiequality.Semantic.DeepEqual(normalizedOpenCodeForceAnnotations(d.Annotations, ""), want) {
		return fmt.Errorf("OpenCode force target annotation ownership changed")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != old.StorageIdentity {
		return fmt.Errorf("OpenCode force target persistent storage changed")
	}
	return nil
}
func (s *runtimeDeploymentService) validateOpenCodeForceSource(ctx context.Context, source, old *OpenCodeUpgradeSource, operation string) error {
	if source == nil || old == nil || operation == "" || old.Replicas < 1 || len(old.PodUIDs) == 0 {
		return fmt.Errorf("OpenCode force target proof incomplete")
	}
	if err := s.ValidateOpenCodeUpgradeSource(ctx, source); err != nil {
		return err
	}
	id, e := strconv.ParseInt(operation, 10, 64)
	if e != nil || id < 1 || strconv.FormatInt(id, 10) != operation {
		return fmt.Errorf("OpenCode force rollout identity invalid")
	}
	name := strings.Trim(strings.ToLower(strings.TrimSpace(source.Target.Name)), "-")
	suffix := "-u" + operation
	if len(name) > 63-len(suffix) {
		name = strings.TrimRight(name[:63-len(suffix)], "-")
	}
	if old.Target.Namespace != source.Target.Namespace || old.Target.Name != name+suffix {
		return fmt.Errorf("OpenCode force target namespace/name differs from source rollout")
	}
	current, e := s.client.AppsV1().Deployments(source.Target.Namespace).Get(ctx, source.Target.Name, metav1.GetOptions{})
	if e != nil {
		return e
	}
	if e = s.validateOpenCodeSchedulingIdentity(ctx, current, source, ""); e != nil {
		return e
	}
	if current.Labels[runtimeSchedulingLabel] != source.Labels[runtimeSchedulingLabel] {
		return fmt.Errorf("OpenCode force source scheduling changed")
	}

	expected, err := expectedOpenCodeUpgradePool(source, old.Target.Name, old.Target.Image, operation)
	if err != nil {
		return err
	}
	if !apiequality.Semantic.DeepEqual(expected.Spec, old.Spec) || !apiequality.Semantic.DeepEqual(expected.Labels, old.Labels) {
		return fmt.Errorf("OpenCode force target differs from frozen source transform")
	}
	for key, value := range expected.Annotations {
		if old.Annotations[key] != value {
			return fmt.Errorf("OpenCode force source lineage changed")
		}
	}
	return nil
}
func (s *runtimeDeploymentService) ScaleOpenCodeForceTargetZero(ctx context.Context, source, old *OpenCodeUpgradeSource, operation, request string) error {
	if request == "" {
		return fmt.Errorf("OpenCode force request required")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := s.validateOpenCodeForceSource(ctx, source, old, operation); err != nil {
			return err
		}
		d, err := s.client.AppsV1().Deployments(old.Target.Namespace).Get(ctx, old.Target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		zero := int32(0)
		zeroSpec := old.Spec.DeepCopy()
		zeroSpec.Replicas = &zero
		if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
			return s.validateOpenCodeForceTarget(ctx, d, old, *zeroSpec, request)
		}
		if err = s.validateOpenCodeForceTarget(ctx, d, old, old.Spec, ""); err != nil {
			return err
		}
		pods, err := s.orphanOwnedPods(ctx, d)
		if err != nil {
			return err
		}
		if len(pods) != len(old.PodUIDs) {
			return fmt.Errorf("OpenCode force old Pod inventory changed")
		}
		known := map[string]bool{}
		for _, uid := range old.PodUIDs {
			known[uid] = true
		}
		for _, p := range pods {
			if !known[string(p.UID)] {
				return fmt.Errorf("OpenCode force old Pod UID changed")
			}
		}
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[openCodeForceRequestAnnotation] = request
		d.Spec.Replicas = &zero
		_, err = s.client.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		return err
	})
}
func (s *runtimeDeploymentService) openCodeForceFrozenPodsGone(ctx context.Context, old *OpenCodeUpgradeSource) (bool, error) {
	pods, err := s.client.CoreV1().Pods(old.Target.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	known := map[string]bool{}
	for _, uid := range old.PodUIDs {
		known[uid] = true
	}
	for _, p := range pods.Items {
		if known[string(p.UID)] {
			return false, nil
		}
		if p.Labels["app"] == old.Target.Name && runtimeContainerImage(p.Spec.Containers) == old.Target.Image {
			return false, nil
		}
	}
	return true, nil
}
func (s *runtimeDeploymentService) OpenCodeForceOldPodsGone(ctx context.Context, old *OpenCodeUpgradeSource, request string) (bool, error) {
	if old == nil {
		return false, fmt.Errorf("OpenCode force old proof missing")
	}
	d, err := s.client.AppsV1().Deployments(old.Target.Namespace).Get(ctx, old.Target.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	zero := int32(0)
	spec := old.Spec.DeepCopy()
	spec.Replicas = &zero
	if err = s.validateOpenCodeForceTarget(ctx, d, old, *spec, request); err != nil {
		return false, err
	}
	gone, err := s.openCodeForceFrozenPodsGone(ctx, old)
	if err != nil || !gone {
		return gone, err
	}
	if err = s.orphanControllerConverged(ctx, d); err != nil {
		if errors.Is(err, ErrRuntimeFaultEvictionPending) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
func (s *runtimeDeploymentService) ReplaceOpenCodeForceTarget(ctx context.Context, source, old *OpenCodeUpgradeSource, image, operation, request string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if old == nil || request == "" || runtimeImageDigest(image) == "" || image == old.Target.Image {
			return fmt.Errorf("OpenCode force new immutable image required")
		}
		if err := s.validateOpenCodeForceSource(ctx, source, old, operation); err != nil {
			return err
		}
		expected, err := expectedOpenCodeUpgradePool(source, old.Target.Name, image, operation)
		if err != nil {
			return err
		}
		d, err := s.client.AppsV1().Deployments(old.Target.Namespace).Get(ctx, old.Target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err = s.validateOpenCodeForceTarget(ctx, d, old, expected.Spec, request); err == nil {
			gone, e := s.openCodeForceFrozenPodsGone(ctx, old)
			if e != nil {
				return e
			}
			if !gone {
				return fmt.Errorf("OpenCode force old Pod still exists")
			}
			return nil
		}
		gone, err := s.OpenCodeForceOldPodsGone(ctx, old, request)
		if err != nil {
			return err
		}
		if !gone {
			return fmt.Errorf("%w: OpenCode force waiting for all old Pod UIDs and ReplicaSets to exit", ErrRuntimeFaultEvictionPending)
		}
		// The exact GET object/resourceVersion must still be the proved old zero.
		zero := int32(0)
		zeroSpec := old.Spec.DeepCopy()
		zeroSpec.Replicas = &zero
		if err = s.validateOpenCodeForceTarget(ctx, d, old, *zeroSpec, request); err != nil {
			return err
		}
		d.Spec = *expected.Spec.DeepCopy()
		_, err = s.client.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		return err
	})
}

// After replacement, observe the exact new template without requiring the old
// zero replica state. This also recovers a committed Update with a lost reply.
func (s *runtimeDeploymentService) ObserveOpenCodeForceTarget(ctx context.Context, source, old *OpenCodeUpgradeSource, image, operation, request string) (bool, error) {
	if err := s.validateOpenCodeForceSource(ctx, source, old, operation); err != nil {
		return false, err
	}
	expected, err := expectedOpenCodeUpgradePool(source, old.Target.Name, image, operation)
	if err != nil {
		return false, err
	}
	d, err := s.client.AppsV1().Deployments(old.Target.Namespace).Get(ctx, old.Target.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if err = s.validateOpenCodeForceTarget(ctx, d, old, expected.Spec, request); err != nil {
		return false, err
	}
	return s.openCodeForceFrozenPodsGone(ctx, old)
}

// Distinguish a first zero submission from an already committed zero before
// re-observing an endpoint that may legitimately have disappeared.
func (s *runtimeDeploymentService) ObserveOpenCodeForceTargetZero(ctx context.Context, source, old *OpenCodeUpgradeSource, operation, request string) (bool, error) {
	if err := s.validateOpenCodeForceSource(ctx, source, old, operation); err != nil {
		return false, err
	}
	d, err := s.client.AppsV1().Deployments(old.Target.Namespace).Get(ctx, old.Target.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
		spec := old.Spec.DeepCopy()
		zero := int32(0)
		spec.Replicas = &zero
		return true, s.validateOpenCodeForceTarget(ctx, d, old, *spec, request)
	}
	return false, s.validateOpenCodeForceTarget(ctx, d, old, old.Spec, "")
}
