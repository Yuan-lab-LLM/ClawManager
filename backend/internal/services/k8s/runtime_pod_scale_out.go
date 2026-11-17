package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Automatic capacity requests are floors; exact Scale remains available to
// rollout drain operations. Kept optional for legacy deployment adapters.
type RuntimeDeploymentScaleOut interface {
	ScaleOut(context.Context, string, string, int32) error
}

// ErrRuntimeScaleOutPending requests a fresh observation on the next tick.
var ErrRuntimeScaleOutPending = errors.New("automatic Runtime scale-out awaits observation")

// ErrRuntimeScaleOutRejected prevents transport diagnostics from overriding a confirmed refusal.
var ErrRuntimeScaleOutRejected = errors.New("automatic Runtime scale-out rejected")

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
		diagnostic := fmt.Errorf("automatic Runtime scale-out runtime_type=%s namespace=%s deployment=%s floor=%d uid=%s resourceVersion=%s was not confirmed: %w", d.Labels["clawmanager.io/runtime-type"], namespace, name, minReplicas, d.UID, d.ResourceVersion, err)
		ambiguousPatchRejection := false
		var status apierrors.APIStatus
		if errors.As(err, &status) {
			st := status.Status()
			ambiguousPatchRejection = st.Code == 422 && (st.Details == nil || len(st.Details.Causes) == 0)
			var causes []metav1.StatusCause
			if st.Details != nil {
				causes = st.Details.Causes
			}
			diagnostic = fmt.Errorf("%w (code=%d reason=%s causes=%v)", diagnostic, st.Code, st.Reason, causes)
		}
		var networkError net.Error
		uncertain := apierrors.IsConflict(err) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || errors.As(err, &networkError) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
		if uncertain || ambiguousPatchRejection {
			current, readErr := deployments.Get(ctx, name, metav1.GetOptions{})
			if readErr == nil {
				if current.UID != d.UID || current.Labels[RuntimeScaleInOperationLabel] != "" || current.Spec.Replicas == nil || *current.Spec.Replicas <= 0 || !ordinaryRuntimeDeployment(*current) {
					return errors.Join(ErrRuntimeScaleOutRejected, diagnostic)
				}
				if uncertain || current.ResourceVersion != d.ResourceVersion {
					return errors.Join(ErrRuntimeScaleOutPending, diagnostic)
				}
			} else if uncertain {
				return errors.Join(ErrRuntimeScaleOutPending, diagnostic)
			}
		}
		return errors.Join(ErrRuntimeScaleOutRejected, diagnostic)
	}
	return nil
}
