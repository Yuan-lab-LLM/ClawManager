package systembackup

import (
	"testing"
	"time"
)

func TestRegisteredBackupCannotBeCanceledOrFailed(t *testing.T) {
	task := Task{Type: Backup, Status: "publishing", Registered: true, CommitStarted: true, CanCancel: false}
	for _, next := range []string{"failed", "canceling", "canceled"} {
		if _, err := task.Transition(next, "runner_failed", time.Now()); err == nil {
			t.Fatal("registered fact reversed")
		}
	}
	got, err := task.Transition("succeeded", "none", time.Now())
	if err != nil || !got.Eligible || got.CanCancel || got.FinishedAt == nil {
		t.Fatal(got, err)
	}
}
func TestStateEdgesDoNotSkipCaptureOrVerifier(t *testing.T) {
	for kind, path := range normalPaths {
		task := Task{Type: kind, Status: "pending", CanCancel: true}
		if _, err := task.Transition("succeeded", "none", time.Now()); err == nil {
			t.Fatal("skipped all checks")
		}
		for _, next := range path[1:] {
			var err error
			task, err = task.Transition(next, "none", time.Now())
			if err != nil {
				t.Fatal(kind, next, err)
			}
		}
		if _, err := task.Transition("pending", "none", time.Now()); err == nil {
			t.Fatal("terminal task resurrected")
		}
	}
}
func TestEligibilityRequiresEveryIndependentFact(t *testing.T) {
	input := EligibilityInput{"acceptance", true, true, true, true, true, true, true, true, true}
	if ok, _ := Eligibility(input); !ok {
		t.Fatal("complete evidence rejected")
	}
	input.Catalog = false
	if ok, _ := Eligibility(input); ok {
		t.Fatal("upload mistaken for disaster eligibility")
	}
	input.Catalog = true
	input.Cleanup = false
	if ok, _ := Eligibility(input); ok {
		t.Fatal("cleanup ignored")
	}
}

func TestFailureCategoryIsClosedAndCancellationIsNotFailure(t *testing.T) {
	task := Task{Type: Backup, Status: "preparing", CanCancel: true}
	for _, category := range []string{"made_up", "state_conflict", "canceled_by_actor", "canceled_by_shutdown"} {
		if _, e := task.Transition("failed", category, time.Now()); e == nil {
			t.Errorf("invalid failure category accepted: %s", category)
		}
	}
}
