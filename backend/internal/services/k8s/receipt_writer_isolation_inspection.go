package k8s

import (
	"context"
	"errors"
	"strconv"

	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

var ErrReceiptIsolationUnproven = errors.New("receipt writer isolation has not been independently proven")

// All identities must be pinned from server placement/storage/upgrade state.
// Names or an empty Pod list alone are never fencing proof.
type ReceiptWriterPin struct {
	InstanceID, OwnerID, Generation      int
	Operation, Role                      string
	Namespace, Deployment, DeploymentUID string
	PVC, PVCUID, PV, PVUID               string
	OldPodUID, OldNode, OldNodeUID       string
}

func (p ReceiptWriterPin) valid() bool {
	if p.InstanceID <= 0 || p.OwnerID <= 0 || p.Generation < 0 || int64(p.Generation) > 2147483647 || !models.ValidRuntimeOperation(p.Operation) || (p.Role != "pro" && p.Role != "lite") || len(validation.IsDNS1123Label(p.Namespace)) != 0 {
		return false
	}
	for _, s := range []string{p.Deployment, p.PVC, p.PV, p.OldNode} {
		if s == "" || len(validation.IsDNS1123Subdomain(s)) != 0 {
			return false
		}
	}
	for _, s := range []string{p.DeploymentUID, p.PVCUID, p.PVUID, p.OldPodUID, p.OldNodeUID} {
		if s == "" || len(s) > 128 {
			return false
		}
		for _, c := range []byte(s) {
			if c < 33 || c > 126 {
				return false
			}
		}
	}
	return true
}

// WriterFenceAuthority must independently validate durable admission isolation,
// old node/process isolation and revoked old volume access for EXACTLY this pin.
// A manager acknowledgement, Pod deletion or SQL fence is insufficient. There
// is intentionally no default provider that returns success.
type WriterFenceAuthority interface {
	ValidateWriterFence(context.Context, ReceiptWriterPin) (string, error)
}
type ReceiptWriterIsolationInspector struct {
	client    kubernetes.Interface
	authority WriterFenceAuthority
}

func NewReceiptWriterIsolationInspector(c kubernetes.Interface, a WriterFenceAuthority) *ReceiptWriterIsolationInspector {
	return &ReceiptWriterIsolationInspector{c, a}
}

type ReceiptWriterIsolationInspection struct {
	InfrastructureObservedClear bool   `json:"infrastructure_observed_clear"`
	IsolationAuthorityChecked   bool   `json:"isolation_authority_checked"`
	LifecycleAuthorized         bool   `json:"lifecycle_authorized"`
	DeploymentResourceVersion   string `json:"deployment_resource_version"`
	PVCResourceVersion          string `json:"pvc_resource_version"`
	PVResourceVersion           string `json:"pv_resource_version"`
}

// Inspect is read-only. It never scales, deletes, evicts, detaches, releases a
// fence or starts an instance, even when all checks succeed.
func (s *ReceiptWriterIsolationInspector) Inspect(ctx context.Context, p ReceiptWriterPin) (*ReceiptWriterIsolationInspection, error) {
	if s == nil || s.client == nil || s.authority == nil || !p.valid() || ctx.Err() != nil {
		return nil, ErrReceiptIsolationUnproven
	}
	epoch, err := s.authority.ValidateWriterFence(ctx, p)
	if err != nil || epoch == "" {
		return nil, ErrReceiptIsolationUnproven
	}
	d, err := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Deployment, metav1.GetOptions{})
	if err != nil || d == nil || string(d.UID) != p.DeploymentUID || d.DeletionTimestamp != nil || d.Generation < 1 || d.Status.ObservedGeneration < d.Generation || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.Status.Replicas != 0 || d.Status.ReadyReplicas != 0 || d.Status.AvailableReplicas != 0 || d.Status.UpdatedReplicas != 0 || d.ResourceVersion == "" {
		return nil, ErrReceiptIsolationUnproven
	}
	claim, err := s.client.CoreV1().PersistentVolumeClaims(p.Namespace).Get(ctx, p.PVC, metav1.GetOptions{})
	if err != nil || claim == nil || string(claim.UID) != p.PVCUID || claim.DeletionTimestamp != nil || claim.Spec.VolumeName != p.PV || claim.Status.Phase != corev1.ClaimBound || claim.ResourceVersion == "" || len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOncePod || claim.Labels["clawmanager.io/storage-purpose"] != "task-receipts" {
		return nil, ErrReceiptIsolationUnproven
	}
	if p.Role == "pro" && (claim.Labels["instance-id"] != strconv.Itoa(p.InstanceID) || claim.Labels["user-id"] != strconv.Itoa(p.OwnerID)) {
		return nil, ErrReceiptIsolationUnproven
	}
	if p.Role == "lite" && claim.Annotations["clawmanager.io/runtime-deployment"] != p.Deployment {
		return nil, ErrReceiptIsolationUnproven
	}
	volume, err := s.client.CoreV1().PersistentVolumes().Get(ctx, p.PV, metav1.GetOptions{})
	if err != nil || volume == nil || string(volume.UID) != p.PVUID || volume.DeletionTimestamp != nil || volume.Status.Phase != corev1.VolumeBound || volume.ResourceVersion == "" || volume.Spec.ClaimRef == nil || volume.Spec.ClaimRef.Namespace != p.Namespace || volume.Spec.ClaimRef.Name != p.PVC || string(volume.Spec.ClaimRef.UID) != p.PVCUID {
		return nil, ErrReceiptIsolationUnproven
	}
	node, err := s.client.CoreV1().Nodes().Get(ctx, p.OldNode, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, ErrReceiptIsolationUnproven
	}
	if err == nil && (node == nil || string(node.UID) != p.OldNodeUID) {
		return nil, ErrReceiptIsolationUnproven
	}
	pods, err := s.client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil || pods == nil {
		return nil, ErrReceiptIsolationUnproven
	}
	for _, pod := range pods.Items {
		if string(pod.UID) == p.OldPodUID {
			return nil, ErrReceiptIsolationUnproven
		}
		if pod.Namespace == p.Namespace {
			for _, v := range pod.Spec.Volumes {
				if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == p.PVC {
					return nil, ErrReceiptIsolationUnproven
				}
			}
		}
	}
	attachments, err := s.client.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil || attachments == nil {
		return nil, ErrReceiptIsolationUnproven
	}
	for _, a := range attachments.Items {
		if a.Spec.Source.PersistentVolumeName != nil && *a.Spec.Source.PersistentVolumeName == p.PV {
			return nil, ErrReceiptIsolationUnproven
		}
	}
	autoscalers, err := s.client.AutoscalingV2().HorizontalPodAutoscalers(p.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || autoscalers == nil {
		return nil, ErrReceiptIsolationUnproven
	}
	for _, a := range autoscalers.Items {
		if a.Spec.ScaleTargetRef.Kind == "Deployment" && a.Spec.ScaleTargetRef.Name == p.Deployment {
			return nil, ErrReceiptIsolationUnproven
		}
	}
	// Detect replacement or spec changes during the scan. Authority must close
	// controller/host races; repeated API reads do not make a distributed lock.
	d2, e1 := s.client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Deployment, metav1.GetOptions{})
	c2, e2 := s.client.CoreV1().PersistentVolumeClaims(p.Namespace).Get(ctx, p.PVC, metav1.GetOptions{})
	v2, e3 := s.client.CoreV1().PersistentVolumes().Get(ctx, p.PV, metav1.GetOptions{})
	if e1 != nil || e2 != nil || e3 != nil || d2 == nil || c2 == nil || v2 == nil || d2.UID != d.UID || d2.ResourceVersion != d.ResourceVersion || c2.UID != claim.UID || c2.ResourceVersion != claim.ResourceVersion || v2.UID != volume.UID || v2.ResourceVersion != volume.ResourceVersion {
		return nil, ErrReceiptIsolationUnproven
	}
	finalEpoch, err := s.authority.ValidateWriterFence(ctx, p)
	if err != nil || finalEpoch != epoch || ctx.Err() != nil {
		return nil, ErrReceiptIsolationUnproven
	}
	return &ReceiptWriterIsolationInspection{InfrastructureObservedClear: true, IsolationAuthorityChecked: true, DeploymentResourceVersion: d.ResourceVersion, PVCResourceVersion: claim.ResourceVersion, PVResourceVersion: volume.ResourceVersion}, nil
}
