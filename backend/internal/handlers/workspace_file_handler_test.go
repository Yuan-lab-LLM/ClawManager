package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeWorkspaceHandlerInstanceService struct {
	instances map[int]*models.Instance
}

func (s *fakeWorkspaceHandlerInstanceService) Create(userID int, req services.CreateInstanceRequest) (*models.Instance, error) {
	return nil, nil
}
func (s *fakeWorkspaceHandlerInstanceService) CreatePrevalidated(userID int, req services.CreateInstanceRequest) (*models.Instance, error) {
	return nil, nil
}
func (s *fakeWorkspaceHandlerInstanceService) ValidateCreateRequests(userID int, requests []services.CreateInstanceRequest) error {
	return nil
}
func (s *fakeWorkspaceHandlerInstanceService) GetByID(id int) (*models.Instance, error) {
	return s.instances[id], nil
}
func (s *fakeWorkspaceHandlerInstanceService) GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error) {
	return nil, 0, nil
}
func (s *fakeWorkspaceHandlerInstanceService) GetAllInstances(offset, limit int) ([]models.Instance, int, error) {
	return nil, 0, nil
}
func (s *fakeWorkspaceHandlerInstanceService) Start(instanceID int) error   { return nil }
func (s *fakeWorkspaceHandlerInstanceService) Stop(instanceID int) error    { return nil }
func (s *fakeWorkspaceHandlerInstanceService) Restart(instanceID int) error { return nil }
func (s *fakeWorkspaceHandlerInstanceService) GetEnvironmentOverrideNames(instanceID int) ([]string, error) {
	instance, ok := s.instances[instanceID]
	if !ok || instance == nil || instance.EnvironmentOverridesJSON == nil {
		return []string{}, nil
	}
	return []string{"CONFIGURED_VARIABLE"}, nil
}

func (s *fakeWorkspaceHandlerInstanceService) RestartWithEnvironment(instanceID int, environmentOverrides map[string]string, environmentOverrideRemovals []string) error {
	return nil
}
func (s *fakeWorkspaceHandlerInstanceService) Delete(instanceID int) error { return nil }
func (s *fakeWorkspaceHandlerInstanceService) Update(instanceID int, req services.UpdateInstanceRequest) error {
	return nil
}
func (s *fakeWorkspaceHandlerInstanceService) GetInstanceStatus(instanceID int) (*services.InstanceStatus, error) {
	return nil, nil
}
func (s *fakeWorkspaceHandlerInstanceService) ForceSyncInstance(instanceID int) error { return nil }

type fakeWorkspaceFileService struct {
	lastScope services.WorkspaceFileScope
	listCalls int
	file      *os.File
	filename  string
	size      int64
}

func (s *fakeWorkspaceFileService) List(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) ([]services.WorkspaceEntry, error) {
	s.lastScope = scope
	s.listCalls++
	return []services.WorkspaceEntry{{Name: "readme.md", Path: "readme.md", ModifiedAt: time.Unix(1, 0), Previewable: true, Downloadable: true}}, nil
}
func (s *fakeWorkspaceFileService) Preview(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*services.WorkspacePreview, error) {
	s.lastScope = scope
	return &services.WorkspacePreview{Kind: "text", Text: "ok"}, nil
}
func (s *fakeWorkspaceFileService) OpenPreview(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	s.lastScope = scope
	return s.file, "text/plain; charset=utf-8", s.size, nil
}
func (s *fakeWorkspaceFileService) OpenDownload(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	s.lastScope = scope
	return s.file, s.filename, s.size, nil
}
func (s *fakeWorkspaceFileService) Upload(ctx context.Context, scope services.WorkspaceFileScope, relativeDir string, filename string, reader io.Reader, size int64) (*services.WorkspaceEntry, error) {
	s.lastScope = scope
	return &services.WorkspaceEntry{Name: filename, Path: filename, Downloadable: true}, nil
}
func (s *fakeWorkspaceFileService) Mkdir(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*services.WorkspaceEntry, error) {
	s.lastScope = scope
	return &services.WorkspaceEntry{Name: "docs", Path: relativePath, IsDir: true}, nil
}
func (s *fakeWorkspaceFileService) Rename(ctx context.Context, scope services.WorkspaceFileScope, oldPath, newPath string) (*services.WorkspaceEntry, error) {
	s.lastScope = scope
	return &services.WorkspaceEntry{Name: "renamed", Path: newPath}, nil
}
func (s *fakeWorkspaceFileService) Delete(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) error {
	s.lastScope = scope
	return nil
}

func TestWorkspaceFileHandlerRejectsNonOwnerUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 20, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/files", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/files", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
	if fileService.listCalls != 0 {
		t.Fatalf("workspace file service called %d times, want 0", fileService.listCalls)
	}
}

func TestWorkspaceFileHandlerAdminUsesInstanceOwnerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 20, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "admin")
	router.GET("/api/v1/instances/:id/workspace/files", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/files?path=", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.lastScope.InstanceID != 77 || fileService.lastScope.UserID != 20 || fileService.lastScope.WorkspacePath != workspacePath {
		t.Fatalf("scope = %#v, want instance owner scope", fileService.lastScope)
	}
}

func TestWorkspaceFileHandlerRequiresWorkspacePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 10},
	}}
	fileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/files", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/files", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
	}
	if fileService.listCalls != 0 {
		t.Fatalf("workspace file service called %d times, want 0", fileService.listCalls)
	}
}

func TestWorkspaceFileHandlerProDesktopUsesRuntimeConfigWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {
			ID:           77,
			UserID:       10,
			RuntimeType:  services.RuntimeBackendDesktop,
			InstanceMode: services.InstanceModePro,
		},
	}}
	localFileService := &fakeWorkspaceFileService{}
	runtimeFileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, localFileService, runtimeFileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/files", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/files?path=", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if localFileService.listCalls != 0 {
		t.Fatalf("local workspace service called %d times, want 0", localFileService.listCalls)
	}
	if runtimeFileService.listCalls != 1 {
		t.Fatalf("runtime workspace service called %d times, want 1", runtimeFileService.listCalls)
	}
	if runtimeFileService.lastScope.WorkspacePath != "/config" {
		t.Fatalf("runtime workspace path = %q, want /config", runtimeFileService.lastScope.WorkspacePath)
	}
}

func TestWorkspaceFileHandlerDownloadUsesSafeContentDisposition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	downloadFile, err := os.CreateTemp(t.TempDir(), "download-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := downloadFile.WriteString("body"); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 10, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{file: downloadFile, filename: `bad"name/ignored.txt`, size: int64(len("body"))}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/download", handler.Download)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/download?path=report.txt", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	disposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment;") || strings.Contains(disposition, `"name/`) || strings.Contains(disposition, "\r") || strings.Contains(disposition, "\n") {
		t.Fatalf("Content-Disposition = %q, want safe attachment filename", disposition)
	}
}

func workspaceFileTestRouter(userID int, role string) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("userRole", role)
		c.Next()
	})
	return router
}
