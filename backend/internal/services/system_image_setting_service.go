package services

import (
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

var orderedSystemImageTypes = []string{
	"openclaw",
	"ubuntu",
	"webtop",
	"debian",
	"centos",
	"custom",
}

var supportedSystemImageTypes = map[string]string{
	"openclaw": "OpenClaw Desktop",
	"ubuntu":   "Ubuntu Desktop",
	"webtop":   "Webtop Desktop",
	"debian":   "Debian Desktop",
	"centos":   "CentOS Desktop",
	"custom":   "Custom Image",
}

var defaultSystemImageSettings = map[string]string{
	"openclaw": "ericpearlee/openclaw:v2026.3.24",
	"ubuntu":   "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"webtop":   "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"debian":   "docker.io/clawreef/debian-desktop:12",
	"centos":   "docker.io/clawreef/centos-desktop:9",
	"custom":   "registry.example.com/your-custom-image:latest",
}

var defaultEnabledSystemImageTypes = map[string]bool{
	"openclaw": true,
	"ubuntu":   true,
}

// RuntimeImageSettingsProvider exposes runtime image lookup for instance types.
type RuntimeImageSettingsProvider interface {
	GetRuntimeImage(instanceType string) (string, bool)
}

var runtimeImageSettingsProvider RuntimeImageSettingsProvider

// SetRuntimeImageSettingsProvider configures the global runtime image provider used by runtime resolution.
func SetRuntimeImageSettingsProvider(provider RuntimeImageSettingsProvider) {
	runtimeImageSettingsProvider = provider
}

type SystemImageSettingService interface {
	List() ([]models.SystemImageSetting, error)
	Save(setting *models.SystemImageSetting) error
	Delete(instanceType string) error
	GetRuntimeImage(instanceType string) (string, bool)
}

type systemImageSettingService struct {
	repo repository.SystemImageSettingRepository
}

// NewSystemImageSettingService creates a new system image setting service.
func NewSystemImageSettingService(repo repository.SystemImageSettingRepository) SystemImageSettingService {
	return &systemImageSettingService{repo: repo}
}

func (s *systemImageSettingService) List() ([]models.SystemImageSetting, error) {
	stored, err := s.repo.List()
	if err != nil {
		return nil, err
	}

	byType := make(map[string]models.SystemImageSetting, len(stored))
	for _, item := range stored {
		byType[item.InstanceType] = item
	}

	settings := make([]models.SystemImageSetting, 0, len(orderedSystemImageTypes))
	for _, instanceType := range orderedSystemImageTypes {
		displayName := supportedSystemImageTypes[instanceType]
		if storedItem, ok := byType[instanceType]; ok {
			if strings.TrimSpace(storedItem.DisplayName) == "" {
				storedItem.DisplayName = displayName
			}
			settings = append(settings, storedItem)
			continue
		}

		settings = append(settings, models.SystemImageSetting{
			InstanceType: instanceType,
			DisplayName:  displayName,
			Image:        defaultSystemImageSettings[instanceType],
			IsEnabled:    defaultEnabledSystemImageTypes[instanceType],
		})
	}

	return settings, nil
}

func (s *systemImageSettingService) Save(setting *models.SystemImageSetting) error {
	setting.InstanceType = strings.TrimSpace(strings.ToLower(setting.InstanceType))
	if _, ok := supportedSystemImageTypes[setting.InstanceType]; !ok {
		return errors.New("unsupported instance type")
	}

	setting.Image = strings.TrimSpace(setting.Image)
	if setting.Image == "" {
		return errors.New("image is required")
	}

	if strings.TrimSpace(setting.DisplayName) == "" {
		setting.DisplayName = supportedSystemImageTypes[setting.InstanceType]
	}
	setting.IsEnabled = true

	return s.repo.Upsert(setting)
}

func (s *systemImageSettingService) Delete(instanceType string) error {
	instanceType = strings.TrimSpace(strings.ToLower(instanceType))
	if _, ok := supportedSystemImageTypes[instanceType]; !ok {
		return errors.New("unsupported instance type")
	}

	existing, err := s.repo.GetByInstanceType(instanceType)
	if err != nil {
		return err
	}

	if existing == nil {
		return s.repo.Upsert(&models.SystemImageSetting{
			InstanceType: instanceType,
			DisplayName:  supportedSystemImageTypes[instanceType],
			Image:        defaultSystemImageSettings[instanceType],
			IsEnabled:    false,
		})
	}

	existing.IsEnabled = false
	return s.repo.Upsert(existing)
}

func (s *systemImageSettingService) GetRuntimeImage(instanceType string) (string, bool) {
	instanceType = strings.TrimSpace(strings.ToLower(instanceType))
	setting, err := s.repo.GetByInstanceType(instanceType)
	if err != nil {
		return "", false
	}

	if setting == nil {
		image := strings.TrimSpace(defaultSystemImageSettings[instanceType])
		return image, image != "" && defaultEnabledSystemImageTypes[instanceType]
	}

	if !setting.IsEnabled {
		return "", false
	}

	image := strings.TrimSpace(setting.Image)
	return image, image != ""
}

func runtimeImageOverride(instanceType string) (string, bool) {
	if runtimeImageSettingsProvider == nil {
		return "", false
	}
	return runtimeImageSettingsProvider.GetRuntimeImage(instanceType)
}

func displayNameForSystemImageType(instanceType string) string {
	if name, ok := supportedSystemImageTypes[instanceType]; ok {
		return name
	}
	return fmt.Sprintf("%s Image", instanceType)
}
