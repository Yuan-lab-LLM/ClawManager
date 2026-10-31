package systembackup

import (
	"errors"
	"slices"
	"time"
)

type TaskType string

const (
	Backup         TaskType = "backup"
	Drill          TaskType = "drill"
	Preflight      TaskType = "preflight"
	ArtifactVerify TaskType = "artifact_verify"
)

var normalPaths = map[TaskType][]string{
	Backup:         {"pending", "preparing", "ready_for_capture", "acquiring_gate", "capturing", "validating", "publishing", "succeeded"},
	Drill:          {"pending", "preflighting", "provisioning", "restoring", "normalizing", "verifying", "succeeded"},
	Preflight:      {"pending", "running", "succeeded"},
	ArtifactVerify: {"pending", "running", "succeeded"},
}

type Task struct {
	ID              string     `json:"public_id"`
	Type            TaskType   `json:"task_type"`
	Purpose         string     `json:"task_purpose,omitempty"`
	Status          string     `json:"status"`
	Version         uint64     `json:"row_version"`
	ConfigVersion   uint64     `json:"config_version"`
	ActorID         int        `json:"actor_id"`
	CanCancel       bool       `json:"can_cancel"`
	CommitStarted   bool       `json:"commit_started"`
	Registered      bool       `json:"artifact_registered"`
	Eligible        bool       `json:"acceptance_eligible"`
	Failure         string     `json:"failure_category"`
	PendingReason   string     `json:"pending_reason"`
	AttentionReason string     `json:"attention_reason"`
	CreatedAt       time.Time  `json:"created_at"`
	DeadlineAt      time.Time  `json:"deadline_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	LeaseOwner      string     `json:"-"`
	LeaseUntil      *time.Time `json:"-"`
	SourceID        string     `json:"source_public_id,omitempty"`
}

func (t Task) Terminal() bool {
	return slices.Contains([]string{"succeeded", "failed", "canceled"}, t.Status)
}

// Transition validates a task edge before the repository executes a row-version
// CAS. An externally registered backup cannot be changed into failed/canceled.
func (t Task) Transition(next, failure string, now time.Time) (Task, error) {
	if !slices.Contains(FailureCategories, failure) || (next == "failed" && (failure == "none" || failure == "canceled_by_actor" || failure == "canceled_by_shutdown")) {
		return t, errors.New("invalid failure category")
	}
	if t.Terminal() {
		return t, errors.New("state_conflict")
	}
	if t.Registered && (next != "succeeded" || failure != "none") {
		return t, errors.New("registered backup must converge to success")
	}
	if t.Status == "finalization_unknown" && next != "succeeded" && next != "failed" {
		return t, errors.New("state_conflict")
	}
	valid := false
	path := normalPaths[t.Type]
	i := slices.Index(path, t.Status)
	if i >= 0 && i+1 < len(path) && path[i+1] == next {
		valid = true
	}
	if next == "canceling" && t.CanCancel && !t.CommitStarted {
		valid = true
	}
	if next == "failed" && failure != "none" && failure != "" && !t.Registered {
		valid = true
	}
	if next == "canceled" && t.Status == "canceling" && slices.Contains([]string{"canceled_by_actor", "canceled_by_shutdown"}, failure) {
		valid = true
	}
	if next == "finalization_unknown" && t.Type == Backup && t.CommitStarted {
		valid = true
	}
	if t.Status == "finalization_unknown" && next == "succeeded" && t.Registered {
		valid = true
	}
	if t.Registered && next == "succeeded" {
		valid = true
	}
	if !valid {
		return t, errors.New("state_conflict")
	}
	if next == "succeeded" && failure != "none" {
		return t, errors.New("success cannot carry failure")
	}
	if next != "failed" && next != "canceled" && failure != "none" {
		return t, errors.New("invalid failure category for active state")
	}
	// Advancing requires new durable phase evidence. Do not clear attention
	// merely because a caller proposed an invalid transition.
	if i >= 0 && i+1 < len(path) && path[i+1] == next {
		t.AttentionReason = ""
	}
	t.Status = next
	if t.StartedAt == nil && next != "pending" && next != "canceling" && !t.Terminal() {
		t.StartedAt = &now
	}
	t.Failure = failure
	t.Version++
	if t.Terminal() {
		t.CanCancel = false
		t.FinishedAt = &now
	} else if next == "canceling" || next == "finalization_unknown" {
		t.CanCancel = false
	}
	if next == "finalization_unknown" {
		t.AttentionReason = "external_result_unknown"
		t.FinishedAt = nil
	}
	if t.Terminal() {
		t.AttentionReason = ""
		t.PendingReason = ""
	}
	if t.Registered {
		t.Eligible = true
	}
	return t, nil
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}
type EligibilityInput struct {
	Purpose                                                                                                          string
	RequiredParts, RequiredVerifiers, Integrity, Cleanup, Strong, ExternalTarget, RecoverableKey, Signature, Catalog bool
}

func Eligibility(input EligibilityInput) (bool, []string) {
	var reasons []string
	checks := []struct {
		ok   bool
		code string
	}{
		{input.Purpose == "acceptance", "functional_test"}, {input.RequiredParts, "required_part_failed"}, {input.RequiredVerifiers, "required_verifier_failed"}, {input.Integrity, "manifest_drift"}, {input.Cleanup, "staging_cleanup_failed"}, {input.Strong, "consistency_not_strong"}, {input.ExternalTarget, "target_not_external"}, {input.RecoverableKey, "kek_not_recoverable"}, {input.Signature, "attestation_invalid"}, {input.Catalog, "catalog_unavailable"},
	}
	for _, c := range checks {
		if !c.ok {
			reasons = append(reasons, c.code)
		}
	}
	if len(reasons) == 0 {
		return true, []string{"eligible"}
	}
	slices.Sort(reasons)
	return false, reasons
}
