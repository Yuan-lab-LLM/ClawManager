package services

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const slotKernelFenceSource = "kubelet-container-fence-v1"

// This is an immutable administrator request, created BEFORE a manual external
// container replacement. It is not a receipt, a boolean override or an API that
// an instance/owner can invoke. Kubelet termination is still mandatory.
type runtimeSlotFenceRequest struct {
	Version          int                   `json:"version"`
	Intent           runtimeSlotStopIntent `json:"intent"`
	ContainerID      string                `json:"container_id"`
	ReplacementImage string                `json:"replacement_image"`
}

func validSlotProofSource(p runtimeSlotContainerProof) bool {
	if p.ProofSource == "" {
		return p.FenceRequestUID == ""
	}
	return p.ProofSource == slotKernelFenceSource && p.FenceRequestUID != "" && len(p.FenceRequestUID) <= 128 && !p.Graceful && p.Kind == ""
}

func closedSlotContainer(p *corev1.Pod, name, image string) bool {
	if p == nil || p.DeletionTimestamp != nil || p.Spec.NodeName == "" || p.Spec.HostPID || p.Spec.HostIPC || p.Spec.HostNetwork || p.Spec.ShareProcessNamespace == nil || *p.Spec.ShareProcessNamespace || p.Spec.HostUsers == nil || !*p.Spec.HostUsers || len(p.Spec.EphemeralContainers) > 0 {
		return false
	}
	if p.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || p.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return false
	}
	for _, v := range p.Spec.Volumes {
		if v.EmptyDir == nil && v.PersistentVolumeClaim == nil && v.Secret == nil {
			return false
		}
	}
	found := false
	for _, c := range p.Spec.Containers {
		s := c.SecurityContext
		if s == nil || s.Privileged == nil || *s.Privileged || s.AllowPrivilegeEscalation == nil || *s.AllowPrivilegeEscalation || s.Capabilities == nil {
			return false
		}
		dropped := false
		for _, cap := range s.Capabilities.Drop {
			if cap == "ALL" {
				dropped = true
			}
		}
		if !dropped {
			return false
		}
		for _, cap := range s.Capabilities.Add {
			if cap != "SETUID" && cap != "SETGID" {
				return false
			}
		}
		if s.ProcMount != nil && *s.ProcMount != corev1.DefaultProcMount {
			return false
		}
		if c.Name == name {
			found = c.Image == image
		}
	}
	return found
}

func runtimeSlotOperatorFenceProof(pod *corev1.Pod, intent runtimeSlotStopIntent, cm *corev1.ConfigMap) (*runtimeSlotContainerProof, error) {
	deny := fmt.Errorf("explicit operator request and exact terminated closed slot are required")
	if !validContainerIntent(intent) || intent.Graceful || intent.Kind != "" || pod == nil || string(pod.UID) != intent.PodUID || cm == nil || cm.Namespace != pod.Namespace || cm.Name != "runtime-slot-fence-"+intent.OperationID || cm.UID == "" || cm.Immutable == nil || !*cm.Immutable || cm.CreationTimestamp.IsZero() || cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/operator-recovery"] != slotKernelFenceSource {
		return nil, deny
	}
	var req runtimeSlotFenceRequest
	if decodeContainerProof([]byte(cm.Data["request"]), &req) != nil || req.Version != 1 || req.Intent != intent || req.ContainerID == "" {
		return nil, deny
	}
	image := strings.Split(req.ReplacementImage, "@sha256:")
	if len(image) != 2 || len(image[1]) != 64 || !closedSlotContainer(pod, intent.Slot, req.ReplacementImage) {
		return nil, deny
	}
	if digest, err := hex.DecodeString(image[1]); err != nil || len(digest) != 32 {
		return nil, deny
	}
	var found *runtimeSlotContainerProof
	for _, s := range pod.Status.ContainerStatuses {
		if s.Name != intent.Slot || s.RestartCount < 1 || s.ContainerID == "" || s.ContainerID == req.ContainerID || s.State.Running == nil {
			continue
		}
		t := s.LastTerminationState.Terminated
		if t == nil || t.ContainerID != req.ContainerID || t.FinishedAt.IsZero() || !t.FinishedAt.After(cm.CreationTimestamp.Time) || s.State.Running.StartedAt.Before(&t.FinishedAt) {
			continue
		}
		if found != nil {
			return nil, deny
		}
		found = &runtimeSlotContainerProof{runtimeSlotTerminationMessage: runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}, ContainerID: t.ContainerID, FinishedAt: t.FinishedAt.Time, ProofSource: slotKernelFenceSource, FenceRequestUID: string(cm.UID)}
	}
	if found == nil {
		return nil, deny
	}
	return found, nil
}

func (c *slotRuntimeAgentClient) operatorContainerFenceProof(ctx context.Context, pod *corev1.Pod, intent runtimeSlotStopIntent) (*runtimeSlotContainerProof, error) {
	cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, "runtime-slot-fence-"+intent.OperationID, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return runtimeSlotOperatorFenceProof(pod, intent, cm)
}
