package services

import (
	cmk8s "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
)

func runtimeAssignmentReady(pod *corev1.Pod, request RuntimeAgentCreateGatewayRequest) bool {
	return cmk8s.RuntimeAssignmentReady(pod, request.GatewayPort, RuntimeGatewayPortBlockSize(request.AgentType))
}
