package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type recordingSystemImageSettingService struct {
	saved *models.SystemImageSetting
}

func (s *recordingSystemImageSettingService) List() ([]models.SystemImageSetting, error) {
	return nil, nil
}

func (s *recordingSystemImageSettingService) Save(setting *models.SystemImageSetting) (*models.SystemImageSetting, error) {
	saved := *setting
	s.saved = &saved
	return setting, nil
}

func (s *recordingSystemImageSettingService) DeleteByID(int) error {
	return nil
}

func (s *recordingSystemImageSettingService) DisableType(string) error {
	return nil
}

func (s *recordingSystemImageSettingService) GetRuntimeImage(string) (services.RuntimeImageConfig, bool) {
	return services.RuntimeImageConfig{}, false
}

func (s *recordingSystemImageSettingService) GetRuntimeImageForRuntimeType(string, string) (services.RuntimeImageConfig, bool) {
	return services.RuntimeImageConfig{}, false
}

func (s *recordingSystemImageSettingService) GetRuntimeImageForImage(string, string) (services.RuntimeImageConfig, bool) {
	return services.RuntimeImageConfig{}, false
}

func TestUpsertSystemImageSettingPreservesRuntimeVariant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &recordingSystemImageSettingService{}
	router := gin.New()
	router.PUT("/system-settings/images", NewSystemSettingsHandler(service).UpsertSystemImageSetting)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/system-settings/images", strings.NewReader(`{
		"instance_type":"codex",
		"runtime_type":"desktop",
		"runtime_variant":"linux",
		"display_name":"Codex Pro",
		"image":"docker.io/library/codex-linux:2026.8.24"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if service.saved == nil {
		t.Fatal("expected system image setting to be saved")
	}
	if service.saved.RuntimeVariant != services.WorkbuddyRuntimeLinux {
		t.Fatalf("expected runtime variant %q, got %q", services.WorkbuddyRuntimeLinux, service.saved.RuntimeVariant)
	}
	if !strings.Contains(recorder.Body.String(), `"runtime_variant":"linux"`) {
		t.Fatalf("expected response to preserve Linux runtime variant: %s", recorder.Body.String())
	}
}

func TestUpsertSystemImageSettingRejectsInvalidRuntimeVariant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &recordingSystemImageSettingService{}
	router := gin.New()
	router.PUT("/system-settings/images", NewSystemSettingsHandler(service).UpsertSystemImageSetting)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/system-settings/images", strings.NewReader(`{
		"instance_type":"codex",
		"runtime_type":"desktop",
		"runtime_variant":"macos",
		"display_name":"Codex Pro",
		"image":"docker.io/library/codex-linux:2026.8.24"
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	if service.saved != nil {
		t.Fatal("invalid runtime variant must not be saved")
	}
}
