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
	"workbuddy",
	"debian",
	"centos",
	"custom",
}

var supportedSystemImageTypes = map[string]string{
	"openclaw":  "OpenClaw Pro",
	"ubuntu":    "Ubuntu Desktop",
	"webtop":    "Webtop Desktop",
	"hermes":    "Hermes Pro",
	"workbuddy": "Workbuddy Pro",
	"debian":    "Debian Desktop",
	"centos":    "CentOS Desktop",
	"custom":    "Custom Image",
}

var defaultSystemImageSettings = map[string]string{
	"openclaw":  "ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest",
	"ubuntu":    "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"webtop":    "lscr.io/linuxserver/webtop:ubuntu-xfce",
	"hermes":    "ghcr.io/yuan-lab-llm/agentsruntime/hermes:latest",
	"workbuddy": "ghcr.io/yuan-lab-llm/agentsruntime/workbuddy-linux:latest",
	"debian":    "docker.io/clawreef/debian-desktop:12",
	"centos":    "docker.io/clawreef/centos-desktop:9",
	"custom":    "registry.example.com/your-custom-image:latest",
}

var defaultGatewaySystemImageSettings = map[string]string{
	"openclaw": "ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest",
	"ubuntu":   "ubuntu:22.04",
	"webtop":   "ubuntu:22.04",
	"hermes":   "ghcr.io/yuan-lab-llm/agentsruntime/hermes-lite:latest",
	"debian":   "debian:12",
	"centos":   "quay.io/centos/centos:stream9",
	"custom":   "registry.example.com/your-custom-shell-image:latest",
}

var defaultEnabledSystemImageTypes = map[string]bool{
	"openclaw":  true,
	"ubuntu":    true,
	"hermes":    true,
	"workbuddy": true,
}

var defaultEnabledGatewaySystemImageTypes = map[string]bool{
	"openclaw": true,
	"hermes":   true,
}

// RuntimeImageConfig is the runtime card selected for an instance type.
type RuntimeImageConfig struct {
	Image       string
	RuntimeType string
}

// RuntimeImageSettingsProvider exposes runtime image lookup for instance types.
type RuntimeImageSettingsProvider interface {
	GetRuntimeImage(instanceType string) (RuntimeImageConfig, bool)
	GetRuntimeImageForImage(instanceType, image string) (RuntimeImageConfig, bool)
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
	GetRuntimeImage(instanceType string) (RuntimeImageConfig, bool)
	GetRuntimeImageForImage(instanceType, image string) (RuntimeImageConfig, bool)
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
		item.RuntimeType = normalizeSystemImageRuntimeType(item.RuntimeType)
		if strings.TrimSpace(item.DisplayName) == "" {
			item.DisplayName = displayNameForSystemImageType(normalizedType)
		}
		byType[normalizedType] = append(byType[normalizedType], item)
	}

	settings := make([]models.SystemImageSetting, 0, len(stored)+len(orderedSystemImageTypes))
	for _, instanceType := range orderedSystemImageTypes {
		items := byType[instanceType]
		if len(items) == 0 {
			settings = append(settings, defaultSystemImagePresetsForType(instanceType)...)
			continue
		}

		settings = append(settings, enabledSystemImageSettingsForType(instanceType, items)...)
		delete(byType, instanceType)
	}

	for instanceType, items := range byType {
		settings = append(settings, enabledSystemImageSettingsForType(instanceType, items)...)
	}

	return settings, nil
}

func (s *systemImageSettingService) Save(setting *models.SystemImageSetting) (*models.SystemImageSetting, error) {
	normalizedType := strings.TrimSpace(strings.ToLower(setting.InstanceType))
	if _, ok := supportedSystemImageTypes[normalizedType]; !ok {
		return nil, errors.New("unsupported instance type")
	}

	runtimeType, err := validateSystemImageRuntimeType(setting.RuntimeType)
	if err != nil {
		return nil, err
	}

	image := strings.TrimSpace(setting.Image)
	if image == "" {
		return nil, errors.New("image is required")
	}

	setting.InstanceType = normalizedType
	setting.RuntimeType = runtimeType
	setting.Image = image
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	if setting.DisplayName == "" {
		setting.DisplayName = displayNameForSystemImagePreset(normalizedType, runtimeType)
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
		RuntimeType:  "desktop",
		DisplayName:  displayNameForSystemImagePreset(instanceType, "desktop"),
		Image:        defaultSystemImageSettings[instanceType],
		IsEnabled:    false,
	})
}

func (s *systemImageSettingService) GetRuntimeImage(instanceType string) (RuntimeImageConfig, bool) {
	normalizedType := strings.TrimSpace(strings.ToLower(instanceType))
	items, err := s.repo.ListByInstanceType(normalizedType)
	if err != nil {
		return RuntimeImageConfig{}, false
	}

	if len(items) == 0 {
		for _, item := range defaultSystemImagePresetsForType(normalizedType) {
			image := strings.TrimSpace(item.Image)
			if item.IsEnabled && image != "" {
				return RuntimeImageConfig{Image: image, RuntimeType: item.RuntimeType}, true
			}
		}
		return RuntimeImageConfig{}, false
	}

	for _, item := range enabledSystemImageSettingsForType(normalizedType, items) {
		image := strings.TrimSpace(item.Image)
		if image != "" {
			return RuntimeImageConfig{
				Image:       image,
				RuntimeType: normalizeSystemImageRuntimeType(item.RuntimeType),
			}, true
		}
	}

	return RuntimeImageConfig{}, false
}

func (s *systemImageSettingService) GetRuntimeImageForImage(instanceType, image string) (RuntimeImageConfig, bool) {
	normalizedType := strings.TrimSpace(strings.ToLower(instanceType))
	normalizedImage := strings.TrimSpace(image)
	if normalizedType == "" || normalizedImage == "" {
		return RuntimeImageConfig{}, false
	}

	items, err := s.repo.ListByInstanceType(normalizedType)
	if err != nil {
		return RuntimeImageConfig{}, false
	}

	if len(items) == 0 {
		for _, item := range defaultSystemImagePresetsForType(normalizedType) {
			defaultImage := strings.TrimSpace(item.Image)
			if item.IsEnabled && defaultImage == normalizedImage {
				return RuntimeImageConfig{Image: defaultImage, RuntimeType: item.RuntimeType}, true
			}
		}
		return RuntimeImageConfig{}, false
	}

	for _, item := range enabledSystemImageSettingsForType(normalizedType, items) {
		if strings.TrimSpace(item.Image) == normalizedImage {
			return RuntimeImageConfig{
				Image:       normalizedImage,
				RuntimeType: normalizeSystemImageRuntimeType(item.RuntimeType),
			}, true
		}
	}

	return RuntimeImageConfig{}, false
}

func runtimeImageOverride(instanceType string) (RuntimeImageConfig, bool) {
	if runtimeImageSettingsProvider == nil {
		return RuntimeImageConfig{}, false
	}
	return runtimeImageSettingsProvider.GetRuntimeImage(instanceType)
}

func runtimeImageOverrideForImage(instanceType, image string) (RuntimeImageConfig, bool) {
	if runtimeImageSettingsProvider == nil {
		return RuntimeImageConfig{}, false
	}
	return runtimeImageSettingsProvider.GetRuntimeImageForImage(instanceType, image)
}

func displayNameForSystemImageType(instanceType string) string {
	if name, ok := supportedSystemImageTypes[instanceType]; ok {
		return name
	}
	return fmt.Sprintf("%s Image", instanceType)
}

func displayNameForSystemImagePreset(instanceType, runtimeType string) string {
	normalizedRuntimeType := normalizeSystemImageRuntimeType(runtimeType)
	if instanceType == "openclaw" {
		if normalizedRuntimeType == "gateway" {
			return "OpenClaw Lite"
		}
		return "OpenClaw Pro"
	}
	if instanceType == "hermes" {
		if normalizedRuntimeType == "gateway" {
			return "Hermes Lite"
		}
		return "Hermes Pro"
	}
	return displayNameForSystemImageType(instanceType)
}

func defaultSystemImagePresetsForType(instanceType string) []models.SystemImageSetting {
	settings := []models.SystemImageSetting{{
		InstanceType: instanceType,
		RuntimeType:  "desktop",
		DisplayName:  displayNameForSystemImagePreset(instanceType, "desktop"),
		Image:        defaultSystemImageSettings[instanceType],
		IsEnabled:    defaultEnabledSystemImageTypes[instanceType],
	}}

	if image := strings.TrimSpace(defaultGatewaySystemImageSettings[instanceType]); image != "" {
		if defaultEnabledGatewaySystemImageTypes[instanceType] {
			settings = append(settings, models.SystemImageSetting{
				InstanceType: instanceType,
				RuntimeType:  "gateway",
				DisplayName:  displayNameForSystemImagePreset(instanceType, "gateway"),
				Image:        image,
				IsEnabled:    true,
			})
		}
	}

	return settings
}

func enabledSystemImageSettingsForType(instanceType string, stored []models.SystemImageSetting) []models.SystemImageSetting {
	if len(stored) == 0 {
		return defaultSystemImagePresetsForType(instanceType)
	}

	hasEnabled := false
	runtimeTypesWithRows := map[string]bool{}
	result := make([]models.SystemImageSetting, 0, len(stored)+2)
	for _, item := range stored {
		item.RuntimeType = normalizeSystemImageRuntimeType(item.RuntimeType)
		runtimeTypesWithRows[item.RuntimeType] = true
		if strings.TrimSpace(item.DisplayName) == "" {
			item.DisplayName = displayNameForSystemImagePreset(instanceType, item.RuntimeType)
		}
		if item.IsEnabled {
			hasEnabled = true
			result = append(result, item)
		}
	}

	if !hasEnabled {
		return result
	}

	for _, preset := range defaultSystemImagePresetsForType(instanceType) {
		if runtimeTypesWithRows[preset.RuntimeType] || !preset.IsEnabled || strings.TrimSpace(preset.Image) == "" {
			continue
		}
		result = append(result, preset)
	}
	return result
}

func isSupportedSystemImageType(instanceType string) bool {
	_, ok := supportedSystemImageTypes[instanceType]
	return ok
}

func normalizeSystemImageRuntimeType(runtimeType string) string {
	normalized := strings.TrimSpace(strings.ToLower(runtimeType))
	if normalized == "gateway" || normalized == "shell" {
		return "gateway"
	}
	return "desktop"
}

func validateSystemImageRuntimeType(runtimeType string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(runtimeType))
	if normalized == "" {
		return "desktop", nil
	}
	if normalized == "shell" {
		return "gateway", nil
	}
	if normalized != "desktop" && normalized != "gateway" {
		return "", errors.New("unsupported runtime type")
	}
	return normalized, nil
}
