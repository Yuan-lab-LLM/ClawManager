package services

import (
	"context"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestTeamMemberStartupIsTransitionNotFailure(t *testing.T) {
	for _, runtime := range []string{"openclaw", "hermes"} {
		for _, mode := range []string{"lite", "pro"} {
			for _, phase := range []string{"creating", "starting", "pending"} {
				t.Run(runtime+"/"+mode+"/"+phase, func(t *testing.T) {
					oldReason := teamRuntimeUnavailableReasonPrefix + "linked instance is starting"
					member := &models.TeamMember{ID: 1, InstanceMode: mode, Status: "creating", Availability: "offline", BlockedReason: &oldReason}
					unavailable, changed := projectTeamMemberInstanceAvailability(member, &models.Instance{Type: runtime, InstanceMode: mode, Status: phase}, nil)
					if !unavailable || !changed || member.Status != "creating" || member.Availability != "unknown" || member.BlockedReason != nil || derefTeamString(member.RuntimeStatus) != phase {
						t.Fatalf("normal transition became fault: %+v", member)
					}
				})
			}
		}
	}
}

func TestTeamMemberRuntimeRecoveryPreservesBusinessState(t *testing.T) {
	for _, phase := range []string{"starting", "running"} {
		member := &models.TeamMember{ID: 1, Status: "busy", Availability: "unknown", CurrentTaskID: intPtr(45), Progress: 37, RuntimeStatus: optionalString("starting")}
		// A previous successful item must not clear a newer pending binding.
		items := []models.TeamWorkItem{{ID: 1, RootTaskID: 44, OwnerMemberID: &member.ID, Status: "succeeded"}}
		projectTeamMemberInstanceAvailability(member, &models.Instance{Status: phase}, items)
		if member.Status != "busy" || member.CurrentTaskID == nil || *member.CurrentTaskID != 45 || member.Progress != 37 {
			t.Fatalf("%s cleared active binding/progress: %+v", phase, member)
		}
	}
	for _, itemStatus := range []string{"running", "waiting", "succeeded", "failed"} {
		member := &models.TeamMember{ID: 1, Status: "creating", Availability: "unknown", RuntimeStatus: optionalString("starting")}
		item := models.TeamWorkItem{ID: 2, RootTaskID: 45, OwnerMemberID: &member.ID, Status: itemStatus}
		projectTeamMemberInstanceAvailability(member, &models.Instance{Status: "running"}, []models.TeamWorkItem{item})
		if itemStatus == "running" || itemStatus == "waiting" {
			if member.Status != "busy" || member.Availability != "busy" || member.CurrentTaskID == nil || *member.CurrentTaskID != 45 {
				t.Fatalf("active ledger was made idle: %+v", member)
			}
		} else if derefTeamString(member.RuntimeStatus) != itemStatus || (itemStatus == "failed" && member.Availability != "blocked") {
			t.Fatalf("terminal business state overwritten by liveness: %+v", member)
		}
	}
	for _, phase := range []string{"starting", "running"} {
		reason := "Missing required source file"
		member := &models.TeamMember{ID: 1, Status: "busy", Availability: "unknown", CurrentTaskID: intPtr(45), BlockedReason: &reason}
		projectTeamMemberInstanceAvailability(member, &models.Instance{Status: phase}, []models.TeamWorkItem{{RootTaskID: 45, OwnerMemberID: &member.ID, Status: "running"}})
		if derefTeamString(member.BlockedReason) != reason || member.Availability != "blocked" {
			t.Fatalf("%s hid independent business blocker: %+v", phase, member)
		}
	}
}

func TestTeamMemberFaultAndRecoveryWithoutAssignment(t *testing.T) {
	member := &models.TeamMember{ID: 1, Status: "idle", Availability: "idle"}
	errText := "gateway exited"
	projectTeamMemberInstanceAvailability(member, &models.Instance{Status: "error", RuntimeErrorMessage: &errText}, nil)
	if member.Status != "offline" || member.Availability != "offline" || member.BlockedReason == nil {
		t.Fatalf("real Runtime fault hidden: %+v", member)
	}
	projectTeamMemberInstanceAvailability(member, &models.Instance{Status: "running"}, nil)
	if member.Status != "idle" || member.Availability != "idle" || member.BlockedReason != nil {
		t.Fatalf("Runtime recovery stuck: %+v", member)
	}
}

func TestTeamMemberRuntimeRecoveryRestoresNewerLedgerBinding(t *testing.T) {
	member := &models.TeamMember{ID: 1, Status: "offline", Availability: "offline", CurrentTaskID: intPtr(44), BlockedReason: optionalString(teamRuntimeUnavailableReasonPrefix + "gateway exited")}
	items := []models.TeamWorkItem{{ID: 1, RootTaskID: 44, OwnerMemberID: &member.ID, Status: "succeeded"}, {ID: 2, RootTaskID: 45, OwnerMemberID: &member.ID, Status: "running"}}
	projectTeamMemberInstanceAvailability(member, &models.Instance{Status: "running"}, items)
	if member.Status != "busy" || member.CurrentTaskID == nil || *member.CurrentTaskID != 45 || member.BlockedReason != nil {
		t.Fatalf("newer active ledger binding lost: %+v", member)
	}
}

func TestTeamMemberDeletedAndMissingInstanceBoundaries(t *testing.T) {
	for _, status := range []string{"deleted", "deleting"} {
		member := &models.TeamMember{ID: 1, InstanceID: intPtr(9), Status: status, Availability: "offline"}
		service := &teamService{instanceService: &teamRuntimeAvailabilityInstanceServiceStub{instances: map[int]*models.Instance{9: {ID: 9, Status: "running"}}}}
		_, _, changed, err := service.reconcileTeamMemberRuntimeAvailability(member, nil, time.Now())
		if err != nil || changed || member.Status != status {
			t.Fatal("deleted member resurrected")
		}
	}
	member := &models.TeamMember{ID: 1, InstanceID: intPtr(9), Status: "idle", Availability: "idle"}
	service := &teamService{instanceService: &teamRuntimeAvailabilityInstanceServiceStub{instances: map[int]*models.Instance{}}}
	unavailable, _, _, err := service.reconcileTeamMemberRuntimeAvailability(member, nil, time.Now())
	if err != nil || !unavailable || member.Availability != "offline" {
		t.Fatal("missing instance was made healthy")
	}
}

func TestGetTeamProjectsCurrentInstanceWithoutWrites(t *testing.T) {
	for _, mode := range []string{"leader_mediated", "peer"} {
		now := time.Now().UTC()
		member := &models.TeamMember{ID: 1, TeamID: 7, UserID: 8, InstanceID: intPtr(9), Status: "creating", Availability: "offline", RuntimeStatus: optionalString("starting"), BlockedReason: optionalString(teamRuntimeUnavailableReasonPrefix + "linked instance is starting"), UpdatedAt: now}
		repo := &teamRepositoryStub{teamsByID: map[int]*models.Team{7: {ID: 7, UserID: 8, CommunicationMode: mode}}, membersByID: map[int]*models.TeamMember{1: member}}
		instances := &teamRuntimeAvailabilityInstanceServiceStub{instances: map[int]*models.Instance{9: {ID: 9, UserID: 8, Status: "running"}}}
		service := &teamService{repo: repo, instanceService: instances}
		data, err := service.GetTeam(8, 7)
		if err != nil || len(data.Members) != 1 || data.Members[0].Availability != "idle" || data.Members[0].BlockedReason != nil {
			t.Fatalf("fresh instance not projected: %+v %v", data, err)
		}
		if member.Availability != "offline" || member.BlockedReason == nil || repo.updatedMember != nil || repo.updatedTask != nil || !data.Members[0].UpdatedAt.Equal(now) || data.Members[0].LastSeenAt != nil {
			t.Fatal("GET mutated persisted state or invented a heartbeat")
		}
		instances.err = context.DeadlineExceeded
		data, err = service.GetTeam(8, 7)
		if err != nil || data.Members[0].Availability != "offline" || repo.updatedMember != nil {
			t.Fatal("transient read failure changed state or broke Team details")
		}
		instances.err = nil
		instances.instances[9].UserID = 10
		data, err = service.GetTeam(8, 7)
		if err != nil || data.Members[0].Availability != "offline" {
			t.Fatal("foreign-owner instance facts leaked into projection")
		}
		if _, err := service.GetTeam(10, 7); err == nil {
			t.Fatal("foreign Team owner allowed")
		}
	}
}

type startupConsistencyRepository struct{ *teamRepositoryStub }

func (r *startupConsistencyRepository) ListActiveTeams() ([]models.Team, error) {
	return []models.Team{*r.teamsByID[7]}, nil
}

func TestStartupConsistencySweepDoesNotEraseBusinessBlocker(t *testing.T) {
	member := &models.TeamMember{ID: 1, TeamID: 7, UserID: 8, InstanceID: intPtr(9), Status: "busy", Availability: "blocked", CurrentTaskID: intPtr(45), RuntimeStatus: optionalString("starting"), BlockedReason: optionalString("Missing source file")}
	base := &teamRepositoryStub{teamsByID: map[int]*models.Team{7: {ID: 7, UserID: 8, CommunicationMode: "leader_mediated"}}, membersByID: map[int]*models.TeamMember{1: member}, tasksByID: map[int]*models.TeamTask{45: {ID: 45, TeamID: 7, Status: "running"}}, workItems: []models.TeamWorkItem{{RootTaskID: 45, OwnerMemberID: &member.ID, Status: "running"}}}
	service := &teamService{repo: &startupConsistencyRepository{base}, instanceService: &teamRuntimeAvailabilityInstanceServiceStub{instances: map[int]*models.Instance{9: {ID: 9, UserID: 8, Status: "running"}}}}
	if err := service.sweepTeamControlPlaneConsistency(); err != nil {
		t.Fatal(err)
	}
	for _, update := range base.updatedMembers {
		if update.Availability != "blocked" || derefTeamString(update.BlockedReason) != "Missing source file" || update.CurrentTaskID == nil || *update.CurrentTaskID != 45 {
			t.Fatalf("sweep erased business blocker: %+v", update)
		}
	}
	if base.updatedTask != nil || len(base.createdEvents) != 0 {
		t.Fatal("liveness synthesized business events")
	}
}
