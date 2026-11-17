package services

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/repository"
)

type directSessionDetailRepo struct {
	*stubSessionUsageInvocationRepo
	calls   int
	err     error
	missing bool
}

func (r *directSessionDetailRepo) AggregateByInstanceSession(int, repository.SessionUsageFilter) ([]repository.InstanceSessionTokenAggregate, error) {
	return nil, errors.New("single-session detail must not load all session aggregates")
}
func (r *directSessionDetailRepo) GetSessionUsageReportItem(instanceID int, sessionID string, filter repository.SessionUsageFilter) (*repository.SessionUsageReportItem, error) {
	r.calls++
	if r.missing || r.err != nil {
		return nil, r.err
	}
	return &repository.SessionUsageReportItem{SessionID: sessionID, TotalTokens: 42, InvocationCount: 7, EstimatedCost: 2, Currency: "USD", FirstSeenAt: time.Unix(1, 0), LastSeenAt: time.Unix(2, 0)}, nil
}

func TestSessionDetailUsesExactSQLSessionWithoutFullHistory(t *testing.T) {
	s := feeDetailService(&feeDetailCostRepo{stubSessionUsageCostRepo: &stubSessionUsageCostRepo{}})
	r := &directSessionDetailRepo{stubSessionUsageInvocationRepo: &stubSessionUsageInvocationRepo{}}
	s.invocationRepo = r
	detail, err := s.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{})
	if err != nil || detail.TotalTokens != 42 || detail.InvocationCount != 7 || detail.EstimatedCost != 2 || r.calls != 1 {
		t.Fatalf("exact detail not used: %+v calls=%d err=%v", detail, r.calls, err)
	}
	r.err = errors.New("exact session query unavailable")
	if _, err := s.GetInstanceSessionUsageDetail(9, "main", repository.SessionUsageFilter{}); !errors.Is(err, r.err) {
		t.Fatalf("exact query error hidden: %v", err)
	}
	r.err, r.missing = nil, true
	if _, err := s.GetInstanceSessionUsageDetail(9, "missing", repository.SessionUsageFilter{}); err == nil {
		t.Fatal("missing exact session reported as found")
	}
}
