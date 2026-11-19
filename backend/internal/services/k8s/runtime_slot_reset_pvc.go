package k8s

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"clawreef/internal/runtimeeviction"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// The namespace and claim name determine the helper mount. The UID binds it
// to the preflight PVC; each operation owns a separate finalizer on that UID.
type RuntimeSlotResetClaim struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

func slotResetFinalizer(operationID string) string {
	hash := sha256.Sum256([]byte(operationID))
	return fmt.Sprintf("clawmanager.io/slot-reset-%x", hash[:16])
}

func RuntimeSlotResetWorkspaceClaim(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, root, digest string) (*RuntimeSlotResetClaim, error) {
	if pod == nil || digest == "" {
		return nil, fmt.Errorf("slot reset workspace identity is missing")
	}
	var name string
	for _, v := range pod.Spec.Volumes {
		if v.Name == runtimeWorkspaceVolume && v.PersistentVolumeClaim != nil {
			if name != "" {
				return nil, fmt.Errorf("ambiguous slot reset workspace claim")
			}
			name = v.PersistentVolumeClaim.ClaimName
		}
	}
	if name == "" {
		return nil, fmt.Errorf("slot reset workspace claim is absent")
	}
	claim, err := client.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if claim.UID == "" || claim.ResourceVersion == "" || claim.DeletionTimestamp != nil {
		return nil, fmt.Errorf("slot reset workspace claim cannot be pinned")
	}
	got, err := RuntimeFaultStorageIdentity(ctx, client, pod, root, time.Time{})
	if err != nil {
		return nil, err
	}
	if got != digest {
		return nil, fmt.Errorf("slot reset workspace provenance changed before pin")
	}
	return &RuntimeSlotResetClaim{Namespace: pod.Namespace, Name: name, UID: string(claim.UID)}, nil
}

func VerifyRuntimeSlotResetStorage(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, root, digest string, pin *RuntimeSlotResetClaim, operationID string) error {
	if pin == nil || operationID == "" || digest == "" {
		return fmt.Errorf("slot reset workspace identity is incomplete")
	}
	got, err := runtimeFaultStorageIdentity(ctx, client, pod, root, time.Time{}, pin, operationID)
	if err != nil {
		return err
	}
	if got != digest {
		return fmt.Errorf("slot reset workspace storage identity changed")
	}
	return nil
}

func PinRuntimeSlotResetClaim(ctx context.Context, client kubernetes.Interface, p RuntimeSlotResetClaim, operationID string) error {
	return changeRuntimeSlotResetClaim(ctx, client, p, operationID, true)
}
func ReleaseRuntimeSlotResetClaim(ctx context.Context, client kubernetes.Interface, p RuntimeSlotResetClaim, operationID string) error {
	return changeRuntimeSlotResetClaim(ctx, client, p, operationID, false)
}
func changeRuntimeSlotResetClaim(ctx context.Context, client kubernetes.Interface, p RuntimeSlotResetClaim, operationID string, acquire bool) error {
	if client == nil || operationID == "" || p.Namespace == "" || p.Name == "" || p.UID == "" {
		return fmt.Errorf("incomplete slot reset PVC protection identity")
	}
	finalizer := slotResetFinalizer(operationID)
	claims := client.CoreV1().PersistentVolumeClaims(p.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		claim, err := claims.Get(ctx, p.Name, metav1.GetOptions{})
		if !acquire && apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if string(claim.UID) != p.UID {
			if !acquire {
				return nil
			}
			return fmt.Errorf("slot reset PVC UID changed")
		}
		if claim.ResourceVersion == "" {
			return fmt.Errorf("slot reset PVC resourceVersion unavailable")
		}
		index := slices.Index(claim.Finalizers, finalizer)
		if acquire {
			if index >= 0 {
				return nil
			}
			if claim.DeletionTimestamp != nil {
				return fmt.Errorf("original slot reset PVC is being deleted")
			}
			claim.Finalizers = append(claim.Finalizers, finalizer)
		} else {
			if index < 0 {
				return nil
			}
			claim.Finalizers = slices.Delete(claim.Finalizers, index, index+1)
		}
		// Update carries both observed resourceVersion and UID. A same-name
		// replacement or simultaneous pin cannot receive a stale modification.
		_, err = claims.Update(ctx, claim, metav1.UpdateOptions{})
		return err
	})
}

// A successful helper container can report before the Job controller has
// stopped scheduling retries. Keep the claim through terminal Job and Pod state.
func SlotResetWriterFinished(ctx context.Context, client kubernetes.Interface, scope runtimeeviction.Scope, pod *corev1.Pod, image string, receipt *runtimeeviction.Receipt) error {
	expected, err := buildRuntimeEvictionJob(scope, pod, image)
	if err != nil {
		return err
	}
	job, err := client.BatchV1().Jobs(scope.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	missing := apierrors.IsNotFound(err)
	if !missing {
		if job.UID == "" || job.Annotations["clawmanager.io/recovery-scope"] != runtimeeviction.ScopeDigest(scope) || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
			return fmt.Errorf("slot reset PVC retained: writer Job identity changed")
		}
		terminal := false
		for _, cond := range job.Status.Conditions {
			if (cond.Type == batchv1.JobComplete || cond.Type == batchv1.JobFailed) && cond.Status == corev1.ConditionTrue {
				terminal = true
			}
		}
		if !terminal || job.Status.Active != 0 {
			return fmt.Errorf("%w: PVC pinned until writer Job terminates", ErrRuntimeFaultEvictionPending)
		}
	} else if receipt == nil || runtimeeviction.ValidateReceipt(scope, receipt) != nil {
		return fmt.Errorf("slot reset PVC retained: writer Job disappeared without a durable completion receipt")
	}
	pods, err := client.CoreV1().Pods(scope.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + expected.Name})
	if err != nil {
		return err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			return fmt.Errorf("%w: PVC pinned until all writer Pods terminate", ErrRuntimeFaultEvictionPending)
		}
	}
	return nil
}
