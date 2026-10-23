package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type blockingContextAuditRepo struct {
	repository.AuditEventRepository
	entered chan context.Context
}

func (*blockingContextAuditRepo) Create(*models.AuditEvent) error {
	return fmt.Errorf("unbounded audit write must not be called")
}

func (r *blockingContextAuditRepo) CreateContext(ctx context.Context, _ *models.AuditEvent) error {
	r.entered <- ctx
	<-ctx.Done()
	return ctx.Err()
}

func TestEgressAuditWriteIsBoundedAndRequestCancellationReleasesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &blockingContextAuditRepo{entered: make(chan context.Context, 1)}
	h := NewEgressProxyHandler(services.NewAuditEventService(repo))
	h.policy = egresspolicy.Policy{Mode: egresspolicy.ModeAllowlist}
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://unlisted.invalid/", nil).WithContext(requestCtx)
	done := make(chan struct{})
	go func() { defer close(done); h.Handle(c) }()
	select {
	case ctx := <-repo.entered:
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("audit database write has no bounded deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("context-aware audit repository was not called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled request remained stuck on audit write")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
