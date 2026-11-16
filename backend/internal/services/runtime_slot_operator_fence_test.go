package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOperatorFenceRequiresRequestBeforeExactClosedContainerExit(t *testing.T) {
	intent := runtimeSlotStopIntent{Version: 1, OperationID: strings.Repeat("1", 32), PodUID: "pod-uid", Slot: "slot-0", GatewayID: "gw-7-1", InstanceID: 7, UserID: 2, Generation: 1, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: strings.Repeat("d", 64)}
	yes, no := true, false
	image := "registry/runtime@sha256:" + strings.Repeat("a", 64)
	now := time.Now().UTC().Truncate(time.Second)
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "runtime", UID: "pod-uid", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}, Spec: corev1.PodSpec{NodeName: "node", HostUsers: &yes, ShareProcessNamespace: &no, Containers: []corev1.Container{{Name: "slot-0", Image: image, SecurityContext: &corev1.SecurityContext{Privileged: &no, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SETUID", "SETGID"}}}}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://new", RestartCount: 1, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(time.Second))}}, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://old", FinishedAt: metav1.NewTime(now)}}}}}}
	raw, _ := json.Marshal(runtimeSlotFenceRequest{Version: 1, Intent: intent, ContainerID: "containerd://old", ReplacementImage: image})
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "runtime-slot-fence-" + intent.OperationID, Namespace: p.Namespace, UID: "request-uid", CreationTimestamp: metav1.NewTime(now.Add(-time.Minute)), Labels: map[string]string{"clawmanager.io/operator-recovery": slotKernelFenceSource}}, Immutable: &yes, Data: map[string]string{"request": string(raw)}}
	proof, err := runtimeSlotOperatorFenceProof(p, intent, cm)
	if err != nil || proof.ProofSource != slotKernelFenceSource || proof.FenceRequestUID != "request-uid" || proof.Graceful {
		t.Fatalf("missing external evidence: %+v %v", proof, err)
	}
	if _, err := runtimeSlotTerminationProof(p, intent); err == nil {
		t.Fatal("external termination pretended to be a native cleanup commitment")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Pod, *corev1.ConfigMap, *runtimeSlotStopIntent)
	}{
		{"graceful", func(_ *corev1.Pod, _ *corev1.ConfigMap, i *runtimeSlotStopIntent) { i.Graceful = true }},
		{"maintenance", func(_ *corev1.Pod, _ *corev1.ConfigMap, i *runtimeSlotStopIntent) { i.Kind = "maintenance" }},
		{"wrong Pod", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) { p.UID = "replacement" }},
		{"no immutable intent", func(_ *corev1.Pod, c *corev1.ConfigMap, _ *runtimeSlotStopIntent) { c.Immutable = &no }},
		{"intent created after death", func(_ *corev1.Pod, c *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			c.CreationTimestamp = metav1.NewTime(now.Add(time.Second))
		}},
		{"no kubelet exit", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated = nil
		}},
		{"different old CID", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated.ContainerID = "containerd://other"
		}},
		{"same container still alive", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Status.ContainerStatuses[0].ContainerID = "containerd://old"
		}},
		{"shared PIDs", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Spec.ShareProcessNamespace = &yes
		}},
		{"host PID", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) { p.Spec.HostPID = true }},
		{"privileged", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Spec.Containers[0].SecurityContext.Privileged = &yes
		}},
		{"outer SYS_ADMIN", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Spec.Containers[0].SecurityContext.Capabilities.Add = append(p.Spec.Containers[0].SecurityContext.Capabilities.Add, "SYS_ADMIN")
		}},
		{"host mount", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Spec.Volumes = []corev1.Volume{{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		}},
		{"other replacement image", func(p *corev1.Pod, _ *corev1.ConfigMap, _ *runtimeSlotStopIntent) {
			p.Spec.Containers[0].Image = "other"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c, i := p.DeepCopy(), cm.DeepCopy(), intent
			tc.mutate(p, c, &i)
			if _, err := runtimeSlotOperatorFenceProof(p, i, c); err == nil {
				t.Fatal("unproven container accepted")
			}
		})
	}
}
