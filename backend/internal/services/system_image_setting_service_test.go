package services

import (
	"testing"

	"clawreef/internal/models"
)

type stubSystemImageSettingRepository struct {
	items  []models.SystemImageSetting
	nextID int
}

func (r *stubSystemImageSettingRepository) List() ([]models.SystemImageSetting, error) {
	out := make([]models.SystemImageSetting, len(r.items))
	copy(out, r.items)
	return out, nil
}

func (r *stubSystemImageSettingRepository) GetByID(id int) (*models.SystemImageSetting, error) {
	for _, item := range r.items {
		if item.ID == id {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, nil
}

func (r *stubSystemImageSettingRepository) ListByInstanceType(instanceType string) ([]models.SystemImageSetting, error) {
	var out []models.SystemImageSetting
	for _, item := range r.items {
		if item.InstanceType == instanceType {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *stubSystemImageSettingRepository) Save(setting *models.SystemImageSetting) error {
	if setting.ID > 0 {
		for i := range r.items {
			if r.items[i].ID == setting.ID {
				r.items[i] = *setting
				return nil
			}
		}
	}

	r.nextID++
	copyItem := *setting
	copyItem.ID = r.nextID
	*setting = copyItem
	r.items = append(r.items, copyItem)
	return nil
}

func (r *stubSystemImageSettingRepository) DeleteByID(id int) error {
	filtered := r.items[:0]
	for _, item := range r.items {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	r.items = filtered
	return nil
}

func (r *stubSystemImageSettingRepository) DeleteByInstanceType(instanceType string) error {
	filtered := r.items[:0]
	for _, item := range r.items {
		if item.InstanceType != instanceType {
			filtered = append(filtered, item)
		}
	}
	r.items = filtered
	return nil
}

func TestSystemImageSettingServiceListAllowsMultipleImagesPerType(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{ID: 1, InstanceType: "openclaw", DisplayName: "OpenClaw Stable", Image: "registry/openclaw:stable", IsEnabled: true},
			{ID: 2, InstanceType: "openclaw", DisplayName: "OpenClaw Canary", Image: "registry/openclaw:canary", IsEnabled: true},
			{ID: 3, InstanceType: "ubuntu", DisplayName: "Ubuntu Desktop", Image: "lscr.io/linuxserver/webtop:ubuntu-xfce", IsEnabled: false},
		},
		nextID: 3,
	}

	service := NewSystemImageSettingService(repo)
	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	openClawDesktopCount := 0
	openClawGatewayCount := 0
	for _, item := range items {
		if item.InstanceType != "openclaw" {
			continue
		}
		switch item.RuntimeType {
		case "desktop":
			openClawDesktopCount++
		case "gateway":
			openClawGatewayCount++
		}
	}

	if openClawDesktopCount != 2 {
		t.Fatalf("expected 2 openclaw desktop images, got %d", openClawDesktopCount)
	}
	if openClawGatewayCount != 1 {
		t.Fatalf("expected openclaw gateway default to be backfilled, got %d", openClawGatewayCount)
	}
}

func TestSystemImageSettingServiceDeleteByIDCreatesDisabledFallback(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{ID: 1, InstanceType: "openclaw", DisplayName: "OpenClaw Stable", Image: "registry/openclaw:stable", IsEnabled: true},
		},
		nextID: 1,
	}

	service := NewSystemImageSettingService(repo)
	if err := service.DeleteByID(1); err != nil {
		t.Fatalf("DeleteByID returned error: %v", err)
	}

	items, err := repo.ListByInstanceType("openclaw")
	if err != nil {
		t.Fatalf("ListByInstanceType returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 fallback row after deleting last image, got %d", len(items))
	}
	if items[0].IsEnabled {
		t.Fatalf("expected fallback row to be disabled")
	}

	selection, ok := service.GetRuntimeImage("openclaw")
	if ok || selection.Image != "" {
		t.Fatalf("expected runtime image lookup to be disabled after fallback, got %q %v", selection.Image, ok)
	}
}

func TestSystemImageSettingServiceGetRuntimeImageFallsBackToDefaultWhenNoRowsExist(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	selection, ok := service.GetRuntimeImage("openclaw")
	if !ok {
		t.Fatalf("expected default openclaw runtime image to be available")
	}
	if selection.Image != defaultSystemImageSettings["openclaw"] {
		t.Fatalf("expected default openclaw image %q, got %q", defaultSystemImageSettings["openclaw"], selection.Image)
	}
	if selection.RuntimeType != "desktop" {
		t.Fatalf("expected default openclaw runtime type desktop, got %q", selection.RuntimeType)
	}
}

func TestSystemImageSettingServiceListIncludesOpenClawGatewayDefault(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	found := false
	for _, item := range items {
		if item.InstanceType == "openclaw" && item.RuntimeType == "gateway" {
			found = true
			if item.Image != defaultGatewaySystemImageSettings["openclaw"] {
				t.Fatalf("expected default openclaw gateway image %q, got %q", defaultGatewaySystemImageSettings["openclaw"], item.Image)
			}
			if !item.IsEnabled {
				t.Fatalf("expected default openclaw gateway image to be enabled")
			}
		}
	}

	if !found {
		t.Fatalf("expected default openclaw gateway runtime image")
	}
}

func TestSystemImageSettingServiceListIncludesHermesLiteDefault(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	found := false
	for _, item := range items {
		if item.InstanceType == "hermes" && item.RuntimeType == "gateway" {
			found = true
			if item.Image != defaultGatewaySystemImageSettings["hermes"] {
				t.Fatalf("expected default hermes lite image %q, got %q", defaultGatewaySystemImageSettings["hermes"], item.Image)
			}
			if item.DisplayName != "Hermes Lite" {
				t.Fatalf("expected Hermes Lite display name, got %q", item.DisplayName)
			}
			if !item.IsEnabled {
				t.Fatalf("expected default hermes lite image to be enabled")
			}
		}
	}

	if !found {
		t.Fatalf("expected default hermes lite runtime image")
	}
}

func TestSystemImageSettingServiceListIncludesWorkbuddyProDefault(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	for _, item := range items {
		if item.InstanceType != "workbuddy" || item.RuntimeType != "desktop" {
			continue
		}
		if item.DisplayName != "Workbuddy Pro" {
			t.Fatalf("expected Workbuddy Pro display name, got %q", item.DisplayName)
		}
		if item.Image != defaultSystemImageSettings["workbuddy"] {
			t.Fatalf("expected Workbuddy image %q, got %q", defaultSystemImageSettings["workbuddy"], item.Image)
		}
		if !item.IsEnabled {
			t.Fatalf("expected Workbuddy Pro default to be enabled")
		}
		return
	}

	t.Fatalf("expected Workbuddy Pro runtime image")
}

func TestSystemImageSettingServiceListBackfillsHermesProDefaultWhenLiteStored(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{
				ID:           1,
				InstanceType: "hermes",
				RuntimeType:  "gateway",
				DisplayName:  "Hermes Lite",
				Image:        "registry/hermes-lite:stored",
				IsEnabled:    true,
			},
		},
		nextID: 1,
	}
	service := NewSystemImageSettingService(repo)

	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	foundLite := false
	foundPro := false
	for _, item := range items {
		if item.InstanceType != "hermes" {
			continue
		}
		if item.RuntimeType == "gateway" && item.Image == "registry/hermes-lite:stored" {
			foundLite = true
		}
		if item.RuntimeType == "desktop" && item.Image == defaultSystemImageSettings["hermes"] {
			foundPro = true
			if item.DisplayName != "Hermes Pro" {
				t.Fatalf("expected Hermes Pro display name, got %q", item.DisplayName)
			}
		}
	}

	if !foundLite {
		t.Fatalf("expected stored hermes lite image to remain available")
	}
	if !foundPro {
		t.Fatalf("expected missing hermes pro default image to be backfilled")
	}
}

func TestSystemImageSettingServiceGetRuntimeImageForImageFallsBackToOpenClawGatewayDefault(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	selection, ok := service.GetRuntimeImageForImage("openclaw", defaultGatewaySystemImageSettings["openclaw"])
	if !ok {
		t.Fatalf("expected default openclaw gateway runtime image to resolve")
	}
	if selection.RuntimeType != "gateway" {
		t.Fatalf("expected runtime type gateway, got %q", selection.RuntimeType)
	}
	if selection.Image != defaultGatewaySystemImageSettings["openclaw"] {
		t.Fatalf("expected gateway image %q, got %q", defaultGatewaySystemImageSettings["openclaw"], selection.Image)
	}
}

func TestSystemImageSettingServiceGetRuntimeImageForImageBackfillsHermesProDefaultWhenLiteStored(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{
				ID:           1,
				InstanceType: "hermes",
				RuntimeType:  "gateway",
				DisplayName:  "Hermes Lite",
				Image:        "registry/hermes-lite:stored",
				IsEnabled:    true,
			},
		},
		nextID: 1,
	}
	service := NewSystemImageSettingService(repo)

	selection, ok := service.GetRuntimeImageForImage("hermes", defaultSystemImageSettings["hermes"])
	if !ok {
		t.Fatalf("expected default hermes pro image to resolve")
	}
	if selection.RuntimeType != "desktop" {
		t.Fatalf("expected runtime type desktop, got %q", selection.RuntimeType)
	}
	if selection.Image != defaultSystemImageSettings["hermes"] {
		t.Fatalf("expected hermes pro image %q, got %q", defaultSystemImageSettings["hermes"], selection.Image)
	}
}

func TestSystemImageSettingServiceGetRuntimeImageForImageUsesCardRuntimeType(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{ID: 1, InstanceType: "openclaw", RuntimeType: "desktop", DisplayName: "OpenClaw Desktop", Image: "registry/openclaw-desktop:latest", IsEnabled: true},
			{ID: 2, InstanceType: "openclaw", RuntimeType: "gateway", DisplayName: "OpenClaw Lite", Image: "registry/openclaw-lite:latest", IsEnabled: true},
		},
		nextID: 2,
	}

	service := NewSystemImageSettingService(repo)
	selection, ok := service.GetRuntimeImageForImage("openclaw", "registry/openclaw-lite:latest")
	if !ok {
		t.Fatalf("expected selected gateway runtime image to resolve")
	}
	if selection.RuntimeType != "gateway" {
		t.Fatalf("expected runtime type gateway, got %q", selection.RuntimeType)
	}
	if selection.Image != "registry/openclaw-lite:latest" {
		t.Fatalf("expected gateway image to resolve, got %q", selection.Image)
	}
}

func TestSystemImageSettingServiceSaveAcceptsGatewayRuntimeType(t *testing.T) {
	repo := &stubSystemImageSettingRepository{}
	service := NewSystemImageSettingService(repo)

	saved, err := service.Save(&models.SystemImageSetting{
		InstanceType: "OpenClaw",
		RuntimeType:  "gateway",
		DisplayName:  "OpenClaw Lite",
		Image:        "registry/openclaw-lite:v2",
	})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if saved.InstanceType != "openclaw" {
		t.Fatalf("expected normalized instance type openclaw, got %q", saved.InstanceType)
	}
	if saved.RuntimeType != "gateway" {
		t.Fatalf("expected gateway runtime type, got %q", saved.RuntimeType)
	}
}

func TestSystemImageSettingServiceNormalizesLegacyShellRuntimeTypeToGateway(t *testing.T) {
	repo := &stubSystemImageSettingRepository{
		items: []models.SystemImageSetting{
			{ID: 1, InstanceType: "hermes", RuntimeType: "shell", DisplayName: "Hermes Lite", Image: "registry/hermes-lite:legacy", IsEnabled: true},
		},
		nextID: 1,
	}
	service := NewSystemImageSettingService(repo)

	items, err := service.List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	for _, item := range items {
		if item.ID == 1 && item.RuntimeType != "gateway" {
			t.Fatalf("expected legacy shell row to be exposed as gateway, got %q", item.RuntimeType)
		}
	}

	selection, ok := service.GetRuntimeImageForImage("hermes", "registry/hermes-lite:legacy")
	if !ok {
		t.Fatalf("expected legacy shell image to resolve")
	}
	if selection.RuntimeType != "gateway" {
		t.Fatalf("expected legacy shell image to resolve as gateway, got %q", selection.RuntimeType)
	}
}
