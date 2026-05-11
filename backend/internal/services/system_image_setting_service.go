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
	"hermes",
	"debian",
	"centos",
	"custom",
}

var supportedSystemImageTypes = map[string]string{
	"openclaw": "OpenClaw Desktop",
	"ubuntu":   "Ubuntu Desktop",
	"webtop":   "Webtop Desktop",
	"hermes":   "Hermes Runtime",
	"debian":   "Debian Desktop",
	"centos":   "CentOS Desktop",
	"custom":   "Custom Image",
}

var defaultSystemImageSettings = map[string]string{
	"openclaw": "ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest",
	"ubuntu":   "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"webtop":   "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"hermes":   "ghcr.io/yuan-lab-llm/agentsruntime/hermes:latest",
	"debian":   "docker.io/clawreef/debian-desktop:12",
	"centos":   "docker.io/clawreef/centos-desktop:9",
	"custom":   "registry.example.com/your-custom-image:latest",
}

var defaultEnabledSystemImageTypes = map[string]bool{
	"openclaw": true,
	"ubuntu":   true,
	"hermes":   true,
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
	Save(setting *models.SystemImageSetting) (*models.SystemImageSetting, error)
	DeleteByID(id int) error
	DisableType(instanceType string) error
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

	byType := make(map[string][]models.SystemImageSetting, len(orderedSystemImageTypes))
	for _, item := range stored {
		normalizedType := strings.TrimSpace(strings.ToLower(item.InstanceType))
		item.InstanceType = normalizedType
		if strings.TrimSpace(item.DisplayName) == "" {
			item.DisplayName = displayNameForSystemImageType(normalizedType)
		}
		byType[normalizedType] = append(byType[normalizedType], item)
	}

	settings := make([]models.SystemImageSetting, 0, len(stored)+len(orderedSystemImageTypes))
	for _, instanceType := range orderedSystemImageTypes {
		items := byType[instanceType]
		if len(items) == 0 {
			settings = append(settings, models.SystemImageSetting{
				InstanceType: instanceType,
				DisplayName:  displayNameForSystemImageType(instanceType),
				Image:        defaultSystemImageSettings[instanceType],
				IsEnabled:    defaultEnabledSystemImageTypes[instanceType],
			})
			continue
		}

		for _, item := range items {
			if item.IsEnabled {
				settings = append(settings, item)
			}
		}
		delete(byType, instanceType)
	}

	for instanceType, items := range byType {
		for _, item := range items {
			if item.IsEnabled {
				if strings.TrimSpace(item.DisplayName) == "" {
					item.DisplayName = displayNameForSystemImageType(instanceType)
				}
				settings = append(settings, item)
			}
		}
	}

	return settings, nil
}

func (s *systemImageSettingService) Save(setting *models.SystemImageSetting) (*models.SystemImageSetting, error) {
	normalizedType := strings.TrimSpace(strings.ToLower(setting.InstanceType))
	if _, ok := supportedSystemImageTypes[normalizedType]; !ok {
		return nil, errors.New("unsupported instance type")
	}

	image := strings.TrimSpace(setting.Image)
	if image == "" {
		return nil, errors.New("image is required")
	}

	setting.InstanceType = normalizedType
	setting.Image = image
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	if setting.DisplayName == "" {
		setting.DisplayName = supportedSystemImageTypes[normalizedType]
	}
	setting.IsEnabled = true

	if err := s.repo.Save(setting); err != nil {
		return nil, err
	}

	return setting, nil
}

func (s *systemImageSettingService) DeleteByID(id int) error {
	if id <= 0 {
		return errors.New("invalid image setting id")
	}

	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}

	if err := s.repo.DeleteByID(id); err != nil {
		return err
	}

	remaining, err := s.repo.ListByInstanceType(existing.InstanceType)
	if err != nil {
		return err
	}
	if len(remaining) > 0 || !isSupportedSystemImageType(existing.InstanceType) {
		return nil
	}

	return s.disableTypeWithFallback(existing.InstanceType)
}

func (s *systemImageSettingService) DisableType(instanceType string) error {
	normalizedType := strings.TrimSpace(strings.ToLower(instanceType))
	if !isSupportedSystemImageType(normalizedType) {
		return errors.New("unsupported instance type")
	}

	return s.disableTypeWithFallback(normalizedType)
}

func (s *systemImageSettingService) disableTypeWithFallback(instanceType string) error {
	if err := s.repo.DeleteByInstanceType(instanceType); err != nil {
		return err
	}

	return s.repo.Save(&models.SystemImageSetting{
		InstanceType: instanceType,
		DisplayName:  displayNameForSystemImageType(instanceType),
		Image:        defaultSystemImageSettings[instanceType],
		IsEnabled:    false,
	})
}

func (s *systemImageSettingService) GetRuntimeImage(instanceType string) (string, bool) {
	normalizedType := strings.TrimSpace(strings.ToLower(instanceType))
	items, err := s.repo.ListByInstanceType(normalizedType)
	if err != nil {
		return "", false
	}

	if len(items) == 0 {
		image := strings.TrimSpace(defaultSystemImageSettings[normalizedType])
		return image, image != "" && defaultEnabledSystemImageTypes[normalizedType]
	}

	for _, item := range items {
		if !item.IsEnabled {
			continue
		}
		image := strings.TrimSpace(item.Image)
		if image != "" {
			return image, true
		}
	}

	return "", false
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

func isSupportedSystemImageType(instanceType string) bool {
	_, ok := supportedSystemImageTypes[instanceType]
	return ok
}
