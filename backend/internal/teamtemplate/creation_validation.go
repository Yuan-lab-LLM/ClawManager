package teamtemplate

import (
	"fmt"
	"strings"
)

func reservedLeaderRole(role string) bool {
	role = strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(role)), "_", "-"), " ", "-")
	return role == "leader" || role == "team-leader"
}

// ValidateForCreation checks old persisted templates without normalizing IDs,
// reordering members or rewriting a user's template revision during a read.
func ValidateForCreation(spec TemplateSpec, requested *int) error {
	if err := validateRequestedMemberCount(requested); err != nil {
		return err
	}
	if len(spec.Members) < minCustomTeamMembers || len(spec.Members) > maxCustomTeamMembers || requested != nil && len(spec.Members) != *requested {
		return fmt.Errorf("自定义 Team 模板人数必须为 2～6 人，且与指定人数一致")
	}
	seen := map[string]bool{}
	for i, m := range spec.Members {
		id := normalizeMemberID(m.MemberID)
		if id == "" || seen[id] {
			return fmt.Errorf("自定义 Team 成员标识为空或重复：%s", m.MemberID)
		}
		seen[id] = true
		if i == 0 {
			if !m.IsLeader {
				return fmt.Errorf("自定义 Team 首位必须是唯一 Leader")
			}
		} else if m.IsLeader || reservedLeaderRole(m.Role) {
			return fmt.Errorf("自定义 Team 成员 %s 与唯一 Leader 身份冲突", m.MemberID)
		}
	}
	return nil
}
