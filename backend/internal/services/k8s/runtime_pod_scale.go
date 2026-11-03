package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Only these two fields are accepted at the HTTP boundary. Configuration is
// inherited from the current default Deployment, never supplied by a caller.
type RuntimePodScaleRequest struct {
	RuntimeType string `json:"runtime_type"`
	PodCount    int    `json:"pod_count"`
}

func (r RuntimePodScaleRequest) Validate() error {
	switch r.RuntimeType {
	case "openclaw", "hermes", "opencode", "deepseek-harness":
	default:
		return fmt.Errorf("unsupported runtime_type")
	}
	if r.PodCount < 1 || r.PodCount > 10 {
		return fmt.Errorf("pod_count must be an integer between 1 and 10")
	}
	return nil
}

type RuntimePodScaleOption struct {
	RuntimeType          string                `json:"runtime_type"`
	Namespace            string                `json:"namespace"`
	DeploymentName       string                `json:"deployment_name"`
	DeploymentUID        string                `json:"deployment_uid"`
	Replicas             int32                 `json:"replicas"`
	SlotCount            int                   `json:"slot_count"`
	PodResources         RuntimeResourceBudget `json:"pod_resources"`
	CoordinatorResources RuntimeResourceBudget `json:"coordinator_resources"`
	SlotResources        RuntimeResourceBudget `json:"slot_resources"`
	ImageRef             string                `json:"image_ref"`
}
type RuntimePodScaleResult struct {
	RuntimeType      string `json:"runtime_type"`
	Namespace        string `json:"namespace"`
	DeploymentName   string `json:"deployment_name"`
	DeploymentUID    string `json:"deployment_uid"`
	AddedPods        int    `json:"added_pods"`
	PreviousReplicas int32  `json:"previous_replicas"`
	Replicas         int32  `json:"replicas"`
	SlotCount        int    `json:"slot_count"`
}

// Server-side proof, resolved from the registered live Pod and committed rollout.
type RuntimePodScaleTarget struct {
	RuntimeType, Namespace, DeploymentName, PodName, PodUID string
	UpgradeID, SourceDeployment, ImageRef                   string
}
type RuntimePodScaleSnapshot struct {
	Option           RuntimePodScaleOption
	Template         corev1.PodTemplateSpec
	ResourceVersion  string
	ScaleInOperation string
}
type RuntimePodScaleDeploymentService interface {
	InspectRuntimeScale(context.Context, RuntimePodScaleTarget) (*RuntimePodScaleSnapshot, error)
	ExpandRuntimeScale(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot, int) (*RuntimePodScaleResult, error)
}

func (s *runtimeDeploymentService) InspectRuntimeScale(ctx context.Context, target RuntimePodScaleTarget) (*RuntimePodScaleSnapshot, error) {
	d, p, image, err := s.maintenanceSource(ctx, target.Namespace, target.PodName, target.PodUID)
	if err != nil {
		return nil, err
	}
	if d.Name != target.DeploymentName || d.Labels["clawmanager.io/runtime-type"] != target.RuntimeType || d.UID == "" || d.ResourceVersion == "" || d.Spec.Replicas == nil || *d.Spec.Replicas <= 0 || d.Spec.Paused {
		return nil, fmt.Errorf("current default Deployment identity and positive serving replicas required")
	}
	for _, labels := range []map[string]string{d.Labels, d.Spec.Template.Labels, p.Labels} {
		if labels[runtimeManagedPoolLabel] == "true" || labels[upgradeLabPurposeLabel] == "runtime-managed-pool" {
			return nil, fmt.Errorf("custom managed pools cannot be expanded as the default")
		}
	}
	if target.UpgradeID != "" {
		if d.Labels[runtimeUpgradeIDLabel] != target.UpgradeID || d.Labels[runtimeSourceLabel] != target.SourceDeployment || d.Labels[runtimePoolRoleLabel] != "upgrade-target" || d.Labels[runtimeSchedulingLabel] != "true" {
			return nil, fmt.Errorf("current default upgrade target proof changed")
		}
	} else if d.Labels[runtimePoolRoleLabel] != "" || d.Labels[runtimeUpgradeIDLabel] != "" {
		return nil, fmt.Errorf("default upgrade target requires committed rollout proof")
	}
	spec := d.Spec.Template.Spec
	if len(spec.Containers) != len(p.Spec.Containers) || len(spec.Containers) < 2 {
		return nil, fmt.Errorf("serving Pod does not match current default slot configuration")
	}
	// Admission adds fields to actual Pods. Compare the native contract rather than
	// copying their defaulted PodSpec back into the controller template.
	for i, c := range spec.Containers {
		actual := p.Spec.Containers[i]
		if c.Name != actual.Name || runtimeImageDigest(c.Image) == "" || runtimeImageDigest(c.Image) != runtimeImageDigest(image) || !reflect.DeepEqual(c.Env, actual.Env) {
			return nil, fmt.Errorf("default template native identity or image differs from serving Pod")
		}
		for _, mount := range c.VolumeMounts {
			found := false
			for _, actualMount := range actual.VolumeMounts {
				if reflect.DeepEqual(mount, actualMount) {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("default template mount differs from serving Pod")
			}
		}
	}
	if target.ImageRef != "" && runtimeImageDigest(target.ImageRef) != runtimeImageDigest(image) {
		return nil, fmt.Errorf("serving image differs from committed default rollout")
	}
	for _, volume := range spec.Volumes {
		found := false
		for _, actual := range p.Spec.Volumes {
			if reflect.DeepEqual(volume, actual) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("default template storage differs from serving Pod")
		}
	}
	if err := validateRuntimeSlotTLSDeployment(d); err != nil {
		return nil, err
	}
	resources := spec.Resources.DeepCopy()
	if resources == nil {
		resources = &corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
		for _, c := range spec.Containers {
			for key, q := range c.Resources.Requests {
				sum := resources.Requests[key]
				sum.Add(q)
				resources.Requests[key] = sum
			}
			for key, q := range c.Resources.Limits {
				sum := resources.Limits[key]
				sum.Add(q)
				resources.Limits[key] = sum
			}
		}
	}
	option := RuntimePodScaleOption{RuntimeType: target.RuntimeType, Namespace: d.Namespace, DeploymentName: d.Name, DeploymentUID: string(d.UID), Replicas: *d.Spec.Replicas, SlotCount: len(spec.Containers) - 1, PodResources: runtimeResourceBudget(*resources), CoordinatorResources: runtimeResourceBudget(spec.Containers[0].Resources), SlotResources: runtimeResourceBudget(spec.Containers[1].Resources), ImageRef: spec.Containers[0].Image}
	return &RuntimePodScaleSnapshot{Option: option, Template: *d.Spec.Template.DeepCopy(), ResourceVersion: d.ResourceVersion, ScaleInOperation: d.Labels[RuntimeScaleInOperationLabel]}, nil
}

func (s *runtimeDeploymentService) ExpandRuntimeScale(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot, count int) (*RuntimePodScaleResult, error) {
	if err := (RuntimePodScaleRequest{RuntimeType: target.RuntimeType, PodCount: count}).Validate(); err != nil {
		return nil, err
	}
	if expected == nil {
		return nil, fmt.Errorf("verified default Deployment snapshot required")
	}
	latest, err := s.InspectRuntimeScale(ctx, target)
	if err != nil {
		return nil, err
	}
	if latest.ScaleInOperation != "" {
		return nil, fmt.Errorf("Runtime scale-in is active; replica changes are fenced")
	}
	if latest.Option.DeploymentUID != expected.Option.DeploymentUID || !reflect.DeepEqual(latest.Template, expected.Template) {
		return nil, fmt.Errorf("default Deployment UID or template changed; refresh before retrying")
	}
	before := latest.Option.Replicas
	if int64(before)+int64(count) > math.MaxInt32 {
		return nil, fmt.Errorf("replica count overflow")
	}
	after := before + int32(count)
	patch, err := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": latest.Option.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": latest.ResourceVersion}, {"op": "replace", "path": "/spec/replicas", "value": after}})
	if err != nil {
		return nil, err
	}
	// A transport error can follow an accepted write. Never replay an increment:
	// return the error and leave the live desired replica count observable.
	if _, err = s.client.AppsV1().Deployments(target.Namespace).Patch(ctx, target.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return nil, fmt.Errorf("default Runtime expansion was not confirmed; inspect replicas before retrying: %w", err)
	}
	return &RuntimePodScaleResult{RuntimeType: target.RuntimeType, Namespace: target.Namespace, DeploymentName: target.DeploymentName, DeploymentUID: latest.Option.DeploymentUID, AddedPods: count, PreviousReplicas: before, Replicas: after, SlotCount: latest.Option.SlotCount}, nil
}
