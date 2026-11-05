package services

import (
	"errors"
	"testing"

	"clawreef/internal/models"
)

// Source-only regression: local tests/builds remain prohibited by the user.
type idleStoppedTeamInstances struct {
	InstanceService
	instance *models.Instance
	starts   int
}

func (s *idleStoppedTeamInstances) CreatePrevalidated(int, CreateInstanceRequest) (*models.Instance, error) {
	return s.instance, nil
}
func (s *idleStoppedTeamInstances) Start(int) error { s.starts++; return nil }

func TestIdleTeamReuseNeverStartsStoppedInstanceWithoutReason(t *testing.T) {
	for _, reason := range []string{"", "manual", "idle_timeout"} {
		t.Run("reason="+reason, func(t *testing.T) {
			owner := "owner@example.test"
			instances := &idleStoppedTeamInstances{instance: &models.Instance{ID: 71, UserID: 7, Owner: &owner, Type: "hermes", InstanceMode: "lite", Status: "stopped", StoppedReason: reason}}
			svc := &teamService{repo: &teamRepositoryStub{}, instanceService: instances}
			team := &models.Team{ID: 12, UserID: 7, Owner: &owner, Name: "reuse", ResourceScope: models.TeamScopeNorthbound, SharedMountPath: "/team"}
			plan := plannedTeamMember{MemberKey: "worker", DisplayName: "Worker", RuntimeType: "hermes", InstanceMode: "lite"}
			if _, err := svc.createTeamMemberInstance(7, team, plan, nil, ""); !errors.Is(err, models.ErrInstanceStopping) {
				t.Fatalf("stopped Team member reused: %v", err)
			}
			if instances.starts != 0 {
				t.Fatalf("Team reuse woke a stopped instance: %d Start calls", instances.starts)
			}
		})
	}
}

// Nil store/native dependencies prove upgrade rejection happens before any
// preflight, stop claim, cleanup, or slot release, for both recovery modes.
func TestIdleControllerHonorsWorkspaceUpgradeFenceBeforeEffects(t *testing.T) {
	for _, operation := range []string{"claim", "resume-idle", "resume-manual"} {
		t.Run(operation, func(t *testing.T) {
			guard := &mergeBlockingUpgradeGuard{}
			lifecycle := &instanceService{instanceRepo: newV2LifecycleInstanceRepo(), runtimeUpgradeGuard: guard}
			svc := &InstanceAutoStopService{lifecycle: lifecycle}
			if operation == "claim" {
				if err := svc.tryStop(t.Context(), models.InstanceAutoStopSettings{}, models.InstanceModelActivity{InstanceID: 17}); !errors.Is(err, ErrWorkspaceUpgradeBusy) {
					t.Fatalf("idle stop bypassed upgrade fence: %v", err)
				}
			} else {
				kind := "idle"
				if operation == "resume-manual" {
					kind = "manual"
				}
				svc.resume(t.Context(), models.InstanceIdleStopOperation{ID: "pending", InstanceID: 17, Kind: kind})
			}
			if len(guard.calls) != 1 || guard.calls[0] != 17 {
				t.Fatalf("mutation guard missing or reacquired: %v", guard.calls)
			}
			release, err := acquireInstanceLifecycleOperation(t.Context(), lifecycle.instanceRepo, 17)
			if err != nil {
				t.Fatalf("lifecycle lock leaked: %v", err)
			}
			release()
		})
	}
}
