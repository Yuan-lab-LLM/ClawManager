package k8s

import (
	"clawreef/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net"
	"strings"
	"time"
)

const RuntimeFaultStorageAnnotation = "clawmanager.io/fault-eviction-storage"

func VerifyRuntimeFaultSourceTemplate(ctx context.Context, client kubernetes.Interface, rs *appsv1.ReplicaSet, dep *appsv1.Deployment) error {
	return verifyRetainedRuntimeTemplate(ctx, client, rs, dep)
}

// The source volume, original bound PVC/PV and current retained mount must agree.
// The helper must not resolve a recycled claim name to a different filesystem.
func RuntimeFaultStorageIdentity(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, root string, before time.Time) (string, error) {
	if pod == nil || pod.Namespace == "" {
		return "", fmt.Errorf("source storage identity missing")
	}
	if err := verifyRecoveryLedgerCoverage(pod, root); err != nil {
		return "", err
	}
	var claim *corev1.PersistentVolumeClaim
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != runtimeWorkspaceVolume {
			continue
		}
		if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ReadOnly {
			return "", fmt.Errorf("fault recovery requires retained workspace PVC")
		}
		var err error
		claim, err = client.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, volume.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
	}
	if claim == nil || claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" {
		return "", fmt.Errorf("original workspace PVC is not bound")
	}
	if !before.IsZero() && (claim.CreationTimestamp.IsZero() || claim.CreationTimestamp.Time.After(before)) {
		return "", fmt.Errorf("workspace PVC was replaced after the source")
	}
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if pv.UID == "" || pv.DeletionTimestamp != nil || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.UID != claim.UID || pv.Spec.ClaimRef.Name != claim.Name || pv.Spec.ClaimRef.Namespace != claim.Namespace {
		return "", fmt.Errorf("workspace PV binding changed")
	}
	mapping := recoveryWorkspaceMapping(pod, claim, root)
	if mapping == "" {
		return "", fmt.Errorf("source workspace mapping is ambiguous")
	}
	raw, _ := json.Marshal([]any{mapping, pv.UID, pv.Spec.PersistentVolumeSource})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Reconstruct future-placement constraints from the verified original template,
// not a replacement Pod or a fabricated free slot observation.
func RuntimeFaultSourcePlacement(pod *corev1.Pod, claim *corev1.PersistentVolumeClaim, source models.RuntimePod, slot string) (models.RuntimePod, error) {
	if pod == nil || pod.Namespace != source.Namespace || string(pod.UID) != stringValueFault(source.PodUID) {
		return source, fmt.Errorf("source placement Pod identity changed")
	}
	for _, container := range pod.Spec.Containers {
		if container.Name != slot {
			continue
		}
		if container.Image != source.ImageRef || source.ImageRef == "" {
			return source, fmt.Errorf("source original image differs from registration")
		}
		if source.ImageDigest == nil || *source.ImageDigest == "" {
			parts := strings.Split(source.ImageRef, "@")
			if len(parts) != 2 || !strings.HasPrefix(parts[1], "sha256:") {
				return source, fmt.Errorf("source immutable image digest unavailable")
			}
			source.ImageDigest = &parts[1]
		}
		cpuLimit, hasCPU := container.Resources.Limits[corev1.ResourceCPU]
		memLimit, hasMem := container.Resources.Limits[corev1.ResourceMemory]
		cpuReq, hasCPUReq := container.Resources.Requests[corev1.ResourceCPU]
		memReq, hasMemReq := container.Resources.Requests[corev1.ResourceMemory]
		if !hasCPU || !hasMem || !hasCPUReq || !hasMemReq {
			return source, fmt.Errorf("original slot resource profile is incomplete")
		}
		a, b, c, d := cpuLimit.MilliValue(), memLimit.Value(), cpuReq.MilliValue(), memReq.Value()
		if a <= 0 || b <= 0 || c < 0 || d < 0 {
			return source, fmt.Errorf("original slot resource profile is invalid")
		}
		source.Slots = []models.RuntimeSlot{{ContainerName: slot, CPUMillisLimit: &a, MemoryBytesLimit: &b, CPUMillisRequest: &c, MemoryBytesRequest: &d}}
		source.WorkspaceStorageIdentity = runtimeWorkspaceStorageIdentity(*pod, claim)
		if source.WorkspaceStorageIdentity == "" {
			return source, fmt.Errorf("source persistent storage layout unavailable")
		}
		return source, nil
	}
	return source, fmt.Errorf("original source slot is missing")
}
func stringValueFault(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func RuntimeFaultHistoricalNode(ctx context.Context, client kubernetes.Interface, source models.RuntimePod) (*corev1.Node, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(stringValueFault(source.PodIP))
	var found *corev1.Node
	for i := range nodes.Items {
		node := &nodes.Items[i]
		matched := false
		for _, cidr := range append([]string{node.Spec.PodCIDR}, node.Spec.PodCIDRs...) {
			_, network, e := net.ParseCIDR(cidr)
			if e == nil && ip != nil && network.Contains(ip) {
				matched = true
			}
		}
		if matched {
			if found != nil {
				return nil, fmt.Errorf("historical source node attribution is ambiguous")
			}
			found = node
		}
	}
	if found == nil {
		var e error
		found, _, e = readCalicoRecoveryNode(ctx, client, source, nodes.Items)
		if e != nil {
			return nil, e
		}
	}
	if found == nil || found.Name != stringValueFault(source.NodeName) || found.UID == "" || found.DeletionTimestamp != nil || found.CreationTimestamp.IsZero() || found.CreationTimestamp.Time.After(source.CreatedAt) {
		return nil, fmt.Errorf("historical source node attribution is unproven")
	}
	return found, nil
}
