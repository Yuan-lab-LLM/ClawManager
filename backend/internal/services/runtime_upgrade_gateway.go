package services

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const upgradeTargetIdentityMismatch = "UPGRADE_TARGET_IDENTITY_MISMATCH: "

// Explain a live target's rejected identity without trusting the Agent's
// deployment name or silently rewriting persisted placement metadata.
func upgradeTargetIdentityProblem(reported, inventory []models.RuntimePod) string {
	for _, live := range inventory {
		for _, p := range reported {
			if p.Namespace != live.Namespace || p.PodName != live.PodName || p.DeploymentName == live.DeploymentName {
				continue
			}
			return fmt.Sprintf("%sPod %s reports deployment %q; Kubernetes owner is %q", upgradeTargetIdentityMismatch, live.PodName, p.DeploymentName, live.DeploymentName)
		}
	}
	return ""
}

// Use exact pod identity, not the deployment-level metadata annotation (which
// can represent several replicas). Missing inventory never means permission.
// Traffic readiness is not identity: upgrade standby deliberately reports
// NotReady until migration and activation finish. Existing phase checks own it.
func upgradePodsWithLiveIdentity(reported, inventory []models.RuntimePod) []models.RuntimePod {
	byIdentity := map[string]models.RuntimePod{}
	for _, p := range inventory {
		byIdentity[runtimePodIdentity(p)] = p
	}
	result := make([]models.RuntimePod, 0, len(reported))
	for _, p := range reported {
		live, ok := byIdentity[runtimePodIdentity(p)]
		if !ok || live.PodUID == nil || *live.PodUID == "" || live.PodIP == nil {
			continue
		}
		if p.PodUID != nil && *p.PodUID != *live.PodUID {
			continue
		}
		u, err := url.Parse(stringValue(p.AgentEndpoint))
		if err != nil || net.ParseIP(u.Hostname()) == nil || u.Hostname() != *live.PodIP {
			continue
		}
		if p.PodIP != nil && *p.PodIP != *live.PodIP {
			continue
		}
		p.KubernetesPodUID = live.PodUID
		result = append(result, p)
	}
	return result
}

// Unlike ordinary assignment, an upgrade owns its durable reservation until
// the same Agent confirms the writer has stopped. Do not use the ordinary
// cleanup/port-fallback path here: it can discard an ambiguous start.
func (s *RuntimeScheduler) createUpgradeGateway(ctx context.Context, instance models.Instance, pod models.RuntimePod, start *runtimeGatewayStart) error {
	resp, err := s.agentClient.CreateGateway(ctx, start.endpoint, RuntimeAgentCreateGatewayRequest{
		InstanceID: instance.ID, UserID: instance.UserID, AgentType: RuntimeTypeOpenClaw,
		WorkspacePath: start.workspacePath, GatewayPort: start.reservedBinding.GatewayPort,
		ProjectRelativePath: strings.TrimSpace(start.environment["CLAWMANAGER_DEFAULT_PROJECT_RELATIVE_PATH"]),
		PortRange:           RuntimeAgentPortRange{Start: s.gatewayPortStart, End: s.gatewayPortEnd},
		UID:                 start.uid, GID: start.gid, CPUCores: instance.CPUCores, MemoryMB: instance.MemoryGB * 1024, DiskQuotaMB: instance.DiskGB * 1024,
		Generation: instance.RuntimeGeneration, UpgradeID: start.upgradeID, Environment: start.environment,
	})
	if err != nil {
		return err
	} // reservation deliberately retained
	if resp == nil {
		return errRuntimeGatewayStartPending
	}
	if resp.Port != start.reservedBinding.GatewayPort {
		return fmt.Errorf("%w: returned port does not match upgrade reservation", repository.ErrUpgradeReservationDenied)
	}
	lifecycle := NormalizeRuntimeGatewayLifecycle(resp.Status, nil)
	var healthy *time.Time
	if lifecycle.Running {
		now := time.Now().UTC()
		healthy = &now
	}
	if err := s.bindingRepo.UpdateGatewayAssignment(ctx, instance.ID, instance.RuntimeGeneration, resp.GatewayID, resp.PID, lifecycle.BindingState, healthy); err != nil {
		return err
	}
	if !lifecycle.CreateAccepted() {
		return fmt.Errorf("upgrade Gateway returned %q", resp.Status)
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, start.workspacePath); err != nil {
		return err
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, lifecycle.InstanceState, instance.RuntimeGeneration, nil); err != nil {
		return err
	}
	if s.events != nil {
		// Preserve the normal instance lifecycle notification without making an
		// event transport failure repeat the already accepted Gateway start.
		_ = s.events.Publish(ctx, lifecycle.EventType, map[string]any{"instance_id": instance.ID, "runtime_type": RuntimeTypeOpenClaw, "runtime_pod_id": pod.ID, "gateway_id": resp.GatewayID, "gateway_port": resp.Port, "gateway_state": lifecycle.BindingState, "workspace_path": start.workspacePath, "generation": instance.RuntimeGeneration})
	}
	return nil
}
