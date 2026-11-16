package services

import (
	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"context"
	"fmt"
	"time"
)

// Read-only admission before Team/PVC/member allocation. The scheduler rechecks
// capability on its actual selected binding; this is not a slot reservation.
func (s *instanceService) ValidateTeamRuntimeRequests(requests []CreateInstanceRequest) error {
	needed := 0
	for _, req := range requests {
		if req.Type == RuntimeTypeDeepSeekHarness && req.Team != nil {
			needed++
		}
	}
	if needed == 0 {
		return nil
	}
	if s.runtimePodRepo == nil {
		return fmt.Errorf("DeepSeek Harness Team 运行池尚未就绪")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pods, err := s.runtimePodRepo.ListSchedulable(ctx, RuntimeTypeDeepSeekHarness)
	if err != nil {
		return fmt.Errorf("无法核实 DeepSeek Harness Team 运行池: %w", err)
	}
	available := 0
	for _, p := range pods {
		if p.State == "ready" && !p.Draining && p.LastSeenAt != nil && time.Since(*p.LastSeenAt) <= heartbeat.Timeout && stringValue(p.TeamPluginVersion) == "dsh-team-v1" {
			available += max(0, p.Capacity-p.UsedSlots)
		}
	}
	if available < needed {
		return fmt.Errorf("DeepSeek Harness Team 需要 %d 个兼容空闲位置，当前 %d 个；请先准备 dsh-team-v1 运行池", needed, available)
	}
	return nil
}

func (s *teamService) validateDshTeamAdmission(team *models.Team, plans []plannedTeamMember) error {
	needs := false
	for _, p := range plans {
		if p.RuntimeType == RuntimeTypeDeepSeekHarness {
			needs = true
		}
	}
	if !needs {
		return nil
	}
	admission, ok := s.instanceService.(interface {
		ValidateTeamRuntimeRequests([]CreateInstanceRequest) error
	})
	if !ok {
		return fmt.Errorf("DeepSeek Harness Team 运行池准入不可用")
	}
	return admission.ValidateTeamRuntimeRequests(s.buildTeamMemberInstanceRequests(team, plans))
}
