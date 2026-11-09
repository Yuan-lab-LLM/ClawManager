package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/middleware"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
)

type protocolTestStore struct {
	state               *models.RuntimeProtocolState
	err                 error
	calls               int
	instanceID, ownerID int
}

func (s *protocolTestStore) Get(context.Context, int) (*models.RuntimeProtocolState, error) {
	s.calls++
	return s.state, s.err
}
func (s *protocolTestStore) Negotiate(_ context.Context, instanceID, ownerID int, report models.RuntimeCapabilityReport) error {
	s.calls++
	s.instanceID, s.ownerID = instanceID, ownerID
	return s.err
}

func TestRuntimeProtocolLegacyAndEnrolledAcrossAllEntrypoints(t *testing.T) {
	for _, path := range []string{"/chat/completions", "/responses", "/v1/responses", "/v1/messages"} {
		for _, tc := range []struct {
			name          string
			protocol      int
			task, partial bool
			storeErr      error
			want          int
		}{
			{"legacy", 0, false, false, nil, 200},
			{"legacy with identity", 0, true, false, nil, 200},
			{"enrolled", 1, true, false, nil, 200},
			{"cannot omit identity", 1, false, false, nil, 409},
			{"cannot partially omit identity", 0, false, true, nil, 400},
			{"cannot bypass unreadable policy", 1, true, false, errors.New("offline"), 503},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				store := &protocolTestStore{err: tc.storeErr}
				if tc.protocol > 0 {
					store.state = &models.RuntimeProtocolState{Protocol: tc.protocol}
				}
				r := gin.New()
				r.POST(path, func(c *gin.Context) { c.Set("instanceID", 8); c.Set("userID", 7); c.Set("runtimeGeneration", 3) }, middleware.RuntimeProtocolPolicy(store), func(c *gin.Context) {
					var req aigateway.ChatCompletionRequest
					if attachRuntimeTask(c, &req) {
						c.Status(200)
					}
				})
				request := httptest.NewRequest("POST", path, nil)
				if tc.task || tc.partial {
					request.Header.Set("X-ClawManager-Session-ID", "ses_one")
					request.Header.Set("X-ClawManager-Runtime-Generation", "3")
				}
				if tc.task {
					request.Header.Set("X-ClawManager-Task-ID", "msg_one")
				}
				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)
				if response.Code != tc.want {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
}

func TestRuntimeProtocolNegotiationUsesTrustedInstanceOwner(t *testing.T) {
	for _, bound := range []bool{false, true} {
		store := &protocolTestStore{}
		h := NewRuntimeProtocolHandler(store, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"protocol":1,"generation":3,"runtime_version":"2.0.27","adapter_version":"cm.1","capabilities":["task_identity","turn_cancel","cancel_receipt","live_catalog"],"instance_id":999,"user_id":999}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("userID", 7)
		if bound {
			c.Set("instanceID", 8)
			c.Set("runtimeGeneration", 3)
		}
		h.Negotiate(c)
		if bound {
			if w.Code != 200 || store.instanceID != 8 || store.ownerID != 7 {
				t.Fatalf("bad negotiation: %s %+v", w.Body.String(), store)
			}
		} else if w.Code != 403 || store.calls != 0 {
			t.Fatal("unbound key enrolled")
		}
	}
}

type protocolInstanceStore struct {
	repository.InstanceRepository
	instance *models.Instance
}

func (s protocolInstanceStore) GetByID(int) (*models.Instance, error) { return s.instance, nil }

func TestRuntimeProtocolStatusDoesNotClaimStaleRuntimeIsGoverned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *models.RuntimeProtocolState
		want  string
	}{
		{"legacy", nil, "legacy"},
		{"fresh", &models.RuntimeProtocolState{Protocol: 1, OwnerUserID: 7, Generation: 3, ReportedAt: time.Now().UTC(), CapabilitiesJSON: "[]"}, "governed"},
		{"previous generation", &models.RuntimeProtocolState{Protocol: 1, OwnerUserID: 7, Generation: 2, ReportedAt: time.Now().UTC(), CapabilitiesJSON: "[]"}, "awaiting_runtime"},
		{"previous owner", &models.RuntimeProtocolState{Protocol: 1, OwnerUserID: 9, Generation: 3, ReportedAt: time.Now().UTC(), CapabilitiesJSON: "[]"}, "awaiting_runtime"},
		{"stale", &models.RuntimeProtocolState{Protocol: 1, OwnerUserID: 7, Generation: 3, ReportedAt: time.Now().Add(-time.Minute), CapabilitiesJSON: "[]"}, "awaiting_runtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &protocolTestStore{state: tc.state}
			h := NewRuntimeProtocolHandler(store, protocolInstanceStore{instance: &models.Instance{ID: 8, UserID: 7, RuntimeGeneration: 3, Status: "running"}})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Params = gin.Params{{Key: "id", Value: "8"}}
			c.Set("userID", 7)
			h.Status(c)
			var body struct {
				Data struct {
					Mode string `json:"mode"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Data.Mode != tc.want {
				t.Fatalf("%s %v", w.Body.String(), err)
			}
		})
	}
}

func TestRuntimeProtocolStatusRejectsOtherOwnerBeforeReadingCapabilities(t *testing.T) {
	store := &protocolTestStore{}
	h := NewRuntimeProtocolHandler(store, protocolInstanceStore{instance: &models.Instance{ID: 8, UserID: 7}})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Params = gin.Params{{Key: "id", Value: "8"}}
	c.Set("userID", 9)
	h.Status(c)
	if w.Code != 403 || store.calls != 0 {
		t.Fatal("other owner's runtime state exposed")
	}
}
