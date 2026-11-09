package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Automatic capacity requests are floors; exact Scale remains available to
// rollout drain operations. Kept optional for legacy deployment adapters.
type RuntimeDeploymentScaleOut interface {
	ScaleOut(context.Context, string, string, int32) error
}

func (s *runtimeDeploymentService) ScaleOut(ctx context.Context, namespace, name string, minReplicas int32) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	if minReplicas <= 0 {
		return fmt.Errorf("automatic scale-out requires a positive replica floor")
	}
	deployments := s.client.AppsV1().Deployments(namespace)
	d, err := deployments.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if d.Labels[RuntimeScaleInOperationLabel] != "" {
		return fmt.Errorf("Runtime scale-in is active; replica changes are fenced")
	}
	// A drain or scheduling fence may have committed since scheduler inventory
	// was read. Never reactivate that pool from a stale automatic request.
	if d.UID == "" || d.ResourceVersion == "" || d.Spec.Replicas == nil || *d.Spec.Replicas <= 0 || !ordinaryRuntimeDeployment(*d) {
		return fmt.Errorf("automatic scale-out requires an identified active Deployment")
	}
	if *d.Spec.Replicas >= minReplicas {
		return nil
	}
	patch, err := json.Marshal([]map[string]any{{"op": "test", "path": "/metadata/uid", "value": string(d.UID)}, {"op": "test", "path": "/metadata/resourceVersion", "value": d.ResourceVersion}, {"op": "replace", "path": "/spec/replicas", "value": minReplicas}})
	if err != nil {
		return err
	}
	// A CAS failure or ambiguous transport response is returned to the scheduler;
	// a subsequent tick reads inventory again rather than replaying this write.
	if _, err = deployments.Patch(ctx, name, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("automatic Runtime scale-out was not confirmed: %w", err)
	}
	return nil
}
