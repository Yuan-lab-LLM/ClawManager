package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Immutable source configuration is separate from scheduling labels, which the
// controller changes only after all preservation receipts have committed.
type OpenCodeUpgradeSource struct {
	Spec            appsv1.DeploymentSpec  `json:"spec"`
	Labels          map[string]string      `json:"labels"`
	PodUIDs         []string               `json:"pod_uids"`
	Target          RuntimeRolloutTarget   `json:"target"`
	Template        corev1.PodTemplateSpec `json:"template"`
	TemplateSHA256  string                 `json:"template_sha256"`
	StorageIdentity string                 `json:"storage_identity"`
	Replicas        int32                  `json:"replicas"`
}
type OpenCodeUpgradeDeploymentService interface {
	EnsureOpenCodeUpgradePool(context.Context, *OpenCodeUpgradeSource, string, string, string) (RuntimeRolloutTarget, error)
	ValidateOpenCodeUpgradeTarget(context.Context, *OpenCodeUpgradeSource, RuntimeRolloutTarget, string) error
	CaptureOpenCodeUpgradeSource(context.Context, RuntimeRolloutTarget) (*OpenCodeUpgradeSource, error)
	ValidateOpenCodeUpgradeSource(context.Context, *OpenCodeUpgradeSource) error
	InspectOpenCodeUpgradeTarget(context.Context, string, string, string, string) (RuntimeRolloutTarget, error)
	ColdRuntimeStandbyReady(context.Context, RuntimeRolloutTarget, string) (bool, error)
	RetireOpenCodeUpgradeSource(context.Context, *OpenCodeUpgradeSource, string) error
	OpenCodeUpgradeSourceRetired(context.Context, *OpenCodeUpgradeSource, string) (bool, error)
}

func (s *runtimeDeploymentService) CaptureOpenCodeUpgradeSource(ctx context.Context, t RuntimeRolloutTarget) (*OpenCodeUpgradeSource, error) {
	d, err := s.client.AppsV1().Deployments(t.Namespace).Get(ctx, t.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if t.UID == "" || string(d.UID) != t.UID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas < 1 || d.Labels[RuntimeScaleInOperationLabel] != "" || d.Labels["clawmanager.io/runtime-type"] != "opencode" || runtimeContainerImage(d.Spec.Template.Spec.Containers) != t.Image || runtimeImageDigest(t.Image) == "" || d.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || d.Spec.Template.Labels["clawmanager.io/sandbox-profile"] != RuntimeNFSRootlessProfile {
		return nil, fmt.Errorf("OpenCode source controller/slot/image identity is incomplete or changed")
	}
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Image != t.Image {
			return nil, fmt.Errorf("OpenCode source contains a different worker image")
		}
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return nil, err
	}
	if storage == "" {
		return nil, fmt.Errorf("OpenCode source workspace storage identity is missing")
	}
	pods, err := s.orphanOwnedPods(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(pods) != int(*d.Spec.Replicas) {
		return nil, fmt.Errorf("OpenCode source has no exact positive Pod inventory")
	}
	podUIDs := []string{}
	for _, pod := range pods {
		if pod.UID == "" || pod.DeletionTimestamp != nil {
			return nil, fmt.Errorf("OpenCode source Pod identity changed")
		}
		actualStorage, e := s.idleRuntimeStorageIdentity(ctx, pod.Namespace, pod.Spec)
		if e != nil {
			return nil, e
		}
		if actualStorage != storage {
			return nil, fmt.Errorf("OpenCode source Pod storage differs from frozen controller")
		}
		for _, container := range pod.Spec.Containers {
			if container.Image != t.Image {
				return nil, fmt.Errorf("OpenCode source Pod worker image differs from controller")
			}
		}
		podUIDs = append(podUIDs, string(pod.UID))
	}
	sort.Strings(podUIDs)
	raw, err := json.Marshal(d.Spec.Template)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &OpenCodeUpgradeSource{Spec: *d.Spec.DeepCopy(), Labels: copyStringMap(d.Labels), PodUIDs: podUIDs, Target: t, Template: *d.Spec.Template.DeepCopy(), TemplateSHA256: hex.EncodeToString(sum[:]), StorageIdentity: storage, Replicas: *d.Spec.Replicas}, nil
}
func (s *runtimeDeploymentService) ValidateOpenCodeUpgradeSource(ctx context.Context, p *OpenCodeUpgradeSource) error {
	if p == nil {
		return fmt.Errorf("OpenCode source proof is missing")
	}
	current, err := s.CaptureOpenCodeUpgradeSource(ctx, p.Target)
	if err != nil {
		return err
	}
	if current.TemplateSHA256 != p.TemplateSHA256 || current.StorageIdentity != p.StorageIdentity || current.Replicas != p.Replicas || !apiequality.Semantic.DeepEqual(current.Spec, p.Spec) || !apiequality.Semantic.DeepEqual(current.PodUIDs, p.PodUIDs) {
		return fmt.Errorf("OpenCode source template, replicas or workspace storage changed")
	}
	return nil
}
func (s *runtimeDeploymentService) InspectOpenCodeUpgradeTarget(ctx context.Context, namespace, name, image, upgradeID string) (RuntimeRolloutTarget, error) {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return RuntimeRolloutTarget{}, err
	}
	if d.UID == "" || d.Labels["clawmanager.io/runtime-type"] != "opencode" || d.Labels[runtimePoolRoleLabel] != "upgrade-target" || d.Labels[runtimeUpgradeIDLabel] != upgradeID || strings.TrimSpace(image) == "" || runtimeContainerImage(d.Spec.Template.Spec.Containers) != image {
		return RuntimeRolloutTarget{}, fmt.Errorf("OpenCode target controller identity differs from frozen plan")
	}
	return runtimeRolloutTarget(*d)
}

const openCodeRetirementAnnotation = "clawmanager.io/opencode-upgrade-retirement"

// Retire only the frozen, non-schedulable source. A timeout may have committed
// this same scale CAS; the operation annotation permits an exact replay.
func (s *runtimeDeploymentService) RetireOpenCodeUpgradeSource(ctx context.Context, p *OpenCodeUpgradeSource, operation string) error {
	if p == nil || operation == "" {
		return fmt.Errorf("OpenCode source retirement proof is incomplete")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		d, err := s.client.AppsV1().Deployments(p.Target.Namespace).Get(ctx, p.Target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
			return s.validateOpenCodeRetiredSource(ctx, p, operation, d)
		}
		if err = s.ValidateOpenCodeUpgradeSource(ctx, p); err != nil {
			return err
		}
		if d.Labels[runtimeSchedulingLabel] != "false" {
			return fmt.Errorf("OpenCode source remains schedulable")
		}
		if d.Annotations[openCodeRetirementAnnotation] != "" && d.Annotations[openCodeRetirementAnnotation] != operation {
			return fmt.Errorf("OpenCode source retirement ownership changed")
		}
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[openCodeRetirementAnnotation] = operation
		zero := int32(0)
		d.Spec.Replicas = &zero
		_, err = s.client.AppsV1().Deployments(p.Target.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		return err
	})
}
func (s *runtimeDeploymentService) validateOpenCodeRetiredSource(ctx context.Context, p *OpenCodeUpgradeSource, operation string, d *appsv1.Deployment) error {
	if d == nil || string(d.UID) != p.Target.UID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.Labels[runtimeSchedulingLabel] != "false" || d.Annotations[openCodeRetirementAnnotation] != operation {
		return fmt.Errorf("OpenCode retired source UID/scale/operation changed")
	}
	expectedSpec := p.Spec.DeepCopy()
	zero := int32(0)
	expectedSpec.Replicas = &zero
	if !apiequality.Semantic.DeepEqual(d.Spec, *expectedSpec) {
		return fmt.Errorf("OpenCode retired source selector/configuration changed")
	}
	raw, err := json.Marshal(d.Spec.Template)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != p.TemplateSHA256 {
		return fmt.Errorf("OpenCode retired source template changed")
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, d.Namespace, d.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != p.StorageIdentity {
		return fmt.Errorf("OpenCode retired source storage changed")
	}
	return nil
}
func (s *runtimeDeploymentService) OpenCodeUpgradeSourceRetired(ctx context.Context, p *OpenCodeUpgradeSource, operation string) (bool, error) {
	d, err := s.client.AppsV1().Deployments(p.Target.Namespace).Get(ctx, p.Target.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if err = s.validateOpenCodeRetiredSource(ctx, p, operation, d); err != nil {
		return false, err
	}
	pods, err := s.orphanOwnedPods(ctx, d)
	if err != nil {
		return false, err
	}
	return len(pods) == 0 && d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == 0, nil
}

func expectedOpenCodeUpgradePool(source *OpenCodeUpgradeSource, name, image, operation string) (*appsv1.Deployment, error) {
	if source == nil || source.Target.UID == "" || source.Spec.Replicas == nil || source.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || source.Labels["clawmanager.io/runtime-type"] != "opencode" {
		return nil, fmt.Errorf("OpenCode frozen source configuration is incomplete")
	}
	original := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: source.Target.Name, Namespace: source.Target.Namespace, Labels: copyStringMap(source.Labels)}, Spec: *source.Spec.DeepCopy()}
	target, err := buildUpgradeRuntimePool(original, source.Target.Namespace, source.Target.Name, name, image, operation, nil)
	if err != nil {
		return nil, err
	}
	storage := sha256.Sum256([]byte(source.StorageIdentity))
	target.Annotations = map[string]string{"clawmanager.io/opencode-source-uid": source.Target.UID, "clawmanager.io/opencode-source-template": source.TemplateSHA256, "clawmanager.io/opencode-source-storage": hex.EncodeToString(storage[:])}
	return target, nil
}
func (s *runtimeDeploymentService) EnsureOpenCodeUpgradePool(ctx context.Context, source *OpenCodeUpgradeSource, name, image, operation string) (RuntimeRolloutTarget, error) {
	if err := s.ValidateOpenCodeUpgradeSource(ctx, source); err != nil {
		return RuntimeRolloutTarget{}, err
	}
	expected, err := expectedOpenCodeUpgradePool(source, name, image, operation)
	if err != nil {
		return RuntimeRolloutTarget{}, err
	}
	actual, err := s.client.AppsV1().Deployments(source.Target.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err = s.client.AppsV1().Deployments(source.Target.Namespace).Create(ctx, expected, metav1.CreateOptions{})
	}
	if err != nil {
		return RuntimeRolloutTarget{}, err
	}
	target, err := runtimeRolloutTarget(*actual)
	if err != nil {
		return target, err
	}
	if err = s.ValidateOpenCodeUpgradeTarget(ctx, source, target, operation); err != nil {
		return target, err
	}
	return target, nil
}
func (s *runtimeDeploymentService) ValidateOpenCodeUpgradeTarget(ctx context.Context, source *OpenCodeUpgradeSource, target RuntimeRolloutTarget, operation string) error {
	expected, err := expectedOpenCodeUpgradePool(source, target.Name, target.Image, operation)
	if err != nil {
		return err
	}
	actual, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if target.Namespace != source.Target.Namespace || target.UID == "" || string(actual.UID) != target.UID || actual.DeletionTimestamp != nil || len(actual.OwnerReferences) != 0 || !apiequality.Semantic.DeepEqual(actual.Spec, expected.Spec) || !apiequality.Semantic.DeepEqual(actual.Labels, expected.Labels) {
		return fmt.Errorf("OpenCode target differs from exact frozen source standby transform")
	}
	for key, want := range expected.Annotations {
		if actual.Annotations[key] != want {
			return fmt.Errorf("OpenCode target source UID/template/storage lineage changed")
		}
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, actual.Namespace, actual.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != source.StorageIdentity {
		return fmt.Errorf("OpenCode target persistent storage differs from frozen source")
	}
	return nil
}
