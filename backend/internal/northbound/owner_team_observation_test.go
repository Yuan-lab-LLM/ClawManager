package northbound

import (
	"clawreef/internal/models"
	"testing"
	"time"
)

func TestOwnerTeamSummaryUsesIndependentLivenessWithoutFirstTask(t *testing.T) {
	now := time.Now().UTC()
	for _, state := range []string{"online", "unknown", "offline", "starting"} {
		m := models.TeamMember{ID: 1, TeamID: 7, Status: "idle", Availability: map[string]string{"online": "idle", "unknown": "unknown", "offline": "offline", "starting": "unknown"}[state], RuntimeObservation: &models.TeamMemberRuntimeObservation{State: state, ObservedAt: &now}}
		summary := ownerTeamSummary(models.Team{ID: 7, Status: "running"}, []models.TeamMember{m})
		if (summary["available_count"].(int) == 1) != (state == "online") {
			t.Fatalf("bad count %+v", summary)
		}
		if (summary["fault_count"].(int) == 1) != (state == "offline") {
			t.Fatalf("confirmation became fault %+v", summary)
		}
		view := summary["members"].([]map[string]interface{})[0]
		if view["availability"] != m.Availability || view["last_seen_at"] != (*time.Time)(nil) {
			t.Fatalf("task heartbeat fabricated %+v", view)
		}
		if m.LastSeenAt != nil || m.CurrentTaskID != nil {
			t.Fatal("read changed member")
		}
	}
}
