package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/workbench"
	"github.com/gin-gonic/gin"
)

type sharedWorkbenchHandlerService struct {
	workbenchHandlerService
	shared *workbench.SharedPreviewAccess
}

func (s *sharedWorkbenchHandlerService) Capabilities(context.Context, int, bool, int) (map[string]any, error) {
	s.calls++
	return map[string]any{"generation": 3, "git": true, "terminal": true, "processes": true, "preview": true}, nil
}
func (s *sharedWorkbenchHandlerService) CreateSharedPreview(_ context.Context, user int, id, generation int, address string, shared workbench.SharedPreviewAccess) (map[string]any, error) {
	s.calls++
	s.user = user
	s.instance = id
	s.generation = generation
	s.shared = &shared
	return map[string]any{"url": "https://preview.test"}, nil
}

func TestSharedWorkbenchPermissionsAndSessionScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, access, method, path, body, auth string
		csrf                                   bool
		status                                 int
	}{
		{"capabilities", "read", "GET", "/capabilities", "", "valid", false, 200},
		{"missing session", "read", "GET", "/capabilities", "", "", false, 401},
		{"wrong owner session", "read", "GET", "/capabilities", "", "owner", false, 401},
		{"wrong share session", "read", "GET", "/capabilities", "", "binding", false, 401},
		{"none", "none", "GET", "/capabilities", "", "valid", false, 403},
		{"disabled", "disabled", "GET", "/capabilities", "", "valid", false, 401},
		{"expired", "expired", "GET", "/capabilities", "", "valid", false, 401},
		{"read terminal list", "read", "GET", "/terminals", "", "valid", false, 403},
		{"read terminal output", "read", "GET", "/terminals/s/output", "", "valid", false, 403},
		{"read processes", "read", "GET", "/processes", "", "valid", false, 403},
		{"git read", "read", "POST", "/git", `{"operation":"status","instance_id":999,"user_id":999}`, "valid", true, 200},
		{"git projects read", "read", "POST", "/git", `{"operation":"projects"}`, "valid", true, 200},
		{"git diff read", "read", "POST", "/git", `{"operation":"diff"}`, "valid", true, 200},
		{"git branches read", "read", "POST", "/git", `{"operation":"branches"}`, "valid", true, 200},
		{"git history read", "read", "POST", "/git", `{"operation":"history"}`, "valid", true, 200},
		{"git identity read", "read", "POST", "/git", `{"operation":"identity","name":"ignored"}`, "valid", true, 200},
		{"git read requires csrf", "read", "POST", "/git", `{"operation":"status"}`, "valid", false, 403},
		{"git write denied", "read", "POST", "/git", `{"operation":"commit"}`, "valid", true, 403},
		{"git identity write denied", "read", "POST", "/git", `{"operation":"set-identity"}`, "valid", true, 403},
		{"unknown git denied", "read", "POST", "/git", `{"operation":"unknown"}`, "valid", true, 403},
		{"read preview", "read", "POST", "/previews", `{"url":"localhost:4000"}`, "valid", true, 200},
		{"preview requires csrf", "read", "POST", "/previews", `{"url":"localhost:4000"}`, "valid", false, 403},
		{"write terminal", "write", "POST", "/terminals", `{}`, "valid", true, 200},
		{"write requires csrf", "write", "DELETE", "/terminals/s", "", "valid", false, 403},
		{"write process", "write", "POST", "/processes/p/stop", `{}`, "valid", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := &models.InstanceExternalAccess{InstanceID: 77, Enabled: true, AuthMode: services.ExternalAccessModeShareLink, WorkspaceAccess: tc.access}
			if tc.access == "disabled" {
				access.Enabled = false
				access.WorkspaceAccess = "read"
			}
			if tc.access == "expired" {
				expired := time.Now().Add(-time.Hour)
				access.ExpiresAt = &expired
				access.WorkspaceAccess = "read"
			}
			instances := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{77: {ID: 77, UserID: 20}}}
			tokens := services.NewInstanceAccessService()
			defer tokens.Stop()
			user, binding := 20, sharedExternalAccessSessionBinding("sl_test", access)
			if tc.auth == "owner" {
				user = 21
			}
			if tc.auth == "binding" {
				binding = "other"
			}
			token, err := tokens.GenerateBoundToken(user, 77, "openclaw", "/", "", 3001, time.Hour, binding)
			if err != nil {
				t.Fatal(err)
			}
			service := &sharedWorkbenchHandlerService{}
			router := gin.New()
			// Bearer identity must never override the share's owner or instance.
			router.Use(func(c *gin.Context) { c.Set("userID", 999); c.Set("userRole", "admin") })
			NewSharedInstanceWorkbenchHandler(service, instances, &fakeSharedExternalAccessService{access: access}, tokens).Register(router.Group("/shared-instances/:code/workbench"))
			req := httptest.NewRequest(tc.method, "/shared-instances/sl_test/workbench"+tc.path+"?generation=3&id=999", strings.NewReader(tc.body))
			if tc.auth != "" {
				req.AddCookie(&http.Cookie{Name: shortExternalAccessCookieName("sl_test"), Value: token.Token})
			}
			if tc.csrf {
				req.Header.Set(sharedWorkspaceCSRFHeader, sharedExternalAccessCSRFToken("sl_test", token.Token))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if (tc.name == "git read requires csrf" || tc.name == "preview requires csrf" || tc.name == "write requires csrf") && !strings.Contains(rec.Body.String(), `"error_code":"share_csrf_invalid"`) {
				t.Fatalf("CSRF denial cannot be safely renewed: %s", rec.Body.String())
			}
			if tc.status != 200 && service.calls != 0 {
				t.Fatal("denied request reached runtime")
			}
			if tc.status == 200 && tc.path != "/capabilities" && (service.user != 20 || service.instance != 77) {
				t.Fatalf("scope came from caller: %+v", service)
			}
			if tc.path == "/capabilities" && tc.status == 200 {
				var result struct {
					Data map[string]any `json:"data"`
				}
				if json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data["terminal"] != false || result.Data["processes"] != false {
					t.Fatalf("read capability leaked: %s", rec.Body.String())
				}
			}
			if tc.path == "/previews" && tc.status == 200 && (service.shared == nil || service.shared.SessionBinding != binding || service.shared.ExpiresAt.Unix() != token.ExpiresAt.Unix()) {
				t.Fatalf("preview lost share authority: %+v", service.shared)
			}
		})
	}
}
