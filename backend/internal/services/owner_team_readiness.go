package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

// Waiting for scheduling is not another provisioning attempt. The operation
// worker persists this observation and releases its lease between checks.
type OwnerTeamRuntimePending struct{ InstanceID int }

func (e *OwnerTeamRuntimePending) Error() string {
	return fmt.Sprintf("成员实例 #%d 正在等待调度或启动验证", e.InstanceID)
}

type ownerTeamRuntimeReadiness interface {
	OwnerTeamRuntimeReady(context.Context, *models.Instance) (bool, error)
}

func (s *instanceService) OwnerTeamRuntimeReady(ctx context.Context, instance *models.Instance) (bool, error) {
	if instance == nil || instance.Status != "running" {
		return false, nil
	}
	if s.podService == nil || s.podService.GetClient() == nil {
		return false, fmt.Errorf("runtime observation unavailable")
	}
	if runtimeType, lite := v2RuntimeTypeForInstance(instance); lite {
		if s.bindingRepo == nil || s.runtimePodRepo == nil {
			return false, fmt.Errorf("runtime binding observation unavailable")
		}
		binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instance.ID)
		if err != nil {
			return false, err
		}
		if binding == nil || binding.Generation != instance.RuntimeGeneration || binding.RuntimeType != runtimeType || binding.GatewayPort <= 0 || binding.GatewayID == "" {
			return false, nil
		}
		pod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
		if err != nil {
			return false, err
		}
		if pod == nil || pod.State != "ready" || pod.Draining || pod.LastSeenAt == nil || time.Since(*pod.LastSeenAt) > heartbeat.Timeout {
			return false, nil
		}
		deployments := k8s.NewRuntimeDeploymentService(s.podService.GetClient().Clientset)
		live, err := deployments.ListDeploymentPods(ctx, []k8s.RuntimeDeploymentRef{{Namespace: pod.Namespace, Name: pod.DeploymentName}})
		if err != nil {
			return false, err
		}
		matched := false
		for _, p := range live {
			if p.State == "ready" && p.PodUID != "" && runtimeRolloutAgentMatchesPod(*pod, p) && runtimePodMatchesImage(*pod, p.ImageRef) {
				matched = true
			}
		}
		if !matched {
			return false, nil
		}
		if observer, ok := s.agentClient.(interface {
			GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
		}); ok {
			state, err := observer.GatewayState(ctx, stringValue(pod.AgentEndpoint), binding.GatewayID)
			if err != nil && !errors.Is(err, ErrRuntimeAgentUnsupported) {
				return false, err
			}
			if err == nil {
				return state != nil && state.InstanceID == instance.ID && state.Generation == binding.Generation && state.GatewayID == binding.GatewayID && NormalizeRuntimeGatewayLifecycle(state.State, nil).Running, nil
			}
		}
		// Older agents without a per-Gateway observation need a current scheduler
		// health receipt for this exact binding, not merely a runtime heartbeat.
		return binding.LastHealthAt != nil && time.Since(*binding.LastHealthAt) <= heartbeat.Timeout, nil
	}
	pod, err := s.podService.GetPod(ctx, instance.UserID, instance.ID)
	if err != nil {
		return false, err
	}
	return pod != nil && pod.DeletionTimestamp == nil && isPodReady(pod), nil
}

func (s *teamService) finishOwnerTeamProvision(ctx context.Context, team *models.Team) error {
	checker, ok := s.instanceService.(ownerTeamRuntimeReadiness)
	if !ok {
		return fmt.Errorf("Owner Team runtime readiness observer unavailable")
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return fmt.Errorf("Team has no provisioned members")
	}
	for _, member := range members {
		if member.InstanceID == nil {
			return fmt.Errorf("member binding missing")
		}
		instance, err := s.instanceService.GetByID(*member.InstanceID)
		if err != nil {
			return err
		}
		if instance == nil || instance.UserID != team.UserID || stringValue(instance.Owner) != stringValue(team.Owner) || stringValue(instance.ProvisioningOperationID) != ownerTeamMemberProvisionID(team.ID, member.MemberKey) {
			return fmt.Errorf("member runtime ownership is not confirmed")
		}
		checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		ready, checkErr := checker.OwnerTeamRuntimeReady(checkCtx, instance)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if checkErr != nil || !ready {
			return &OwnerTeamRuntimePending{InstanceID: instance.ID}
		}
	}
	// Warm the same read-only observations used by the portal for every member.
	// This neither invents a task heartbeat nor waits for a model/first task.
	_ = s.ObserveTeamMembers(ctx, []models.Team{*team}, members)
	team.Status, team.UpdatedAt = models.TeamStatusRunning, time.Now().UTC()
	if err := s.repo.UpdateTeam(team); err != nil {
		return err
	}
	existing, err := s.repo.GetTaskByMessageID(team.ID, initialLeaderTaskMessageID(team.ID))
	if err != nil {
		return err
	}
	if existing == nil || existing.Status == models.TeamTaskStatusPending {
		return s.dispatchInitialLeaderTask(team.UserID, team)
	}
	return nil
}
