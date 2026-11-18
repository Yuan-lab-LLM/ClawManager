package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
)

func (s *RuntimeScheduler) ColdTeamNFSWorkspace(ctx context.Context, source, target models.RuntimePod, workspace string) (bool, error) {
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || client.kube == nil || source.Namespace != target.Namespace {
		return false, nil
	}
	return k8s.VerifyColdNFSWorkspace(ctx, client.kube, source.Namespace, source.PodName, stringValue(source.PodUID), target.PodName, stringValue(target.PodUID), workspace)
}

// Verify retained cold-Team storage before the ordinary identity filter. A PVC
// and its exact inline NFS backing have different serialized identities; only
// the existing full stop/version/Pod/storage proof may establish equivalence.
func (s *RuntimeScheduler) filterResumeWorkspace(ctx context.Context, instance models.Instance, pods []models.RuntimePod, storage string, inspect nativePlacementInspector) ([]models.RuntimePod, bool, error) {
	if instance.Type == RuntimeTypeOpenClaw && instance.RuntimeSecurityProfile == RuntimeSecurityProfile && s.upgrade != nil {
		native, verified, err := s.upgrade.coldTeamNativePlacement(ctx, instance.ID, pods, inspect)
		if err != nil || verified {
			return native, verified, err
		}
	}
	if storage == "" {
		return pods, false, nil
	}
	matching := make([]models.RuntimePod, 0, len(pods))
	for _, p := range pods {
		if runtimeWorkspaceStorageCompatible(storage, p.WorkspaceStorageIdentity) {
			matching = append(matching, p)
		}
	}
	return matching, false, nil
}
