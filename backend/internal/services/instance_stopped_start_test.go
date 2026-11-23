package services

import (
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestOwnerStartStoppedChecksStateWithoutResettingOrReplaying(t *testing.T) {
	for _, state := range []string{"running", "creating", "error", "pending"} {
		t.Run(state, func(t *testing.T) {
			repo := newFakeRuntimeInstanceRepo()
			workspace := "/saved/owner-workspace"
			repo.byID[7] = &models.Instance{ID: 7, Status: state, WorkspacePath: &workspace}
			s := &instanceService{instanceRepo: repo}
			err := s.StartStopped(7)
			if state == "running" || state == "creating" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrInstanceNotStopped) {
				t.Fatalf("unexpected state accepted: %v", err)
			}
			if repo.byID[7].Status != state || *repo.byID[7].WorkspacePath != workspace {
				t.Fatal("start changed saved identity or state")
			}
		})
	}
}

func TestOwnerStartStoppedRetainsTeamGuard(t *testing.T) {
	blocked := errors.New("Team member must use Team lifecycle")
	s := &instanceService{ownerTeamMutationGuard: func(id int, action string, teamID int) error {
		if id != 7 || action != "start" || teamID != 0 {
			t.Fatal("wrong guard identity")
		}
		return blocked
	}}
	// A nil repository would panic if the guard were bypassed.
	if err := s.StartStopped(7); !errors.Is(err, blocked) {
		t.Fatalf("Team protection lost: %v", err)
	}
}
