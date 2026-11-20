package handlers

import (
	"bytes"
	"clawreef/internal/config"
	"clawreef/internal/models"
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type shutdownHistoryStore struct {
	recordCalls  int
	recordOwner  int
	recordSource string
	calls        int
	owner, id    int
	before       int64
	items        []models.RuntimeShutdownAudit
	err          error
}

func (s *shutdownHistoryStore) Record(_ context.Context, _ int, owner int, source string, _ models.RuntimeShutdownObservation) error {
	s.recordCalls++
	s.recordOwner = owner
	s.recordSource = source
	return s.err
}

func TestRuntimeShutdownLiteReportIsScopedAndRetryable(t *testing.T) {
	for _, tc := range []struct {
		name, token, claim                              string
		pod                                             int64
		generation, observationGeneration, want, writes int
		storageErr                                      bool
	}{
		{"accepted", "secret", strings.Repeat("a", 64), 9, 3, 3, 200, 1, false},
		{"unauthenticated", "wrong", strings.Repeat("a", 64), 9, 3, 3, 401, 0, false},
		{"foreign pod", "secret", strings.Repeat("a", 64), 10, 3, 3, 200, 0, false},
		{"old generation", "secret", strings.Repeat("a", 64), 9, 2, 2, 200, 0, false},
		{"mismatched observation", "secret", strings.Repeat("a", 64), 9, 3, 2, 400, 0, false},
		{"malformed claim", "secret", "bad", 9, 3, 3, 400, 0, false},
		{"storage unavailable", "secret", strings.Repeat("a", 64), 9, 3, 3, 503, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &shutdownHistoryStore{}
			if tc.storageErr {
				store.err = errors.New("password=secret")
			}
			bindings := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{8: {InstanceID: 8, RuntimePodID: 9, Generation: 3, GatewayID: "gw-8-3", State: "error"}}}
			instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{8: {ID: 8, UserID: 7, RuntimeGeneration: 3, Type: "openclaw"}}}
			h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindings, instances, nil, nil).WithShutdownAudits(store)
			r := gin.New()
			r.POST("/gateways", h.ReportGateways)
			body := fmt.Sprintf(`{"pod_id":%d,"gateways":[{"instance_id":8,"generation":%d,"gateway_id":"gw-8-3","state":"stop_error","shutdown_observation":{"generation":%d,"claim_id":%q,"outcome":"cleanup_unconfirmed"}}]}`, tc.pod, tc.generation, tc.observationGeneration, tc.claim)
			request := httptest.NewRequest("POST", "/gateways", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(runtimeAgentTokenHeader, tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			if w.Code != tc.want || store.recordCalls != tc.writes {
				t.Fatalf("status=%d writes=%d body=%s", w.Code, store.recordCalls, w.Body.String())
			}
			if tc.writes > 0 && (store.recordOwner != 7 || store.recordSource != "lite_agent") {
				t.Fatal("source/owner not server-bound")
			}
			if tc.storageErr && strings.Contains(w.Body.String(), "password") {
				t.Fatal("leaked storage details")
			}
		})
	}
}
func (s *shutdownHistoryStore) List(_ context.Context, id, owner int, before int64) ([]models.RuntimeShutdownAudit, error) {
	s.calls++
	s.id = id
	s.owner = owner
	s.before = before
	return s.items, s.err
}
func TestRuntimeShutdownAuditHistoryAuthorizationAndCursor(t *testing.T) {
	for _, tc := range []struct {
		name, id, query, role string
		user, want            int
	}{
		{"owner", "8", "", "", 7, 200}, {"admin", "8", "", "admin", 9, 200}, {"foreign owner", "8", "", "", 9, 403}, {"anonymous admin", "8", "", "admin", 0, 403}, {"invalid id", "../8", "", "", 7, 400}, {"zero cursor", "8", "?before_id=0", "", 7, 400}, {"negative cursor", "8", "?before_id=-1", "", 7, 400}, {"bad cursor", "8", "?before_id=secret", "", 7, 400}, {"overflow cursor", "8", "?before_id=9223372036854775808", "", 7, 400}, {"older page", "8", "?before_id=51", "", 7, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &shutdownHistoryStore{items: []models.RuntimeShutdownAudit{}}
			h := NewRuntimeShutdownAuditHandler(protocolInstanceStore{instance: &models.Instance{ID: 8, UserID: 7, RuntimeGeneration: 3}}, s)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/"+tc.query, nil)
			c.Params = gin.Params{{Key: "id", Value: tc.id}}
			c.Set("userID", tc.user)
			c.Set("userRole", tc.role)
			h.List(c)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable")
			}
			if tc.want != 200 && s.calls != 0 {
				t.Fatal("unauthorized storage read")
			}
			if tc.want == 200 && (s.owner != 7 || s.id != 8 || !strings.Contains(w.Body.String(), `"diagnostic_only":true`) || !strings.Contains(w.Body.String(), `"items":[]`)) {
				t.Fatal(w.Body.String())
			}
			if tc.name == "older page" && s.before != 51 {
				t.Fatal(s.before)
			}
		})
	}
}
func TestRuntimeShutdownAuditHistoryStorageErrorsStayUnknown(t *testing.T) {
	for _, s := range []*shutdownHistoryStore{nil, {err: errors.New("password=secret")}} {
		h := NewRuntimeShutdownAuditHandler(protocolInstanceStore{instance: &models.Instance{ID: 8, UserID: 7}}, nil)
		if s != nil {
			h.audits = s
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Params = gin.Params{{Key: "id", Value: "8"}}
		c.Set("userID", 7)
		h.List(c)
		if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Body.String())
		}
	}
}
func TestRuntimeShutdownAuditHistoryBoundedCursor(t *testing.T) {
	if nextShutdownCursor(nil) != 0 {
		t.Fatal("empty cursor")
	}
	items := make([]models.RuntimeShutdownAudit, 50)
	items[49].ID = 123
	if nextShutdownCursor(items) != 123 {
		t.Fatal("cursor")
	}
}
