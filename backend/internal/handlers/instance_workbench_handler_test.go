package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type workbenchHandlerService struct {
	calls                      int
	user, instance, generation int
	operation                  string
	data                       json.RawMessage
}

func (s *workbenchHandlerService) Capabilities(context.Context, int, bool, int) (map[string]any, error) {
	s.calls++
	return map[string]any{"generation": 3}, nil
}
func (s *workbenchHandlerService) CreatePreview(context.Context, int, bool, int, int, string) (map[string]any, error) {
	s.calls++
	return nil, services.ErrWorkbenchDenied
}
func (s *workbenchHandlerService) Execute(_ context.Context, user int, _ bool, instance, generation int, operation string, data json.RawMessage) (json.RawMessage, error) {
	s.calls++
	s.user = user
	s.instance = instance
	s.generation = generation
	s.operation = operation
	s.data = data
	return json.RawMessage(`{"sessions":[]}`), nil
}
func TestWorkbenchHandlerRequiresOwnerAuthenticationAndGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path string
		auth       bool
		status     int
	}{
		{"unauthenticated", "/instances/42/workbench/capabilities", false, 401},
		{"file share context is insufficient", "/instances/42/workbench/terminals?generation=3", false, 401},
		{"missing generation", "/instances/42/workbench/terminals", true, 409},
		{"current generation", "/instances/42/workbench/terminals?generation=3", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &workbenchHandlerService{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.auth {
					c.Set("userID", 7)
					c.Set("userRole", "user")
				}
				c.Set("shared-instance-workspace", true)
			})
			NewInstanceWorkbenchHandler(service).Register(router.Group("/instances/:id/workbench"))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", tc.path, nil))
			if recorder.Code != tc.status {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			if tc.status != 200 && service.calls != 0 {
				t.Fatal("unauthorized request reached executor")
			}
		})
	}
}
func TestWorkbenchHandlerPinsSessionFromRouteAndPassesRawInput(t *testing.T) {
	service := &workbenchHandlerService{}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", 7) })
	NewInstanceWorkbenchHandler(service).Register(router.Group("/instances/:id/workbench"))
	request := httptest.NewRequest(http.MethodPost, "/instances/42/workbench/terminals/owned-session/input?generation=3", strings.NewReader(`{"session":"injected","input":"echo hello\r"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 200 || service.operation != "terminal.write" || service.user != 7 || service.instance != 42 || service.generation != 3 {
		t.Fatalf("wrong execution: %+v %s", service, recorder.Body.String())
	}
	var data map[string]string
	if json.Unmarshal(service.data, &data) != nil || data["session"] != "owned-session" || data["input"] != "echo hello\r" {
		t.Fatalf("wrong payload: %s", service.data)
	}
}

func TestWorkbenchFailurePreservesTransientCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"instance_busy", 423}, {"stop_unconfirmed", 423}, {"cancelled", 409}, {"timeout", 504}, {"execution_failed", 400}, {"invalid_request", 400},
	} {
		t.Run(tc.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			workbenchFailure(ctx, &services.WorkbenchOperationError{Code: tc.code, Message: "Worker supplied explanation"})
			if recorder.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", recorder.Code, tc.status, recorder.Body.String())
			}
			var data map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &data) != nil || data["error_code"] != tc.code || data["error"] != "Worker supplied explanation" {
				t.Fatalf("lost worker failure: %s", recorder.Body.String())
			}
		})
	}
}
