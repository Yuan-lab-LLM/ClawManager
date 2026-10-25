package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// LLMModelService defines admin model catalog operations.
type LLMModelService interface {
	ListModels() ([]models.LLMModel, error)
	ListActiveModels() ([]models.LLMModel, error)
	ListExpandedActiveModels() ([]models.LLMModel, error)
	SaveModel(req SaveLLMModelRequest) (*models.LLMModel, error)
	DeleteModel(id int) error
	DiscoverProviderModels(req DiscoverLLMModelsRequest) ([]DiscoveredLLMModel, error)
}

// SaveLLMModelRequest contains editable model catalog fields.
type SaveLLMModelRequest struct {
	VisibilityScope            *string
	AllowedUserIDs             *[]int
	AccessPolicyVersion        *int
	ID                         int
	DisplayName                string
	Description                *string
	ProviderType               string
	ProtocolType               string
	ModelKind                  *string
	ImageGenerationEnabled     *bool
	ImageEditEnabled           *bool
	ImagePricePerImage         *float64
	ResetImagePrice            bool
	ModelGroup                 string
	DeploymentPriority         int
	MaxConcurrentRequests      int
	ContextWindowTokens        int
	QualityTier                *int
	BaseURL                    string
	ProviderModelName          string
	ReasoningEnabled           *bool
	ReasoningControlMode       *string
	APIKey                     *string
	APIKeySecretRef            *string
	IsSecure                   bool
	IsActive                   bool
	InputPrice                 float64
	OutputPrice                float64
	Currency                   string
	SupportsTools              bool
	ToolsSupportOverride       *bool
	SupportsVision             bool
	VisionSupportOverride      *bool
	ResetVisionSupportOverride bool
	CapabilitySource           string
	ProviderModels             []models.LLMProviderModel
}

// DiscoverLLMModelsRequest contains fields needed to query a provider for available models.
type DiscoverLLMModelsRequest struct {
	ProviderType    string
	ProtocolType    string
	BaseURL         string
	APIKey          *string
	APIKeySecretRef *string
}

// DiscoveredLLMModel is a normalized provider model entry returned by discovery.
type DiscoveredLLMModel struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"display_name"`
	SuggestedPool        string   `json:"suggested_pool"`
	SuggestedQualityTier int      `json:"suggested_quality_tier"`
	SuggestedPriority    int      `json:"suggested_priority"`
	Confidence           int      `json:"confidence"`
	ClassificationSource string   `json:"classification_source"`
	Reasons              []string `json:"reasons,omitempty"`
	ChatCapable          bool     `json:"chat_capable"`
	ContextWindowTokens  int      `json:"context_window_tokens,omitempty"`
	SupportsTools        bool     `json:"supports_tools,omitempty"`
	SupportsVision       bool     `json:"supports_vision,omitempty"`
	SupportsReasoning    bool     `json:"supports_reasoning,omitempty"`
	ModelKind            string   `json:"model_kind,omitempty"`
}

// classifyDiscoveredModel creates a conservative first-pass routing plan.
// It is deliberately a suggestion: unknown models remain usable in the
// default pool and are never promoted to a high tier automatically.
func classifyDiscoveredModel(id, displayName string) DiscoveredLLMModel {
	item := DiscoveredLLMModel{ID: id, DisplayName: displayName, SuggestedPool: "default", SuggestedPriority: 50, ClassificationSource: "unknown", ChatCapable: true}
	name := strings.ToLower(strings.TrimSpace(id + " " + displayName))
	contains := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(name, value) {
				return true
			}
		}
		return false
	}
	if imageModelNamePattern.MatchString(id) || imageModelNamePattern.MatchString(displayName) || contains("image-generation") {
		item.ChatCapable = false
		item.ModelKind = models.ModelKindImage
		item.SuggestedPool = "specialized"
		item.SuggestedPriority = 100
		item.ClassificationSource = "name_rule"
		item.Confidence = 90
		item.Reasons = []string{"image model name; administrator must confirm generation/edit capability"}
		return item
	}
	if contains("embed", "embedding", "rerank", "moderation", "whisper", "tts") {
		item.ChatCapable = false
		item.ModelKind = models.ModelKindOther
		item.SuggestedPool = "specialized"
		item.SuggestedPriority = 100
		item.Confidence = 95
		item.ClassificationSource = "name_rule"
		item.Reasons = []string{"non-chat/specialized model name"}
		return item
	}
	if modelNameTokenPattern.MatchString(strings.TrimSpace(id)) || modelNameTokenPattern.MatchString(strings.TrimSpace(displayName)) || contains("deepseek-r1") {
		item.SuggestedPool = "reasoning"
		item.SuggestedQualityTier = 4
		item.SuggestedPriority = 10
		item.Confidence = 85
		item.ClassificationSource = "name_rule"
		item.Reasons = []string{"reasoning marker in model name"}
		return item
	}
	if codingModelTokenPattern.MatchString(name) {
		item.SuggestedPool = "coding"
		item.SuggestedQualityTier = 2
		item.SuggestedPriority = 20
		item.Confidence = 75
		item.ClassificationSource = "name_rule"
		item.Reasons = []string{"coding marker in model name"}
		return item
	}
	if contains("mini", "small", "lite", "flash") {
		item.SuggestedPool = "cheap"
		item.SuggestedQualityTier = 1
		item.SuggestedPriority = 30
		item.Confidence = 70
		item.ClassificationSource = "name_rule"
		item.Reasons = []string{"small/fast model marker in model name"}
		return item
	}
	if premiumModelTokenPattern.MatchString(name) {
		item.SuggestedPool = "general"
		item.SuggestedQualityTier = 3
		item.SuggestedPriority = 10
		item.Confidence = 65
		item.ClassificationSource = "name_rule"
		item.Reasons = []string{"large/premium model marker in model name"}
		return item
	}
	if contains("qwen", "glm", "kimi", "llama", "mistral", "gemma", "gpt", "claude", "command", "yi") {
		item.SuggestedPool = "general"
		item.SuggestedQualityTier = 2
		item.SuggestedPriority = 40
		item.Confidence = 35
		item.ClassificationSource = "default_rule"
		item.Reasons = []string{"known model family without a stronger capability marker"}
		return item
	}
	item.SuggestedPool = "default"
	item.SuggestedQualityTier = 0
	item.SuggestedPriority = 50
	item.Confidence = 0
	item.ClassificationSource = "unknown"
	item.Reasons = []string{"model family could not be identified"}
	return item
}

func enrichDiscoveredModel(item DiscoveredLLMModel, contextWindow int, modelKind string, capabilities []string) DiscoveredLLMModel {
	item.ContextWindowTokens = contextWindow
	if kind := strings.TrimSpace(strings.ToLower(modelKind)); kind != "" {
		item.ModelKind = kind
	}
	switch item.ModelKind {
	case "chat", "chat-completion", "chat_completion":
		item.ModelKind = models.ModelKindChat
		item.ChatCapable = true
		if strings.TrimSpace(modelKind) != "" {
			item.ClassificationSource = "provider_metadata"
		}
	case "image", "image-generation", "image_generation", "text-to-image", "image-edit", "image_edit":
		item.ModelKind = models.ModelKindImage
		item.ChatCapable = false
		item.SuggestedPool = "specialized"
		item.SuggestedQualityTier = 0
		if strings.TrimSpace(modelKind) != "" {
			item.ClassificationSource = "provider_metadata"
		}
	}
	for _, capability := range capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case "tools", "tool_choice", "function_calling", "function-calling":
			item.SupportsTools = true
		case "vision", "image", "image_input":
			item.SupportsVision = true
		case "reasoning", "thinking", "reasoning_content":
			item.SupportsReasoning = true
		}
	}
	if item.SupportsTools || item.SupportsVision || item.SupportsReasoning || item.ModelKind == "embedding" || item.ModelKind == "embeddings" || item.ModelKind == "rerank" || item.ModelKind == "reranking" || item.ModelKind == "moderation" {
		item.ClassificationSource = "provider_metadata"
	}
	if item.ChatCapable && item.SupportsReasoning && item.SuggestedQualityTier < 4 {
		item.SuggestedPool = "reasoning"
		item.SuggestedQualityTier = 4
		item.SuggestedPriority = 10
		item.Confidence = maxDiscoveryInt(item.Confidence, 80)
		item.Reasons = append(item.Reasons, "provider metadata reports reasoning support")
	}
	if item.ModelKind == "embedding" || item.ModelKind == "embeddings" || item.ModelKind == "rerank" || item.ModelKind == "reranking" || item.ModelKind == "moderation" {
		item.ChatCapable = false
		item.SuggestedPool = "specialized"
		item.SuggestedQualityTier = 0
		item.Reasons = append(item.Reasons, "provider metadata reports non-chat model type")
	}
	return item
}

func maxDiscoveryInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type llmModelService struct {
	repo             repository.LLMModelRepository
	httpClient       *http.Client
	secretRefService SecretRefService
	discoveryCache   *providerDiscoveryCache
}

var versionSegmentPattern = regexp.MustCompile(`(?i)^v\d+(?:[a-z0-9._-]*)?$`)
var modelNameTokenPattern = regexp.MustCompile(`(?i)(^|[/_.:-])(reasoner|reasoning|thinking|r1|o1|o3)([/_.:-]|$)`)
var imageModelNamePattern = regexp.MustCompile(`(?i)(^|[/_.:-])(gpt-image|dall-e|stable-diffusion|flux)([/_.:0-9-]|$)`)
var codingModelTokenPattern = regexp.MustCompile(`(?i)(^|[/_.:-])(coder|coding|code)([/_.:-]|$)`)
var premiumModelTokenPattern = regexp.MustCompile(`(?i)(^|[/_.:-])(pro|ultra|max|70b|72b|32b|27b|34b)([/_.:-]|$)`)

// NewLLMModelService creates a new LLM model service.
func NewLLMModelService(repo repository.LLMModelRepository) LLMModelService {
	return &llmModelService{
		repo:             repo,
		secretRefService: NewSecretRefService(),
		discoveryCache:   newProviderDiscoveryCache(),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				previous := via[len(via)-1].URL
				if !strings.EqualFold(previous.Scheme, req.URL.Scheme) || !strings.EqualFold(previous.Host, req.URL.Host) {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

func (s *llmModelService) ListModels() ([]models.LLMModel, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list llm models: %w", err)
	}
	populateReasoningCapabilities(items)
	return items, nil
}

func (s *llmModelService) ListActiveModels() ([]models.LLMModel, error) {
	items, err := s.repo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list active llm models: %w", err)
	}
	populateReasoningCapabilities(items)
	return items, nil
}

func (s *llmModelService) SaveModel(req SaveLLMModelRequest) (*models.LLMModel, error) {
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return nil, errors.New("display name is required")
	}

	providerType := strings.TrimSpace(strings.ToLower(req.ProviderType))
	if providerType == "" {
		return nil, errors.New("provider type is required")
	}
	protocolType, err := models.ResolveLLMProtocolType(providerType, req.ProtocolType)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		return nil, errors.New("base URL is required")
	}
	if providerType != models.ProviderTypeLocal && discoveryURLTargetsPrivateNetwork(baseURL) {
		return nil, errors.New("provider discovery URL targets a private or loopback network; use provider type local for explicitly trusted local services")
	}

	providerModelName := strings.TrimSpace(req.ProviderModelName)
	if providerModelName == "" {
		return nil, errors.New("provider model name is required")
	}

	modelGroup := strings.TrimSpace(req.ModelGroup)
	if modelGroup == "" {
		// Keep legacy catalog entries routable without requiring a migration of
		// every administrator-managed model at once.
		modelGroup = displayName
	}
	if len(modelGroup) > 100 {
		return nil, errors.New("model group must be at most 100 characters")
	}
	if req.DeploymentPriority < 0 {
		return nil, errors.New("deployment priority must be non-negative")
	}
	if req.MaxConcurrentRequests < 0 {
		return nil, errors.New("max concurrent requests must be non-negative")
	}
	if req.ContextWindowTokens < 0 {
		return nil, errors.New("context window tokens must be non-negative")
	}
	if req.QualityTier != nil && (*req.QualityTier < 0 || *req.QualityTier > 4) {
		return nil, errors.New("quality tier must be between 0 (unclassified) and 4 (reasoning)")
	}
	if int64(req.ContextWindowTokens) > int64(2147483647) {
		return nil, errors.New("context window tokens exceeds the supported maximum")
	}

	if req.InputPrice < 0 {
		return nil, errors.New("input price must be non-negative")
	}

	if req.OutputPrice < 0 {
		return nil, errors.New("output price must be non-negative")
	}

	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}

	existingByName, err := s.repo.GetByDisplayName(displayName)
	if err != nil {
		return nil, fmt.Errorf("failed to validate model display name: %w", err)
	}
	if existingByName != nil && existingByName.ID != req.ID {
		return nil, errors.New("display name already exists")
	}

	var current *models.LLMModel
	if req.ID != 0 {
		current, err = s.repo.GetByID(req.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get llm model: %w", err)
		}
		if current == nil {
			return nil, errors.New("model not found")
		}
	}

	qualityTier := 0
	if current != nil {
		qualityTier = current.QualityTier
	}
	if req.QualityTier != nil {
		qualityTier = *req.QualityTier
	}
	var description *string
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		if trimmed != "" {
			description = &trimmed
		}
	}

	var apiKey *string
	if req.APIKey != nil {
		trimmed := strings.TrimSpace(*req.APIKey)
		if trimmed != "" {
			apiKey = &trimmed
		}
	}

	var apiKeySecretRef *string
	if req.APIKeySecretRef != nil {
		trimmed := strings.TrimSpace(*req.APIKeySecretRef)
		if trimmed != "" {
			apiKeySecretRef = &trimmed
		}
	}

	model := &models.LLMModel{
		ID:                    req.ID,
		DisplayName:           displayName,
		Description:           description,
		ProviderType:          providerType,
		ProtocolType:          protocolType,
		ModelGroup:            modelGroup,
		DeploymentPriority:    req.DeploymentPriority,
		MaxConcurrentRequests: req.MaxConcurrentRequests,
		ContextWindowTokens:   req.ContextWindowTokens,
		QualityTier:           qualityTier,
		BaseURL:               baseURL,
		ProviderModelName:     providerModelName,
		APIKey:                apiKey,
		APIKeySecretRef:       apiKeySecretRef,
		IsSecure:              req.IsSecure,
		IsActive:              req.IsActive,
		InputPrice:            req.InputPrice,
		OutputPrice:           req.OutputPrice,
		Currency:              currency,
		SupportsTools:         req.SupportsTools,
		ToolsSupportOverride:  req.ToolsSupportOverride,
		SupportsVision:        req.SupportsVision,
		VisionSupportOverride: req.VisionSupportOverride,
		CapabilitySource:      strings.TrimSpace(req.CapabilitySource),
	}
	if current != nil && strings.TrimSpace(req.CapabilitySource) == "" {
		model.SupportsTools = current.SupportsTools
		model.SupportsVision = current.SupportsVision
		model.CapabilitySource = current.CapabilitySource
	}
	// Older clients and automatic discovery must not erase an explicit choice.
	if current != nil && req.ToolsSupportOverride == nil {
		model.ToolsSupportOverride = current.ToolsSupportOverride
	}
	if req.ResetVisionSupportOverride {
		model.VisionSupportOverride = nil
	} else if current != nil && req.VisionSupportOverride == nil {
		model.VisionSupportOverride = current.VisionSupportOverride
	}
	if err := applyModelOperations(model, current, req); err != nil {
		return nil, err
	}
	if req.ReasoningControlMode != nil {
		model.ReasoningControlMode = strings.TrimSpace(*req.ReasoningControlMode)
	} else if current != nil {
		model.ReasoningControlMode = current.ReasoningControlMode
	}
	if !models.ValidReasoningControlMode(model.ReasoningControlMode) {
		return nil, errors.New("invalid reasoning control mode")
	}
	if model.ReasoningControlMode != "" && model.ReasoningControlMode != "none" &&
		protocolType != models.ProtocolTypeOpenAI && protocolType != models.ProtocolTypeOpenAICompatible {
		return nil, errors.New("reasoning control mode requires an OpenAI-compatible upstream protocol")
	}
	// Provider catalog metadata is preserved for compatibility only. Saving one
	// deployment must not discover/import other models or change its grants.
	if req.ProviderModels != nil {
		if err := models.SetLLMProviderModels(model, req.ProviderModels); err != nil {
			return nil, fmt.Errorf("failed to encode provider models: %w", err)
		}
	} else if current != nil && sameModelDiscoveryConfig(current, model) {
		model.ProviderModelsJSON = current.ProviderModelsJSON
		model.ProviderModels = current.ProviderModels
	}
	models.PopulateLLMReasoningCapability(model)
	if req.ReasoningEnabled != nil {
		if *req.ReasoningEnabled && !model.SupportsReasoning {
			return nil, errors.New("reasoning control is not supported for this provider model")
		}
		model.ReasoningEnabled = *req.ReasoningEnabled
	} else if current != nil && model.SupportsReasoning {
		model.ReasoningEnabled = current.ReasoningEnabled
	}
	if !model.SupportsReasoning {
		model.ReasoningEnabled = false
	}

	if current != nil {
		model.CreatedAt = current.CreatedAt
	}

	if err := applyModelAccessPolicy(model, current, req); err != nil {
		return nil, err
	}
	if err := s.repo.Save(model); err != nil {
		return nil, fmt.Errorf("failed to save llm model: %w", err)
	}
	s.resetProviderDiscoveryCache()

	return model, nil
}

func discoveryURLTargetsPrivateNetwork(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

func sameModelDiscoveryConfig(current, next *models.LLMModel) bool {
	if current == nil || next == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(current.ProviderType), strings.TrimSpace(next.ProviderType)) &&
		strings.EqualFold(strings.TrimSpace(current.ProtocolType), strings.TrimSpace(next.ProtocolType)) &&
		strings.EqualFold(strings.TrimRight(strings.TrimSpace(current.BaseURL), "/"), strings.TrimRight(strings.TrimSpace(next.BaseURL), "/")) &&
		optionalStringEqual(current.APIKey, next.APIKey) &&
		optionalStringEqual(current.APIKeySecretRef, next.APIKeySecretRef)
}

func optionalStringEqual(left, right *string) bool {
	leftValue := ""
	if left != nil {
		leftValue = strings.TrimSpace(*left)
	}
	rightValue := ""
	if right != nil {
		rightValue = strings.TrimSpace(*right)
	}
	return leftValue == rightValue
}

func populateReasoningCapabilities(items []models.LLMModel) {
	for index := range items {
		models.PopulateLLMReasoningCapability(&items[index])
		if !items[index].SupportsReasoning {
			items[index].ReasoningEnabled = false
		}
	}
}

func (s *llmModelService) DeleteModel(id int) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return fmt.Errorf("failed to get llm model: %w", err)
	}
	if existing == nil {
		return errors.New("model not found")
	}

	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete llm model: %w", err)
	}
	s.resetProviderDiscoveryCache()
	return nil
}

func (s *llmModelService) DiscoverProviderModels(req DiscoverLLMModelsRequest) ([]DiscoveredLLMModel, error) {
	providerType := strings.TrimSpace(strings.ToLower(req.ProviderType))
	if providerType == "" {
		return nil, errors.New("provider type is required")
	}
	protocolType, err := models.ResolveLLMProtocolType(providerType, req.ProtocolType)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		return nil, errors.New("base URL is required")
	}

	resolvedAPIKey, err := s.secretRefService.ResolveString(context.Background(), req.APIKey, req.APIKeySecretRef)
	if err != nil {
		return nil, err
	}

	var apiKey string
	if resolvedAPIKey != nil {
		apiKey = strings.TrimSpace(*resolvedAPIKey)
	}

	switch protocolType {
	case models.ProtocolTypeOpenAI, models.ProtocolTypeOpenAICompatible:
		return s.discoverOpenAICompatibleModels(baseURL, apiKey)
	case models.ProtocolTypeAnthropic:
		return s.discoverAnthropicModels(baseURL, apiKey)
	case models.ProtocolTypeGoogle:
		return s.discoverGoogleModels(baseURL, apiKey)
	case models.ProtocolTypeAzureOpenAI:
		return nil, errors.New("automatic model discovery for azure-openai is not supported yet")
	default:
		return nil, errors.New("provider discovery is not supported")
	}
}

func (s *llmModelService) discoverOpenAICompatibleModels(baseURL, apiKey string) ([]DiscoveredLLMModel, error) {
	endpoint, err := buildProviderEndpoint(baseURL, "v1", "models")
	if err != nil {
		return nil, err
	}

	return s.discoverOpenAICompatiblePages(endpoint, apiKey, false)
}

// discoverOpenAICompatiblePages reads the complete model collection. Most
// providers return one page, but a number of gateways expose OpenAI-style
// pagination or use a `models` array instead of `data`.
func (s *llmModelService) discoverOpenAICompatiblePages(endpoint, apiKey string, anthropic bool) ([]DiscoveredLLMModel, error) {
	type modelItem struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		OwnedBy          string   `json:"owned_by"`
		DisplayName      string   `json:"display_name"`
		Type             string   `json:"type"`
		ContextLength    int      `json:"context_length"`
		MaxContextLength int      `json:"max_context_length"`
		Capabilities     []string `json:"supported_parameters"`
		Features         []string `json:"capabilities"`
	}
	type responsePayload struct {
		Data          []modelItem `json:"data"`
		Models        []modelItem `json:"models"`
		HasMore       bool        `json:"has_more"`
		Next          string      `json:"next"`
		NextPage      string      `json:"next_page"`
		NextPageToken string      `json:"next_page_token"`
		LastID        string      `json:"last_id"`
	}

	const maxPages = 20
	models := make([]DiscoveredLLMModel, 0)
	seen := make(map[string]struct{})
	visited := make(map[string]struct{})
	currentURL := endpoint
	for page := 0; page < maxPages && currentURL != ""; page++ {
		if _, ok := visited[currentURL]; ok {
			break
		}
		visited[currentURL] = struct{}{}
		request, err := http.NewRequest(http.MethodGet, currentURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build discovery request: %w", err)
		}
		if apiKey != "" {
			if anthropic {
				request.Header.Set("anthropic-version", "2023-06-01")
				request.Header.Set("x-api-key", apiKey)
			} else {
				request.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}

		var payload responsePayload
		if err := s.doJSON(request, &payload); err != nil {
			return nil, err
		}
		items := payload.Data
		if len(items) == 0 {
			items = payload.Models
		}
		for _, item := range items {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				id = strings.TrimSpace(item.Name)
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			displayName := strings.TrimSpace(item.DisplayName)
			if displayName == "" {
				displayName = id
			}
			if owner := strings.TrimSpace(item.OwnedBy); owner != "" && displayName == id {
				displayName = fmt.Sprintf("%s (%s)", id, owner)
			}
			plan := classifyDiscoveredModel(id, displayName)
			contextWindow := item.ContextLength
			if item.MaxContextLength > contextWindow {
				contextWindow = item.MaxContextLength
			}
			capabilities := append(append([]string{}, item.Capabilities...), item.Features...)
			models = append(models, enrichDiscoveredModel(plan, contextWindow, item.Type, capabilities))
		}

		next := strings.TrimSpace(payload.NextPage)
		if next == "" {
			next = strings.TrimSpace(payload.Next)
		}
		if next == "" && payload.NextPageToken != "" {
			queryKey := "after"
			if anthropic {
				queryKey = "after_id"
			}
			next = appendDiscoveryQuery(currentURL, queryKey, payload.NextPageToken)
		}
		if next == "" && anthropic && payload.LastID != "" {
			next = appendDiscoveryQuery(currentURL, "after_id", payload.LastID)
		}
		if next == "" || next == currentURL {
			break
		}
		if page == maxPages-1 {
			return nil, fmt.Errorf("provider model list exceeds the discovery limit of %d pages", maxPages)
		}
		resolvedNext, ok := resolveDiscoveryURL(currentURL, next)
		if !ok {
			return nil, errors.New("provider discovery returned a pagination URL outside the provider origin")
		}
		currentURL = resolvedNext
	}
	return models, nil
}

func (s *llmModelService) discoverAnthropicModels(baseURL, apiKey string) ([]DiscoveredLLMModel, error) {
	endpoint, err := buildProviderEndpoint(baseURL, "v1", "models")
	if err != nil {
		return nil, err
	}

	return s.discoverOpenAICompatiblePages(endpoint, apiKey, true)
}

func (s *llmModelService) discoverGoogleModels(baseURL, apiKey string) ([]DiscoveredLLMModel, error) {
	endpoint, err := buildProviderEndpoint(baseURL, "v1beta", "models")
	if err != nil {
		return nil, err
	}

	if apiKey != "" {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint = endpoint + separator + "key=" + url.QueryEscape(apiKey)
	}

	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build discovery request: %w", err)
	}

	type responsePayload struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}

	var payload responsePayload
	if err := s.doJSON(request, &payload); err != nil {
		return nil, err
	}

	models := make([]DiscoveredLLMModel, 0, len(payload.Models))
	for _, item := range payload.Models {
		id := strings.TrimSpace(strings.TrimPrefix(item.Name, "models/"))
		if id == "" {
			continue
		}
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = id
		}
		models = append(models, classifyDiscoveredModel(id, displayName))
	}
	return models, nil
}

func (s *llmModelService) doJSON(request *http.Request, target any) error {
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to call provider discovery endpoint: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("provider discovery failed: %s", message)
	}

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode provider discovery response: %w", err)
	}
	return nil
}

func buildProviderEndpoint(baseURL, versionPrefix, resource string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if trimmed == "" {
		return "", errors.New("base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("base URL is invalid")
	}

	versionPath := "/" + strings.Trim(versionPrefix, "/")
	resourcePath := strings.Trim(resource, "/")
	basePath := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(strings.ToLower(parsed.Path), strings.ToLower(versionPath)) {
		parsed.Path = basePath + "/" + resourcePath
		return parsed.String(), nil
	}

	pathSegments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	lastSegment := ""
	if len(pathSegments) > 0 {
		lastSegment = pathSegments[len(pathSegments)-1]
	}
	if versionSegmentPattern.MatchString(lastSegment) {
		parsed.Path = basePath + "/" + resourcePath
		return parsed.String(), nil
	}

	parsed.Path = basePath + versionPath + "/" + resourcePath
	return parsed.String(), nil
}

func resolveDiscoveryURL(currentURL, next string) (string, bool) {
	nextURL, err := url.Parse(strings.TrimSpace(next))
	if err != nil {
		return "", false
	}
	current, err := url.Parse(currentURL)
	if err != nil {
		return "", false
	}
	resolved := current.ResolveReference(nextURL)
	if !strings.EqualFold(resolved.Scheme, current.Scheme) || !strings.EqualFold(resolved.Host, current.Host) {
		return "", false
	}
	return resolved.String(), true
}

func appendDiscoveryQuery(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
