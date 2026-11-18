package services

import (
	"fmt"
	"strings"
)

// TeamCreationRules is an admission contract, not the representation of existing
// Teams. Historical Pro/other-Leader runtimes still need their normal lifecycle.
type TeamCreationRules struct {
	RuntimeTypes       []string `json:"runtime_types"`
	LeaderRuntimeTypes []string `json:"leader_runtime_types"`
	WorkerRuntimeTypes []string `json:"worker_runtime_types"`
	InstanceModes      []string `json:"instance_modes"`
	CommunicationModes []string `json:"communication_modes"`
}

func CurrentTeamCreationRules() TeamCreationRules {
	return TeamCreationRules{RuntimeTypes: []string{"openclaw", "hermes", "opencode", "deepseek-harness"}, LeaderRuntimeTypes: []string{"openclaw"}, WorkerRuntimeTypes: []string{"openclaw", "hermes", "opencode", "deepseek-harness"}, InstanceModes: []string{InstanceModeLite}, CommunicationModes: []string{teamCommunicationModeLeaderMediated}}
}

// ValidateNewTeamRequest performs no I/O. Call before name-conflict cleanup,
// operation reservation or any instance/PVC/secret allocation. Do not apply it
// to ordinary instances or successful historical Team lifecycle operations.
func ValidateNewTeamRequest(req CreateTeamRequest) error {
	mode, err := normalizeTeamCommunicationMode(req.CommunicationMode)
	if err != nil || mode != teamCommunicationModeLeaderMediated {
		return fmt.Errorf("新建 Team 仅支持 Leader 中介协作模式")
	}
	if len(req.Members) == 0 {
		return fmt.Errorf("Team 至少需要一个成员")
	}
	for _, m := range req.Members {
		for _, raw := range []string{m.Mode, m.InstanceMode} {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			if normalized, ok := NormalizeInstanceMode(raw); !ok || normalized != InstanceModeLite {
				return fmt.Errorf("Team 成员 %s 仅支持 Lite 模式", m.MemberID)
			}
		}
	}
	plans, err := planTeamMembers(req.Name, req.Members)
	if err != nil {
		return err
	}
	for _, p := range plans {
		if p.IsLeader && p.RuntimeType != "openclaw" {
			return fmt.Errorf("Team Leader 必须使用 OpenClaw Lite")
		}
	}
	return nil
}
