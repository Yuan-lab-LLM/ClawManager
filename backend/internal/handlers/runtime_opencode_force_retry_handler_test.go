package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type forceRetryHandlerFixture struct {
	preflights, confirms int
	req                  services.OpenCodeForceRetryRequest
	actor                int
	failure              bool
}

func (f *forceRetryHandlerFixture) PreflightOpenCodeForceRetry(context.Context, int64, string) (*services.OpenCodeForceRetryPreflight, error) {
	f.preflights++
	if f.failure {
		return nil, models.ErrRuntimeUpgradeConflict
	}
	return &services.OpenCodeForceRetryPreflight{RolloutID: 14, Allowed: true, Fingerprint: strings.Repeat("a", 64)}, nil
}
func (f *forceRetryHandlerFixture) ConfirmOpenCodeForceRetry(_ context.Context, id int64, r services.OpenCodeForceRetryRequest, actor int) (*models.RuntimeRollout, error) {
	f.confirms++
	f.req = r
	f.actor = actor
	if f.failure {
		return nil, errors.New("private native error TOKEN")
	}
	return &models.RuntimeRollout{ID: id, RuntimeType: "opencode", Status: "running", TargetImageRef: r.TargetImageRef}, nil
}
func TestOpenCodeForceRetryHandlerExplicitAdminContract(t *testing.T) {
	for _, tc := range []struct {
		name, role, body string
		confirm          bool
		status           int
	}{
		{"preflight", "admin", `{"target_image_ref":"clawmanager.local/opencode-lite@sha256:` + strings.Repeat("a", 64) + `"}`, false, 200},
		{"confirm", "admin", `{"target_image_ref":"clawmanager.local/opencode-lite@sha256:` + strings.Repeat("a", 64) + `","force":true,"request_id":"81e247de-ef46-41ad-853e-0bb20524303e","fingerprint":"` + strings.Repeat("b", 64) + `"}`, true, 202},
		{"member", "member", `{}`, false, 403}, {"missingforce", "admin", `{"target_image_ref":"x"}`, true, 400}, {"null", "admin", `null`, false, 400}, {"unknown", "admin", `{"target_image_ref":"x","command":"TOKEN"}`, false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &forceRetryHandlerFixture{}
			h := NewRuntimePoolHandler(nil, nil, nil, nil, nil)
			h.forceRetry = f
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("userRole", tc.role); c.Set("userID", 1); c.Next() })
			if tc.confirm {
				router.POST("/force/:id", h.ConfirmOpenCodeForceRetry)
			} else {
				router.POST("/force/:id", h.PreflightOpenCodeForceRetry)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/force/14", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.status >= 400 && (f.preflights+f.confirms) != 0 {
				t.Fatal("invalid request reached controller")
			}
			if tc.confirm && tc.status == 202 && (f.actor != 1 || !f.req.Force) {
				t.Fatal("explicit actor/force lost")
			}
		})
	}
}
