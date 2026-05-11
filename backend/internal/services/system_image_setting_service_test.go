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

	openClawCount := 0
	for _, item := range items {
		if item.InstanceType == "openclaw" {
			openClawCount++
		}
	}

	if openClawCount != 2 {
		t.Fatalf("expected 2 openclaw runtime images, got %d", openClawCount)
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

	image, ok := service.GetRuntimeImage("openclaw")
	if ok || image != "" {
		t.Fatalf("expected runtime image lookup to be disabled after fallback, got %q %v", image, ok)
	}
}

func TestSystemImageSettingServiceGetRuntimeImageFallsBackToDefaultWhenNoRowsExist(t *testing.T) {
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{})

	image, ok := service.GetRuntimeImage("openclaw")
	if !ok {
		t.Fatalf("expected default openclaw runtime image to be available")
	}
	if image != defaultSystemImageSettings["openclaw"] {
		t.Fatalf("expected default openclaw image %q, got %q", defaultSystemImageSettings["openclaw"], image)
	}
}
