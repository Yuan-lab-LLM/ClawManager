package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeTaskRequiresBoundInstanceAndCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bound      bool
		generation string
		want       bool
	}{
		{"valid", true, "3", true}, {"old process", true, "2", false}, {"personal key", false, "3", false}, {"bad generation", true, "bad", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", nil)
			c.Set("userID", 7)
			if tc.bound {
				c.Set("instanceID", 8)
				c.Set("runtimeGeneration", 3)
			}
			c.Request.Header.Set("X-ClawManager-Session-ID", "ses_one")
			c.Request.Header.Set("X-ClawManager-Task-ID", "msg_one")
			c.Request.Header.Set("X-ClawManager-Runtime-Generation", tc.generation)
			req := aigateway.ChatCompletionRequest{}
			if got := attachRuntimeTask(c, &req); got != tc.want {
				t.Fatalf("got %v body=%s", got, w.Body.String())
			}
			if tc.want && (req.RuntimeTask.UserID != 7 || req.RuntimeTask.InstanceID != 8 || *req.OpenClawSessionKey != "ses_one") {
				t.Fatalf("bad scope %+v", req.RuntimeTask)
			}
		})
	}
}

func TestRuntimeTaskNeverUsesMainAccountingFallback(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	req := aigateway.ChatCompletionRequest{}
	if !attachRuntimeTask(c, &req) || req.RuntimeTask != nil {
		t.Fatal("legacy request synthesized a task")
	}
}

func TestRuntimeTaskLegacyAndEnrolledHeaderBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol int
		session  string
		task     string
		gen      string
		status   int
	}{
		{"legacy-no-headers", 0, "", "", "", 200},
		{"legacy-opt-in-complete-identity", 0, "ses_one", "msg_one", "3", 200},
		{"legacy-partial-identity", 0, "ses_one", "", "", 400},
		{"enrolled-cannot-omit-headers", 1, "", "", "", 409},
		{"enrolled-cannot-omit-task", 1, "ses_one", "", "3", 409},
		{"enrolled-cannot-omit-session", 1, "", "msg_one", "3", 400},
		{"enrolled-cannot-reuse-old-generation", 1, "ses_one", "msg_one", "2", 403},
		{"enrolled-current-identity", 1, "ses_one", "msg_one", "3", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", nil)
			c.Set("userID", 7)
			c.Set("instanceID", 8)
			c.Set("runtimeGeneration", 3)
			c.Set("runtimeGovernanceProtocol", tc.protocol)
			c.Request.Header.Set("X-ClawManager-Session-ID", tc.session)
			c.Request.Header.Set("X-ClawManager-Task-ID", tc.task)
			c.Request.Header.Set("X-ClawManager-Runtime-Generation", tc.gen)
			req := aigateway.ChatCompletionRequest{}
			accepted := attachRuntimeTask(c, &req)
			if accepted != (tc.status == 200) || w.Code != tc.status {
				t.Fatalf("accepted=%v status=%d body=%s", accepted, w.Code, w.Body.String())
			}
			if !accepted && req.RuntimeTask != nil {
				t.Fatal("rejected identity attached to request")
			}
			if accepted && (req.RuntimeTask != nil) != (tc.task != "") {
				t.Fatal("legacy request synthesized identity or valid identity lost")
			}
		})
	}
}

type runtimeReceiptStore struct {
	state    models.GatewayTaskState
	identity models.GatewayTaskIdentity
}

func (s *runtimeReceiptStore) Check(_ context.Context, identity models.GatewayTaskIdentity) (*models.GatewayTaskState, error) {
	s.identity = identity
	state := s.state
	return &state, nil
}
func (*runtimeReceiptStore) Bind(context.Context, models.GatewayTaskIdentity, int) error { return nil }
func (s *runtimeReceiptStore) Finish(_ context.Context, _ models.GatewayTaskIdentity, cancelled bool) error {
	s.state.Finished = true
	s.state.RuntimeAcknowledged = s.state.RuntimeAcknowledged || (s.state.Revoked && cancelled)
	return nil
}

func TestRuntimeTaskReceiptUsesTrustedOwnerAndOnlyAcknowledgesRevocation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal finish", true: "cancel acknowledgement"}[cancelled], func(t *testing.T) {
			store := &runtimeReceiptStore{state: models.GatewayTaskState{Revoked: true}}
			audit := &stubEgressAuditService{}
			h := NewGatewayRuntimeHandler(store, nil, audit)
			for attempt := 0; attempt < 2; attempt++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				flag := "false"
				if cancelled {
					flag = "true"
				}
				c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"instance_id":999,"user_id":999,"session_id":"ses_one","task_id":"msg_one","generation":3,"cancelled":`+flag+`}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set("userID", 7)
				c.Set("instanceID", 8)
				c.Set("runtimeGeneration", 3)
				c.Set("instanceType", "opencode")
				h.FinishTask(c)
				if w.Code != 200 {
					t.Fatalf("receipt failed: %d %s", w.Code, w.Body.String())
				}
			}
			if store.identity.UserID != 7 || store.identity.InstanceID != 8 {
				t.Fatalf("untrusted identity: %+v", store.identity)
			}
			if store.state.RuntimeAcknowledged != cancelled {
				t.Fatal("normal completion became a cancellation acknowledgement")
			}
			if !cancelled {
				if len(audit.events) != 0 {
					t.Fatal("normal completion emitted cancellation audit")
				}
				return
			}
			if len(audit.events) != 1 {
				t.Fatalf("expected one receipt audit, got %d", len(audit.events))
			}
			event := audit.events[0]
			if !strings.HasPrefix(event.TraceID, "cm-task-") || event.SessionID == nil || !strings.Contains(*event.Details, `"task_id":"msg_one"`) {
				t.Fatalf("missing audit identity: %+v", event)
			}
		})
	}
}
