package repository

import (
	"testing"

	"clawreef/internal/models"
)

func TestTeamDeletionRejectsOnlyActiveOrUnknownLifecycle(t *testing.T) {
	for _, state := range []string{"running", "stopped", "recovery_required", "starting", "stopping", "unknown", ""} {
		for _, recovery := range []string{"", "protected-recovery"} {
			op := &models.TeamLifecycleOperation{TeamRuntimeState: models.TeamRuntimeState{State: state}, FaultRecoveryOperationID: recovery}
			want := recovery == "" && (state == "running" || state == "stopped" || state == "recovery_required")
			if got := checkTeamDeletionLifecycle(op) == nil; got != want {
				t.Fatalf("state=%q recovery=%q admitted=%v", state, recovery, got)
			}
		}
	}
}

func TestTeamDeletionFencesStartAndRetainsFailedMemberMySQL(t *testing.T) {
	s, r := teamIdleFixture(t)
	ctx := t.Context()
	if _, err := s.SQL().Exec(`UPDATE instances SET status='error' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	projection, err := r.Get(ctx, 9)
	if err != nil || projection.State != "recovery_required" {
		t.Fatalf("fixture is not a failed-member Team: %+v %v", projection, err)
	}
	if err := r.CheckDeletion(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkDeleting(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Request(ctx, 9, "running", "manual", 0); err == nil {
		t.Fatal("Start bypassed durable Team deletion intent")
	}
	var status string
	if err := teamQueryRow(ctx, s, `SELECT status FROM instances WHERE id=1`).Scan(&status); err != nil || status != "error" {
		t.Fatalf("deletion admission released member without proof: %q %v", status, err)
	}
}
