package services

import (
	"context"
	"fmt"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A never-published first reservation has no source writer to stop. Keep this
// distinct from recovery: neither Pod absence alone nor a missing assignment
// authorizes releasing a possibly published generation.
func (s *RuntimeScheduler) releaseUnpublishedFirstReservation(ctx context.Context, client *slotRuntimeAgentClient, instance models.Instance, binding models.InstanceRuntimeBinding, pod models.RuntimePod) error {
	releaser, ok := s.bindingRepo.(interface {
		ReleaseUnpublishedFirstReservation(context.Context, models.Instance, models.InstanceRuntimeBinding, models.RuntimePod, func() error) (bool, error)
	})
	if !ok || client.kube == nil || client.assignments == nil || instance.RuntimeGeneration != 1 || binding.Generation != 1 || pod.Namespace != client.namespace || pod.PodName == "" || pod.PodUID == nil || *pod.PodUID == "" || pod.AgentEndpoint == nil || *pod.AgentEndpoint == "" {
		return fmt.Errorf("unpublished reservation rollback authority is unavailable")
	}
	released, err := releaser.ReleaseUnpublishedFirstReservation(ctx, instance, binding, pod, func() error {
		_, err := client.kube.CoreV1().Pods(pod.Namespace).Get(ctx, pod.PodName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			if err != nil {
				return err
			}
			return fmt.Errorf("original source Pod name still exists; reservation held")
		}
		if err = client.assignments.CanRetryUnpublished(ctx, k8s.RuntimeAssignment{InstanceID: instance.ID, UserID: instance.UserID, Generation: 1, RuntimeType: instance.Type, Endpoint: *pod.AgentEndpoint, PodUID: *pod.PodUID, GatewayID: fmt.Sprintf("gw-%d-1", instance.ID)}); err != nil {
			return err
		}
		// Inventory both labels and reserved Secret names, including malformed or
		// unlabelled historical credentials; absence of the current Secret is not enough.
		return client.assignments.CheckUnboundInstanceStopped(ctx, instance.ID, instance.UserID, 1, instance.Type, nil)
	})
	if err != nil {
		return err
	}
	if !released {
		return fmt.Errorf("unpublished reservation changed before rollback")
	}
	return nil
}
