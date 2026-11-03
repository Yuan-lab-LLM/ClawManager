package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type fencedWorkspaceRepo struct{ repository.InstanceRepository }

func (fencedWorkspaceRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	return nil, models.ErrRuntimeUpgradeInProgress
}

func TestWorkspaceHandlerUpgradeConflictBeforeLocalOrRemoteIO(t *testing.T) {
	for _, mode := range []string{"lite", "pro"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			instances := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{77: {ID: 77, UserID: 20, RuntimeGeneration: 5, InstanceMode: mode, WorkspacePath: &root}}}
			files := &fakeWorkspaceFileService{}
			local := services.WithWorkspaceUpgradeGuard(files, fencedWorkspaceRepo{}, false)
			remote := services.WithWorkspaceUpgradeGuard(files, fencedWorkspaceRepo{}, true)
			handler := NewWorkspaceFileHandler(instances, local, remote)
			router := workspaceFileTestRouter(10, "admin")
			router.POST("/instances/:id/folders", handler.Mkdir)
			router.DELETE("/instances/:id/files", handler.Delete)
			for _, request := range []struct{ method, path, body string }{
				{http.MethodPost, "/instances/77/folders", `{"path":"new"}`},
				{http.MethodDelete, "/instances/77/files?path=old", ""},
			} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "升级") {
					t.Fatal(rec.Code, rec.Body.String())
				}
			}
			if files.deleteCalls != 0 {
				t.Fatal("write escaped guard")
			}
		})
	}
}

func TestWorkspaceScopeCarriesCurrentGeneration(t *testing.T) {
	root := t.TempDir()
	instances := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{77: {ID: 77, UserID: 20, RuntimeGeneration: 9, WorkspacePath: &root}}}
	files := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instances, files)
	router := workspaceFileTestRouter(10, "admin")
	router.GET("/instances/:id/files", handler.List)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/instances/77/files", nil))
	if rec.Code != 200 || files.lastScope.RuntimeGeneration != 9 || files.lastScope.UserID != 20 {
		t.Fatal(rec.Code, files.lastScope)
	}
}

func TestSharedWorkspaceWriteCannotBypassUpgradeFence(t *testing.T) {
	root := t.TempDir()
	instances := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{77: {ID: 77, UserID: 20, RuntimeGeneration: 9, WorkspacePath: &root}}}
	files := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instances, services.WithWorkspaceUpgradeGuard(files, fencedWorkspaceRepo{}, false))
	tokens := services.NewInstanceAccessService()
	defer tokens.Stop()
	token, err := tokens.GenerateBoundToken(20, 77, "opencode", "/api/v1/instances/77/proxy/", "", 3001, time.Hour, sharedExternalAccessSessionBinding("sl_test"))
	if err != nil {
		t.Fatal(err)
	}
	access := &models.InstanceExternalAccess{InstanceID: 77, Enabled: true, AuthMode: services.ExternalAccessModeShareLink, WorkspaceAccess: services.ExternalWorkspaceAccessWrite}
	handler.SetExternalAccessServices(&fakeSharedExternalAccessService{access: access}, tokens)
	router := gin.New()
	router.DELETE("/shared/:code/files", handler.SharedDelete)
	req := httptest.NewRequest(http.MethodDelete, "/shared/sl_test/files?path=old", nil)
	req.AddCookie(&http.Cookie{Name: shortExternalAccessCookieName("sl_test"), Value: token.Token})
	req.Header.Set(sharedWorkspaceCSRFHeader, sharedExternalAccessCSRFToken("sl_test", token.Token))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 409 || files.deleteCalls != 0 {
		t.Fatal(rec.Code, rec.Body.String(), files.deleteCalls)
	}
}
