package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

const (
	OpenClawConfigResourceTypeChannel         = "channel"
	OpenClawConfigResourceTypeSkill           = "skill"
	OpenClawConfigResourceTypeSessionTemplate = "session_template"
	OpenClawConfigResourceTypeLogPolicy       = "log_policy"
	OpenClawConfigResourceTypeAgent           = "agent"
	OpenClawConfigResourceTypeScheduledTask   = "scheduled_task"

	OpenClawConfigPlanModeNone   = "none"
	OpenClawConfigPlanModeBundle = "bundle"
	OpenClawConfigPlanModeManual = "manual"

	OpenClawBootstrapManifestEnv     = "CLAWMANAGER_OPENCLAW_BOOTSTRAP_MANIFEST_JSON"
	OpenClawChannelsEnv              = "CLAWMANAGER_OPENCLAW_CHANNELS_JSON"
	OpenClawSkillsEnv                = "CLAWMANAGER_OPENCLAW_SKILLS_JSON"
	OpenClawSessionTemplatesEnv      = "CLAWMANAGER_OPENCLAW_SESSION_TEMPLATES_JSON"
	OpenClawLogPoliciesEnv           = "CLAWMANAGER_OPENCLAW_LOG_POLICIES_JSON"
	OpenClawAgentsEnv                = "CLAWMANAGER_OPENCLAW_AGENTS_JSON"
	OpenClawScheduledTasksEnv        = "CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON"
	openClawBootstrapPayloadMaxBytes = 64 * 1024

	openClawCompiledSnapshotStatus = "compiled"
	openClawActiveSnapshotStatus   = "active"
	openClawFailedSnapshotStatus   = "failed"
	defaultSnapshotListLimit       = 50
)

var (
	openClawAllowedResourceTypes = map[string]struct{}{
		OpenClawConfigResourceTypeChannel:         {},
		OpenClawConfigResourceTypeSkill:           {},
		OpenClawConfigResourceTypeSessionTemplate: {},
		OpenClawConfigResourceTypeLogPolicy:       {},
		OpenClawConfigResourceTypeAgent:           {},
		OpenClawConfigResourceTypeScheduledTask:   {},
	}
	openClawPlanModes = map[string]struct{}{
		OpenClawConfigPlanModeNone:   {},
		OpenClawConfigPlanModeBundle: {},
		OpenClawConfigPlanModeManual: {},
	}
	openClawResourceTypeOrder = []string{
		OpenClawConfigResourceTypeChannel,
		OpenClawConfigResourceTypeSkill,
		OpenClawConfigResourceTypeSessionTemplate,
		OpenClawConfigResourceTypeLogPolicy,
		OpenClawConfigResourceTypeAgent,
		OpenClawConfigResourceTypeScheduledTask,
	}
	openClawEnvByResourceType = map[string]string{
		OpenClawConfigResourceTypeChannel:         OpenClawChannelsEnv,
		OpenClawConfigResourceTypeSkill:           OpenClawSkillsEnv,
		OpenClawConfigResourceTypeSessionTemplate: OpenClawSessionTemplatesEnv,
		OpenClawConfigResourceTypeLogPolicy:       OpenClawLogPoliciesEnv,
		OpenClawConfigResourceTypeAgent:           OpenClawAgentsEnv,
		OpenClawConfigResourceTypeScheduledTask:   OpenClawScheduledTasksEnv,
	}
	openClawResourceKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{1,99}$`)
)

type OpenClawConfigDependency struct {
	Type     string `json:"type"`
	Key      string `json:"key"`
	Required bool   `json:"required"`
}

type OpenClawConfigEnvelope struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Kind          string                     `json:"kind"`
	Format        string                     `json:"format"`
	DependsOn     []OpenClawConfigDependency `json:"dependsOn"`
	Config        json.RawMessage            `json:"config"`
}

type OpenClawConfigPlan struct {
	Mode        string `json:"mode"`
	BundleID    *int   `json:"bundle_id,omitempty"`
	ResourceIDs []int  `json:"resource_ids,omitempty"`
}

type OpenClawConfigResourceSummary struct {
	ID           int    `json:"id"`
	ResourceType string `json:"resource_type"`
	ResourceKey  string `json:"resource_key"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Version      int    `json:"version"`
}

type OpenClawConfigResourcePayload struct {
	ID           int             `json:"id"`
	UserID       int             `json:"user_id"`
	ResourceType string          `json:"resource_type"`
	ResourceKey  string          `json:"resource_key"`
	Name         string          `json:"name"`
	Description  *string         `json:"description,omitempty"`
	Enabled      bool            `json:"enabled"`
	Version      int             `json:"version"`
	Tags         []string        `json:"tags"`
	Content      json.RawMessage `json:"content"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type UpsertOpenClawConfigResourceRequest struct {
	ResourceType string          `json:"resource_type"`
	ResourceKey  string          `json:"resource_key"`
	Name         string          `json:"name"`
	Description  *string         `json:"description,omitempty"`
	Enabled      bool            `json:"enabled"`
	Tags         []string        `json:"tags"`
	Content      json.RawMessage `json:"content"`
}

type OpenClawConfigBundleItemPayload struct {
	ResourceID int                            `json:"resource_id"`
	SortOrder  int                            `json:"sort_order"`
	Required   bool                           `json:"required"`
	Resource   *OpenClawConfigResourceSummary `json:"resource,omitempty"`
}

type OpenClawConfigBundlePayload struct {
	ID          int                               `json:"id"`
	UserID      int                               `json:"user_id"`
	Name        string                            `json:"name"`
	Description *string                           `json:"description,omitempty"`
	Enabled     bool                              `json:"enabled"`
	Version     int                               `json:"version"`
	Items       []OpenClawConfigBundleItemPayload `json:"items"`
	CreatedAt   time.Time                         `json:"created_at"`
	UpdatedAt   time.Time                         `json:"updated_at"`
}

type UpsertOpenClawConfigBundleRequest struct {
	Name        string                            `json:"name"`
	Description *string                           `json:"description,omitempty"`
	Enabled     bool                              `json:"enabled"`
	Items       []OpenClawConfigBundleItemPayload `json:"items"`
}

type OpenClawConfigCompilePreview struct {
	Mode              string                          `json:"mode"`
	Bundle            *OpenClawConfigBundlePayload    `json:"bundle,omitempty"`
	SelectedResources []OpenClawConfigResourceSummary `json:"selected_resources"`
	ResolvedResources []OpenClawConfigResourceSummary `json:"resolved_resources"`
	AutoIncluded      []OpenClawConfigResourceSummary `json:"auto_included"`
	Warnings          []string                        `json:"warnings"`
	EnvNames          []string                        `json:"env_names"`
	PayloadSizes      map[string]int                  `json:"payload_sizes"`
	TotalPayloadBytes int                             `json:"total_payload_bytes"`
	Manifest          json.RawMessage                 `json:"manifest"`
}

type OpenClawInjectionSnapshotPayload struct {
	ID                  int                             `json:"id"`
	InstanceID          *int                            `json:"instance_id,omitempty"`
	UserID              int                             `json:"user_id"`
	Mode                string                          `json:"mode"`
	BundleID            *int                            `json:"bundle_id,omitempty"`
	SelectedResourceIDs []int                           `json:"selected_resource_ids"`
	ResolvedResources   []OpenClawConfigResourceSummary `json:"resolved_resources"`
	Manifest            json.RawMessage                 `json:"manifest"`
	EnvNames            []string                        `json:"env_names"`
	PayloadSizes        map[string]int                  `json:"payload_sizes"`
	SecretName          *string                         `json:"secret_name,omitempty"`
	Status              string                          `json:"status"`
	ErrorMessage        *string                         `json:"error_message,omitempty"`
	CreatedAt           time.Time                       `json:"created_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
	ActivatedAt         *time.Time                      `json:"activated_at,omitempty"`
}

type OpenClawConfigService interface {
	ListResources(userID int, resourceType string) ([]OpenClawConfigResourcePayload, error)
	GetResource(userID, id int) (*OpenClawConfigResourcePayload, error)
	CreateResource(userID int, req UpsertOpenClawConfigResourceRequest) (*OpenClawConfigResourcePayload, error)
	UpdateResource(userID, id int, req UpsertOpenClawConfigResourceRequest) (*OpenClawConfigResourcePayload, error)
	DeleteResource(userID, id int) error
	CloneResource(userID, id int) (*OpenClawConfigResourcePayload, error)
	ValidateResource(req UpsertOpenClawConfigResourceRequest) error

	ListBundles(userID int) ([]OpenClawConfigBundlePayload, error)
	GetBundle(userID, id int) (*OpenClawConfigBundlePayload, error)
	CreateBundle(userID int, req UpsertOpenClawConfigBundleRequest) (*OpenClawConfigBundlePayload, error)
	UpdateBundle(userID, id int, req UpsertOpenClawConfigBundleRequest) (*OpenClawConfigBundlePayload, error)
	DeleteBundle(userID, id int) error
	CloneBundle(userID, id int) (*OpenClawConfigBundlePayload, error)

	CompilePreview(userID int, plan OpenClawConfigPlan) (*OpenClawConfigCompilePreview, error)
	CreateSnapshotForInstance(userID int, instance *models.Instance, plan *OpenClawConfigPlan) (*models.OpenClawInjectionSnapshot, error)
	MarkSnapshotActive(snapshot *models.OpenClawInjectionSnapshot) error
	MarkSnapshotFailed(snapshot *models.OpenClawInjectionSnapshot, err error) error
	EnsureSnapshotSecret(ctx context.Context, userID int, instance *models.Instance, snapshotID int) (string, error)
	ListSnapshots(userID int, limit int) ([]OpenClawInjectionSnapshotPayload, error)
	GetSnapshot(userID, id int) (*OpenClawInjectionSnapshotPayload, error)
}

type openClawConfigService struct {
	repo          repository.OpenClawConfigRepository
	secretService *k8s.SecretService
}

type compiledOpenClawResource struct {
	model    models.OpenClawConfigResource
	tags     []string
	envelope OpenClawConfigEnvelope
}

type compiledOpenClawConfig struct {
	plan             OpenClawConfigPlan
	bundle           *models.OpenClawConfigBundle
	selected         []compiledOpenClawResource
	resolved         []compiledOpenClawResource
	autoIncluded     []compiledOpenClawResource
	warnings         []string
	renderedEnv      map[string]string
	manifest         string
	payloadSizes     map[string]int
	totalPayloadSize int
}

func NewOpenClawConfigService(repo repository.OpenClawConfigRepository) OpenClawConfigService {
	return &openClawConfigService{
		repo:          repo,
		secretService: k8s.NewSecretService(),
	}
}

func (s *openClawConfigService) ListResources(userID int, resourceType string) ([]OpenClawConfigResourcePayload, error) {
	if resourceType != "" && !isValidOpenClawResourceType(resourceType) {
		return nil, fmt.Errorf("invalid openclaw resource type")
	}

	items, err := s.repo.ListResources(userID, resourceType)
	if err != nil {
		return nil, err
	}

	result := make([]OpenClawConfigResourcePayload, 0, len(items))
	for _, item := range items {
		payload, err := resourcePayloadFromModel(item)
		if err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, nil
}

func (s *openClawConfigService) GetResource(userID, id int) (*OpenClawConfigResourcePayload, error) {
	item, err := s.repo.GetResourceByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config resource not found")
	}

	payload, err := resourcePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) CreateResource(userID int, req UpsertOpenClawConfigResourceRequest) (*OpenClawConfigResourcePayload, error) {
	resourceType := normalizeResourceType(req.ResourceType)
	resourceKey := normalizeResourceKey(req.ResourceKey)
	normalizedContent, err := normalizeOpenClawResourceContent(resourceType, resourceKey, req.Content)
	if err != nil {
		return nil, err
	}
	req.Content = normalizedContent
	if err := s.ValidateResource(req); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetResourceByUserTypeKey(userID, resourceType, resourceKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("openclaw config resource key already exists")
	}

	now := time.Now()
	item := &models.OpenClawConfigResource{
		UserID:       userID,
		ResourceType: resourceType,
		ResourceKey:  resourceKey,
		Name:         strings.TrimSpace(req.Name),
		Description:  normalizeOptionalString(req.Description),
		Enabled:      req.Enabled,
		Version:      1,
		TagsJSON:     encodeStringArray(normalizeTags(req.Tags)),
		ContentJSON:  string(normalizedContent),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateResource(item); err != nil {
		return nil, err
	}

	payload, err := resourcePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) UpdateResource(userID, id int, req UpsertOpenClawConfigResourceRequest) (*OpenClawConfigResourcePayload, error) {
	item, err := s.repo.GetResourceByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config resource not found")
	}

	resourceType := normalizeResourceType(req.ResourceType)
	resourceKey := normalizeResourceKey(req.ResourceKey)
	normalizedContent, err := normalizeOpenClawResourceContent(resourceType, resourceKey, req.Content)
	if err != nil {
		return nil, err
	}
	req.Content = normalizedContent
	if err := s.ValidateResource(req); err != nil {
		return nil, err
	}
	existing, err := s.repo.GetResourceByUserTypeKey(userID, resourceType, resourceKey)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != id {
		return nil, fmt.Errorf("openclaw config resource key already exists")
	}

	item.ResourceType = resourceType
	item.ResourceKey = resourceKey
	item.Name = strings.TrimSpace(req.Name)
	item.Description = normalizeOptionalString(req.Description)
	item.Enabled = req.Enabled
	item.Version++
	item.TagsJSON = encodeStringArray(normalizeTags(req.Tags))
	item.ContentJSON = string(normalizedContent)
	item.UpdatedAt = time.Now()

	if err := s.repo.UpdateResource(item); err != nil {
		return nil, err
	}

	// Cascade: recompile active snapshots that reference this resource.
	// Non-blocking — errors are logged but do not fail the update.
	go s.cascadeSnapshotsForResource(userID, id)

	payload, err := resourcePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) DeleteResource(userID, id int) error {
	item, err := s.repo.GetResourceByID(id)
	if err != nil {
		return err
	}
	if item == nil || item.UserID != userID {
		return fmt.Errorf("openclaw config resource not found")
	}
	return s.repo.DeleteResource(id)
}

func (s *openClawConfigService) CloneResource(userID, id int) (*OpenClawConfigResourcePayload, error) {
	item, err := s.repo.GetResourceByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config resource not found")
	}

	clone := *item
	clone.ID = 0
	clone.Name = fmt.Sprintf("%s Copy", item.Name)
	clone.ResourceKey = fmt.Sprintf("%s-copy-%d", item.ResourceKey, time.Now().Unix())
	clone.Version = 1
	clone.CreatedAt = time.Now()
	clone.UpdatedAt = clone.CreatedAt
	if normalizedContent, err := normalizeOpenClawResourceContent(clone.ResourceType, item.ResourceKey, json.RawMessage(clone.ContentJSON)); err == nil {
		clone.ContentJSON = string(normalizedContent)
	}

	if err := s.repo.CreateResource(&clone); err != nil {
		return nil, err
	}

	payload, err := resourcePayloadFromModel(clone)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) ValidateResource(req UpsertOpenClawConfigResourceRequest) error {
	resourceType := normalizeResourceType(req.ResourceType)
	resourceKey := normalizeResourceKey(req.ResourceKey)
	name := strings.TrimSpace(req.Name)

	if !isValidOpenClawResourceType(resourceType) {
		return fmt.Errorf("invalid openclaw resource type")
	}
	if name == "" {
		return fmt.Errorf("openclaw config resource name is required")
	}
	if !openClawResourceKeyPattern.MatchString(resourceKey) {
		return fmt.Errorf("openclaw config resource key is invalid")
	}

	envelope, err := parseOpenClawEnvelope(resourceType, req.Content)
	if err != nil {
		return err
	}
	for _, dep := range envelope.DependsOn {
		if !isValidOpenClawResourceType(dep.Type) {
			return fmt.Errorf("openclaw config dependency type is invalid")
		}
		if normalizeResourceKey(dep.Key) == "" {
			return fmt.Errorf("openclaw config dependency key is required")
		}
	}
	return nil
}

func (s *openClawConfigService) ListBundles(userID int) ([]OpenClawConfigBundlePayload, error) {
	items, err := s.repo.ListBundles(userID)
	if err != nil {
		return nil, err
	}

	result := make([]OpenClawConfigBundlePayload, 0, len(items))
	for _, item := range items {
		payload, err := s.bundlePayloadFromModel(item)
		if err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, nil
}

func (s *openClawConfigService) GetBundle(userID, id int) (*OpenClawConfigBundlePayload, error) {
	item, err := s.repo.GetBundleByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config bundle not found")
	}

	payload, err := s.bundlePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) CreateBundle(userID int, req UpsertOpenClawConfigBundleRequest) (*OpenClawConfigBundlePayload, error) {
	if err := s.validateBundleRequest(userID, req); err != nil {
		return nil, err
	}

	now := time.Now()
	item := &models.OpenClawConfigBundle{
		UserID:      userID,
		Name:        strings.TrimSpace(req.Name),
		Description: normalizeOptionalString(req.Description),
		Enabled:     req.Enabled,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateBundle(item); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceBundleItems(item.ID, normalizeBundleItems(req.Items)); err != nil {
		return nil, err
	}

	payload, err := s.bundlePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) UpdateBundle(userID, id int, req UpsertOpenClawConfigBundleRequest) (*OpenClawConfigBundlePayload, error) {
	if err := s.validateBundleRequest(userID, req); err != nil {
		return nil, err
	}

	item, err := s.repo.GetBundleByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config bundle not found")
	}

	item.Name = strings.TrimSpace(req.Name)
	item.Description = normalizeOptionalString(req.Description)
	item.Enabled = req.Enabled
	item.Version++
	item.UpdatedAt = time.Now()

	if err := s.repo.UpdateBundle(item); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceBundleItems(item.ID, normalizeBundleItems(req.Items)); err != nil {
		return nil, err
	}

	payload, err := s.bundlePayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) DeleteBundle(userID, id int) error {
	item, err := s.repo.GetBundleByID(id)
	if err != nil {
		return err
	}
	if item == nil || item.UserID != userID {
		return fmt.Errorf("openclaw config bundle not found")
	}
	return s.repo.DeleteBundle(id)
}

func (s *openClawConfigService) CloneBundle(userID, id int) (*OpenClawConfigBundlePayload, error) {
	item, err := s.repo.GetBundleByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw config bundle not found")
	}

	items, err := s.repo.ListBundleItems(id)
	if err != nil {
		return nil, err
	}

	clone := *item
	clone.ID = 0
	clone.Name = fmt.Sprintf("%s Copy", item.Name)
	clone.Version = 1
	clone.CreatedAt = time.Now()
	clone.UpdatedAt = clone.CreatedAt
	if err := s.repo.CreateBundle(&clone); err != nil {
		return nil, err
	}
	for idx := range items {
		items[idx].ID = 0
		items[idx].BundleID = clone.ID
	}
	if err := s.repo.ReplaceBundleItems(clone.ID, items); err != nil {
		return nil, err
	}

	payload, err := s.bundlePayloadFromModel(clone)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) CompilePreview(userID int, plan OpenClawConfigPlan) (*OpenClawConfigCompilePreview, error) {
	compiled, err := s.compilePlan(userID, plan)
	if err != nil {
		return nil, err
	}
	return s.previewFromCompiled(compiled)
}

func (s *openClawConfigService) CreateSnapshotForInstance(userID int, instance *models.Instance, plan *OpenClawConfigPlan) (*models.OpenClawInjectionSnapshot, error) {
	if instance == nil || plan == nil || !hasOpenClawConfigSelections(*plan) {
		return nil, nil
	}

	compiled, err := s.compilePlan(userID, *plan)
	if err != nil {
		return nil, err
	}

	selectedIDs := make([]int, 0, len(compiled.selected))
	for _, resource := range compiled.selected {
		selectedIDs = append(selectedIDs, resource.model.ID)
	}

	resolvedSummaries := make([]OpenClawConfigResourceSummary, 0, len(compiled.resolved))
	for _, resource := range compiled.resolved {
		resolvedSummaries = append(resolvedSummaries, resourceSummaryFromModel(resource.model))
	}

	selectedJSON, err := marshalJSONString(selectedIDs)
	if err != nil {
		return nil, err
	}
	resolvedJSON, err := marshalJSONString(resolvedSummaries)
	if err != nil {
		return nil, err
	}
	envJSON, err := marshalJSONString(compiled.renderedEnv)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	instanceID := instance.ID
	snapshot := &models.OpenClawInjectionSnapshot{
		InstanceID:              &instanceID,
		UserID:                  userID,
		Mode:                    compiled.plan.Mode,
		BundleID:                compiled.plan.BundleID,
		SelectedResourceIDsJSON: selectedJSON,
		ResolvedResourcesJSON:   resolvedJSON,
		RenderedManifestJSON:    compiled.manifest,
		RenderedEnvJSON:         envJSON,
		Status:                  openClawCompiledSnapshotStatus,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := s.repo.CreateSnapshot(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *openClawConfigService) MarkSnapshotActive(snapshot *models.OpenClawInjectionSnapshot) error {
	if snapshot == nil {
		return nil
	}
	now := time.Now()
	snapshot.Status = openClawActiveSnapshotStatus
	snapshot.ActivatedAt = &now
	snapshot.UpdatedAt = now
	return s.repo.UpdateSnapshot(snapshot)
}

func (s *openClawConfigService) MarkSnapshotFailed(snapshot *models.OpenClawInjectionSnapshot, failedErr error) error {
	if snapshot == nil {
		return nil
	}
	message := strings.TrimSpace(errorString(failedErr))
	if message == "" {
		message = "unknown bootstrap failure"
	}
	now := time.Now()
	snapshot.Status = openClawFailedSnapshotStatus
	snapshot.ErrorMessage = &message
	snapshot.UpdatedAt = now
	return s.repo.UpdateSnapshot(snapshot)
}

func (s *openClawConfigService) EnsureSnapshotSecret(ctx context.Context, userID int, instance *models.Instance, snapshotID int) (string, error) {
	if instance == nil || snapshotID <= 0 {
		return "", nil
	}

	snapshot, err := s.repo.GetSnapshotByID(snapshotID)
	if err != nil {
		return "", err
	}
	if snapshot == nil || snapshot.UserID != userID {
		return "", fmt.Errorf("openclaw injection snapshot not found")
	}

	var envValues map[string]string
	if err := json.Unmarshal([]byte(snapshot.RenderedEnvJSON), &envValues); err != nil {
		return "", fmt.Errorf("openclaw injection snapshot env payload is invalid")
	}

	secretName := snapshot.SecretName
	if secretName == nil || strings.TrimSpace(*secretName) == "" {
		client := k8s.GetClient()
		if client == nil {
			return "", fmt.Errorf("k8s client not initialized")
		}
		name := client.GetOpenClawBootstrapSecretName(instance.ID, instance.Name)
		secretName = &name
	}

	if err := s.secretService.UpsertSecret(ctx, userID, *secretName, envValues, map[string]string{
		"app":           "clawreef",
		"instance-id":   fmt.Sprintf("%d", instance.ID),
		"instance-name": instance.Name,
		"user-id":       fmt.Sprintf("%d", userID),
		"managed-by":    "clawreef",
		"resource-type": "openclaw-bootstrap",
	}); err != nil {
		return "", err
	}

	if snapshot.SecretName == nil || *snapshot.SecretName != *secretName {
		snapshot.SecretName = secretName
		snapshot.UpdatedAt = time.Now()
		if err := s.repo.UpdateSnapshot(snapshot); err != nil {
			return "", err
		}
	}

	return *secretName, nil
}

func (s *openClawConfigService) ListSnapshots(userID int, limit int) ([]OpenClawInjectionSnapshotPayload, error) {
	if limit <= 0 {
		limit = defaultSnapshotListLimit
	}

	items, err := s.repo.ListSnapshotsByUser(userID, limit)
	if err != nil {
		return nil, err
	}

	result := make([]OpenClawInjectionSnapshotPayload, 0, len(items))
	for _, item := range items {
		payload, err := snapshotPayloadFromModel(item)
		if err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, nil
}

func (s *openClawConfigService) GetSnapshot(userID, id int) (*OpenClawInjectionSnapshotPayload, error) {
	item, err := s.repo.GetSnapshotByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("openclaw injection snapshot not found")
	}

	payload, err := snapshotPayloadFromModel(*item)
	if err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *openClawConfigService) validateBundleRequest(userID int, req UpsertOpenClawConfigBundleRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("openclaw config bundle name is required")
	}
	if len(req.Items) == 0 {
		return fmt.Errorf("openclaw config bundle must include at least one resource")
	}

	seen := map[int]struct{}{}
	for _, item := range req.Items {
		if item.ResourceID <= 0 {
			return fmt.Errorf("openclaw config bundle resource id is required")
		}
		if _, exists := seen[item.ResourceID]; exists {
			return fmt.Errorf("openclaw config bundle contains duplicate resources")
		}
		seen[item.ResourceID] = struct{}{}

		resource, err := s.repo.GetResourceByID(item.ResourceID)
		if err != nil {
			return err
		}
		if resource == nil || resource.UserID != userID {
			return fmt.Errorf("openclaw config resource not found")
		}
	}
	return nil
}

func normalizeBundleItems(items []OpenClawConfigBundleItemPayload) []models.OpenClawConfigBundleItem {
	result := make([]models.OpenClawConfigBundleItem, 0, len(items))
	for idx, item := range items {
		sortOrder := item.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		result = append(result, models.OpenClawConfigBundleItem{
			ResourceID: item.ResourceID,
			SortOrder:  sortOrder,
			Required:   item.Required,
		})
	}
	return result
}

func (s *openClawConfigService) bundlePayloadFromModel(item models.OpenClawConfigBundle) (OpenClawConfigBundlePayload, error) {
	bundleItems, err := s.repo.ListBundleItems(item.ID)
	if err != nil {
		return OpenClawConfigBundlePayload{}, err
	}

	payloadItems := make([]OpenClawConfigBundleItemPayload, 0, len(bundleItems))
	for _, bundleItem := range bundleItems {
		var summary *OpenClawConfigResourceSummary
		resource, err := s.repo.GetResourceByID(bundleItem.ResourceID)
		if err != nil {
			return OpenClawConfigBundlePayload{}, err
		}
		if resource != nil {
			resourceSummary := resourceSummaryFromModel(*resource)
			summary = &resourceSummary
		}

		payloadItems = append(payloadItems, OpenClawConfigBundleItemPayload{
			ResourceID: bundleItem.ResourceID,
			SortOrder:  bundleItem.SortOrder,
			Required:   bundleItem.Required,
			Resource:   summary,
		})
	}

	return OpenClawConfigBundlePayload{
		ID:          item.ID,
		UserID:      item.UserID,
		Name:        item.Name,
		Description: item.Description,
		Enabled:     item.Enabled,
		Version:     item.Version,
		Items:       payloadItems,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}, nil
}

func (s *openClawConfigService) compilePlan(userID int, plan OpenClawConfigPlan) (*compiledOpenClawConfig, error) {
	plan.Mode = normalizePlanMode(plan.Mode)
	if plan.Mode == "" {
		plan.Mode = OpenClawConfigPlanModeNone
	}
	if _, ok := openClawPlanModes[plan.Mode]; !ok {
		return nil, fmt.Errorf("invalid openclaw config plan mode")
	}

	if plan.Mode == OpenClawConfigPlanModeNone {
		manifestJSON, err := marshalJSONString(map[string]interface{}{
			"schemaVersion": 1,
			"mode":          OpenClawConfigPlanModeNone,
			"resources":     []interface{}{},
			"payloads":      []interface{}{},
		})
		if err != nil {
			return nil, err
		}
		return &compiledOpenClawConfig{
			plan:         plan,
			renderedEnv:  map[string]string{},
			manifest:     manifestJSON,
			payloadSizes: map[string]int{},
		}, nil
	}

	selected, bundle, err := s.loadSelectedResources(userID, plan)
	if err != nil {
		return nil, err
	}

	selectedCompiled := make([]compiledOpenClawResource, 0, len(selected))
	resolvedOrdered := make([]compiledOpenClawResource, 0, len(selected))
	autoIncluded := make([]compiledOpenClawResource, 0)
	resourceByRef := map[string]compiledOpenClawResource{}
	warnings := []string{}

	for _, item := range selected {
		compiled, err := compiledResourceFromModel(item)
		if err != nil {
			return nil, err
		}
		ref := openClawResourceRef(compiled.model.ResourceType, compiled.model.ResourceKey)
		resourceByRef[ref] = compiled
		selectedCompiled = append(selectedCompiled, compiled)
		resolvedOrdered = append(resolvedOrdered, compiled)
	}

	queue := append([]compiledOpenClawResource{}, selectedCompiled...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, dependency := range current.envelope.DependsOn {
			depType := normalizeResourceType(dependency.Type)
			depKey := normalizeResourceKey(dependency.Key)
			if depType == "" || depKey == "" {
				return nil, fmt.Errorf("openclaw config dependency is invalid")
			}

			ref := openClawResourceRef(depType, depKey)
			if _, exists := resourceByRef[ref]; exists {
				continue
			}

			resource, err := s.repo.GetResourceByUserTypeKey(userID, depType, depKey)
			if err != nil {
				return nil, err
			}
			if resource == nil {
				if dependency.Required {
					return nil, fmt.Errorf("missing required openclaw config dependency: %s/%s", depType, depKey)
				}
				warnings = append(warnings, fmt.Sprintf("Optional dependency %s/%s is not configured", depType, depKey))
				continue
			}
			if !resource.Enabled {
				if dependency.Required {
					return nil, fmt.Errorf("required openclaw config dependency is disabled: %s/%s", depType, depKey)
				}
				warnings = append(warnings, fmt.Sprintf("Optional dependency %s/%s is disabled", depType, depKey))
				continue
			}

			compiledDep, err := compiledResourceFromModel(*resource)
			if err != nil {
				return nil, err
			}
			resourceByRef[ref] = compiledDep
			resolvedOrdered = append(resolvedOrdered, compiledDep)
			autoIncluded = append(autoIncluded, compiledDep)
			queue = append(queue, compiledDep)
		}
	}

	sortCompiledResources(resolvedOrdered)
	sortCompiledResources(autoIncluded)

	renderedEnv, manifest, payloadSizes, totalPayloadSize, err := renderCompiledOpenClawPayload(plan, bundle, resolvedOrdered)
	if err != nil {
		return nil, err
	}

	return &compiledOpenClawConfig{
		plan:             plan,
		bundle:           bundle,
		selected:         selectedCompiled,
		resolved:         resolvedOrdered,
		autoIncluded:     autoIncluded,
		warnings:         warnings,
		renderedEnv:      renderedEnv,
		manifest:         manifest,
		payloadSizes:     payloadSizes,
		totalPayloadSize: totalPayloadSize,
	}, nil
}

func (s *openClawConfigService) loadSelectedResources(userID int, plan OpenClawConfigPlan) ([]models.OpenClawConfigResource, *models.OpenClawConfigBundle, error) {
	switch plan.Mode {
	case OpenClawConfigPlanModeBundle:
		if plan.BundleID == nil || *plan.BundleID <= 0 {
			return nil, nil, fmt.Errorf("openclaw config bundle is required")
		}

		bundle, err := s.repo.GetBundleByID(*plan.BundleID)
		if err != nil {
			return nil, nil, err
		}
		if bundle == nil || bundle.UserID != userID {
			return nil, nil, fmt.Errorf("openclaw config bundle not found")
		}
		if !bundle.Enabled {
			return nil, nil, fmt.Errorf("openclaw config bundle is disabled")
		}

		items, err := s.repo.ListBundleItems(bundle.ID)
		if err != nil {
			return nil, nil, err
		}
		if len(items) == 0 {
			return nil, nil, fmt.Errorf("openclaw config bundle is empty")
		}

		result := make([]models.OpenClawConfigResource, 0, len(items))
		for _, item := range items {
			resource, err := s.repo.GetResourceByID(item.ResourceID)
			if err != nil {
				return nil, nil, err
			}
			if resource == nil || resource.UserID != userID {
				return nil, nil, fmt.Errorf("openclaw config resource not found")
			}
			if !resource.Enabled {
				return nil, nil, fmt.Errorf("openclaw config bundle contains a disabled resource")
			}
			result = append(result, *resource)
		}
		return result, bundle, nil
	case OpenClawConfigPlanModeManual:
		if len(plan.ResourceIDs) == 0 {
			return nil, nil, fmt.Errorf("at least one openclaw config resource must be selected")
		}

		result := make([]models.OpenClawConfigResource, 0, len(plan.ResourceIDs))
		seen := map[int]struct{}{}
		for _, id := range plan.ResourceIDs {
			if id <= 0 {
				return nil, nil, fmt.Errorf("openclaw config resource id is invalid")
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}

			resource, err := s.repo.GetResourceByID(id)
			if err != nil {
				return nil, nil, err
			}
			if resource == nil || resource.UserID != userID {
				return nil, nil, fmt.Errorf("openclaw config resource not found")
			}
			if !resource.Enabled {
				return nil, nil, fmt.Errorf("openclaw config resource is disabled")
			}
			result = append(result, *resource)
		}
		return result, nil, nil
	default:
		return nil, nil, fmt.Errorf("invalid openclaw config plan mode")
	}
}

func (s *openClawConfigService) previewFromCompiled(compiled *compiledOpenClawConfig) (*OpenClawConfigCompilePreview, error) {
	var bundlePayload *OpenClawConfigBundlePayload
	if compiled.bundle != nil {
		payload, err := s.bundlePayloadFromModel(*compiled.bundle)
		if err != nil {
			return nil, err
		}
		bundlePayload = &payload
	}

	return &OpenClawConfigCompilePreview{
		Mode:              compiled.plan.Mode,
		Bundle:            bundlePayload,
		SelectedResources: summarizeCompiledResources(compiled.selected),
		ResolvedResources: summarizeCompiledResources(compiled.resolved),
		AutoIncluded:      summarizeCompiledResources(compiled.autoIncluded),
		Warnings:          compiled.warnings,
		EnvNames:          sortedEnvNames(compiled.renderedEnv),
		PayloadSizes:      compiled.payloadSizes,
		TotalPayloadBytes: compiled.totalPayloadSize,
		Manifest:          json.RawMessage(compiled.manifest),
	}, nil
}

func resourcePayloadFromModel(item models.OpenClawConfigResource) (OpenClawConfigResourcePayload, error) {
	tags, err := decodeStringArray(item.TagsJSON)
	if err != nil {
		return OpenClawConfigResourcePayload{}, fmt.Errorf("failed to parse openclaw config resource tags")
	}

	content := json.RawMessage(item.ContentJSON)
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	if normalizedContent, err := normalizeOpenClawResourceContent(item.ResourceType, item.ResourceKey, content); err == nil {
		content = normalizedContent
	}

	return OpenClawConfigResourcePayload{
		ID:           item.ID,
		UserID:       item.UserID,
		ResourceType: item.ResourceType,
		ResourceKey:  item.ResourceKey,
		Name:         item.Name,
		Description:  item.Description,
		Enabled:      item.Enabled,
		Version:      item.Version,
		Tags:         tags,
		Content:      content,
		CreatedAt:    item.CreatedAt,
		UpdatedAt:    item.UpdatedAt,
	}, nil
}

func resourceSummaryFromModel(item models.OpenClawConfigResource) OpenClawConfigResourceSummary {
	return OpenClawConfigResourceSummary{
		ID:           item.ID,
		ResourceType: item.ResourceType,
		ResourceKey:  item.ResourceKey,
		Name:         item.Name,
		Enabled:      item.Enabled,
		Version:      item.Version,
	}
}

func snapshotPayloadFromModel(item models.OpenClawInjectionSnapshot) (OpenClawInjectionSnapshotPayload, error) {
	selectedIDs := []int{}
	if strings.TrimSpace(item.SelectedResourceIDsJSON) != "" {
		if err := json.Unmarshal([]byte(item.SelectedResourceIDsJSON), &selectedIDs); err != nil {
			return OpenClawInjectionSnapshotPayload{}, fmt.Errorf("failed to parse openclaw snapshot selected ids")
		}
	}

	resolved := []OpenClawConfigResourceSummary{}
	if strings.TrimSpace(item.ResolvedResourcesJSON) != "" {
		if err := json.Unmarshal([]byte(item.ResolvedResourcesJSON), &resolved); err != nil {
			return OpenClawInjectionSnapshotPayload{}, fmt.Errorf("failed to parse openclaw snapshot resources")
		}
	}

	envValues := map[string]string{}
	if strings.TrimSpace(item.RenderedEnvJSON) != "" {
		if err := json.Unmarshal([]byte(item.RenderedEnvJSON), &envValues); err != nil {
			return OpenClawInjectionSnapshotPayload{}, fmt.Errorf("failed to parse openclaw snapshot env payloads")
		}
	}

	payloadSizes := map[string]int{}
	for key, value := range envValues {
		payloadSizes[key] = len(value)
	}

	return OpenClawInjectionSnapshotPayload{
		ID:                  item.ID,
		InstanceID:          item.InstanceID,
		UserID:              item.UserID,
		Mode:                item.Mode,
		BundleID:            item.BundleID,
		SelectedResourceIDs: selectedIDs,
		ResolvedResources:   resolved,
		Manifest:            json.RawMessage(item.RenderedManifestJSON),
		EnvNames:            sortedEnvNames(envValues),
		PayloadSizes:        payloadSizes,
		SecretName:          item.SecretName,
		Status:              item.Status,
		ErrorMessage:        item.ErrorMessage,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
		ActivatedAt:         item.ActivatedAt,
	}, nil
}

func renderCompiledOpenClawPayload(plan OpenClawConfigPlan, bundle *models.OpenClawConfigBundle, resources []compiledOpenClawResource) (map[string]string, string, map[string]int, int, error) {
	payloads := map[string]interface{}{}
	payloadCounts := map[string]int{}
	for _, resourceType := range openClawResourceTypeOrder {
		payloads[resourceType] = defaultOpenClawEnvPayload(resourceType)
		payloadCounts[resourceType] = 0
	}

	resolvedSummaries := make([]map[string]interface{}, 0, len(resources))
	for _, resource := range resources {
		nextPayload, err := appendCompiledOpenClawEnvPayload(payloads[resource.model.ResourceType], resource)
		if err != nil {
			return nil, "", nil, 0, err
		}
		payloads[resource.model.ResourceType] = nextPayload
		payloadCounts[resource.model.ResourceType]++
		resolvedSummaries = append(resolvedSummaries, map[string]interface{}{
			"id":      resource.model.ID,
			"type":    resource.model.ResourceType,
			"key":     resource.model.ResourceKey,
			"name":    resource.model.Name,
			"version": resource.model.Version,
		})
	}

	renderedEnv := map[string]string{}
	payloadSizes := map[string]int{}
	totalPayloadBytes := 0

	for _, resourceType := range openClawResourceTypeOrder {
		envName := openClawEnvByResourceType[resourceType]
		value, err := marshalJSONString(payloads[resourceType])
		if err != nil {
			return nil, "", nil, 0, err
		}
		renderedEnv[envName] = value
		payloadSizes[envName] = len(value)
		totalPayloadBytes += len(value)
	}

	manifest := map[string]interface{}{
		"schemaVersion": 1,
		"mode":          plan.Mode,
		"resources":     resolvedSummaries,
		"payloads":      []map[string]interface{}{},
	}
	if bundle != nil {
		manifest["bundle"] = map[string]interface{}{
			"id":      bundle.ID,
			"name":    bundle.Name,
			"version": bundle.Version,
		}
	}

	for _, resourceType := range openClawResourceTypeOrder {
		envName := openClawEnvByResourceType[resourceType]
		manifest["payloads"] = append(manifest["payloads"].([]map[string]interface{}), map[string]interface{}{
			"env":   envName,
			"count": payloadCounts[resourceType],
		})
	}

	manifestJSON, err := marshalJSONString(manifest)
	if err != nil {
		return nil, "", nil, 0, err
	}
	renderedEnv[OpenClawBootstrapManifestEnv] = manifestJSON
	payloadSizes[OpenClawBootstrapManifestEnv] = len(manifestJSON)
	totalPayloadBytes += len(manifestJSON)

	if totalPayloadBytes > openClawBootstrapPayloadMaxBytes {
		return nil, "", nil, 0, fmt.Errorf("openclaw bootstrap payload is too large")
	}

	return renderedEnv, manifestJSON, payloadSizes, totalPayloadBytes, nil
}

func defaultOpenClawEnvPayload(resourceType string) interface{} {
	if resourceType == OpenClawConfigResourceTypeChannel {
		return map[string]interface{}{}
	}

	return map[string]interface{}{
		"schemaVersion": 1,
		"items":         []map[string]interface{}{},
	}
}

func appendCompiledOpenClawEnvPayload(payload interface{}, resource compiledOpenClawResource) (interface{}, error) {
	if resource.model.ResourceType == OpenClawConfigResourceTypeChannel {
		channelPayload, ok := payload.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to render openclaw channels payload")
		}

		var configPayload interface{}
		if len(resource.envelope.Config) == 0 {
			configPayload = map[string]interface{}{}
		} else if err := json.Unmarshal(resource.envelope.Config, &configPayload); err != nil {
			return nil, fmt.Errorf("failed to parse openclaw channel config")
		}

		channelPayload[resource.model.ResourceKey] = normalizeOpenClawChannelConfigForEnv(resource.model.ResourceKey, configPayload)
		return channelPayload, nil
	}

	group, ok := payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to render openclaw payload for resource type %s", resource.model.ResourceType)
	}

	payloadItem := map[string]interface{}{
		"id":      resource.model.ID,
		"type":    resource.model.ResourceType,
		"key":     resource.model.ResourceKey,
		"name":    resource.model.Name,
		"version": resource.model.Version,
		"tags":    resource.tags,
		"content": json.RawMessage(resource.model.ContentJSON),
	}
	group["items"] = append(group["items"].([]map[string]interface{}), payloadItem)
	return group, nil
}

func normalizeOpenClawChannelConfigForEnv(resourceKey string, configPayload interface{}) interface{} {
	switch strings.ToLower(strings.TrimSpace(resourceKey)) {
	case "dingtalk-connector":
		return normalizeDingTalkChannelConfigForEnv(configPayload)
	case "feishu":
		return normalizeFeishuChannelConfigForEnv(configPayload)
	case "slack":
		return normalizeSlackChannelConfigForEnv(configPayload)
	case "telegram":
		return normalizeTelegramChannelConfigForEnv(configPayload)
	}

	return configPayload
}

func normalizeFeishuChannelConfigForEnv(configPayload interface{}) map[string]interface{} {
	config, ok := configPayload.(map[string]interface{})
	if !ok {
		return map[string]interface{}{
			"enabled": true,
			"accounts": map[string]interface{}{
				"main": map[string]interface{}{
					"appId":     "",
					"appSecret": "",
				},
			},
		}
	}

	accounts, _ := config["accounts"].(map[string]interface{})

	var account map[string]interface{}
	if mainAccount, ok := accounts["main"].(map[string]interface{}); ok {
		account = mainAccount
	} else if defaultAccount, ok := accounts["default"].(map[string]interface{}); ok {
		account = defaultAccount
	} else {
		account = map[string]interface{}{}
	}

	appID, _ := account["appId"].(string)
	if appID == "" {
		appID, _ = config["appId"].(string)
	}

	appSecret, _ := account["appSecret"].(string)
	if appSecret == "" {
		appSecret, _ = config["appSecret"].(string)
	}

	return map[string]interface{}{
		"enabled": true,
		"accounts": map[string]interface{}{
			"main": map[string]interface{}{
				"appId":     appID,
				"appSecret": appSecret,
			},
		},
	}
}

func normalizeSlackChannelConfigForEnv(configPayload interface{}) map[string]interface{} {
	config, ok := configPayload.(map[string]interface{})
	if !ok {
		config = map[string]interface{}{}
	}

	botToken, _ := config["botToken"].(string)
	appToken, _ := config["appToken"].(string)
	groupPolicy, _ := config["groupPolicy"].(string)
	if strings.TrimSpace(groupPolicy) == "" {
		groupPolicy = "allowlist"
	}

	channels, _ := config["channels"].(map[string]interface{})
	if channels == nil {
		channels = map[string]interface{}{
			"#general": map[string]interface{}{
				"allow": true,
			},
		}
	}

	capabilities, _ := config["capabilities"].(map[string]interface{})
	if capabilities == nil {
		capabilities = map[string]interface{}{
			"interactiveReplies": true,
		}
	}

	return map[string]interface{}{
		"enabled":      true,
		"botToken":     botToken,
		"appToken":     appToken,
		"groupPolicy":  groupPolicy,
		"channels":     channels,
		"capabilities": capabilities,
	}
}

func normalizeTelegramChannelConfigForEnv(configPayload interface{}) map[string]interface{} {
	config, ok := configPayload.(map[string]interface{})
	if !ok {
		config = map[string]interface{}{}
	}

	botToken, _ := config["botToken"].(string)
	dmPolicy, _ := config["dmPolicy"].(string)
	if strings.TrimSpace(dmPolicy) == "" {
		dmPolicy = "open"
	}

	allowFrom := normalizeStringArrayForEnv(config["allowFrom"])
	if len(allowFrom) == 0 {
		allowFrom = []string{"*"}
	}

	return map[string]interface{}{
		"enabled":   true,
		"botToken":  botToken,
		"dmPolicy":  dmPolicy,
		"allowFrom": allowFrom,
	}
}

func normalizeDingTalkChannelConfigForEnv(configPayload interface{}) map[string]interface{} {
	config, ok := configPayload.(map[string]interface{})
	if !ok {
		config = map[string]interface{}{}
	}

	clientID, _ := config["clientId"].(string)
	clientSecret, _ := config["clientSecret"].(string)

	allowFrom := normalizeStringArrayForEnv(config["allowFrom"])
	if len(allowFrom) == 0 {
		allowFrom = []string{"*"}
	}

	return map[string]interface{}{
		"enabled":      true,
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"allowFrom":    allowFrom,
	}
}

func normalizeStringArrayForEnv(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		result = append(result, text)
	}
	return result
}

func normalizeOpenClawResourceContent(resourceType, resourceKey string, raw json.RawMessage) (json.RawMessage, error) {
	if normalizeResourceType(resourceType) != OpenClawConfigResourceTypeChannel {
		return raw, nil
	}

	envelope, err := parseOpenClawEnvelope(resourceType, raw)
	if err != nil {
		return nil, err
	}

	var configPayload interface{}
	if err := json.Unmarshal(envelope.Config, &configPayload); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw channel config")
	}

	normalizedConfig := normalizeOpenClawChannelConfigForEnv(resourceKey, configPayload)
	envelope.Format = openClawChannelFormat(resourceKey)
	envelope.Config, err = json.Marshal(normalizedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized openclaw channel config")
	}

	normalizedEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized openclaw content")
	}

	return normalizedEnvelope, nil
}

func compiledResourceFromModel(item models.OpenClawConfigResource) (compiledOpenClawResource, error) {
	tags, err := decodeStringArray(item.TagsJSON)
	if err != nil {
		return compiledOpenClawResource{}, fmt.Errorf("failed to parse openclaw config tags")
	}

	envelope, err := parseOpenClawEnvelope(item.ResourceType, json.RawMessage(item.ContentJSON))
	if err != nil {
		return compiledOpenClawResource{}, err
	}

	return compiledOpenClawResource{
		model:    item,
		tags:     tags,
		envelope: envelope,
	}, nil
}

func parseOpenClawEnvelope(resourceType string, raw json.RawMessage) (OpenClawConfigEnvelope, error) {
	if len(raw) == 0 {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config content is required")
	}

	var envelope OpenClawConfigEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config content must be valid JSON")
	}
	if envelope.SchemaVersion <= 0 {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config schemaVersion is required")
	}
	if canonicalizeOpenClawKind(envelope.Kind) != normalizeResourceType(resourceType) {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config kind does not match resource type")
	}
	if strings.TrimSpace(envelope.Format) == "" {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config format is required")
	}
	if len(envelope.Config) == 0 {
		return OpenClawConfigEnvelope{}, fmt.Errorf("openclaw config config payload is required")
	}
	return envelope, nil
}

func hasOpenClawConfigSelections(plan OpenClawConfigPlan) bool {
	switch normalizePlanMode(plan.Mode) {
	case OpenClawConfigPlanModeBundle:
		return plan.BundleID != nil && *plan.BundleID > 0
	case OpenClawConfigPlanModeManual:
		return len(plan.ResourceIDs) > 0
	default:
		return false
	}
}

func normalizePlanMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func normalizeResourceType(resourceType string) string {
	value := strings.TrimSpace(strings.ToLower(resourceType))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func normalizeResourceKey(resourceKey string) string {
	return strings.TrimSpace(resourceKey)
}

func openClawChannelFormat(resourceKey string) string {
	key := normalizeResourceKey(resourceKey)
	if key == "" {
		return "channel/custom@v1"
	}
	return fmt.Sprintf("channel/%s@v1", key)
}

func canonicalizeOpenClawKind(kind string) string {
	value := normalizeResourceType(kind)
	switch value {
	case "sessiontemplate":
		return OpenClawConfigResourceTypeSessionTemplate
	case "logpolicy":
		return OpenClawConfigResourceTypeLogPolicy
	case "scheduledtask":
		return OpenClawConfigResourceTypeScheduledTask
	default:
		return value
	}
}

func isValidOpenClawResourceType(resourceType string) bool {
	_, ok := openClawAllowedResourceTypes[normalizeResourceType(resourceType)]
	return ok
}

func normalizeTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func encodeStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func decodeStringArray(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func marshalJSONString(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("failed to marshal openclaw payload")
	}
	return string(raw), nil
}

func openClawResourceRef(resourceType, resourceKey string) string {
	return fmt.Sprintf("%s:%s", normalizeResourceType(resourceType), normalizeResourceKey(resourceKey))
}

func summarizeCompiledResources(resources []compiledOpenClawResource) []OpenClawConfigResourceSummary {
	result := make([]OpenClawConfigResourceSummary, 0, len(resources))
	for _, resource := range resources {
		result = append(result, resourceSummaryFromModel(resource.model))
	}
	return result
}

func sortCompiledResources(items []compiledOpenClawResource) {
	priority := map[string]int{}
	for idx, resourceType := range openClawResourceTypeOrder {
		priority[resourceType] = idx
	}

	sort.SliceStable(items, func(i, j int) bool {
		leftType := normalizeResourceType(items[i].model.ResourceType)
		rightType := normalizeResourceType(items[j].model.ResourceType)
		if priority[leftType] != priority[rightType] {
			return priority[leftType] < priority[rightType]
		}
		if items[i].model.ResourceKey != items[j].model.ResourceKey {
			return items[i].model.ResourceKey < items[j].model.ResourceKey
		}
		return items[i].model.ID < items[j].model.ID
	})
}

func sortedEnvNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for key := range values {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// cascadeSnapshotsForResource recompiles all active snapshots that reference
// the given resource ID, so that their rendered_env_json reflects the latest
// resource content. It does NOT restart instances — the new env takes effect
// on the next instance restart.
func (s *openClawConfigService) cascadeSnapshotsForResource(userID, resourceID int) {
	snapshots, err := s.repo.ListActiveSnapshots(userID)
	if err != nil {
		log.Printf("[cascade] failed to list active snapshots for user %d: %v", userID, err)
		return
	}

	for _, snap := range snapshots {
		if !snapshotReferencesResource(snap, resourceID) {
			continue
		}

		// Record the version stamp at read time for CAS.
		readVersion := snap.UpdatedAt

		plan, err := planFromSnapshot(snap)
		if err != nil {
			log.Printf("[cascade] snapshot %d: failed to reconstruct plan: %v", snap.ID, err)
			continue
		}

		compiled, err := s.compilePlan(userID, plan)
		if err != nil {
			log.Printf("[cascade] snapshot %d: recompile failed: %v", snap.ID, err)
			continue
		}

		envJSON, err := marshalJSONString(compiled.renderedEnv)
		if err != nil {
			log.Printf("[cascade] snapshot %d: marshal env failed: %v", snap.ID, err)
			continue
		}

		resolvedSummaries := make([]OpenClawConfigResourceSummary, 0, len(compiled.resolved))
		for _, r := range compiled.resolved {
			resolvedSummaries = append(resolvedSummaries, resourceSummaryFromModel(r.model))
		}
		resolvedJSON, err := marshalJSONString(resolvedSummaries)
		if err != nil {
			log.Printf("[cascade] snapshot %d: marshal resolved failed: %v", snap.ID, err)
			continue
		}

		snap.RenderedEnvJSON = envJSON
		snap.ResolvedResourcesJSON = resolvedJSON
		snap.RenderedManifestJSON = compiled.manifest
		snap.UpdatedAt = time.Now()

		// CAS write: only succeeds if updated_at has not changed since our read.
		ok, err := s.repo.UpdateSnapshotIfUnchanged(&snap, readVersion)
		if err != nil {
			log.Printf("[cascade] snapshot %d: update failed: %v", snap.ID, err)
		} else if !ok {
			log.Printf("[cascade] snapshot %d: skipped — already updated by a newer cascade", snap.ID)
		} else {
			log.Printf("[cascade] snapshot %d: refreshed successfully", snap.ID)
		}
	}
}

// snapshotReferencesResource checks whether a snapshot references the given
// resource ID. It checks ResolvedResourcesJSON first (which contains the full
// set of selected + dependsOn resources), falling back to SelectedResourceIDsJSON
// for backward compatibility with older snapshots.
func snapshotReferencesResource(snap models.OpenClawInjectionSnapshot, resourceID int) bool {
	// Primary: check ResolvedResourcesJSON (contains selected + dependsOn full set).
	if strings.TrimSpace(snap.ResolvedResourcesJSON) != "" {
		var summaries []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal([]byte(snap.ResolvedResourcesJSON), &summaries); err == nil {
			for _, s := range summaries {
				if s.ID == resourceID {
					return true
				}
			}
			return false
		}
		// If ResolvedResourcesJSON is present but malformed, fall through to fallback.
	}

	// Fallback: check SelectedResourceIDsJSON for older snapshots.
	if strings.TrimSpace(snap.SelectedResourceIDsJSON) == "" {
		return false
	}
	var ids []int
	if err := json.Unmarshal([]byte(snap.SelectedResourceIDsJSON), &ids); err != nil {
		return false
	}
	for _, id := range ids {
		if id == resourceID {
			return true
		}
	}
	return false
}

// planFromSnapshot reconstructs an OpenClawConfigPlan from a snapshot's stored
// mode, bundle_id, and selected_resource_ids_json.
func planFromSnapshot(snap models.OpenClawInjectionSnapshot) (OpenClawConfigPlan, error) {
	plan := OpenClawConfigPlan{
		Mode:     snap.Mode,
		BundleID: snap.BundleID,
	}
	if snap.Mode == OpenClawConfigPlanModeManual {
		var ids []int
		if err := json.Unmarshal([]byte(snap.SelectedResourceIDsJSON), &ids); err != nil {
			return plan, fmt.Errorf("failed to parse selected resource ids: %w", err)
		}
		plan.ResourceIDs = ids
	}
	return plan, nil
}
