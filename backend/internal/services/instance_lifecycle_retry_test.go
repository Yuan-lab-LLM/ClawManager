package services

import (
	"clawreef/internal/models"
	"testing"
)

type retryInstanceRepo struct {
	*v2LifecycleInstanceRepo
	replacement *models.Instance
}

func (r *retryInstanceRepo) GetByProvisioningOperationID(id string) (*models.Instance, error) {
	return r.replacement, nil
}

func TestResetRetryNeverDiscardsPromotedReplacement(t *testing.T) {
	for _, sourceExists := range []bool{false, true} {
		base := newV2LifecycleInstanceRepo()
		replacement := &models.Instance{ID: 202, UserID: 7, Name: "user-instance", Owner: factoryResetString("owner@example.com"), Status: "running"}
		base.byID[202] = replacement
		if sourceExists {
			base.byID[101] = &models.Instance{ID: 101, UserID: 7, Name: factoryResetQuarantineName(101), Owner: factoryResetString(factoryResetQuarantineOwner), Status: "stopped"}
		}
		repo := &retryInstanceRepo{base, replacement}
		s := &instanceService{instanceRepo: repo}
		got, err := s.ReconcileResetRetry(101, "op-original")
		if err != nil || got != replacement {
			t.Fatalf("promoted target not resumed: %v %v", got, err)
		}
		if len(base.deleted) != 0 || len(base.updated) != 0 || len(base.created) != 0 {
			t.Fatal("reconciliation mutated promoted data")
		}
	}
}

func TestResetRetryRefusesAmbiguousStaging(t *testing.T) {
	for _, status := range []string{"creating", "starting", "deleting"} {
		base := newV2LifecycleInstanceRepo()
		base.byID[101] = &models.Instance{ID: 101, UserID: 7, Name: "old", Status: "running"}
		replacement := &models.Instance{ID: 202, UserID: 7, Name: "reset-101-original", Owner: factoryResetString(factoryResetStagingOwner), Status: status}
		base.byID[202] = replacement
		s := &instanceService{instanceRepo: &retryInstanceRepo{base, replacement}}
		if _, err := s.ReconcileResetRetry(101, "op-original"); err == nil {
			t.Fatal("unresolved attempt admitted duplicate reset")
		}
		if len(base.deleted) != 0 || len(base.updated) != 0 {
			t.Fatal("ambiguous staging was destroyed")
		}
	}
}

func TestResetRetryResumesHealthyStaging(t *testing.T) {
	base := newV2LifecycleInstanceRepo()
	base.byID[101] = &models.Instance{ID: 101, UserID: 7, Name: "old", Status: "error"}
	replacement := &models.Instance{ID: 202, UserID: 7, Name: "reset-101-original", Owner: factoryResetString(factoryResetStagingOwner), Status: "running"}
	base.byID[202] = replacement
	s := &instanceService{instanceRepo: &retryInstanceRepo{base, replacement}}
	if got, err := s.ReconcileResetRetry(101, "op-original"); err != nil || got != replacement {
		t.Fatalf("healthy staging not reused: %v", err)
	}
}
