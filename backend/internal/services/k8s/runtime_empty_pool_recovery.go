package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const emptyPoolRecoveryAnnotation = "clawmanager.io/empty-pool-recovery"

// Only hashes and identities enter the rollout journal; native credentials stay
// in their original template. No caller can supply a replacement Pod template.
type EmptyRuntimePoolProof struct {
	Source        RuntimeRolloutTarget `json:"source"`
	Configuration string               `json:"configuration"`
	SlotCount     int                  `json:"slot_count"`
}

type EmptyRuntimePoolRecoveryService interface {
	InspectEmptyRuntimePool(context.Context, RuntimePodScaleTarget) (*EmptyRuntimePoolProof, error)
	RecoverEmptyRuntimePool(context.Context, *EmptyRuntimePoolProof, string, string, string, func(context.Context) error) error
}

func (s *runtimeDeploymentService) emptyRuntimeConfiguration(ctx context.Context, d *appsv1.Deployment, rt string) (*EmptyRuntimePoolProof, error) {
	// Reuse the static TLS, storage, native-slot and resource validation. The
	// in-memory replica value is only for that static validator, never evidence
	// of a serving Pod and never written to Kubernetes.
	copy := d.DeepCopy()
	one := int32(1)
	copy.Spec.Replicas = &one
	snapshot, err := s.runtimeScaleConfiguration(ctx, RuntimePodScaleTarget{RuntimeType: rt, Namespace: d.Namespace, DeploymentName: d.Name, DeploymentUID: string(d.UID)}, copy)
	if err != nil {
		return nil, err
	}
	template := snapshot.Template.DeepCopy()
	for i := range template.Spec.Containers {
		template.Spec.Containers[i].Image = "image-identity"
		removeEnvVar(&template.Spec.Containers[i], "CLAWMANAGER_RUNTIME_IMAGE_REF")
		removeEnvVar(&template.Spec.Containers[i], "CLAWMANAGER_RUNTIME_IMAGE_DIGEST")
	}
	raw, err := json.Marshal(struct {
		Template any
		Storage  string
	}{template, snapshot.WorkspaceStorageIdentity})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &EmptyRuntimePoolProof{Source: RuntimeRolloutTarget{Namespace: d.Namespace, Name: d.Name, UID: string(d.UID), Image: snapshot.Option.ImageRef}, Configuration: hex.EncodeToString(sum[:]), SlotCount: snapshot.Option.SlotCount}, nil
}

func (s *runtimeDeploymentService) requireRuntimeTypeStopped(ctx context.Context, ns, rt string) error {
	selector := labels.Set{"clawmanager.io/runtime-type": rt}.String()
	controllers, err := s.client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	if len(controllers.Items) == 0 {
		return fmt.Errorf("no retained Runtime template")
	}
	for _, d := range controllers.Items {
		if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.DeletionTimestamp != nil {
			return fmt.Errorf("Runtime pool %s is not fully retired", d.Name)
		}
	}
	pods, err := s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	if len(pods.Items) != 0 {
		return fmt.Errorf("old Runtime Pods are still present; physical retirement must finish first")
	}
	return nil
}

func (s *runtimeDeploymentService) InspectEmptyRuntimePool(ctx context.Context, target RuntimePodScaleTarget) (*EmptyRuntimePoolProof, error) {
	if target.Namespace == "" || target.DeploymentUID == "" || target.DeploymentName == "" || target.RuntimeType == "" {
		return nil, fmt.Errorf("exact retired default controller identity required")
	}
	if err := s.requireRuntimeTypeStopped(ctx, target.Namespace, target.RuntimeType); err != nil {
		return nil, err
	}
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(d.UID) != target.DeploymentUID || d.Spec.Paused || d.Labels["clawmanager.io/runtime-type"] != target.RuntimeType {
		return nil, fmt.Errorf("retired default identity changed")
	}
	return s.emptyRuntimeConfiguration(ctx, d, target.RuntimeType)
}

func (s *runtimeDeploymentService) RecoverEmptyRuntimePool(ctx context.Context, proof *EmptyRuntimePoolProof, rt, image, operation string, check func(context.Context) error) error {
	if proof == nil || proof.Source.UID == "" || len(proof.Configuration) != 64 || runtimeImageDigest(image) == "" || operation == "" || check == nil {
		return fmt.Errorf("frozen empty pool proof required")
	}
	if strings.SplitN(image, "@", 2)[0] != strings.SplitN(proof.Source.Image, "@", 2)[0] {
		return fmt.Errorf("empty recovery image repository differs from source")
	}
	d, err := s.client.AppsV1().Deployments(proof.Source.Namespace).Get(ctx, proof.Source.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(d.UID) != proof.Source.UID || d.Labels["clawmanager.io/runtime-type"] != rt || d.DeletionTimestamp != nil {
		return fmt.Errorf("empty recovery controller identity changed")
	}
	current, err := s.emptyRuntimeConfiguration(ctx, d, rt)
	if err != nil {
		return err
	}
	if current.Configuration != proof.Configuration || current.SlotCount != proof.SlotCount {
		return fmt.Errorf("empty recovery storage, security or slot configuration changed")
	}
	if d.Annotations[emptyPoolRecoveryAnnotation] == operation {
		if d.Spec.Replicas != nil && *d.Spec.Replicas == 1 && current.Source.Image == image {
			return nil
		}
		return fmt.Errorf("submitted empty recovery was changed outside its operation")
	}
	if current.Source.Image != proof.Source.Image {
		return fmt.Errorf("empty recovery source image changed")
	}
	if err := s.requireRuntimeTypeStopped(ctx, d.Namespace, rt); err != nil {
		return err
	}
	if err := check(ctx); err != nil {
		return err
	}
	updated := d.DeepCopy()
	one := int32(1)
	updated.Spec.Replicas = &one
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[emptyPoolRecoveryAnnotation] = operation
	for i := range updated.Spec.Template.Spec.Containers {
		c := &updated.Spec.Template.Spec.Containers[i]
		c.Image = image
		upsertEnvVar(c, "CLAWMANAGER_RUNTIME_IMAGE_REF", image)
		upsertEnvVar(c, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", runtimeImageDigest(image))
	}
	// Update carries the freshly observed resourceVersion, fencing every replica,
	// storage, credential or security change concurrent with the final checks.
	_, err = s.client.AppsV1().Deployments(d.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	return err
}
