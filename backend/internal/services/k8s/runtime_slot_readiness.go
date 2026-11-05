package k8s

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// ReadyForGateway is for an existing instance binding. Pool scheduling keeps
// using the whole Pod State and cannot treat this as spare capacity.
func (p RuntimeDeploymentPod) ReadyForGateway(port int) bool {
	if p.SandboxProfile == RuntimeNFSRootlessProfile {
		return p.ReadyGatewayPorts[port]
	}
	return p.State == "ready"
}

// A peer slot restart makes the whole Pod unready. Existing NFS assignments
// retain authority only when their exact native port's slot and the trusted
// coordinator remain positively running and ready. Node/other readiness loss
// remains unconfirmed; creation still requires the ordinary PodReady check.
func RuntimeAssignmentReady(pod *corev1.Pod, gatewayPort, block int) bool {
	if pod == nil {
		return false
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true
			}
		}
		return false
	}
	if pod.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
		return false
	}
	conditions := 0
	for _, condition := range pod.Status.Conditions {
		if condition.Type != corev1.PodReady {
			continue
		}
		conditions++
		if condition.Status == corev1.ConditionTrue {
			continue
		}
		if condition.Status != corev1.ConditionFalse || condition.Reason != "ContainersNotReady" || len(pod.Spec.ReadinessGates) != 0 {
			return false
		}
	}
	if conditions != 1 {
		return false
	}
	if gatewayPort < 1024 || block < 1 || gatewayPort > 65535-block+1 {
		return false
	}
	coordinator, target := "", ""
	for _, container := range pod.Spec.Containers {
		role, start, end := "", "", ""
		roles, starts, ends := 0, 0, 0
		for _, env := range container.Env {
			switch env.Name {
			case "RUNTIME_AGENT_ROLE":
				role = env.Value
				roles++
			case "RUNTIME_GATEWAY_PORT_START":
				start = env.Value
				starts++
			case "RUNTIME_GATEWAY_PORT_END":
				end = env.Value
				ends++
			}
		}
		if roles != 1 {
			continue
		}
		if role == "pool" {
			if coordinator != "" {
				return false
			}
			coordinator = container.Name
		}
		if role == "slot" && start == strconv.Itoa(gatewayPort) {
			if starts != 1 || ends != 1 || end != strconv.Itoa(gatewayPort+block-1) || target != "" {
				return false
			}
			target = container.Name
		}
	}
	if coordinator == "" || target == "" || coordinator == target {
		return false
	}
	ready := func(name string) bool {
		matches := 0
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != name {
				continue
			}
			matches++
			if !status.Ready || status.ContainerID == "" || status.State.Running == nil || status.State.Running.StartedAt.IsZero() {
				return false
			}
		}
		return matches == 1
	}
	return ready(coordinator) && ready(target)
}
