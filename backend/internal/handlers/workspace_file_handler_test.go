package handlers

import (
	"context"
	"fmt"
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
	lastScope       services.WorkspaceFileScope
	listCalls       int
	searchCalls     int
	lastSearchQuery string
	lastSearchLimit int
	deleteCalls     int
	file            *os.File
	filename        string
	size            int64
}

func (s *fakeWorkspaceFileService) List(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) ([]services.WorkspaceEntry, error) {
	s.lastScope = scope
	s.listCalls++
	return []services.WorkspaceEntry{{Name: "readme.md", Path: "readme.md", ModifiedAt: time.Unix(1, 0), Previewable: true, Downloadable: true}}, nil
}
func (s *fakeWorkspaceFileService) Search(ctx context.Context, scope services.WorkspaceFileScope, query string, limit int) ([]services.WorkspaceEntry, error) {
	s.lastScope = scope
	s.searchCalls++
	s.lastSearchQuery = query
	s.lastSearchLimit = limit
	return []services.WorkspaceEntry{
		{Name: "readme.md", Path: "docs/readme.md", ModifiedAt: time.Unix(1, 0), Previewable: true, Downloadable: true},
	}, nil
}
func (s *fakeWorkspaceFileService) Preview(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*services.WorkspacePreview, error) {
	s.lastScope = scope
	return &services.WorkspacePreview{Kind: "text", Text: "ok"}, nil
}
func (s *fakeWorkspaceFileService) OpenPreview(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (*os.File, string, int64, error) {
	s.lastScope = scope
	return s.file, "text/plain; charset=utf-8", s.size, nil
}
func (s *fakeWorkspaceFileService) OpenDownload(ctx context.Context, scope services.WorkspaceFileScope, relativePath string) (io.ReadCloser, string, int64, error) {
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
	s.deleteCalls++
	return nil
}

type fakeSharedExternalAccessService struct {
	access *models.InstanceExternalAccess
	err    error
}

func (s *fakeSharedExternalAccessService) Get(ctx context.Context, instanceID int) (*models.InstanceExternalAccess, error) {
	return s.access, s.err
}
func (s *fakeSharedExternalAccessService) EnableShareLink(ctx context.Context, instanceID, createdBy int, expiration services.ExternalAccessExpirationRequest) (*services.EnableShareLinkResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *fakeSharedExternalAccessService) CreatePassword(ctx context.Context, instanceID, createdBy int, expiration services.ExternalAccessExpirationRequest) (*services.PasswordExternalAccessResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *fakeSharedExternalAccessService) ResetURL(ctx context.Context, instanceID, createdBy int) (*services.EnableShareLinkResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *fakeSharedExternalAccessService) ResetPassword(ctx context.Context, instanceID, createdBy int) (*services.PasswordExternalAccessResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *fakeSharedExternalAccessService) Disable(ctx context.Context, instanceID int) error {
	return fmt.Errorf("not implemented")
}
func (s *fakeSharedExternalAccessService) ResolveShortLink(ctx context.Context, code string) (*models.InstanceExternalAccess, error) {
	if s.err != nil {
		return nil, s.err
	}
	if code != "sl_test" {
		return nil, fmt.Errorf("external access is not enabled")
	}
	return s.access, nil
}
func (s *fakeSharedExternalAccessService) ValidateShortLink(ctx context.Context, code, password string) (*models.InstanceExternalAccess, error) {
	return s.ResolveShortLink(ctx, code)
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

func TestWorkspaceFileHandlerSearchPassesQueryAndOwnerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 10, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/search", handler.Search)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/search?q=readme", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.searchCalls != 1 {
		t.Fatalf("workspace file service search called %d times, want 1", fileService.searchCalls)
	}
	if fileService.lastSearchQuery != "readme" {
		t.Fatalf("search query = %q, want readme", fileService.lastSearchQuery)
	}
	if fileService.lastScope.InstanceID != 77 || fileService.lastScope.UserID != 10 {
		t.Fatalf("scope = %#v, want instance owner scope", fileService.lastScope)
	}
	if !strings.Contains(rec.Body.String(), `"limit":200`) {
		t.Fatalf("body = %s, want limit 200 in response", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `docs/readme.md`) {
		t.Fatalf("body = %s, want search entry in response", rec.Body.String())
	}
}

func TestWorkspaceFileHandlerSearchLimitParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 10, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	handler := NewWorkspaceFileHandler(instanceService, fileService)

	router := workspaceFileTestRouter(10, "user")
	router.GET("/api/v1/instances/:id/workspace/search", handler.Search)

	// Explicit limit is passed through to the service and echoed back.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/search?q=readme&limit=50", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.lastSearchLimit != 50 {
		t.Fatalf("search limit = %d, want 50", fileService.lastSearchLimit)
	}
	if !strings.Contains(rec.Body.String(), `"limit":50`) {
		t.Fatalf("body = %s, want limit 50 in response", rec.Body.String())
	}

	// Limits above the ceiling are clamped.
	fileService.lastSearchLimit = 0
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/search?q=readme&limit=99999", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.lastSearchLimit != services.WorkspaceSearchLimitCeiling {
		t.Fatalf("search limit = %d, want ceiling %d", fileService.lastSearchLimit, services.WorkspaceSearchLimitCeiling)
	}

	// An invalid limit falls back to the default.
	fileService.lastSearchLimit = 0
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/77/workspace/search?q=readme&limit=abc", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.lastSearchLimit != services.WorkspaceSearchMaxResults {
		t.Fatalf("search limit = %d, want default %d", fileService.lastSearchLimit, services.WorkspaceSearchMaxResults)
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

func TestSharedWorkspaceFileHandlerUsesCodeBoundInstanceScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 20, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	accessTokens := services.NewInstanceAccessService()
	defer accessTokens.Stop()
	token, err := accessTokens.GenerateBoundToken(20, 77, "openclaw", "/api/v1/instances/77/proxy/", "", 3001, time.Hour, sharedExternalAccessSessionBinding("sl_test"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceFileHandler(instanceService, fileService)
	handler.SetExternalAccessServices(&fakeSharedExternalAccessService{access: &models.InstanceExternalAccess{
		InstanceID:      77,
		Enabled:         true,
		AuthMode:        services.ExternalAccessModeShareLink,
		WorkspaceAccess: services.ExternalWorkspaceAccessRead,
	}}, accessTokens)

	router := gin.New()
	router.GET("/api/v1/shared-instances/:code/workspace/files", handler.SharedList)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shared-instances/sl_test/workspace/files", nil)
	req.AddCookie(&http.Cookie{Name: shortExternalAccessCookieName("sl_test"), Value: token.Token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.lastScope.InstanceID != 77 ||
		fileService.lastScope.UserID != 20 ||
		fileService.lastScope.WorkspacePath != workspacePath ||
		fileService.lastScope.AuditActionPrefix != "share_" {
		t.Fatalf("scope = %#v, want shared instance owner scope", fileService.lastScope)
	}
}

func TestSharedWorkspaceFileHandlerEnforcesReadOnlyAndCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 20, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	accessTokens := services.NewInstanceAccessService()
	defer accessTokens.Stop()
	token, err := accessTokens.GenerateBoundToken(20, 77, "openclaw", "/api/v1/instances/77/proxy/", "", 3001, time.Hour, sharedExternalAccessSessionBinding("sl_test"))
	if err != nil {
		t.Fatal(err)
	}
	externalAccess := &models.InstanceExternalAccess{
		InstanceID:      77,
		Enabled:         true,
		AuthMode:        services.ExternalAccessModeShareLink,
		WorkspaceAccess: services.ExternalWorkspaceAccessRead,
	}
	handler := NewWorkspaceFileHandler(instanceService, fileService)
	handler.SetExternalAccessServices(&fakeSharedExternalAccessService{access: externalAccess}, accessTokens)
	router := gin.New()
	router.DELETE("/api/v1/shared-instances/:code/workspace/entries", handler.SharedDelete)

	requestDelete := func(csrf string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/shared-instances/sl_test/workspace/entries?path=report.md", nil)
		req.AddCookie(&http.Cookie{Name: shortExternalAccessCookieName("sl_test"), Value: token.Token})
		if csrf != "" {
			req.Header.Set(sharedWorkspaceCSRFHeader, csrf)
		}
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := requestDelete(sharedExternalAccessCSRFToken("sl_test", token.Token)); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only delete status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
	if fileService.deleteCalls != 0 {
		t.Fatalf("read-only delete reached file service %d times", fileService.deleteCalls)
	}

	externalAccess.WorkspaceAccess = services.ExternalWorkspaceAccessWrite
	if rec := requestDelete(""); rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF delete status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
	if rec := requestDelete(sharedExternalAccessCSRFToken("sl_test", token.Token)); rec.Code != http.StatusOK {
		t.Fatalf("valid write delete status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if fileService.deleteCalls != 1 {
		t.Fatalf("valid delete reached file service %d times, want 1", fileService.deleteCalls)
	}
}

func TestSharedWorkspaceFileHandlerDoesNotExposeLegacyNoneScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspacePath := t.TempDir()
	instanceService := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		77: {ID: 77, UserID: 20, WorkspacePath: &workspacePath},
	}}
	fileService := &fakeWorkspaceFileService{}
	accessTokens := services.NewInstanceAccessService()
	defer accessTokens.Stop()
	token, err := accessTokens.GenerateBoundToken(20, 77, "openclaw", "/api/v1/instances/77/proxy/", "", 3001, time.Hour, sharedExternalAccessSessionBinding("sl_test"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkspaceFileHandler(instanceService, fileService)
	handler.SetExternalAccessServices(&fakeSharedExternalAccessService{access: &models.InstanceExternalAccess{
		InstanceID:      77,
		Enabled:         true,
		AuthMode:        services.ExternalAccessModeShareLink,
		WorkspaceAccess: services.ExternalWorkspaceAccessNone,
	}}, accessTokens)
	router := gin.New()
	router.GET("/api/v1/shared-instances/:code/workspace/files", handler.SharedList)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shared-instances/sl_test/workspace/files", nil)
	req.AddCookie(&http.Cookie{Name: shortExternalAccessCookieName("sl_test"), Value: token.Token})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("legacy none scope status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
	if fileService.listCalls != 0 {
		t.Fatalf("legacy none scope reached file service %d times", fileService.listCalls)
	}
}

func TestDesktopWorkspaceEligibilityFollowsManagedRuntimeVariant(t *testing.T) {
	tests := []struct {
		name     string
		instance *models.Instance
		want     bool
	}{
		{
			name:     "linux workbuddy",
			instance: &models.Instance{Type: "workbuddy", RuntimeVariant: "linux", InstanceMode: services.InstanceModePro},
			want:     true,
		},
		{
			name:     "windows workbuddy",
			instance: &models.Instance{Type: "workbuddy", RuntimeVariant: "windows", InstanceMode: services.InstanceModePro},
			want:     false,
		},
		{
			name:     "linux codex",
			instance: &models.Instance{Type: services.RuntimeTypeCodex, RuntimeVariant: "linux", InstanceMode: services.InstanceModePro},
			want:     true,
		},
		{
			name:     "windows codex",
			instance: &models.Instance{Type: services.RuntimeTypeCodex, RuntimeVariant: "windows", InstanceMode: services.InstanceModePro},
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isDesktopWorkspaceInstance(test.instance); got != test.want {
				t.Fatalf("isDesktopWorkspaceInstance() = %v, want %v", got, test.want)
			}
		})
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
