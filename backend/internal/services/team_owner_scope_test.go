package services

import (
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type ownerTeamScopeRepoFixture struct {
	repository.TeamRepository
	team *models.Team
}

func (r ownerTeamScopeRepoFixture) GetTeamByID(int) (*models.Team, error) { return r.team, nil }

func TestOwnerTeamCMGuardPreservesLegacyDenialAndHidesNewType(t *testing.T) {
	owner := "owner@example.com"
	for _, test := range []struct {
		name      string
		team      *models.Team
		user      int
		errorText string
	}{
		{"legacy-owner", &models.Team{ID: 1, UserID: 1}, 1, ""},
		{"legacy-foreign", &models.Team{ID: 1, UserID: 1}, 2, "access denied"},
		{"owner-team-same-provider", &models.Team{ID: 1, UserID: 1, ResourceScope: models.TeamScopeNorthbound, Owner: &owner}, 1, "team not found"},
		{"owner-team-foreign-provider", &models.Team{ID: 1, UserID: 1, ResourceScope: models.TeamScopeNorthbound, Owner: &owner}, 2, "team not found"},
		{"unknown-scope", &models.Team{ID: 1, UserID: 1, ResourceScope: "unknown"}, 1, "team not found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &teamService{repo: ownerTeamScopeRepoFixture{team: test.team}}
			err := service.CheckCMTeam(test.user, 1)
			if test.errorText == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != test.errorText {
				t.Fatalf("legacy/type contract mismatch: %v", err)
			}
		})
	}
}

func TestNorthboundTeamMemberInheritsOwnerBeforeCreate(t *testing.T) {
	owner := "owner@example.com"
	team := &models.Team{ID: 12, UserID: 3, Name: "nb-team-12", ResourceScope: models.TeamScopeNorthbound, Owner: &owner}
	s := &teamService{}
	req := s.buildTeamMemberInstanceRequest(team, plannedTeamMember{MemberKey: "leader", RuntimeType: "openclaw", InstanceMode: "lite"})
	if req.Owner == nil || *req.Owner != owner {
		t.Fatalf("member owner must be set before validation/provisioning: %#v", req.Owner)
	}
	team.ResourceScope, team.Owner = models.TeamScopeCM, nil
	if got := s.buildTeamMemberInstanceRequest(team, plannedTeamMember{MemberKey: "leader", RuntimeType: "openclaw", InstanceMode: "lite"}); got.Owner != nil {
		t.Fatal("legacy CM Team must not acquire an inferred owner")
	}
}

func TestOwnerTeamScopeFailsClosed(t *testing.T) {
	owner := "owner@example.com"
	for _, tc := range []struct {
		name  string
		team  models.Team
		valid bool
	}{
		{"legacy", models.Team{}, true},
		{"owner", models.Team{ResourceScope: models.TeamScopeNorthbound, Owner: &owner}, true},
		{"missing-owner", models.Team{ResourceScope: models.TeamScopeNorthbound}, false},
		{"unknown-scope", models.Team{ResourceScope: "invalid", Owner: &owner}, false},
		{"cm-with-owner", models.Team{ResourceScope: models.TeamScopeCM, Owner: &owner}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.team.ValidateScope() == nil) != tc.valid {
				t.Fatal("invalid resource scope accepted, or legacy compatibility broken")
			}
		})
	}
}
