package services

import (
	"clawreef/internal/models"
	"testing"
	"time"
)

func TestTerminalRootCardsCannotReopenMemberAfterNewSuccess(t *testing.T) {
	for _, terminal := range []string{"failed", "succeeded", "stale"} {
		t.Run(terminal, func(t *testing.T) {
			repo := &teamRepositoryStub{tasksByID: map[int]*models.TeamTask{383: {ID: 383, TeamID: 178, Status: terminal}, 386: {ID: 386, TeamID: 178, Status: "succeeded"}}}
			s := &teamService{repo: repo}
			member := &models.TeamMember{ID: 563, TeamID: 178, Role: "worker", Status: "busy", Availability: "busy", CurrentTaskID: intPtr(383)}
			items := []models.TeamWorkItem{{ID: 470, RootTaskID: 383, OwnerMemberID: &member.ID, Status: "running", UpdatedAt: time.Now()}, {ID: 474, RootTaskID: 386, OwnerMemberID: &member.ID, Status: "succeeded", UpdatedAt: time.Now().Add(-time.Minute)}}
			filtered, err := s.memberOperationalItems(178, items)
			if err != nil {
				t.Fatal(err)
			}
			reconcileTeamMemberOperationalState(member, filtered)
			if len(filtered) != 1 || member.Status != "idle" || member.Availability != "idle" || member.CurrentTaskID != nil {
				t.Fatalf("historical root reopened member: %+v", member)
			}
			if items[0].Status != "running" {
				t.Fatal("historical work was mutated")
			}
			// A genuinely current root must survive the same historical card.
			repo.tasksByID[387] = &models.TeamTask{ID: 387, TeamID: 178, Status: "running"}
			items = append(items, models.TeamWorkItem{ID: 476, RootTaskID: 387, OwnerMemberID: &member.ID, Status: "running", UpdatedAt: time.Now()})
			filtered, err = s.memberOperationalItems(178, items)
			if err != nil {
				t.Fatal(err)
			}
			reconcileTeamMemberOperationalState(member, filtered)
			if member.Status != "busy" || member.CurrentTaskID == nil || *member.CurrentTaskID != 387 {
				t.Fatalf("new work was cleared: %+v", member)
			}
			// Missing root facts cannot be guessed terminal.
			items[0].RootTaskID = 999
			filtered, err = s.memberOperationalItems(178, items[:1])
			if err != nil || len(filtered) != 1 {
				t.Fatal("unknown root discarded", err)
			}
		})
	}
}
