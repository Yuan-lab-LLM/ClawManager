package services

import (
	"errors"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type reportingUserRepo struct {
	repository.UserRepository
	batchCalls int
}

func (r *reportingUserRepo) GetByID(int) (*models.User, error) {
	return nil, errors.New("reporting must not load each user separately")
}
func (r *reportingUserRepo) UsernamesByIDs(ids []int) (map[int]string, error) {
	r.batchCalls++
	names := make(map[int]string, len(ids))
	for _, id := range ids {
		names[id] = "operator"
	}
	return names, nil
}

type sqlAuditReporterStub struct {
	*stubSessionUsageInvocationRepo
	query AuditQuery
	err   error
}

func (r *sqlAuditReporterStub) ListRecent(int) ([]models.ModelInvocation, error) {
	return nil, errors.New("recent window must not be used for SQL audit paging")
}
func (r *sqlAuditReporterStub) ListAuditReport(page, limit int, search, status, model string) ([]models.ModelInvocation, int, error) {
	r.query = AuditQuery{Page: page, Limit: limit, Search: search, Status: status, Model: model}
	return []models.ModelInvocation{
		{ID: 1, TraceID: "z", UserID: intPtr(1), Status: "completed", CreatedAt: time.Unix(1, 0)},
		{ID: 2, TraceID: "a", UserID: intPtr(2), Status: "failed", CreatedAt: time.Unix(1, 0)},
	}, 2001, r.err
}

func TestReportingUsernamesUseOneBoundedBatch(t *testing.T) {
	users := &reportingUserRepo{}
	s := &aiObservabilityService{userRepo: users}
	names, err := s.loadUsernamesForAudit([]models.ModelInvocation{{UserID: intPtr(1)}, {UserID: intPtr(2)}, {UserID: intPtr(1)}}, nil, nil)
	if err != nil || len(names) != 2 || users.batchCalls != 1 {
		t.Fatalf("bounded batch not used: %+v calls=%d err=%v", names, users.batchCalls, err)
	}
}

func TestAuditPagingUsesSQLTotalAndFiltersWithoutRecentWindow(t *testing.T) {
	repo := &sqlAuditReporterStub{stubSessionUsageInvocationRepo: &stubSessionUsageInvocationRepo{}}
	users := &reportingUserRepo{}
	s := &aiObservabilityService{invocationRepo: repo, userRepo: users}
	query := AuditQuery{Page: 17, Limit: 20, Search: "historical", Status: "completed", Model: "model"}
	page, err := s.ListAuditItems(query)
	if err != nil || page.Total != 2001 || page.Page != 17 || len(page.Items) != 2 || page.Items[0].TraceID != "z" || repo.query != query || users.batchCalls != 1 {
		t.Fatalf("SQL page semantics changed: %+v query=%+v err=%v", page, repo.query, err)
	}
	repo.err = errors.New("query unavailable")
	if _, err := s.ListAuditItems(query); err == nil {
		t.Fatal("SQL error hidden by an incomplete fallback page")
	}
}
