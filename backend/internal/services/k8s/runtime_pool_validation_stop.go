package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// StopIsolatedPoolValidation reclaims only the ephemeral unschedulable probe.
// Pod API absence is not an independent process-exit proof and never acts as a
// C1 stop receipt for a business assignment.
func StopIsolatedPoolValidation(ctx context.Context, client kubernetes.Interface, scope RuntimePoolValidationScope) error {
	if client == nil {
		return fmt.Errorf("isolated validation Kubernetes authority unavailable")
	}
	deployments := client.AppsV1().Deployments(scope.Namespace)
	frozen, err := deployments.Get(ctx, scope.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	pod, err := VerifyPoolValidationScope(ctx, client, scope)
	if err != nil {
		return err
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != RuntimeNFSRootlessProfile || string(frozen.UID) != scope.DeploymentUID || frozen.ResourceVersion == "" {
		return fmt.Errorf("exact NFS validation controller identity required")
	}
	// Freeze before scope verification so a concurrent template/identity change
	// cannot authorize a different controller when this patch reaches Kubernetes.
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": scope.DeploymentUID},
		{"op": "test", "path": "/metadata/resourceVersion", "value": frozen.ResourceVersion},
		{"op": "replace", "path": "/spec/replicas", "value": 0},
	})
	if err != nil {
		return err
	}
	if _, err = deployments.Patch(ctx, scope.DeploymentName, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return err
	}
	service := &runtimeDeploymentService{client: client}
	return wait.PollUntilContextCancel(ctx, 200*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		current, err := deployments.Get(ctx, scope.DeploymentName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if current.UID != frozen.UID || current.Spec.Replicas == nil || *current.Spec.Replicas != 0 || current.Spec.Paused || current.Labels[runtimePoolRoleLabel] != poolValidationRole || current.Labels[runtimeUpgradeIDLabel] != scope.Operation || current.Labels[runtimeSchedulingLabel] != "false" || !reflect.DeepEqual(current.Spec.Template, frozen.Spec.Template) || !reflect.DeepEqual(current.Spec.Selector, frozen.Spec.Selector) {
			return false, fmt.Errorf("stopping validation controller identity or template changed")
		}
		observed, err := service.ObservePoolValidation(ctx, scope.Namespace, scope.DeploymentName, scope.Operation)
		if err != nil {
			return false, err
		}
		if observed.UID != scope.DeploymentUID {
			return false, fmt.Errorf("stopping validation controller UID changed")
		}
		// Observe counts all Pod API objects, including deleting Pods. An accepted
		// scale request does not establish their absence; business writers still
		// require the separate native C1 termination and release protocol.
		return observed.Gone, nil
	})
}
