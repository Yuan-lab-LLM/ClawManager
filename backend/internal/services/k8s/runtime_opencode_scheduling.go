package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// Read both frozen controllers before either scheduling write. The same objects
// and resourceVersions that passed validation are used by Update.
func (s *runtimeDeploymentService) openCodeSchedulingPair(ctx context.Context, source, target *OpenCodeUpgradeSource, operation, phase string) (*appsv1.Deployment, *appsv1.Deployment, error) {
	if source == nil || target == nil || operation == "" || source.Target.Namespace != target.Target.Namespace {
		return nil, nil, fmt.Errorf("OpenCode scheduling proof is incomplete")
	}
	from, err := s.client.AppsV1().Deployments(source.Target.Namespace).Get(ctx, source.Target.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	to, err := s.client.AppsV1().Deployments(target.Target.Namespace).Get(ctx, target.Target.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	sourceRetirement, targetRetirement := "", ""
	if phase == "retire" || phase == "verify" {
		sourceRetirement = operation
	}
	if phase == "rollback_retire" || phase == "rollback_verify" {
		targetRetirement = "rollback-" + operation
	}
	if err = s.validateOpenCodeSchedulingIdentity(ctx, from, source, sourceRetirement); err != nil {
		return nil, nil, err
	}
	if err = s.validateOpenCodeSchedulingIdentity(ctx, to, target, targetRetirement); err != nil {
		return nil, nil, err
	}
	expected, err := expectedOpenCodeUpgradePool(source, target.Target.Name, target.Target.Image, operation)
	if err != nil {
		return nil, nil, err
	}
	for key, want := range expected.Annotations {
		if to.Annotations[key] != want {
			return nil, nil, fmt.Errorf("OpenCode target source UID/template/storage lineage changed")
		}
	}
	initial := source.Labels[runtimeSchedulingLabel]
	a, b := from.Labels[runtimeSchedulingLabel], to.Labels[runtimeSchedulingLabel]
	valid := a == initial && b == "false"
	switch phase {
	case "switch":
		valid = (a == initial && (b == "false" || b == "true")) || (a == "false" && b == "true")
	case "retire", "verify":
		valid = a == "false" && b == "true"
	case "rollback_retire":
		valid = (a == initial || a == "false" || a == "true") && (b == "false" || b == "true")
	case "rollback_verify":
		valid = a == "true" && b == "false"
	}
	if !valid {
		return nil, nil, fmt.Errorf("OpenCode scheduling state differs from protected phase %s", phase)
	}
	return from, to, nil
}
func (s *runtimeDeploymentService) validateOpenCodeSchedulingIdentity(ctx context.Context, actual *appsv1.Deployment, proof *OpenCodeUpgradeSource, retirement string) error {
	if actual == nil || proof == nil || proof.Target.UID == "" || string(actual.UID) != proof.Target.UID || actual.Namespace != proof.Target.Namespace || actual.Name != proof.Target.Name || actual.DeletionTimestamp != nil || len(actual.OwnerReferences) != 0 || actual.Spec.Replicas == nil {
		return fmt.Errorf("OpenCode frozen scheduling controller UID changed")
	}
	expectedSpec := proof.Spec.DeepCopy()
	if *actual.Spec.Replicas == 0 {
		if retirement == "" || actual.Annotations[openCodeRetirementAnnotation] != retirement || actual.Labels[runtimeSchedulingLabel] != "false" {
			return fmt.Errorf("OpenCode zero-replica scheduling proof is not owned by this operation")
		}
		zero := int32(0)
		expectedSpec.Replicas = &zero
	} else if actual.Annotations[openCodeRetirementAnnotation] != proof.Annotations[openCodeRetirementAnnotation] {
		return fmt.Errorf("OpenCode source retirement ownership changed")
	}
	if !apiequality.Semantic.DeepEqual(actual.Spec, *expectedSpec) {
		return fmt.Errorf("OpenCode scheduling controller template/selector changed")
	}
	wantedLabels, actualLabels := copyStringMap(proof.Labels), copyStringMap(actual.Labels)
	delete(wantedLabels, runtimeSchedulingLabel)
	delete(actualLabels, runtimeSchedulingLabel)
	if !apiequality.Semantic.DeepEqual(wantedLabels, actualLabels) {
		return fmt.Errorf("OpenCode scheduling controller role/source lineage changed")
	}
	for _, key := range []string{"clawmanager.io/opencode-source-uid", "clawmanager.io/opencode-source-template", "clawmanager.io/opencode-source-storage"} {
		if actual.Annotations[key] != proof.Annotations[key] {
			return fmt.Errorf("OpenCode scheduling controller source annotations changed")
		}
	}
	storage, err := s.idleRuntimeStorageIdentity(ctx, actual.Namespace, actual.Spec.Template.Spec)
	if err != nil {
		return err
	}
	if storage != proof.StorageIdentity {
		return fmt.Errorf("OpenCode scheduling controller workspace storage changed")
	}
	return nil
}
func (s *runtimeDeploymentService) ValidateOpenCodeUpgradeScheduling(ctx context.Context, source, target *OpenCodeUpgradeSource, operation, phase string) error {
	_, _, err := s.openCodeSchedulingPair(ctx, source, target, operation, phase)
	return err
}
func (s *runtimeDeploymentService) SetOpenCodeUpgradePoolActive(ctx context.Context, source, target *OpenCodeUpgradeSource, operation string, active bool) error {
	phase := "switch"
	if !active {
		phase = "rollback_retire"
	}
	for step := 0; step < 2; step++ {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			from, to, err := s.openCodeSchedulingPair(ctx, source, target, operation, phase)
			if err != nil {
				return err
			}
			object, value := to, "true"
			if active && step == 1 {
				object, value = from, "false"
			}
			if !active {
				object, value = from, "true"
				if step == 1 {
					object, value = to, "false"
				}
			}
			if object.Labels[runtimeSchedulingLabel] == value {
				return nil
			}
			object.Labels[runtimeSchedulingLabel] = value
			_, err = s.client.AppsV1().Deployments(object.Namespace).Update(ctx, object, metav1.UpdateOptions{})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
