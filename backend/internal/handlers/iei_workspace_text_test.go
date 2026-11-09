package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIEITextOwnerAbnormalAndLifecycleSafety(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "config.yaml"), []byte("ok: true\n"), 0600)
	owner, other := "owner@example.com", "other@example.com"
	instance := &models.Instance{ID: 1, UserID: 10, Owner: &owner, Type: "hermes", InstanceMode: "lite", RuntimeType: "gateway", Status: "error", WorkspacePath: &root}
	svc := &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{1: instance, 2: {ID: 2, UserID: 10, Owner: &other, Type: "hermes", InstanceMode: "lite", Status: "error", WorkspacePath: &root}}}}
	lifecycle := &fakeIEILifecycleService{}
	handler := NewIEISystemHandler(cfg, sso, svc, nil, lifecycle)
	handler.SetWorkspaceFileHandler(NewWorkspaceFileHandler(svc, services.NewWorkspaceFileService(nil)))
	router := gin.New()
	router.POST("/api/v1/ieisystem/session", handler.ExchangeSession)
	router.GET("/instances/:id/text", handler.WorkspaceText)
	router.PUT("/instances/:id/text", handler.WorkspaceText)
	cookie := exchangeIEITestSession(t, router, cfg, owner)
	request := func(method, url, body string, auth bool) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Owner-Action", "1")
		if auth {
			r.AddCookie(cookie)
		}
		router.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/instances/1/text?path=config.yaml", "", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/instances/2/text?path=config.yaml", "", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w := request("GET", "/instances/1/text?path=config.yaml", "", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data services.WorkspaceText `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	body, _ := json.Marshal(map[string]string{"path": "config.yaml", "text": "ok: false\n", "version": response.Data.Version})
	w = request("PUT", "/instances/1/text", string(body), true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request("PUT", "/instances/1/text", string(body), true); w.Code != 409 {
		t.Fatal("stale save", w.Code)
	}
	id := 1
	lifecycle.operations = map[string]*models.NorthboundOperation{"reset": {UserID: 10, InstanceID: &id, Status: "processing"}}
	if w = request("PUT", "/instances/1/text", string(body), true); w.Code != 409 || !strings.Contains(w.Body.String(), "维护操作") {
		t.Fatal("reset write", w.Code, w.Body.String())
	}
	if w = request("GET", "/instances/1/text?path=config.yaml", "", true); w.Code != 200 {
		t.Fatal("maintenance read", w.Code)
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/instances/1/text", strings.NewReader(string(body)))
	r.AddCookie(cookie)
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing CSRF marker", w.Code)
	}
}
