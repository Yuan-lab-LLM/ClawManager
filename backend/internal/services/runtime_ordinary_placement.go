package services

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
)

func (s *RuntimeScheduler) schedulingHeartbeatCutoff() time.Time {
	timeout := s.heartbeatTimeout
	if timeout <= 0 {
		timeout = heartbeat.Timeout
	}
	return time.Now().UTC().Add(-timeout)
}

// Selecting new work is deliberately stricter than destructive heartbeat
// failover. Never wait for that longer grace period to stop using a dead Pod.
func (s *RuntimeScheduler) currentOrdinaryRuntimePods(pods, inventory []models.RuntimePod) []models.RuntimePod {
	result := make([]models.RuntimePod, 0, len(pods))
	cutoff := s.schedulingHeartbeatCutoff()
	for _, reported := range pods {
		// Native slot agents must authenticate the registered Pod identity;
		// live inventory cannot supply an identity missing from its report.
		if requiresConfirmedGatewayStop(s.agentClient) && stringValue(reported.PodUID) == "" {
			continue
		}
		if reported.Namespace != s.runtimeNamespace || reported.State != "ready" || reported.Draining || reported.LastSeenAt == nil || reported.LastSeenAt.Before(cutoff) {
			continue
		}
		for _, live := range inventory {
			if !live.KubernetesReady || stringValue(live.PodUID) == "" || !ordinaryRuntimeIdentityMatches(reported, live) {
				continue
			}
			reported.PoolRole, reported.PoolPurpose, reported.UpgradeID = live.PoolRole, live.PoolPurpose, live.UpgradeID
			reported.SourceDeployment, reported.SchedulingEnabled, reported.DesiredReplicas = live.SourceDeployment, live.SchedulingEnabled, live.DesiredReplicas
			reported.KubernetesPodUID, reported.KubernetesReady = live.PodUID, true
			if runtimePodEligibleForOrdinaryScheduling(reported) {
				result = append(result, reported)
			}
			break
		}
	}
	return result
}

func ordinaryRuntimeIdentityMatches(reported, live models.RuntimePod) bool {
	if reported.RuntimeType != live.RuntimeType || reported.Namespace != live.Namespace || reported.DeploymentName != live.DeploymentName || reported.PodName != live.PodName || reported.PodName == "" {
		return false
	}
	if uid := stringValue(reported.PodUID); uid != "" && uid != stringValue(live.PodUID) {
		return false
	}
	if reported.PodIP == nil || live.PodIP == nil || *reported.PodIP != *live.PodIP {
		return false
	}
	u, err := url.Parse(stringValue(reported.AgentEndpoint))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || net.ParseIP(u.Hostname()) == nil || u.Hostname() != *live.PodIP {
		return false
	}
	return runtimePodMatchesImage(reported, live.ImageRef)
}

func (s *RuntimeScheduler) verifyOrdinaryRuntimePod(ctx context.Context, pod models.RuntimePod) error {
	if s.deployments == nil {
		if requiresConfirmedGatewayStop(s.agentClient) {
			return fmt.Errorf("%w: slot scheduling requires live Kubernetes Pod inventory", errRuntimeControlPlanePending)
		}
		return nil
	} // no Kubernetes integration configured
	latest, err := s.podRepo.GetByID(ctx, pod.ID)
	if err != nil {
		return fmt.Errorf("%w: read selected Runtime: %v", errRuntimeControlPlanePending, err)
	}
	if latest == nil || !ordinaryRuntimeIdentityMatches(pod, *latest) {
		return fmt.Errorf("%w: selected Runtime registration changed", errRuntimeControlPlanePending)
	}
	current, err := s.verifiedOrdinaryRuntimePods(ctx, pod.RuntimeType, []models.RuntimePod{*latest})
	if err != nil {
		return fmt.Errorf("%w: verify runtime identity: %v", errRuntimeControlPlanePending, err)
	}
	if len(current) != 1 {
		return fmt.Errorf("%w: selected Runtime Pod is no longer live and schedulable", errRuntimeControlPlanePending)
	}
	return nil
}

func (s *RuntimeScheduler) verifiedOrdinaryRuntimePods(ctx context.Context, runtimeType string, pods []models.RuntimePod) ([]models.RuntimePod, error) {
	if s.deployments == nil {
		if requiresConfirmedGatewayStop(s.agentClient) {
			return nil, fmt.Errorf("slot scheduling requires live Kubernetes Pod inventory")
		}
		return pods, nil
	}
	inventory, err := s.RuntimeDeploymentPods(ctx, runtimeType)
	if err != nil {
		return nil, err
	}
	return s.currentOrdinaryRuntimePods(pods, inventory), nil
}

type freshRuntimePodClaimer interface {
	TryClaimSlotSince(context.Context, int64, time.Time) (bool, error)
}

func (s *RuntimeScheduler) claimRuntimeSlot(ctx context.Context, id int64) (bool, error) {
	if repo, ok := s.podRepo.(freshRuntimePodClaimer); ok {
		return repo.TryClaimSlotSince(ctx, id, s.schedulingHeartbeatCutoff())
	}
	return s.podRepo.TryClaimSlot(ctx, id)
}
