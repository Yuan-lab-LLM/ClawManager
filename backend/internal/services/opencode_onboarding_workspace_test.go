package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

type onboardingExternalAccessService struct {
	InstanceExternalAccessService
	access *models.InstanceExternalAccess
}

// The local onboarding guide owns first-run navigation. Opening the native
// entry must not create a session or redirect into a managed starter project.
func TestOpenCodeOnboardingPreservesNativeEntry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodGet {
			t.Errorf("unexpected onboarding runtime request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>native page</body></html>"))
	}))
	defer upstream.Close()
	service, token := newOpenCodeV2ProxyTestService(t, upstream.URL, 701, "test-password")
	request := httptest.NewRequest(http.MethodGet, "https://opencode-701.local/api/v1/instances/701/proxy/", nil)
	request.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	recorder := httptest.NewRecorder()
	if err := service.ProxyRequest(context.Background(), 701, token.Token, recorder, request); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" {
		t.Fatalf("native entry unexpectedly redirected: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
	if !strings.Contains(recorder.Body.String(), "clawmanager-opencode-onboarding") {
		t.Fatal("local onboarding guide missing from native entry")
	}
}

func (s onboardingExternalAccessService) Get(context.Context, int) (*models.InstanceExternalAccess, error) {
	return s.access, nil
}

func TestOpenCodeOnboardingProjectDirectoryRequiresExplicitValidRequest(t *testing.T) {
	service, token := newOpenCodeV2ProxyTestService(t, "http://127.0.0.1:43210", 170, "gateway-token")
	workspace := t.TempDir()
	instance, err := service.instanceRepo.GetByID(170)
	if err != nil || instance == nil {
		t.Fatal("test instance unavailable", err)
	}
	instance.WorkspacePath = &workspace
	service.openCodeWorkspace = NewWorkspaceFileService(nil)

	request := func(body string, configure func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/170/proxy"+openCodeOnboardingProjectDirectoryPath, strings.NewReader(body))
		req.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(openCodeOnboardingRequestHeader, "1")
		if configure != nil {
			configure(req)
		}
		rec := httptest.NewRecorder()
		if err := service.ProxyRequest(req.Context(), 170, token.Token, rec, req); err != nil {
			t.Fatal(err)
		}
		return rec
	}

	missingHeader := request(`{"name":"project-a"}`, func(req *http.Request) {
		req.Header.Del(openCodeOnboardingRequestHeader)
	})
	if missingHeader.Code != http.StatusForbidden {
		t.Fatalf("missing header status = %d, want 403", missingHeader.Code)
	}
	if _, err := os.Stat(filepath.Join(workspace, "project-a")); !os.IsNotExist(err) {
		t.Fatal("rejected request created a directory")
	}
	wrongOwner, err := service.accessService.GenerateToken(99, 170, RuntimeTypeOpenCode, "/api/v1/instances/170/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wrongOwnerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/instances/170/proxy"+openCodeOnboardingProjectDirectoryPath, strings.NewReader(`{"name":"wrong-owner"}`))
	wrongOwnerRequest.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	wrongOwnerRequest.Header.Set("Content-Type", "application/json")
	wrongOwnerRequest.Header.Set(openCodeOnboardingRequestHeader, "1")
	wrongOwnerResponse := httptest.NewRecorder()
	if err := service.ProxyRequest(wrongOwnerRequest.Context(), 170, wrongOwner.Token, wrongOwnerResponse, wrongOwnerRequest); err != nil {
		t.Fatal(err)
	}
	if wrongOwnerResponse.Code != http.StatusForbidden {
		t.Fatalf("wrong owner status = %d, want 403", wrongOwnerResponse.Code)
	}
	if _, err := os.Stat(filepath.Join(workspace, "wrong-owner")); !os.IsNotExist(err) {
		t.Fatal("wrong owner token created a directory")
	}

	for _, name := range []string{"../escape", ".hidden", "home", "bad/name"} {
		rec := request(`{"name":"`+name+`"}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid name %q status = %d, want 400", name, rec.Code)
		}
	}

	created := request(`{"name":"project-a"}`, nil)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"name":"project-a"`) {
		t.Fatalf("create response = %d/%s", created.Code, created.Body.String())
	}
	if info, err := os.Stat(filepath.Join(workspace, "project-a")); err != nil || !info.IsDir() {
		t.Fatalf("project directory not created: %v", err)
	}
	conflict := request(`{"name":"project-a"}`, nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", conflict.Code)
	}
	if !strings.Contains(conflict.Body.String(), `"code":"PROJECT_EXISTS"`) {
		t.Fatalf("duplicate response missing conflict code: %s", conflict.Body.String())
	}
}

func TestOpenCodeOnboardingConflictCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{ErrWorkspaceEntryExists, "PROJECT_EXISTS"},
		{models.ErrRuntimeUpgradeConflict, "WORKSPACE_UPGRADING"},
	} {
		status, _, code := openCodeOnboardingWorkspaceError(tc.err)
		if status != http.StatusConflict || code != tc.code {
			t.Fatalf("error %v mapped to %d/%s, want 409/%s", tc.err, status, code, tc.code)
		}
	}
}

func TestOpenCodeOnboardingProjectDirectoryHonorsSharedWorkspaceWriteScope(t *testing.T) {
	service, _ := newOpenCodeV2ProxyTestService(t, "http://127.0.0.1:43210", 171, "gateway-token")
	workspace := t.TempDir()
	instance, err := service.instanceRepo.GetByID(171)
	if err != nil || instance == nil {
		t.Fatal("test instance unavailable", err)
	}
	instance.WorkspacePath = &workspace
	service.openCodeWorkspace = NewWorkspaceFileService(nil)
	code := "shared-read-only"
	access := &models.InstanceExternalAccess{
		InstanceID: 171, Enabled: true, AuthMode: ExternalAccessModeShareLink,
		WorkspaceAccess: ExternalWorkspaceAccessRead, PublicSlug: &code,
	}
	service.externalAccess = onboardingExternalAccessService{access: access}
	bound, err := service.accessService.GenerateBoundToken(
		45, 171, RuntimeTypeOpenCode, "/api/v1/instances/171/proxy/", "", 3000, time.Hour,
		ExternalAccessSessionBinding(code, access),
	)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/171/proxy"+openCodeOnboardingProjectDirectoryPath, strings.NewReader(`{"name":"forbidden"}`))
	req.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(openCodeOnboardingRequestHeader, "1")
	rec := httptest.NewRecorder()
	if err := service.ProxyRequest(req.Context(), 171, bound.Token, rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only share status = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(workspace, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("read-only share created a directory")
	}

	access.WorkspaceAccess = ExternalWorkspaceAccessWrite
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/171/proxy"+openCodeOnboardingProjectDirectoryPath, strings.NewReader(`{"name":"shared-project"}`))
	req.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set(openCodeOnboardingRequestHeader, "1")
	rec = httptest.NewRecorder()
	if err := service.ProxyRequest(req.Context(), 171, bound.Token, rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("write share status = %d/%s, want 201", rec.Code, rec.Body.String())
	}
	if info, err := os.Stat(filepath.Join(workspace, "shared-project")); err != nil || !info.IsDir() {
		t.Fatalf("write share project directory not created: %v", err)
	}
}
