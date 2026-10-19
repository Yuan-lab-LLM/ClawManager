package services

import (
	"clawreef/internal/models"
	"testing"
)

func TestRecoveryAdmissionIsExplicitAndScoped(t *testing.T) {
	for _, tc := range []struct {
		mode string
		ids  []int
		id   int
		want bool
	}{
		{"", []int{481}, 481, false}, {"retry", nil, 481, false},
		{"retry", []int{481}, 481, true}, {"rebuild", []int{481}, 481, true},
		{"retry", []int{481}, 482, false}, {"reset", []int{481}, 481, false},
	} {
		scope := runtimeUpgradeScope{RecoveryMode: tc.mode, CandidateInstanceIDs: tc.ids}
		if got := scope.allowsRecovery(tc.id); got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func TestScopedRolloutDoesNotBlockUnselectedInstances(t *testing.T) {
	raw := `{"candidate_instance_ids":[481],"recovery_mode":"retry"}`
	if activeOpenClawRolloutBlocksInstance("session_migration", "", raw, false, "") {
		t.Fatal("unselected instance was globally blocked")
	}
	if !activeOpenClawRolloutBlocksInstance("session_migration", "", raw, true, "quiescing") {
		t.Fatal("selected writer was unfenced")
	}
}

func TestRecoveryActionNeverUsesReset(t *testing.T) {
	for _, mode := range []string{"retry", "rebuild"} {
		raw := `{"candidate_instance_ids":[481],"recovery_mode":"` + mode + `","recovery_parent_id":136}`
		r := &models.RuntimeRollout{PreflightJSON: &raw}
		s := runtimeUpgradeScopeFromRollout(r)
		if !s.allowsRecovery(481) || s.RecoveryParentID != 136 {
			t.Fatalf("lost recovery scope: %+v", s)
		}
	}
}
