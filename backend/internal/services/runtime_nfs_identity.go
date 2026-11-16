package services

import (
	"context"
	"fmt"
	"net/url"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

// The live, UID-pinned target determines the storage identity contract. User
// environment variables cannot opt into a different sandbox or primary group.
func (s *RuntimeScheduler) gatewayLinuxIDs(ctx context.Context, instanceID int, environment map[string]string, target models.RuntimePod) (int, int, string, error) {
	uid, gid := runtimeGatewayLinuxIDs(instanceID, environment)
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return uid, gid, "", nil
	}
	if client.legacy != nil && target.AgentEndpoint != nil {
		u, e := url.Parse(*target.AgentEndpoint)
		if e == nil && u.Scheme == "http" {
			protected, err := client.protectedInstance(ctx, instanceID)
			if err != nil {
				return 0, 0, "", err
			}
			if !protected && client.allowLegacyHTTPEndpoint(ctx, u) {
				return uid, gid, "", nil
			}
			return 0, 0, "", fmt.Errorf("legacy runtime identity unavailable")
		}
	}
	if target.PodUID == nil || target.AgentEndpoint == nil {
		return 0, 0, "", fmt.Errorf("runtime target lacks a pinned Pod identity")
	}
	pod, err := client.containerStopPod(ctx, *target.AgentEndpoint, *target.PodUID)
	if err != nil {
		return 0, 0, "", err
	}
	profile := pod.Labels["clawmanager.io/sandbox-profile"]
	if profile != "" && profile != k8s.RuntimeNFSRootlessProfile {
		return 0, 0, "", fmt.Errorf("unknown target sandbox profile")
	}
	if profile == k8s.RuntimeNFSRootlessProfile {
		gid = uid
	}
	return uid, gid, profile, nil
}
