package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"

	"github.com/gin-gonic/gin"
)

type stubEgressAuditService struct {
	events []*models.AuditEvent
}

func (s *stubEgressAuditService) RecordEvent(event *models.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *stubEgressAuditService) ListEventsByTraceID(string) ([]models.AuditEvent, error) {
	return nil, nil
}

func TestEgressProxyHandlerBlocksDeniedConnectHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &stubEgressAuditService{}
	handler := &EgressProxyHandler{
		policy: egresspolicy.Policy{
			Mode:               egresspolicy.ModeDenylist,
			DeniedHostSuffixes: []string{"api.openai.com"},
		},
		audit: audit,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodConnect, "https://api.openai.com:443", nil)
	ctx.Request.Host = "api.openai.com:443"
	ctx.Request.Header.Set("X-ClawManager-Instance-Id", "42")

	handler.handleConnect(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != "egress.llm.blocked" {
		t.Fatalf("expected egress audit event, got %+v", audit.events)
	}
	if audit.events[0].InstanceID == nil || *audit.events[0].InstanceID != 42 {
		t.Fatalf("expected instance id 42 on egress audit event, got %+v", audit.events[0].InstanceID)
	}
}

func TestEgressProxyHandlerAcceptsEgressInstanceHeaderAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	audit := &stubEgressAuditService{}
	handler := &EgressProxyHandler{
		policy: egresspolicy.Policy{
			Mode:               egresspolicy.ModeDenylist,
			DeniedHostSuffixes: []string{"api.openai.com"},
		},
		audit: audit,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodConnect, "https://api.openai.com:443", nil)
	ctx.Request.Host = "api.openai.com:443"
	ctx.Request.Header.Set("X-ClawManager-Egress-Instance-Id", "77")

	handler.handleConnect(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if audit.events[0].InstanceID == nil || *audit.events[0].InstanceID != 77 {
		t.Fatalf("expected instance id 77 on egress audit event, got %+v", audit.events[0].InstanceID)
	}
}
