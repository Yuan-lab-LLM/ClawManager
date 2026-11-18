package services

import (
	"context"
	"net/url"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNFSAuthorityKeepsExactHealthySlotDuringPeerReset(t *testing.T) {
	for _, scenario := range []string{"healthy-target", "target-unready", "coordinator-unready", "target-not-running", "missing-target-status", "node-unknown", "custom-readiness-gate", "duplicate-port"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, kube, _, pod := securityFixture(t)
			pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}
			pod.Spec.Containers = []corev1.Container{{Name: "agent", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "pool"}}}, {Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "slot"}, {Name: "RUNTIME_GATEWAY_PORT_START", Value: "24000"}, {Name: "RUNTIME_GATEWAY_PORT_END", Value: "24002"}}}, {Name: "slot-1", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "slot"}, {Name: "RUNTIME_GATEWAY_PORT_START", Value: "24003"}, {Name: "RUNTIME_GATEWAY_PORT_END", Value: "24005"}}}}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "agent", Ready: true, ContainerID: "containerd://coordinator", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}}, {Name: "slot-0", Ready: true, ContainerID: "containerd://target", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}}, {Name: "slot-1", Ready: false, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
			switch scenario {
			case "target-unready":
				pod.Status.ContainerStatuses[1].Ready = false
			case "coordinator-unready":
				pod.Status.ContainerStatuses[0].Ready = false
			case "target-not-running":
				pod.Status.ContainerStatuses[1].State.Running = nil
			case "missing-target-status":
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses[:1], pod.Status.ContainerStatuses[2:]...)
			case "node-unknown":
				pod.Status.Conditions[0].Status = corev1.ConditionUnknown
				pod.Status.Conditions[0].Reason = "NodeStatusUnknown"
			case "custom-readiness-gate":
				pod.Status.Conditions[0].Reason = "ReadinessGatesNotReady"
			case "duplicate-port":
				pod.Spec.Containers[2].Env[1].Value = "24000"
				pod.Spec.Containers[2].Env[2].Value = "24002"
			}
			if _, err := kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			_, err := a.CurrentIdentity(context.Background(), 7)
			if scenario != "healthy-target" {
				if err == nil {
					t.Fatal("unknown/unready target authority was accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("peer reset revoked healthy target authority: %v", err)
			}
			target, _ := url.Parse("http://10.2.0.3:24000/global/health")
			if _, err = a.NativeGatewayCredentialForTarget(context.Background(), 7, target); err != nil {
				t.Fatal("healthy native route lost authorization")
			}
		})
	}
}
