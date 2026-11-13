package k8s

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// ReadyForGateway checks the exact native slot; free capacity still requires
// the scheduler's ownership, storage and fresh runtime exit proofs.
func (p RuntimeDeploymentPod) ReadyForGateway(port int) bool {
	if p.SandboxProfile == RuntimeNFSRootlessProfile {
		return p.ReadyGatewayPorts[port]
	}
	return p.State == "ready"
}

// A peer slot restart makes the whole Pod unready. Native assignments require
// their exact port's slot and coordinator to be positively running and ready.
// Node and external readiness loss remain unconfirmed.
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
	if !RuntimeSlotCoordinatorReady(pod) {
		return false
	}
	if gatewayPort < 1024 || block < 1 || gatewayPort > 65535-block+1 {
		return false
	}
	target := ""
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
		if role == "slot" && start == strconv.Itoa(gatewayPort) {
			if starts != 1 || ends != 1 || end != strconv.Itoa(gatewayPort+block-1) || target != "" {
				return false
			}
			target = container.Name
		}
	}
	if target == "" {
		return false
	}
	return runtimeContainerReady(pod, target)
}

// RuntimeSlotCoordinatorReady permits inspection before choosing a native slot.
// It never grants a free slot or bypasses external Pod readiness failures.
func RuntimeSlotCoordinatorReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels["clawmanager.io/sandbox-profile"] != RuntimeNFSRootlessProfile || pod.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
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
	coordinator := ""
	for _, container := range pod.Spec.Containers {
		role, roles := "", 0
		for _, env := range container.Env {
			if env.Name == "RUNTIME_AGENT_ROLE" {
				if env.ValueFrom != nil {
					return false
				}
				role, roles = env.Value, roles+1
			}
		}
		if role == "pool" {
			if roles != 1 || coordinator != "" {
				return false
			}
			coordinator = container.Name
		}
	}
	return coordinator != "" && runtimeContainerReady(pod, coordinator)
}

func runtimeContainerReady(pod *corev1.Pod, name string) bool {
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
