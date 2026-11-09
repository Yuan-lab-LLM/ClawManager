package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// RuntimeRolloutInventory is separate from the historical registration ledger.
// Inventory is read before any image mutation; API/RBAC errors are not treated
// as an empty pool or permission to fall back to guessed deployment names.
type RuntimeRolloutTarget struct {
	Namespace string
	Name      string
	UID       string
	Image     string
	Ready     bool
	Upgrade   bool
}

type RuntimeRolloutInventory interface {
	RolloutTargets(context.Context, string, string) ([]RuntimeRolloutTarget, error)
}

// Read only the operation's frozen targets during resume/verification. A new,
// unrelated pool must not widen or break an already persisted rollout plan.
type RuntimeRolloutScopedInventory interface {
	RolloutTargetsFor(context.Context, []RuntimeDeploymentRef, string) ([]RuntimeRolloutTarget, error)
}

func (s *runtimeDeploymentService) RolloutTargetsFor(ctx context.Context, refs []RuntimeDeploymentRef, runtimeType string) ([]RuntimeRolloutTarget, error) {
	var result []RuntimeRolloutTarget
	for _, ref := range refs {
		d, err := s.client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect runtime deployment %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		if d.Labels["clawmanager.io/runtime-type"] != runtimeType || !ordinaryRuntimeDeployment(*d) {
			continue
		}
		target, err := runtimeRolloutTarget(*d)
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	return result, nil
}

func ordinaryRuntimeDeployment(d appsv1.Deployment) bool {
	if d.DeletionTimestamp != nil || (d.Spec.Replicas != nil && *d.Spec.Replicas == 0) {
		return false
	}
	meta := d.Labels
	if strings.HasPrefix(d.Name, "openclaw-upgrade-lab-") ||
		meta["clawmanager.io/pool-purpose"] == "openclaw-upgrade-lab" ||
		meta["clawmanager.io/pool-role"] == "upgrade-lab" ||
		meta["clawmanager.io/scheduling-enabled"] == "false" {
		return false
	}
	return meta["clawmanager.io/pool-role"] != "upgrade-target" || meta["clawmanager.io/scheduling-enabled"] == "true"
}

func (s *runtimeDeploymentService) RolloutTargets(ctx context.Context, namespace, runtimeType string) ([]RuntimeRolloutTarget, error) {
	if s == nil || s.client == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(runtimeType) == "" {
		return nil, fmt.Errorf("runtime rollout inventory requires client, namespace and type")
	}
	items, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{"clawmanager.io/runtime-type": runtimeType}.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("inspect live runtime deployments: %w", err)
	}
	result := make([]RuntimeRolloutTarget, 0, len(items.Items))
	for _, d := range items.Items {
		if !ordinaryRuntimeDeployment(d) {
			continue
		}
		target, err := runtimeRolloutTarget(d)
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func runtimeRolloutTarget(d appsv1.Deployment) (RuntimeRolloutTarget, error) {
	image := ""
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == "runtime" {
			image = strings.TrimSpace(c.Image)
		}
	}
	if image == "" {
		return RuntimeRolloutTarget{}, fmt.Errorf("runtime deployment %s/%s has no runtime image", d.Namespace, d.Name)
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	ready := d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == desired && d.Status.Replicas == desired && d.Status.AvailableReplicas == desired
	upgrade := d.Labels["clawmanager.io/pool-role"] == "upgrade-target"
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name != "runtime" {
			continue
		}
		for _, e := range c.Env {
			if e.Name == "CLAWMANAGER_RUNTIME_UPGRADE_ID" && e.Value != "" {
				upgrade = true
			}
		}
	}
	return RuntimeRolloutTarget{Namespace: d.Namespace, Name: d.Name, UID: string(d.UID), Image: image, Ready: ready, Upgrade: upgrade}, nil
}
