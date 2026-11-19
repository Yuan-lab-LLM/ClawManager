package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

const slotResetBody = `{"source_pod_uid":"uid","source_container_id":"containerd://old","scope_fingerprint":"preflight","request_id":"the-original"}`

type slotResetBoundaryService struct {
	calls int
	req   services.RuntimeSlotResetRequest
	err   error
}

func (s *slotResetBoundaryService) SlotResetOperation(_ context.Context, requestID string) (*services.RuntimeManagementOperation, error) {
	s.calls++
	if requestID != "the-original" {
		return nil, nil
	}
	return &services.RuntimeManagementOperation{ID: "original-operation", Kind: "slot_reset"}, nil
}

func (s *slotResetBoundaryService) PreflightSlotReset(_ context.Context, podID int64, slot string, req services.RuntimeSlotResetRequest) (*services.RuntimeSlotResetPreflight, error) {
	s.calls++
	s.req = req
	return &services.RuntimeSlotResetPreflight{Allowed: podID == 7 && slot == "slot-0"}, s.err
}

func (s *slotResetBoundaryService) ResetSlot(_ context.Context, podID int64, slot string, req services.RuntimeSlotResetRequest, actor *int) (*services.RuntimeManagementOperation, error) {
	s.calls++
	s.req = req
	if podID != 7 || slot != "slot-0" || actor == nil || *actor != 23 {
		return nil, errors.New("unexpected target or operator")
	}
	if s.err != nil {
		return nil, s.err
	}
	return &services.RuntimeManagementOperation{ID: "original-operation", Kind: "slot_reset"}, nil
}

func slotResetRequest(service *slotResetBoundaryService, url, role, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	handler := NewRuntimeSlotResetHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userRole", role)
		c.Set("userID", 23)
	})
	router.POST("/runtime-pods/:id/slots/:slot/reset/preflight", handler.Preflight)
	router.POST("/runtime-pods/:id/slots/:slot/reset", handler.Submit)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, url, strings.NewReader(body)))
	return w
}

func TestRuntimeSlotResetAdminAndExactSlotBoundary(t *testing.T) {
	const path = "/runtime-pods/7/slots/slot-0/reset"
	for _, test := range []struct {
		name, path, role, body string
		status                 int
	}{
		{"regular user cannot preflight", path + "/preflight", "user", "{}", 403},
		{"regular user cannot submit", path, "user", slotResetBody, 403},
		{"numeric index is not a slot name", "/runtime-pods/7/slots/1/reset", "admin", slotResetBody, 400},
		{"coordinator is not a slot", "/runtime-pods/7/slots/coordinator/reset/preflight", "admin", "{}", 400},
		{"invalid pod", "/runtime-pods/0/slots/slot-0/reset/preflight", "admin", "{}", 400},
		{"missing reset scope", path, "admin", "{}", 400},
		{"null preview", path + "/preflight", "admin", "null", 400},
		{"undefined override", path + "/preflight", "admin", `{"force":true}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &slotResetBoundaryService{}
			w := slotResetRequest(service, test.path, test.role, test.body)
			if w.Code != test.status || service.calls != 0 {
				t.Fatalf("status=%d service calls=%d: %s", w.Code, service.calls, w.Body.String())
			}
		})
	}
}

func TestRuntimeSlotResetExpiredPreflightAndIdempotencyBoundary(t *testing.T) {
	const path = "/runtime-pods/7/slots/slot-0/reset"
	service := &slotResetBoundaryService{}
	w := slotResetRequest(service, path+"/preflight", "admin", `{"source_pod_uid":"uid"}`)
	if w.Code != 200 || service.req.SourcePodUID != "uid" || !strings.Contains(w.Body.String(), `"allowed":true`) {
		t.Fatalf("preflight = %d %s; request = %+v", w.Code, w.Body.String(), service.req)
	}

	for _, message := range []string{"preflight expired", "source container changed"} {
		service.err = errors.New(message)
		w = slotResetRequest(service, path, "admin", slotResetBody)
		if w.Code != 409 || !strings.Contains(w.Body.String(), message) {
			t.Fatalf("conflict = %d %s", w.Code, w.Body.String())
		}
	}
	service.err = nil
	for i := 0; i < 2; i++ {
		w = slotResetRequest(service, path, "admin", slotResetBody)
		if w.Code != 202 || !strings.Contains(w.Body.String(), "original-operation") ||
			service.req.RequestID != "the-original" || service.req.SourcePodUID != "uid" ||
			service.req.SourceContainerID != "containerd://old" || service.req.ScopeFingerprint != "preflight" {
			t.Fatalf("submit = %d %s; request = %+v", w.Code, w.Body.String(), service.req)
		}
	}
	service.err = services.ErrRuntimeSlotResetCommitUncertain
	w = slotResetRequest(service, path, "admin", slotResetBody)
	if w.Code != 500 {
		t.Fatalf("uncertain submit = %d %s", w.Code, w.Body.String())
	}
}

func TestRuntimeSlotResetExactHistoryLookup(t *testing.T) {
	service := &slotResetBoundaryService{}
	handler := NewRuntimeSlotResetHandler(service)
	router := gin.New()
	router.GET("/runtime-scheduling", func(c *gin.Context) { c.Set("userRole", c.GetHeader("Role")) },
		handler.History(func(c *gin.Context) { c.String(200, "ordinary history") }))
	for _, test := range []struct {
		query, role, body string
		status            int
	}{
		{"", "admin", "ordinary history", 200},
		{"?slot_reset_request_id=the-original", "user", "Admin access required", 403},
		{"?slot_reset_request_id=the-original", "admin", "original-operation", 200},
		{"?slot_reset_request_id=not-found", "admin", "Runtime slot reset operation", 200},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/runtime-scheduling"+test.query, nil)
		req.Header.Set("Role", test.role)
		router.ServeHTTP(w, req)
		if w.Code != test.status || !strings.Contains(w.Body.String(), test.body) {
			t.Fatalf("lookup %s = %d %s", test.query, w.Code, w.Body.String())
		}
	}
	if service.calls != 2 {
		t.Fatalf("got %d exact lookups", service.calls)
	}
}
